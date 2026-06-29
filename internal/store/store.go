// Package store abstracts embedding persistence behind a Store interface so the
// indexer and MCP server are independent of the concrete backend. Two backends
// are provided: SQLite + sqlite-vec (default, embedded, one DB file per codebase)
// and PostgreSQL + pgvector (selectable via config).
package store

import (
	"context"
	"fmt"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/models"
)

// ChunkRecord holds one chunk plus its embedding, ready to persist.
// Embedding may be nil if embedding failed; such chunks are skipped on write.
type ChunkRecord struct {
	ChunkIndex int
	StartLine  int
	EndLine    int
	ChunkType  string
	SymbolName string
	Content    string
	Embedding  []float32
}

// FileWithChunks is a single file plus all of its chunk records, as handed to
// StoreFiles. The backend decides transaction granularity.
type FileWithChunks struct {
	Path         string
	LastModified float64
	FileHash     string
	Language     string
	Chunks       []ChunkRecord
}

// SearchFilters narrows a search. Forward-looking for the roadmap's search
// filters; currently honored only when non-empty by backends that support it.
type SearchFilters struct {
	Language  string
	ChunkType string
}

// Store is the persistence backend for code embeddings.
type Store interface {
	// FileUnchanged reports whether path is already indexed with the same mtime.
	FileUnchanged(ctx context.Context, path string, mtime float64) bool
	// StoreFiles upserts files and their chunk embeddings, replacing any prior
	// records for the same paths. Returns the number of files and chunks stored
	// plus any per-file errors (non-fatal).
	StoreFiles(ctx context.Context, files []FileWithChunks) (storedFiles, storedChunks int, errs []models.ErrorDetail)
	// Search returns the top `limit` most similar chunks by cosine similarity.
	Search(ctx context.Context, embedding []float32, limit int, f SearchFilters) ([]models.SearchResult, error)
	// RecordIndexRun records a completed indexing run.
	RecordIndexRun(ctx context.Context, directory string, processed, skipped, errors int, details []models.ErrorDetail) error
	// Status returns the current state of the index.
	Status(ctx context.Context) (models.IndexStatus, error)
	// Close releases backend resources.
	Close() error
}

// Open returns a Store for the configured backend. For SQLite the store is bound
// to a single codebase rooted at `root` (one DB file per codebase); for Postgres
// `root` is ignored (a single shared database).
func Open(ctx context.Context, cfg config.Config, root string) (Store, error) {
	switch cfg.Backend {
	case "postgres", "pg":
		return OpenPostgres(ctx, cfg)
	case "", "sqlite":
		return OpenSQLite(ctx, cfg, root)
	default:
		return nil, fmt.Errorf("unknown backend %q (want \"sqlite\" or \"postgres\")", cfg.Backend)
	}
}
