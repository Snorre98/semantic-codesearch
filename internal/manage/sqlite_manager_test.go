package manage

import (
	"context"
	"os"
	"testing"
	"time"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/store"
)

// seed registers root with the given model and writes a dummy DB file at its
// canonical path, returning that path.
func seed(t *testing.T, cfg config.Config, root, model string) string {
	t.Helper()
	reg, err := store.LoadRegistry(cfg)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	if err := reg.Upsert(root, model, config.EmbeddingDimensions, 3, 7, time.Now()); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	dbPath := reg.DBPath(root)
	if err := os.WriteFile(dbPath, []byte("dummy-db"), 0o644); err != nil {
		t.Fatalf("write dummy db: %v", err)
	}
	return dbPath
}

func newCfg(t *testing.T) config.Config {
	t.Helper()
	return config.Config{Backend: "sqlite", SQLiteDir: t.TempDir(), EmbeddingModel: "nomic-embed-text"}
}

func newMgr(t *testing.T, cfg config.Config) Manager {
	t.Helper()
	mgr, err := NewManager(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { mgr.Close() })
	return mgr
}

func TestListReportsModelMatch(t *testing.T) {
	cfg := newCfg(t)
	matchRoot := t.TempDir()
	mismatchRoot := t.TempDir()
	seed(t, cfg, matchRoot, "nomic-embed-text")
	seed(t, cfg, mismatchRoot, "embeddinggemma")

	list, err := newMgr(t, cfg).List(context.Background(), false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 active codebases, got %d", len(list))
	}
	byRoot := map[string]Codebase{}
	for _, c := range list {
		byRoot[c.Root] = c
	}
	if !byRoot[absOr(matchRoot)].ModelMatches {
		t.Error("matching codebase should report ModelMatches=true")
	}
	if byRoot[absOr(mismatchRoot)].ModelMatches {
		t.Error("mismatched codebase should report ModelMatches=false")
	}
}

func TestDeprecateArchivesAndPurgeDeletes(t *testing.T) {
	cfg := newCfg(t)
	root := t.TempDir()
	dbPath := seed(t, cfg, root, "nomic-embed-text")
	ctx := context.Background()
	mgr := newMgr(t, cfg)

	if err := mgr.Deprecate(ctx, root, store.ReasonRemoved); err != nil {
		t.Fatalf("Deprecate: %v", err)
	}

	// Canonical DB archived away (data retained, not deleted).
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Error("canonical DB should have been archived (renamed) away")
	}
	active, _ := mgr.List(ctx, false)
	if len(active) != 0 {
		t.Errorf("expected no active codebases, got %d", len(active))
	}
	all, _ := mgr.List(ctx, true)
	if len(all) != 1 || all[0].Status != store.StatusDeprecated {
		t.Fatalf("expected 1 deprecated codebase, got %+v", all)
	}
	archive := all[0].DBFile
	if _, err := os.Stat(archive); err != nil {
		t.Errorf("archived DB should exist on disk: %v", err)
	}

	// Purge deletes the archived data.
	purged, err := mgr.Purge(ctx)
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if len(purged) != 1 {
		t.Fatalf("expected 1 purged, got %d", len(purged))
	}
	if _, err := os.Stat(archive); !os.IsNotExist(err) {
		t.Error("archived DB should be deleted after purge")
	}
	if remaining, _ := mgr.List(ctx, true); len(remaining) != 0 {
		t.Errorf("expected empty registry after purge, got %d", len(remaining))
	}
}

func TestPruneDetectsMissingRoot(t *testing.T) {
	cfg := newCfg(t)
	root := t.TempDir()
	seed(t, cfg, root, "nomic-embed-text")
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove root: %v", err)
	}

	ctx := context.Background()
	mgr := newMgr(t, cfg)
	repaired, err := mgr.Prune(ctx)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(repaired) != 1 {
		t.Fatalf("expected 1 repaired entry (missing root), got %d", len(repaired))
	}
	if active, _ := mgr.List(ctx, false); len(active) != 0 {
		t.Error("stale codebase should no longer be active after prune")
	}
}

func TestPruneDetectsMissingDB(t *testing.T) {
	cfg := newCfg(t)
	root := t.TempDir()
	dbPath := seed(t, cfg, root, "nomic-embed-text")
	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("remove db: %v", err)
	}

	ctx := context.Background()
	mgr := newMgr(t, cfg)
	repaired, err := mgr.Prune(ctx)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(repaired) != 1 {
		t.Fatalf("expected 1 repaired entry (missing db), got %d", len(repaired))
	}
}

func TestDropData(t *testing.T) {
	cfg := newCfg(t)
	root := t.TempDir()
	dbPath := seed(t, cfg, root, "nomic-embed-text")
	ctx := context.Background()
	mgr := newMgr(t, cfg)

	if err := mgr.DropData(ctx, root); err != nil {
		t.Fatalf("DropData: %v", err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Error("DropData should delete the DB file")
	}
}
