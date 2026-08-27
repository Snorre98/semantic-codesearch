package manage

import (
	"context"
	"fmt"
	"sort"
	"time"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/store"
)

// pgManager manages codebases in the shared Postgres database via the codebases
// metadata table. Deprecation flags rows (retained, excluded from search) until a
// purge deletes them; there is no separate archive file as with SQLite.
type pgManager struct {
	cfg config.Config
	st  store.Store
	cbs store.CodebaseStore
}

func newPGManager(ctx context.Context, cfg config.Config) (*pgManager, error) {
	st, err := store.Open(ctx, cfg, "")
	if err != nil {
		return nil, err
	}
	cbs, ok := st.(store.CodebaseStore)
	if !ok {
		st.Close()
		return nil, fmt.Errorf("postgres backend does not support codebase management")
	}
	return &pgManager{cfg: cfg, st: st, cbs: cbs}, nil
}

func (m *pgManager) Backend() string { return "postgres" }
func (m *pgManager) Close() error    { return m.st.Close() }

func (m *pgManager) decorate(e store.RegistryEntry) Codebase {
	cb := Codebase{
		Root:         e.Root,
		Model:        e.Model,
		Dims:         e.Dims,
		Files:        e.Files,
		Chunks:       e.Chunks,
		LastIndexed:  e.LastIndexed,
		Status:       e.Status,
		Reason:       e.DeprecatedReason,
		DeprecatedAt: e.DeprecatedAt,
	}
	decorate(&cb, m.cfg)
	cb.DBExists = true // shared DB; no per-codebase file
	return cb
}

func (m *pgManager) List(ctx context.Context, includeDeprecated bool) ([]Codebase, error) {
	all, err := m.cbs.ListCodebases(ctx)
	if err != nil {
		return nil, err
	}
	out := []Codebase{}
	for _, e := range all {
		if !e.Active() && !includeDeprecated {
			continue
		}
		out = append(out, m.decorate(e))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastIndexed > out[j].LastIndexed })
	return out, nil
}

func (m *pgManager) Info(ctx context.Context, root string) (Codebase, error) {
	all, err := m.cbs.ListCodebases(ctx)
	if err != nil {
		return Codebase{}, err
	}
	want := absOr(root)
	for _, e := range all {
		if e.Root == want {
			return m.decorate(e), nil
		}
	}
	return Codebase{}, fmt.Errorf("no codebase registered for %q", root)
}

func (m *pgManager) Deprecate(ctx context.Context, root, reason string) error {
	return m.cbs.DeprecateCodebase(ctx, absOr(root), reason, time.Now())
}

func (m *pgManager) DropData(ctx context.Context, root string) error {
	_, _, err := m.cbs.DeleteCodebase(ctx, absOr(root))
	return err
}

func (m *pgManager) Purge(ctx context.Context) ([]Codebase, error) {
	all, err := m.cbs.ListCodebases(ctx)
	if err != nil {
		return nil, err
	}
	var purged []Codebase
	for _, e := range all {
		if e.Active() {
			continue
		}
		if _, _, err := m.cbs.DeleteCodebase(ctx, e.Root); err != nil {
			return purged, err
		}
		purged = append(purged, m.decorate(e))
	}
	return purged, nil
}

func (m *pgManager) Prune(ctx context.Context) ([]Codebase, error) {
	all, err := m.cbs.ListCodebases(ctx)
	if err != nil {
		return nil, err
	}
	var repaired []Codebase
	for _, e := range all {
		if !e.Active() || dirExists(e.Root) {
			continue
		}
		if err := m.cbs.DeprecateCodebase(ctx, e.Root, store.ReasonStale, time.Now()); err != nil {
			return repaired, err
		}
		repaired = append(repaired, m.decorate(e))
	}
	return repaired, nil
}
