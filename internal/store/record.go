package store

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"semantic-codesearch/internal/config"
)

// isSQLite reports whether cfg selects the per-codebase SQLite backend.
func isSQLite(cfg config.Config) bool {
	return cfg.Backend == "" || cfg.Backend == "sqlite"
}

// RecordCodebase persists per-codebase metadata after a successful index run,
// using the right mechanism per backend: the JSON registry for SQLite, the
// codebases table for Postgres. It is best-effort and returns any error so the
// caller can log it without failing the index.
func RecordCodebase(ctx context.Context, cfg config.Config, st Store, root string, files, chunks int, when time.Time) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	if isSQLite(cfg) {
		reg, err := LoadRegistry(cfg)
		if err != nil {
			return err
		}
		return reg.Upsert(abs, cfg.EmbeddingModel, config.EmbeddingDimensions, files, chunks, when)
	}
	cbs, ok := st.(CodebaseStore)
	if !ok {
		return nil // backend without codebase metadata; nothing to record
	}
	return cbs.UpsertCodebase(ctx, RegistryEntry{
		Root:   abs,
		Model:  cfg.EmbeddingModel,
		Dims:   config.EmbeddingDimensions,
		Files:  files,
		Chunks: chunks,
	}, when)
}

// GuardModel returns a non-nil error when root is already indexed with a model or
// dimension that differs from the current configuration — searching it would mix
// vector spaces and silently corrupt results. It returns nil when root is not yet
// indexed (first run) or when the recorded model matches the configured one.
//
// st may be nil for SQLite (the registry is consulted); for Postgres an open
// store is used to read the codebases table.
func GuardModel(ctx context.Context, cfg config.Config, st Store, root string) error {
	e, ok, err := lookupCodebase(ctx, cfg, st, root)
	if err != nil || !ok || !e.Active() {
		return err
	}
	if match, reason := MatchesConfig(e, cfg); !match {
		return fmt.Errorf("codebase %q %s; re-embed with `rebuild --reembed` to switch models", e.Root, reason)
	}
	return nil
}

// lookupCodebase finds the recorded entry for an exact root across either backend.
func lookupCodebase(ctx context.Context, cfg config.Config, st Store, root string) (RegistryEntry, bool, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	if isSQLite(cfg) {
		reg, err := LoadRegistry(cfg)
		if err != nil {
			return RegistryEntry{}, false, err
		}
		e, ok := reg.Get(abs)
		return e, ok, nil
	}
	cbs, ok := st.(CodebaseStore)
	if !ok {
		return RegistryEntry{}, false, nil
	}
	list, err := cbs.ListCodebases(ctx)
	if err != nil {
		return RegistryEntry{}, false, err
	}
	for _, e := range list {
		if e.Root == abs {
			return e, true, nil
		}
	}
	return RegistryEntry{}, false, nil
}
