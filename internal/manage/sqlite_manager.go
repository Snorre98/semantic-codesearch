package manage

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/store"
)

// sqliteManager manages per-codebase SQLite DB files via the JSON registry.
type sqliteManager struct {
	cfg config.Config
	reg *store.Registry
}

func newSQLiteManager(cfg config.Config) (*sqliteManager, error) {
	reg, err := store.LoadRegistry(cfg)
	if err != nil {
		return nil, err
	}
	return &sqliteManager{cfg: cfg, reg: reg}, nil
}

func (m *sqliteManager) Backend() string { return "sqlite" }
func (m *sqliteManager) Close() error    { return nil }

func (m *sqliteManager) decorate(e store.RegistryEntry) Codebase {
	cb := Codebase{
		Root:         e.Root,
		DBFile:       e.DBFile,
		Model:        e.Model,
		Dims:         e.Dims,
		Files:        e.Files,
		Chunks:       e.Chunks,
		LastIndexed:  e.LastIndexed,
		Status:       e.Status,
		Reason:       e.DeprecatedReason,
		DeprecatedAt: e.DeprecatedAt,
	}
	if cb.Status == "" {
		cb.Status = store.StatusActive
	}
	decorate(&cb, m.cfg)
	cb.DBExists = fileExists(e.DBFile)
	cb.SizeBytes = dbSize(e.DBFile)
	return cb
}

func (m *sqliteManager) List(_ context.Context, includeDeprecated bool) ([]Codebase, error) {
	out := []Codebase{}
	for _, e := range m.reg.List() {
		out = append(out, m.decorate(e))
	}
	if includeDeprecated {
		for _, e := range m.reg.ListDeprecated() {
			out = append(out, m.decorate(e))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastIndexed > out[j].LastIndexed })
	return out, nil
}

func (m *sqliteManager) Info(_ context.Context, root string) (Codebase, error) {
	if e, ok := m.reg.Get(root); ok {
		return m.decorate(e), nil
	}
	for _, e := range m.reg.ListDeprecated() {
		if e.Root == absOr(root) {
			return m.decorate(e), nil
		}
	}
	return Codebase{}, fmt.Errorf("no codebase registered for %q", root)
}

func (m *sqliteManager) Deprecate(_ context.Context, root, reason string) error {
	e, ok := m.reg.Get(root)
	if !ok {
		return fmt.Errorf("no active codebase registered for %q", root)
	}
	when := time.Now()
	archive := e.DBFile
	// Archive the DB so the canonical path is free for a future re-index. If the
	// file is already gone (stale prune), there is nothing to move.
	if fileExists(e.DBFile) {
		archive = m.reg.DeprecatedDBPath(e.DBFile, e.Model, when)
		if err := moveDBFiles(e.DBFile, archive); err != nil {
			return err
		}
	}
	return m.reg.MoveToDeprecated(root, archive, reason, when)
}

func (m *sqliteManager) DropData(_ context.Context, root string) error {
	e, ok := m.reg.Get(root)
	if !ok {
		return fmt.Errorf("no active codebase registered for %q", root)
	}
	return removeDBFiles(e.DBFile)
}

func (m *sqliteManager) Purge(_ context.Context) ([]Codebase, error) {
	var purged []Codebase
	for _, e := range m.reg.ListDeprecated() {
		if err := removeDBFiles(e.DBFile); err != nil {
			return purged, err
		}
		if err := m.reg.RemoveDeprecated(e.DBFile); err != nil {
			return purged, err
		}
		purged = append(purged, m.decorate(e))
	}
	return purged, nil
}

func (m *sqliteManager) Prune(ctx context.Context) ([]Codebase, error) {
	var repaired []Codebase
	for _, e := range m.reg.List() { // List returns a copy; safe to mutate during loop
		if dirExists(e.Root) && fileExists(e.DBFile) {
			continue
		}
		if err := m.Deprecate(ctx, e.Root, store.ReasonStale); err != nil {
			return repaired, err
		}
		repaired = append(repaired, m.decorate(e))
	}
	return repaired, nil
}

// moveDBFiles renames a SQLite DB and its -wal/-shm sidecars to a new base path.
func moveDBFiles(from, to string) error {
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("archive db: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if fileExists(from + suffix) {
			_ = os.Rename(from+suffix, to+suffix)
		}
	}
	return nil
}

// removeDBFiles deletes a SQLite DB and its sidecars, tolerating absence.
func removeDBFiles(dbFile string) error {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(dbFile + suffix); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", dbFile+suffix, err)
		}
	}
	return nil
}
