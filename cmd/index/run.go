package index

import (
	"context"
	"fmt"
	"os"
	"time"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/embeddings"
	"semantic-codesearch/internal/indexer"
	"semantic-codesearch/internal/store"
)

// Run indexes the given directory and prints a summary to stderr.
func Run(directory string) error {
	ctx := context.Background()
	cfg := config.Load()

	st, err := store.Open(ctx, cfg, directory)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	embedder := embeddings.NewClient(cfg)

	onProgress := func(msg string) {
		fmt.Fprintf(os.Stderr, "%s\n", msg)
	}

	result, err := indexer.IndexDirectory(ctx, directory, cfg, st, embedder, onProgress)
	if err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	// Record this codebase in the registry (SQLite backend).
	if cfg.Backend == "" || cfg.Backend == "sqlite" {
		if reg, rerr := store.LoadRegistry(cfg); rerr == nil {
			if s, serr := st.Status(ctx); serr == nil {
				reg.Upsert(directory, cfg.EmbeddingModel, config.EmbeddingDimensions, s.TotalFiles, s.TotalChunks, time.Now())
			}
		}
	}

	fmt.Fprintf(os.Stderr, "Indexed %d files (%d unchanged, %d errors) in %.1fs\n",
		result.FilesProcessed, result.FilesSkipped, result.Errors, result.Elapsed)

	for _, e := range result.ErrorDetails {
		fmt.Fprintf(os.Stderr, "  error: %s: %s\n", e.File, e.Error)
	}

	if result.Errors > 0 && result.FilesProcessed == 0 {
		return fmt.Errorf("indexing completed with %d errors and no files processed", result.Errors)
	}

	return nil
}
