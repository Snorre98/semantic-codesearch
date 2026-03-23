package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
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

// benchmarkEntry is the JSON structure appended to benchmarks/runs.jsonl.
type benchmarkEntry struct {
	Date      string  `json:"date"`
	Directory string  `json:"directory"`
	Files     int     `json:"files"`
	Skipped   int     `json:"skipped"`
	Errors    int     `json:"errors"`
	Chunks    int     `json:"chunks"`
	Batches   int     `json:"batches"`
	TotalS    float64 `json:"total_s"`
	WalkS     float64 `json:"walk_s"`
	ChunkS    float64 `json:"chunk_s"`
	EmbedS    float64 `json:"embed_s"`
	DBS       float64 `json:"db_s"`
	Model     string  `json:"model"`
	BatchSize int     `json:"batch_size"`
	OS        string  `json:"os"`
	Arch      string  `json:"arch"`
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

	walkStart := time.Now()
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
	walkDuration := time.Since(walkStart)

	onProgress(fmt.Sprintf("Found %d files to index (%d unchanged, skipping)", len(candidates), filesSkipped))

	// Pass 2: chunk all candidate files
	type fileChunks struct {
		cf       candidateFile
		relPath  string
		data     []byte
		chunks   []models.CodeChunk
		texts    []string
		fileHash string
		lang     string
	}

	var chunkedFiles []fileChunks
	var totalTexts []string            // all embedding texts, flattened
	var textFileIndex []int            // maps each text index → chunkedFiles index
	var filesProcessed, totalChunks, totalBatches, errors int
	var errorDetails []models.ErrorDetail
	var chunkDuration, embedDuration, dbDuration time.Duration

	chunkStart := time.Now()
	for _, cf := range candidates {
		relPath, _ := filepath.Rel(root, cf.path)
		if relPath == "" {
			relPath = cf.path
		}

		data, err := os.ReadFile(cf.path)
		if err != nil {
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: cf.path, Error: err.Error()})
			continue
		}

		chunks := chunker.ChunkFile(string(data), cf.path)
		if len(chunks) == 0 {
			continue
		}

		texts := make([]string, len(chunks))
		for j, c := range chunks {
			texts[j] = chunker.FormatChunkForEmbedding(c, cf.path)
		}

		fileIdx := len(chunkedFiles)
		chunkedFiles = append(chunkedFiles, fileChunks{
			cf:       cf,
			relPath:  relPath,
			data:     data,
			chunks:   chunks,
			texts:    texts,
			fileHash: fmt.Sprintf("%x", sha256.Sum256(data)),
			lang:     chunker.DetectLanguage(cf.path),
		})

		for range texts {
			textFileIndex = append(textFileIndex, fileIdx)
		}
		totalTexts = append(totalTexts, texts...)
	}
	chunkDuration = time.Since(chunkStart)

	onProgress(fmt.Sprintf("Chunked %d files into %d chunks, embedding...", len(chunkedFiles), len(totalTexts)))

	// Pass 3: embed all chunks in proper batches across files
	allEmbeddings := make([][]float32, len(totalTexts))
	embedStart := time.Now()
	embErr := false
	for i := 0; i < len(totalTexts); i += cfg.BatchSize {
		end := i + cfg.BatchSize
		if end > len(totalTexts) {
			end = len(totalTexts)
		}
		totalBatches++
		onProgress(fmt.Sprintf("Embedding batch %d/%d (%d chunks)", totalBatches, (len(totalTexts)+cfg.BatchSize-1)/cfg.BatchSize, end-i))

		batch, err := embedder.EmbedBatch(totalTexts[i:end])
		if err != nil {
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: "batch-embed", Error: err.Error()})
			embErr = true
			break
		}
		copy(allEmbeddings[i:end], batch)
	}
	embedDuration = time.Since(embedStart)

	if embErr {
		// Return partial results — no embeddings available
		elapsed := time.Since(startTime).Seconds()
		return models.IndexResult{Errors: errors, ErrorDetails: errorDetails, Elapsed: elapsed}, nil
	}

	// Pass 4: store each file's chunks with their embeddings
	embOffset := 0
	for _, fc := range chunkedFiles {
		n := len(fc.chunks)

		dbStart := time.Now()
		tx, err := pool.Begin(ctx)
		if err != nil {
			dbDuration += time.Since(dbStart)
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: fc.cf.path, Error: err.Error()})
			embOffset += n
			continue
		}

		fileID, err := db.UpsertFile(ctx, tx, fc.cf.path, fc.cf.mtime, fc.fileHash, fc.lang)
		if err != nil {
			tx.Rollback(ctx)
			dbDuration += time.Since(dbStart)
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: fc.cf.path, Error: err.Error()})
			embOffset += n
			continue
		}

		chunkRecords := make([]db.ChunkRecord, n)
		for j, c := range fc.chunks {
			chunkRecords[j] = db.ChunkRecord{
				ChunkIndex: j,
				StartLine:  c.StartLine,
				EndLine:    c.EndLine,
				ChunkType:  c.ChunkType,
				SymbolName: c.SymbolName,
				Content:    c.Content,
				Embedding:  allEmbeddings[embOffset+j],
			}
		}

		if err := db.InsertChunks(ctx, tx, fileID, chunkRecords); err != nil {
			tx.Rollback(ctx)
			dbDuration += time.Since(dbStart)
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: fc.cf.path, Error: err.Error()})
			embOffset += n
			continue
		}

		if err := tx.Commit(ctx); err != nil {
			dbDuration += time.Since(dbStart)
			errors++
			errorDetails = append(errorDetails, models.ErrorDetail{File: fc.cf.path, Error: err.Error()})
			embOffset += n
			continue
		}
		dbDuration += time.Since(dbStart)

		filesProcessed++
		totalChunks += n
		embOffset += n
	}

	elapsed := time.Since(startTime).Seconds()
	walkS := walkDuration.Seconds()
	chunkS := chunkDuration.Seconds()
	embedS := embedDuration.Seconds()
	dbS := dbDuration.Seconds()

	// Structured summary line to stderr
	log.Printf("[index] %d files (%d skipped) %d chunks %d batches | total=%.1fs walk=%.1fs chunk=%.1fs embed=%.1fs db=%.1fs | model=%s batch=%d",
		filesProcessed, filesSkipped, totalChunks, totalBatches,
		elapsed, walkS, chunkS, embedS, dbS,
		cfg.EmbeddingModel, cfg.BatchSize)

	onProgress(fmt.Sprintf("Indexing complete: %d files, %d chunks, %.1fs elapsed", filesProcessed, totalChunks, elapsed))

	// Append to persistent JSONL benchmark file
	writeBenchmark(root, filesProcessed, filesSkipped, errors, totalChunks, totalBatches,
		elapsed, walkS, chunkS, embedS, dbS, cfg.EmbeddingModel, cfg.BatchSize)

	// Record the run
	db.RecordIndexRun(ctx, pool, root, filesProcessed, filesSkipped, errors, errorDetails)

	return models.IndexResult{
		FilesProcessed: filesProcessed,
		FilesSkipped:   filesSkipped,
		Errors:         errors,
		ErrorDetails:   errorDetails,
		Elapsed:        elapsed,
		TotalChunks:    totalChunks,
		TotalBatches:   totalBatches,
		WalkDuration:   walkS,
		ChunkDuration:  chunkS,
		EmbedDuration:  embedS,
		DBDuration:     dbS,
		Model:          cfg.EmbeddingModel,
		BatchSize:      cfg.BatchSize,
	}, nil
}

// writeBenchmark appends a single JSON line to benchmarks/runs.jsonl in the process working directory.
func writeBenchmark(directory string, files, skipped, errors, chunks, batches int,
	totalS, walkS, chunkS, embedS, dbS float64, model string, batchSize int) {

	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("[index] warning: cannot determine working directory for benchmark file: %v", err)
		return
	}

	dir := filepath.Join(cwd, "benchmarks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[index] warning: cannot create benchmarks directory: %v", err)
		return
	}

	entry := benchmarkEntry{
		Date:      time.Now().UTC().Format(time.RFC3339),
		Directory: directory,
		Files:     files,
		Skipped:   skipped,
		Errors:    errors,
		Chunks:    chunks,
		Batches:   batches,
		TotalS:    totalS,
		WalkS:     walkS,
		ChunkS:    chunkS,
		EmbedS:    embedS,
		DBS:       dbS,
		Model:     model,
		BatchSize: batchSize,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[index] warning: cannot marshal benchmark entry: %v", err)
		return
	}

	f, err := os.OpenFile(filepath.Join(dir, "runs.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("[index] warning: cannot open benchmark file: %v", err)
		return
	}
	defer f.Close()

	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		log.Printf("[index] warning: cannot write benchmark entry: %v", err)
	}
}
