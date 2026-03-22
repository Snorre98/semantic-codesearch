package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/models"
)

// ChunkRecord holds the data needed to insert a chunk into the database.
type ChunkRecord struct {
	ChunkIndex int
	StartLine  int
	EndLine    int
	ChunkType  string
	SymbolName string
	Content    string
	Embedding  []float32
}

// NewPool creates a new pgx connection pool.
func NewPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, cfg.PGConnString())
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}

// GetFileIfUnchanged returns the file ID if the file exists with the same mtime, or (0, false).
func GetFileIfUnchanged(ctx context.Context, q querier, filePath string, lastModified float64) (int, bool) {
	var id int
	err := q.QueryRow(ctx,
		"SELECT id FROM indexed_files WHERE file_path = $1 AND last_modified = $2",
		filePath, lastModified,
	).Scan(&id)
	if err != nil {
		return 0, false
	}
	return id, true
}

// UpsertFile deletes any existing record for the file (cascading to chunks) and inserts a new one.
func UpsertFile(ctx context.Context, q querier, filePath string, lastModified float64, fileHash, language string) (int, error) {
	_, err := q.Exec(ctx, "DELETE FROM indexed_files WHERE file_path = $1", filePath)
	if err != nil {
		return 0, fmt.Errorf("delete old file: %w", err)
	}

	var id int
	err = q.QueryRow(ctx,
		`INSERT INTO indexed_files (file_path, last_modified, file_hash, language)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		filePath, lastModified, fileHash, language,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert file: %w", err)
	}
	return id, nil
}

// InsertChunks batch-inserts chunk records with their embeddings.
func InsertChunks(ctx context.Context, q querier, fileID int, chunks []ChunkRecord) error {
	batch := &pgx.Batch{}
	for _, c := range chunks {
		batch.Queue(
			`INSERT INTO code_chunks
			 (file_id, chunk_index, start_line, end_line, chunk_type, symbol_name, content, embedding)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			fileID, c.ChunkIndex, c.StartLine, c.EndLine,
			c.ChunkType, c.SymbolName, c.Content,
			pgvector.NewVector(c.Embedding),
		)
	}

	br := q.SendBatch(ctx, batch)
	defer br.Close()

	for range chunks {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert chunk: %w", err)
		}
	}
	return nil
}

// SearchSimilar finds the most similar code chunks by cosine similarity.
func SearchSimilar(ctx context.Context, pool *pgxpool.Pool, embedding []float32, limit int) ([]models.SearchResult, error) {
	vec := pgvector.NewVector(embedding)
	rows, err := pool.Query(ctx,
		`SELECT
			f.file_path,
			c.start_line,
			c.end_line,
			c.content,
			c.symbol_name,
			c.chunk_type,
			1 - (c.embedding <=> $1) AS score
		FROM code_chunks c
		JOIN indexed_files f ON f.id = c.file_id
		ORDER BY c.embedding <=> $1
		LIMIT $2`,
		vec, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var results []models.SearchResult
	for rows.Next() {
		var r models.SearchResult
		var symbolName *string
		if err := rows.Scan(&r.FilePath, &r.StartLine, &r.EndLine, &r.Snippet, &symbolName, &r.ChunkType, &r.Score); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		if symbolName != nil {
			r.SymbolName = *symbolName
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// RecordIndexRun records an indexing run in the index_runs table.
func RecordIndexRun(ctx context.Context, q querier, directory string, filesProcessed, filesSkipped, errors int, errorDetails []models.ErrorDetail) error {
	detailsJSON := "[]"
	if len(errorDetails) > 0 {
		b, err := json.Marshal(errorDetails)
		if err == nil {
			detailsJSON = string(b)
		}
	}

	_, err := q.Exec(ctx,
		`INSERT INTO index_runs
		 (directory, finished_at, files_processed, files_skipped, errors, error_details)
		 VALUES ($1, NOW(), $2, $3, $4, $5::jsonb)`,
		directory, filesProcessed, filesSkipped, errors, detailsJSON,
	)
	return err
}

// GetIndexStatus returns the current state of the search index.
func GetIndexStatus(ctx context.Context, pool *pgxpool.Pool) (models.IndexStatus, error) {
	var status models.IndexStatus

	err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM indexed_files").Scan(&status.TotalFiles)
	if err != nil {
		return status, fmt.Errorf("count files: %w", err)
	}

	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM code_chunks").Scan(&status.TotalChunks)
	if err != nil {
		return status, fmt.Errorf("count chunks: %w", err)
	}

	var directory *string
	var finishedAt *string
	var errorDetailsJSON []byte

	err = pool.QueryRow(ctx,
		"SELECT directory, finished_at::text, error_details FROM index_runs ORDER BY id DESC LIMIT 1",
	).Scan(&directory, &finishedAt, &errorDetailsJSON)
	if err == pgx.ErrNoRows {
		status.LastErrors = []map[string]any{}
		return status, nil
	}
	if err != nil {
		return status, fmt.Errorf("latest run: %w", err)
	}

	status.LastDirectory = directory
	status.LastIndexTime = finishedAt

	if len(errorDetailsJSON) > 0 {
		var errors []map[string]any
		if json.Unmarshal(errorDetailsJSON, &errors) == nil {
			status.LastErrors = errors
		}
	}
	if status.LastErrors == nil {
		status.LastErrors = []map[string]any{}
	}

	return status, nil
}

// querier is satisfied by *pgxpool.Pool, pgx.Conn, and pgx.Tx.
type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}
