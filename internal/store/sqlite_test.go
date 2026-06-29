package store

import (
	"context"
	"testing"

	"semantic-codesearch/internal/config"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	cfg := config.Config{Backend: "sqlite", SQLiteDir: t.TempDir(), EmbeddingModel: "test"}
	st, err := OpenSQLite(context.Background(), cfg, t.TempDir())
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// vec returns a unit-ish vector of EmbeddingDimensions with a 1.0 spike at idx.
func vec(idx int) []float32 {
	v := make([]float32, config.EmbeddingDimensions)
	v[idx%config.EmbeddingDimensions] = 1
	return v
}

func TestSQLiteRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	files := []FileWithChunks{
		{
			Path: "/repo/a.go", LastModified: 1, FileHash: "h1", Language: "go",
			Chunks: []ChunkRecord{
				{ChunkIndex: 0, StartLine: 1, EndLine: 5, ChunkType: "function", SymbolName: "Alpha", Content: "func Alpha()", Embedding: vec(0)},
				{ChunkIndex: 1, StartLine: 6, EndLine: 9, ChunkType: "function", SymbolName: "Beta", Content: "func Beta()", Embedding: vec(1)},
			},
		},
		{
			Path: "/repo/b.go", LastModified: 2, FileHash: "h2", Language: "go",
			Chunks: []ChunkRecord{
				{ChunkIndex: 0, StartLine: 1, EndLine: 3, ChunkType: "function", SymbolName: "Gamma", Content: "func Gamma()", Embedding: vec(2)},
			},
		},
	}

	gotFiles, gotChunks, errs := st.StoreFiles(ctx, files)
	if len(errs) != 0 {
		t.Fatalf("StoreFiles errors: %+v", errs)
	}
	if gotFiles != 2 || gotChunks != 3 {
		t.Fatalf("stored = (%d files, %d chunks), want (2, 3)", gotFiles, gotChunks)
	}

	// Query with a vector identical to Beta's embedding → Beta is top-1.
	res, err := st.Search(ctx, vec(1), 3, SearchFilters{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("Search returned no results")
	}
	if res[0].SymbolName != "Beta" {
		t.Fatalf("top result = %q, want Beta", res[0].SymbolName)
	}
	if res[0].Score < 0.99 {
		t.Fatalf("self-match score = %.4f, want ~1.0", res[0].Score)
	}

	// FileUnchanged reflects stored mtime.
	if !st.FileUnchanged(ctx, "/repo/a.go", 1) {
		t.Error("FileUnchanged(/repo/a.go, 1) = false, want true")
	}
	if st.FileUnchanged(ctx, "/repo/a.go", 999) {
		t.Error("FileUnchanged with wrong mtime = true, want false")
	}

	// Status counts.
	status, err := st.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.TotalFiles != 2 || status.TotalChunks != 3 {
		t.Fatalf("status = (%d, %d), want (2, 3)", status.TotalFiles, status.TotalChunks)
	}
}

func TestSQLiteReindexReplaces(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	first := []FileWithChunks{{
		Path: "/repo/a.go", LastModified: 1, FileHash: "h1", Language: "go",
		Chunks: []ChunkRecord{
			{ChunkIndex: 0, StartLine: 1, EndLine: 5, SymbolName: "Old", Content: "old", Embedding: vec(0)},
			{ChunkIndex: 1, StartLine: 6, EndLine: 9, SymbolName: "Old2", Content: "old2", Embedding: vec(1)},
		},
	}}
	if _, _, errs := st.StoreFiles(ctx, first); len(errs) != 0 {
		t.Fatalf("first store errors: %+v", errs)
	}

	// Re-index the same path with a single new chunk; old chunks/vectors must go.
	second := []FileWithChunks{{
		Path: "/repo/a.go", LastModified: 2, FileHash: "h2", Language: "go",
		Chunks: []ChunkRecord{
			{ChunkIndex: 0, StartLine: 1, EndLine: 4, SymbolName: "New", Content: "new", Embedding: vec(5)},
		},
	}}
	if _, _, errs := st.StoreFiles(ctx, second); len(errs) != 0 {
		t.Fatalf("second store errors: %+v", errs)
	}

	status, _ := st.Status(ctx)
	if status.TotalFiles != 1 || status.TotalChunks != 1 {
		t.Fatalf("after reindex status = (%d, %d), want (1, 1)", status.TotalFiles, status.TotalChunks)
	}

	// No orphaned vectors: searching returns exactly one row.
	res, err := st.Search(ctx, vec(5), 10, SearchFilters{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 || res[0].SymbolName != "New" {
		t.Fatalf("search results = %+v, want single New", res)
	}
}
