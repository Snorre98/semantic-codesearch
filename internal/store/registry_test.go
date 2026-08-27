package store

import (
	"path/filepath"
	"testing"
	"time"

	"semantic-codesearch/internal/config"
)

func testCfg(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		Backend:        "sqlite",
		SQLiteDir:      t.TempDir(),
		EmbeddingModel: "nomic-embed-text",
	}
}

func TestRegistryUpsertAndGet(t *testing.T) {
	cfg := testCfg(t)
	reg, err := LoadRegistry(cfg)
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	root := t.TempDir()
	if err := reg.Upsert(root, "nomic-embed-text", 768, 12, 34, time.Now()); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Reload from disk to confirm persistence and field round-trip.
	reg2, err := LoadRegistry(cfg)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	e, ok := reg2.Get(root)
	if !ok {
		t.Fatal("Get: entry not found after reload")
	}
	if e.Files != 12 || e.Chunks != 34 || e.Dims != 768 {
		t.Errorf("round-trip mismatch: %+v", e)
	}
	if !e.Active() {
		t.Errorf("new entry should be active, got status %q", e.Status)
	}
}

func TestRegistryDeprecateLifecycle(t *testing.T) {
	cfg := testCfg(t)
	reg, _ := LoadRegistry(cfg)
	root := t.TempDir()
	if err := reg.Upsert(root, "nomic-embed-text", 768, 1, 2, time.Now()); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	when := time.Now()
	archive := reg.DeprecatedDBPath(reg.DBPath(root), "nomic-embed-text", when)
	if err := reg.MoveToDeprecated(root, archive, ReasonRemoved, when); err != nil {
		t.Fatalf("MoveToDeprecated: %v", err)
	}

	if _, ok := reg.Get(root); ok {
		t.Error("entry should no longer be active after deprecation")
	}
	dep := reg.ListDeprecated()
	if len(dep) != 1 {
		t.Fatalf("expected 1 deprecated entry, got %d", len(dep))
	}
	if dep[0].Status != StatusDeprecated || dep[0].DeprecatedReason != ReasonRemoved {
		t.Errorf("unexpected deprecated entry: %+v", dep[0])
	}
	if dep[0].DBFile != archive {
		t.Errorf("deprecated DBFile = %q, want archive %q", dep[0].DBFile, archive)
	}

	// Persisted across reload.
	reg2, _ := LoadRegistry(cfg)
	if len(reg2.ListDeprecated()) != 1 {
		t.Error("deprecated entry not persisted")
	}

	// Purge removes it.
	if err := reg.RemoveDeprecated(archive); err != nil {
		t.Fatalf("RemoveDeprecated: %v", err)
	}
	if len(reg.ListDeprecated()) != 0 {
		t.Error("deprecated entry should be gone after RemoveDeprecated")
	}
}

func TestMatchesConfig(t *testing.T) {
	cfg := config.Config{EmbeddingModel: "nomic-embed-text"}
	cases := []struct {
		name  string
		entry RegistryEntry
		want  bool
	}{
		{"match", RegistryEntry{Model: "nomic-embed-text", Dims: config.EmbeddingDimensions}, true},
		{"model diff", RegistryEntry{Model: "embeddinggemma", Dims: config.EmbeddingDimensions}, false},
		{"dim diff", RegistryEntry{Model: "nomic-embed-text", Dims: 1024}, false},
		{"empty entry tolerated", RegistryEntry{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := MatchesConfig(tc.entry, cfg)
			if got != tc.want {
				t.Errorf("MatchesConfig = %v (reason %q), want %v", got, reason, tc.want)
			}
			if !got && reason == "" {
				t.Error("expected a non-empty mismatch reason")
			}
		})
	}
}

func TestDBPathStableAndDistinct(t *testing.T) {
	reg, _ := LoadRegistry(testCfg(t))
	a, _ := filepath.Abs("/tmp/projects/alpha")
	b, _ := filepath.Abs("/tmp/projects/beta")
	if reg.DBPath(a) == reg.DBPath(b) {
		t.Error("distinct roots produced the same DB path")
	}
	if reg.DBPath(a) != reg.DBPath(a) {
		t.Error("DBPath is not stable for the same root")
	}
}

func TestDeprecatedDBPathDistinctFromCanonical(t *testing.T) {
	reg, _ := LoadRegistry(testCfg(t))
	canonical := reg.DBPath(t.TempDir())
	archive := reg.DeprecatedDBPath(canonical, "nomic-embed-text", time.Unix(1700000000, 0))
	if archive == canonical {
		t.Error("archive path must differ from the canonical path")
	}
	if filepath.Ext(archive) != ".db" {
		t.Errorf("archive path should end in .db, got %q", archive)
	}
}
