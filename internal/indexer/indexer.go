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

// ProgressFunc is a callback for reporting indexing progress.
type ProgressFunc func(message string)

// candidateFile holds metadata collected during the scan pass.
type candidateFile struct {
	path  string
	mtime float64
}

// IndexDirectory walks a directory, chunks files, embeds them, and stores in pgvector.
func IndexDirectory(ctx context.Context, directory string, cfg config.Config, pool *pgxpool.Pool, embedder *embeddings.Client, onProgress ProgressFunc) (models.IndexResult, error) {
	if onProgress == nil {
		onProgress = func(string) {}
	}

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
	startTime := time.Now()

	// Pass 1: scan and collect candidate files
	onProgress("Scanning files...")

	var candidates []candidateFile
	var filesSkipped int

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

		candidates = append(candidates, candidateFile{path: path, mtime: mtime})
		return nil
	})

	onProgress(fmt.Sprintf("Found %d files to index (%d unchanged, skipping)", len(candidates), filesSkipped))

	// Pass 2: process each candidate file
	var filesProcessed, totalChunks, errors int
	var errorDetails []models.ErrorDetail

	for i, cf := range candidates {
		relPath, _ := filepath.Rel(root, cf.path)
		if relPath == "" {
			relPath = cf.path
		}
		onProgress(fmt.Sprintf("Embedding file %d/%d: %s", i+1, len(candidates), relPath))

		data, err := os.ReadFile(cf.path)
		if err != nil {
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: cf.path, Error: err.Error()})
			continue
		}

		content := string(data)
		chunks := chunker.ChunkFile(content, cf.path)
		if len(chunks) == 0 {
			continue
		}

		// Prepare texts for embedding
		texts := make([]string, len(chunks))
		for j, c := range chunks {
			texts[j] = chunker.FormatChunkForEmbedding(c, cf.path)
		}

		// Embed in batches
		var allEmbeddings [][]float32
		embErr := false
		for j := 0; j < len(texts); j += cfg.BatchSize {
			end := j + cfg.BatchSize
			if end > len(texts) {
				end = len(texts)
			}
			batch, err := embedder.EmbedBatch(texts[j:end])
			if err != nil {
				errors++
				errorDetails = append(errorDetails, models.ErrorDetail{File: cf.path, Error: err.Error()})
				embErr = true
				break
			}
			allEmbeddings = append(allEmbeddings, batch...)
		}
		if embErr {
			continue
		}

		fileHash := fmt.Sprintf("%x", sha256.Sum256(data))
		lang := chunker.DetectLanguage(cf.path)

		// Store in a transaction
		tx, err := pool.Begin(ctx)
		if err != nil {
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: cf.path, Error: err.Error()})
			continue
		}

		fileID, err := db.UpsertFile(ctx, tx, cf.path, cf.mtime, fileHash, lang)
		if err != nil {
			tx.Rollback(ctx)
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: cf.path, Error: err.Error()})
			continue
		}

		chunkRecords := make([]db.ChunkRecord, len(chunks))
		for j, c := range chunks {
			chunkRecords[j] = db.ChunkRecord{
				ChunkIndex: j,
				StartLine:  c.StartLine,
				EndLine:    c.EndLine,
				ChunkType:  c.ChunkType,
				SymbolName: c.SymbolName,
				Content:    c.Content,
				Embedding:  allEmbeddings[j],
			}
		}

		if err := db.InsertChunks(ctx, tx, fileID, chunkRecords); err != nil {
			tx.Rollback(ctx)
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: cf.path, Error: err.Error()})
			continue
		}

		if err := tx.Commit(ctx); err != nil {
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: cf.path, Error: err.Error()})
			continue
		}

		filesProcessed++
		totalChunks += len(chunks)
	}

	elapsed := time.Since(startTime).Seconds()
	onProgress(fmt.Sprintf("Indexing complete: %d files, %d chunks, %.1fs elapsed", filesProcessed, totalChunks, elapsed))

	// Record the run
	db.RecordIndexRun(ctx, pool, root, filesProcessed, filesSkipped, errors, errorDetails)

	return models.IndexResult{
		FilesProcessed: filesProcessed,
		FilesSkipped:   filesSkipped,
		Errors:         errors,
		ErrorDetails:   errorDetails,
		Elapsed:        elapsed,
	}, nil
}
