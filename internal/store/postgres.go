package store

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

// postgresStore implements Store backed by PostgreSQL + pgvector.
type postgresStore struct {
	pool *pgxpool.Pool
}

// querier is satisfied by *pgxpool.Pool, pgx.Conn, and pgx.Tx.
type querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// OpenPostgres creates a Store backed by a pgx connection pool.
func OpenPostgres(ctx context.Context, cfg config.Config) (Store, error) {
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		cfg.PGUser, cfg.PGPassword, cfg.PGHost, cfg.PGPort, cfg.PGDatabase)
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	// Ensure the codebase-metadata table exists (parity with the SQLite registry).
	if _, err := pool.Exec(ctx, pgCodebasesSchema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("create codebases table: %w", err)
	}
	return &postgresStore{pool: pool}, nil
}

func (s *postgresStore) Close() error {
	s.pool.Close()
	return nil
}

func (s *postgresStore) FileUnchanged(ctx context.Context, path string, mtime float64) bool {
	var id int
	err := s.pool.QueryRow(ctx,
		"SELECT id FROM indexed_files WHERE file_path = $1 AND last_modified = $2",
		path, mtime,
	).Scan(&id)
	return err == nil
}

// StoreFiles writes each file in its own transaction (atomic per file).
func (s *postgresStore) StoreFiles(ctx context.Context, files []FileWithChunks) (int, int, []models.ErrorDetail) {
	var storedFiles, storedChunks int
	var errs []models.ErrorDetail

	for _, f := range files {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return storedFiles, storedChunks, errs
			}
			errs = append(errs, models.ErrorDetail{File: f.Path, Error: err.Error()})
			continue
		}

		fileID, err := upsertFile(ctx, tx, f)
		if err != nil {
			tx.Rollback(ctx)
			errs = append(errs, models.ErrorDetail{File: f.Path, Error: err.Error()})
			continue
		}

		records := nonEmptyChunks(f.Chunks)
		if len(records) == 0 {
			tx.Rollback(ctx)
			continue
		}

		if err := insertChunks(ctx, tx, fileID, records); err != nil {
			tx.Rollback(ctx)
			errs = append(errs, models.ErrorDetail{File: f.Path, Error: err.Error()})
			continue
		}

		if err := tx.Commit(ctx); err != nil {
			errs = append(errs, models.ErrorDetail{File: f.Path, Error: err.Error()})
			continue
		}

		storedFiles++
		storedChunks += len(records)
	}

	return storedFiles, storedChunks, errs
}

func upsertFile(ctx context.Context, q querier, f FileWithChunks) (int, error) {
	if _, err := q.Exec(ctx, "DELETE FROM indexed_files WHERE file_path = $1", f.Path); err != nil {
		return 0, fmt.Errorf("delete old file: %w", err)
	}
	var id int
	err := q.QueryRow(ctx,
		`INSERT INTO indexed_files (file_path, last_modified, file_hash, language)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		f.Path, f.LastModified, f.FileHash, f.Language,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert file: %w", err)
	}
	return id, nil
}

func insertChunks(ctx context.Context, q querier, fileID int, chunks []ChunkRecord) error {
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

func (s *postgresStore) Search(ctx context.Context, embedding []float32, limit int, _ SearchFilters) ([]models.SearchResult, error) {
	vec := pgvector.NewVector(embedding)
	rows, err := s.pool.Query(ctx,
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
		WHERE NOT EXISTS (
			SELECT 1 FROM codebases cb
			WHERE cb.status = 'deprecated'
			  AND (f.file_path = cb.root OR f.file_path LIKE cb.root || '/%')
		)
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

func (s *postgresStore) RecordIndexRun(ctx context.Context, directory string, processed, skipped, errors int, details []models.ErrorDetail) error {
	detailsJSON := "[]"
	if len(details) > 0 {
		if b, err := json.Marshal(details); err == nil {
			detailsJSON = string(b)
		}
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO index_runs
		 (directory, finished_at, files_processed, files_skipped, errors, error_details)
		 VALUES ($1, NOW(), $2, $3, $4, $5::jsonb)`,
		directory, processed, skipped, errors, detailsJSON,
	)
	return err
}

func (s *postgresStore) Status(ctx context.Context) (models.IndexStatus, error) {
	var status models.IndexStatus

	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM indexed_files").Scan(&status.TotalFiles); err != nil {
		return status, fmt.Errorf("count files: %w", err)
	}
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM code_chunks").Scan(&status.TotalChunks); err != nil {
		return status, fmt.Errorf("count chunks: %w", err)
	}

	var directory, finishedAt *string
	var errorDetailsJSON []byte
	err := s.pool.QueryRow(ctx,
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
		var errs []map[string]any
		if json.Unmarshal(errorDetailsJSON, &errs) == nil {
			status.LastErrors = errs
		}
	}
	if status.LastErrors == nil {
		status.LastErrors = []map[string]any{}
	}
	return status, nil
}

// nonEmptyChunks filters out chunks whose embedding failed (nil).
func nonEmptyChunks(chunks []ChunkRecord) []ChunkRecord {
	out := chunks[:0:0]
	for _, c := range chunks {
		if c.Embedding != nil {
			out = append(out, c)
		}
	}
	return out
}
