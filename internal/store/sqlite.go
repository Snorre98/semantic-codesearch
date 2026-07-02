package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/models"

	_ "modernc.org/sqlite"     // pure-Go SQLite driver ("sqlite")
	_ "modernc.org/sqlite/vec" // registers the sqlite-vec (vec0) extension
)

// sqliteStore implements Store backed by SQLite + sqlite-vec (one DB per codebase).
type sqliteStore struct {
	db   *sql.DB
	dims int
}

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS indexed_files (
    id            INTEGER PRIMARY KEY,
    file_path     TEXT UNIQUE NOT NULL,
    last_modified REAL NOT NULL,
    file_hash     TEXT NOT NULL,
    language      TEXT,
    indexed_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE IF NOT EXISTS code_chunks (
    id          INTEGER PRIMARY KEY,
    file_id     INTEGER NOT NULL REFERENCES indexed_files(id) ON DELETE CASCADE,
    chunk_index INTEGER,
    start_line  INTEGER,
    end_line    INTEGER,
    chunk_type  TEXT,
    symbol_name TEXT,
    content     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chunks_file ON code_chunks(file_id);
CREATE TABLE IF NOT EXISTS index_runs (
    id              INTEGER PRIMARY KEY,
    directory       TEXT NOT NULL,
    finished_at     TEXT NOT NULL DEFAULT (datetime('now')),
    files_processed INTEGER DEFAULT 0,
    files_skipped   INTEGER DEFAULT 0,
    errors          INTEGER DEFAULT 0,
    error_details   TEXT DEFAULT '[]'
);`

// OpenSQLite opens (creating if needed) the per-codebase DB for root.
func OpenSQLite(ctx context.Context, cfg config.Config, root string) (Store, error) {
	reg, err := LoadRegistry(cfg)
	if err != nil {
		return nil, err
	}
	path := reg.DBPath(root)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single connection keeps the per-connection PRAGMAs in effect and serializes
	// writes (single user — concurrent access is rare and serialization is fine).
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q: %w", pragma, err)
		}
	}

	if _, err := db.ExecContext(ctx, sqliteSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	// vec0 virtual table; its dimension is fixed at creation time.
	vecDDL := fmt.Sprintf(
		"CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks USING vec0(chunk_id INTEGER PRIMARY KEY, embedding float[%d] distance_metric=cosine)",
		config.EmbeddingDimensions)
	if _, err := db.ExecContext(ctx, vecDDL); err != nil {
		db.Close()
		return nil, fmt.Errorf("create vec table: %w", err)
	}

	return &sqliteStore{db: db, dims: config.EmbeddingDimensions}, nil
}

func (s *sqliteStore) Close() error { return s.db.Close() }

func (s *sqliteStore) FileUnchanged(ctx context.Context, path string, mtime float64) bool {
	var one int
	err := s.db.QueryRowContext(ctx,
		"SELECT 1 FROM indexed_files WHERE file_path = ? AND last_modified = ?",
		path, mtime,
	).Scan(&one)
	return err == nil
}

// StoreFiles writes all files in a single transaction — the key write-speed lever
// for SQLite (one fsync for the whole flush under WAL).
func (s *sqliteStore) StoreFiles(ctx context.Context, files []FileWithChunks) (int, int, []models.ErrorDetail) {
	var errs []models.ErrorDetail

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, []models.ErrorDetail{{File: "", Error: fmt.Sprintf("begin tx: %v", err)}}
	}

	storedFiles, storedChunks, err := s.storeFilesTx(ctx, tx, files, &errs)
	if err != nil {
		tx.Rollback()
		return 0, 0, append(errs, models.ErrorDetail{File: "", Error: err.Error()})
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, append(errs, models.ErrorDetail{File: "", Error: fmt.Sprintf("commit: %v", err)})
	}
	return storedFiles, storedChunks, errs
}

func (s *sqliteStore) storeFilesTx(ctx context.Context, tx *sql.Tx, files []FileWithChunks, errs *[]models.ErrorDetail) (int, int, error) {
	delVec, err := tx.PrepareContext(ctx,
		`DELETE FROM vec_chunks WHERE chunk_id IN (
			SELECT c.id FROM code_chunks c JOIN indexed_files f ON f.id = c.file_id WHERE f.file_path = ?)`)
	if err != nil {
		return 0, 0, err
	}
	defer delVec.Close()
	delFile, err := tx.PrepareContext(ctx, "DELETE FROM indexed_files WHERE file_path = ?")
	if err != nil {
		return 0, 0, err
	}
	defer delFile.Close()
	insFile, err := tx.PrepareContext(ctx,
		"INSERT INTO indexed_files (file_path, last_modified, file_hash, language) VALUES (?, ?, ?, ?)")
	if err != nil {
		return 0, 0, err
	}
	defer insFile.Close()
	insChunk, err := tx.PrepareContext(ctx,
		"INSERT INTO code_chunks (file_id, chunk_index, start_line, end_line, chunk_type, symbol_name, content) VALUES (?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return 0, 0, err
	}
	defer insChunk.Close()
	insVec, err := tx.PrepareContext(ctx, "INSERT INTO vec_chunks (chunk_id, embedding) VALUES (?, ?)")
	if err != nil {
		return 0, 0, err
	}
	defer insVec.Close()

	var storedFiles, storedChunks int
	for _, f := range files {
		records := nonEmptyChunks(f.Chunks)
		if len(records) == 0 {
			continue
		}

		if _, err := delVec.ExecContext(ctx, f.Path); err != nil {
			return storedFiles, storedChunks, fmt.Errorf("delete old vectors: %w", err)
		}
		if _, err := delFile.ExecContext(ctx, f.Path); err != nil {
			return storedFiles, storedChunks, fmt.Errorf("delete old file: %w", err)
		}
		res, err := insFile.ExecContext(ctx, f.Path, f.LastModified, f.FileHash, f.Language)
		if err != nil {
			return storedFiles, storedChunks, fmt.Errorf("insert file: %w", err)
		}
		fileID, err := res.LastInsertId()
		if err != nil {
			return storedFiles, storedChunks, err
		}

		for _, c := range records {
			cres, err := insChunk.ExecContext(ctx, fileID, c.ChunkIndex, c.StartLine, c.EndLine, c.ChunkType, c.SymbolName, c.Content)
			if err != nil {
				return storedFiles, storedChunks, fmt.Errorf("insert chunk: %w", err)
			}
			chunkID, err := cres.LastInsertId()
			if err != nil {
				return storedFiles, storedChunks, err
			}
			if _, err := insVec.ExecContext(ctx, chunkID, serializeFloat32(c.Embedding)); err != nil {
				return storedFiles, storedChunks, fmt.Errorf("insert vector: %w", err)
			}
		}
		storedFiles++
		storedChunks += len(records)
	}
	return storedFiles, storedChunks, nil
}

func (s *sqliteStore) Search(ctx context.Context, embedding []float32, limit int, f SearchFilters) ([]models.SearchResult, error) {
	q := `SELECT f.file_path, c.start_line, c.end_line, c.content, c.symbol_name, c.chunk_type, v.distance
		 FROM vec_chunks v
		 JOIN code_chunks c ON c.id = v.chunk_id
		 JOIN indexed_files f ON f.id = c.file_id
		 WHERE v.embedding MATCH ? AND k = ?`
	args := []any{serializeFloat32(embedding), limit}
	if f.AreaLikePattern != "" {
		q += " AND f.file_path LIKE ?"
		args = append(args, f.AreaLikePattern)
	}
	q += " ORDER BY v.distance"

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var results []models.SearchResult
	for rows.Next() {
		var r models.SearchResult
		var symbol sql.NullString
		var distance float64
		if err := rows.Scan(&r.FilePath, &r.StartLine, &r.EndLine, &r.Snippet, &symbol, &r.ChunkType, &distance); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}
		r.SymbolName = symbol.String
		r.Score = 1 - distance
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *sqliteStore) RecordIndexRun(ctx context.Context, directory string, processed, skipped, errors int, details []models.ErrorDetail) error {
	detailsJSON := "[]"
	if len(details) > 0 {
		if b, err := json.Marshal(details); err == nil {
			detailsJSON = string(b)
		}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO index_runs (directory, files_processed, files_skipped, errors, error_details)
		 VALUES (?, ?, ?, ?, ?)`,
		directory, processed, skipped, errors, detailsJSON,
	)
	return err
}

func (s *sqliteStore) Status(ctx context.Context) (models.IndexStatus, error) {
	var status models.IndexStatus
	status.LastErrors = []map[string]any{}

	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM indexed_files").Scan(&status.TotalFiles); err != nil {
		return status, fmt.Errorf("count files: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM code_chunks").Scan(&status.TotalChunks); err != nil {
		return status, fmt.Errorf("count chunks: %w", err)
	}

	var directory, finishedAt sql.NullString
	var errorDetailsJSON sql.NullString
	err := s.db.QueryRowContext(ctx,
		"SELECT directory, finished_at, error_details FROM index_runs ORDER BY id DESC LIMIT 1",
	).Scan(&directory, &finishedAt, &errorDetailsJSON)
	if err == sql.ErrNoRows {
		return status, nil
	}
	if err != nil {
		return status, fmt.Errorf("latest run: %w", err)
	}

	if directory.Valid {
		status.LastDirectory = &directory.String
	}
	if finishedAt.Valid {
		status.LastIndexTime = &finishedAt.String
	}
	if errorDetailsJSON.Valid && errorDetailsJSON.String != "" {
		var errs []map[string]any
		if json.Unmarshal([]byte(errorDetailsJSON.String), &errs) == nil {
			status.LastErrors = errs
		}
	}
	return status, nil
}

// serializeFloat32 encodes a vector as a little-endian float32 blob for sqlite-vec.
func serializeFloat32(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}
