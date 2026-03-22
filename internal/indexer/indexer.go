package indexer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"semantic-codesearch/internal/chunker"
	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/db"
	"semantic-codesearch/internal/embeddings"
	"semantic-codesearch/internal/ignore"
	"semantic-codesearch/internal/models"
)

// IndexDirectory walks a directory, chunks files, embeds them, and stores in pgvector.
func IndexDirectory(ctx context.Context, directory string, cfg config.Config, pool *pgxpool.Pool, embedder *embeddings.Client) (models.IndexResult, error) {
	root, err := filepath.Abs(directory)
	if err != nil {
		return models.IndexResult{}, fmt.Errorf("resolve path: %w", err)
	}

	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return models.IndexResult{}, fmt.Errorf("not a directory: %s", root)
	}

	spec := ignore.BuildIgnoreSpec(root)
	maxBytes := int64(cfg.MaxFileSizeKB) * 1024

	var filesProcessed, filesSkipped, errors int
	var errorDetails []models.ErrorDetail
	startTime := time.Now()

	filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}

		if d.IsDir() {
			if ignore.ShouldIgnore(path, root, spec) {
				return fs.SkipDir
			}
			return nil
		}

		if ignore.ShouldIgnore(path, root, spec) {
			return nil
		}
		if ignore.IsBinary(path) {
			return nil
		}

		finfo, err := d.Info()
		if err != nil {
			return nil
		}
		if finfo.Size() > maxBytes {
			return nil
		}

		mtime := float64(finfo.ModTime().Unix())

		// Incremental: skip unchanged files
		if _, unchanged := db.GetFileIfUnchanged(ctx, pool, path, mtime); unchanged {
			filesSkipped++
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: path, Error: err.Error()})
			return nil
		}

		content := string(data)
		chunks := chunker.ChunkFile(content, path)
		if len(chunks) == 0 {
			return nil
		}

		// Prepare texts for embedding
		texts := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = chunker.FormatChunkForEmbedding(c, path)
		}

		// Embed in batches
		var allEmbeddings [][]float32
		for i := 0; i < len(texts); i += cfg.BatchSize {
			end := i + cfg.BatchSize
			if end > len(texts) {
				end = len(texts)
			}
			batch, err := embedder.EmbedBatch(texts[i:end])
			if err != nil {
				errors++
				errorDetails = append(errorDetails, models.ErrorDetail{File: path, Error: err.Error()})
				return nil
			}
			allEmbeddings = append(allEmbeddings, batch...)
		}

		fileHash := fmt.Sprintf("%x", sha256.Sum256(data))
		lang := chunker.DetectLanguage(path)

		// Store in a transaction
		tx, err := pool.Begin(ctx)
		if err != nil {
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: path, Error: err.Error()})
			return nil
		}

		fileID, err := db.UpsertFile(ctx, tx, path, mtime, fileHash, lang)
		if err != nil {
			tx.Rollback(ctx)
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: path, Error: err.Error()})
			return nil
		}

		chunkRecords := make([]db.ChunkRecord, len(chunks))
		for i, c := range chunks {
			chunkRecords[i] = db.ChunkRecord{
				ChunkIndex: i,
				StartLine:  c.StartLine,
				EndLine:    c.EndLine,
				ChunkType:  c.ChunkType,
				SymbolName: c.SymbolName,
				Content:    c.Content,
				Embedding:  allEmbeddings[i],
			}
		}

		if err := db.InsertChunks(ctx, tx, fileID, chunkRecords); err != nil {
			tx.Rollback(ctx)
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: path, Error: err.Error()})
			return nil
		}

		if err := tx.Commit(ctx); err != nil {
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: path, Error: err.Error()})
			return nil
		}

		filesProcessed++
		return nil
	})

	// Record the run
	db.RecordIndexRun(ctx, pool, root, filesProcessed, filesSkipped, errors, errorDetails)

	return models.IndexResult{
		FilesProcessed: filesProcessed,
		FilesSkipped:   filesSkipped,
		Errors:         errors,
		ErrorDetails:   errorDetails,
		Elapsed:        time.Since(startTime).Seconds(),
	}, nil
}
