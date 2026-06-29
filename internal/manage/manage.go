// Package manage provides backend-agnostic codebase management (list, info,
// deprecate, prune, purge, drop) over either storage backend. SQLite is backed by
// the JSON registry and per-codebase DB files; Postgres by the shared codebases
// metadata table. The CLI talks only to the Manager interface.
package manage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/store"
)

// Codebase is a decorated, presentation-ready view of one registered codebase.
type Codebase struct {
	Root         string `json:"root"`
	DBFile       string `json:"db_file,omitempty"`
	Model        string `json:"model"`
	Dims         int    `json:"dims"`
	Files        int    `json:"files"`
	Chunks       int    `json:"chunks"`
	LastIndexed  string `json:"last_indexed"`
	Status       string `json:"status"` // "active" | "deprecated"
	Reason       string `json:"deprecated_reason,omitempty"`
	DeprecatedAt string `json:"deprecated_at,omitempty"`

	OnDisk       bool   `json:"root_exists"`   // root directory still present
	DBExists     bool   `json:"db_exists"`     // backing DB file present (SQLite)
	SizeBytes    int64  `json:"size_bytes"`    // DB file size incl. wal/shm (SQLite)
	ModelMatches bool   `json:"model_matches"` // model+dim match current config
	MatchReason  string `json:"mismatch_reason,omitempty"`
}

// Manager is the backend-agnostic management surface used by the CLI.
type Manager interface {
	// Backend names the active backend ("sqlite" or "postgres").
	Backend() string
	// List returns active codebases, plus deprecated ones when includeDeprecated.
	List(ctx context.Context, includeDeprecated bool) ([]Codebase, error)
	// Info returns the single codebase matching root (active or deprecated).
	Info(ctx context.Context, root string) (Codebase, error)
	// Deprecate transitions an active codebase to deprecated, retaining its data
	// (archived DB file for SQLite, flagged rows for Postgres) until Purge.
	Deprecate(ctx context.Context, root, reason string) error
	// DropData discards an active codebase's indexed data in place (for a full
	// rebuild) without archiving it.
	DropData(ctx context.Context, root string) error
	// Purge permanently deletes every deprecated codebase's data and returns what
	// was purged.
	Purge(ctx context.Context) ([]Codebase, error)
	// Prune deprecates active codebases whose root path or DB file has gone
	// missing, and returns the entries it repaired.
	Prune(ctx context.Context) ([]Codebase, error)
	// Close releases any backend resources.
	Close() error
}

// NewManager returns a Manager for the configured backend.
func NewManager(ctx context.Context, cfg config.Config) (Manager, error) {
	switch cfg.Backend {
	case "", "sqlite":
		return newSQLiteManager(cfg)
	case "postgres", "pg":
		return newPGManager(ctx, cfg)
	default:
		return nil, fmt.Errorf("unknown backend %q", cfg.Backend)
	}
}

// decorate fills the computed fields shared by both backends.
func decorate(cb *Codebase, cfg config.Config) {
	cb.OnDisk = dirExists(cb.Root)
	match, reason := store.MatchesConfig(store.RegistryEntry{Model: cb.Model, Dims: cb.Dims}, cfg)
	cb.ModelMatches = match
	cb.MatchReason = reason
	if cb.Status == "" {
		cb.Status = store.StatusActive
	}
}

// absOr returns the absolute form of path, or path unchanged on error.
func absOr(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// dbSize returns the total on-disk size of a SQLite DB including its -wal/-shm
// sidecar files, or 0 if the main file is absent.
func dbSize(dbFile string) int64 {
	var total int64
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if info, err := os.Stat(dbFile + suffix); err == nil {
			total += info.Size()
		}
	}
	return total
}
