package index

import (
	"context"
	"fmt"
	"os"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/db"
	"semantic-codesearch/internal/embeddings"
	"semantic-codesearch/internal/indexer"
)

// Run indexes the given directory and prints a summary to stderr.
func Run(directory string) error {
	ctx := context.Background()
	cfg := config.Load()

	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer pool.Close()

	embedder := embeddings.NewClient(cfg)

	onProgress := func(msg string) {
		fmt.Fprintf(os.Stderr, "%s\n", msg)
	}

	result, err := indexer.IndexDirectory(ctx, directory, cfg, pool, embedder, onProgress)
	if err != nil {
		return fmt.Errorf("indexing failed: %w", err)
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
