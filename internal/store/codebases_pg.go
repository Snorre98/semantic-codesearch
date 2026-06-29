package store

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
)

// CodebaseStore is an optional companion to Store, implemented by backends whose
// single shared database holds many codebases (Postgres). It mirrors the registry
// that the SQLite backend keeps as a JSON file, so the management CLI can offer
// list/info/remove/prune with parity across both backends.
//
// SQLite does not implement this interface — its per-codebase DB files are managed
// through the JSON Registry instead.
type CodebaseStore interface {
	// ListCodebases returns every recorded codebase (active and deprecated).
	ListCodebases(ctx context.Context) ([]RegistryEntry, error)
	// UpsertCodebase records/updates an active codebase's metadata.
	UpsertCodebase(ctx context.Context, e RegistryEntry, when time.Time) error
	// DeprecateCodebase flags a codebase deprecated; its rows are retained (and
	// excluded from search) until DeleteCodebase purges them.
	DeprecateCodebase(ctx context.Context, root, reason string, when time.Time) error
	// DeleteCodebase removes all data under root (the codebase row plus every
	// file/chunk whose path is at or under root) and reports what was deleted.
	DeleteCodebase(ctx context.Context, root string) (files, chunks int, err error)
}

// pgCodebasesSchema creates the codebases metadata table idempotently so existing
// Postgres databases gain it on the next open (it is also in docker/init.sql).
const pgCodebasesSchema = `
CREATE TABLE IF NOT EXISTS codebases (
    root              TEXT PRIMARY KEY,
    model             TEXT NOT NULL,
    dims              INTEGER NOT NULL,
    files             INTEGER NOT NULL DEFAULT 0,
    chunks            INTEGER NOT NULL DEFAULT 0,
    last_indexed      TIMESTAMPTZ,
    status            TEXT NOT NULL DEFAULT 'active',
    deprecated_at     TIMESTAMPTZ,
    deprecated_reason TEXT
);`

var _ CodebaseStore = (*postgresStore)(nil)

func (s *postgresStore) ListCodebases(ctx context.Context) ([]RegistryEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT root, model, dims, files, chunks,
		        COALESCE(last_indexed::text, ''), status,
		        COALESCE(deprecated_at::text, ''), COALESCE(deprecated_reason, '')
		 FROM codebases ORDER BY root`)
	if err != nil {
		return nil, fmt.Errorf("list codebases: %w", err)
	}
	defer rows.Close()

	var out []RegistryEntry
	for rows.Next() {
		var e RegistryEntry
		if err := rows.Scan(&e.Root, &e.Model, &e.Dims, &e.Files, &e.Chunks,
			&e.LastIndexed, &e.Status, &e.DeprecatedAt, &e.DeprecatedReason); err != nil {
			return nil, fmt.Errorf("scan codebase: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *postgresStore) UpsertCodebase(ctx context.Context, e RegistryEntry, when time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO codebases (root, model, dims, files, chunks, last_indexed, status)
		 VALUES ($1, $2, $3, $4, $5, $6, 'active')
		 ON CONFLICT (root) DO UPDATE SET
		   model = EXCLUDED.model,
		   dims = EXCLUDED.dims,
		   files = EXCLUDED.files,
		   chunks = EXCLUDED.chunks,
		   last_indexed = EXCLUDED.last_indexed,
		   status = 'active',
		   deprecated_at = NULL,
		   deprecated_reason = NULL`,
		e.Root, e.Model, e.Dims, e.Files, e.Chunks, when.UTC())
	if err != nil {
		return fmt.Errorf("upsert codebase: %w", err)
	}
	return nil
}

func (s *postgresStore) DeprecateCodebase(ctx context.Context, root, reason string, when time.Time) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE codebases SET status = 'deprecated', deprecated_at = $2, deprecated_reason = $3
		 WHERE root = $1`,
		root, when.UTC(), reason)
	if err != nil {
		return fmt.Errorf("deprecate codebase: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no codebase registered for %q", root)
	}
	return nil
}

func (s *postgresStore) DeleteCodebase(ctx context.Context, root string) (int, int, error) {
	// Match the codebase root and everything beneath it. LIKE wildcards in root
	// are escaped so a path containing % or _ can't widen the match.
	prefix := escapeLike(root) + string(filepath.Separator) + "%"

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	var files, chunks int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM indexed_files WHERE file_path = $1 OR file_path LIKE $2 ESCAPE '\'`,
		root, prefix).Scan(&files); err != nil {
		return 0, 0, fmt.Errorf("count files: %w", err)
	}
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM code_chunks c JOIN indexed_files f ON f.id = c.file_id
		 WHERE f.file_path = $1 OR f.file_path LIKE $2 ESCAPE '\'`,
		root, prefix).Scan(&chunks); err != nil {
		return 0, 0, fmt.Errorf("count chunks: %w", err)
	}
	// code_chunks rows cascade from indexed_files.
	if _, err := tx.Exec(ctx,
		`DELETE FROM indexed_files WHERE file_path = $1 OR file_path LIKE $2 ESCAPE '\'`,
		root, prefix); err != nil {
		return 0, 0, fmt.Errorf("delete files: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM codebases WHERE root = $1`, root); err != nil {
		return 0, 0, fmt.Errorf("delete codebase row: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return files, chunks, nil
}

// escapeLike escapes LIKE metacharacters using a backslash escape character.
func escapeLike(s string) string {
	r := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\', '%', '_':
			r = append(r, '\\')
		}
		r = append(r, s[i])
	}
	return string(r)
}
