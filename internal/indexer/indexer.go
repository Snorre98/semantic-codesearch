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
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"semantic-codesearch/internal/chunker"
	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/embeddings"
	"semantic-codesearch/internal/ignore"
	"semantic-codesearch/internal/models"
	"semantic-codesearch/internal/store"
)

// ProgressFunc is a callback for reporting indexing progress.
type ProgressFunc func(message string)

// candidateFile holds metadata collected during the scan pass.
type candidateFile struct {
	path  string
	mtime float64
}

// pendingFile holds a chunked file waiting to be embedded and stored.
type pendingFile struct {
	path       string
	relPath    string
	mtime      float64
	fileHash   string
	lang       string
	chunks     []models.CodeChunk
	texts      []string    // formatted embedding texts, parallel to chunks
	embeddings [][]float32 // filled after embed; nil entries = failed
}

// textRef maps a text index in the flush buffer back to its owning file and chunk.
type textRef struct {
	fileIdx  int
	chunkIdx int
}

// flushBuffer accumulates files and their texts until a batch is ready.
type flushBuffer struct {
	files   []pendingFile
	texts   []string
	textMap []textRef
}

func (b *flushBuffer) reset() {
	for i := range b.files {
		b.files[i] = pendingFile{}
	}
	b.files = b.files[:0]
	b.texts = b.texts[:0]
	b.textMap = b.textMap[:0]
}

// indexMetrics holds mutable counters passed into flushAndStore.
type indexMetrics struct {
	filesProcessed int
	totalChunks    int
	totalBatches   int
	errors         int
	errorDetails   []models.ErrorDetail
	chunkDuration  time.Duration
	embedDuration  time.Duration
	dbDuration     time.Duration
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

// IndexDirectory walks a directory, chunks files, embeds them, and writes to the Store.
func IndexDirectory(ctx context.Context, directory string, cfg config.Config, st store.Store, embedder *embeddings.Client, onProgress ProgressFunc) (models.IndexResult, error) {
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
		if st.FileUnchanged(ctx, path, mtime) {
			filesSkipped++
			return nil
		}

		candidates = append(candidates, candidateFile{path: path, mtime: mtime})
		return nil
	})
	walkDuration := time.Since(walkStart)

	onProgress(fmt.Sprintf("Found %d files to index (%d unchanged, skipping)", len(candidates), filesSkipped))

	// Streaming pipeline: chunk → buffer → embed+store in batches.
	// Files are accumulated until the buffer reaches BatchSize texts,
	// then flushed (embedded and written to DB) before continuing.
	// This gives incremental DB commits and interrupt safety.
	const maxCharsPerText = 30000

	var buf flushBuffer
	m := &indexMetrics{}

	for i, cf := range candidates {
		// Chunk this file
		chunkStart := time.Now()
		relPath, _ := filepath.Rel(root, cf.path)
		if relPath == "" {
			relPath = cf.path
		}

		data, err := os.ReadFile(cf.path)
		if err != nil {
			m.errors++
			m.errorDetails = append(m.errorDetails, models.ErrorDetail{File: cf.path, Error: err.Error()})
			m.chunkDuration += time.Since(chunkStart)
			continue
		}

		chunks := chunker.ChunkFile(string(data), cf.path)
		if len(chunks) == 0 {
			m.chunkDuration += time.Since(chunkStart)
			continue
		}

		texts := make([]string, len(chunks))
		for j, c := range chunks {
			t := chunker.FormatChunkForEmbedding(c, cf.path)
			if len(t) > maxCharsPerText {
				t = t[:maxCharsPerText]
			}
			texts[j] = t
		}
		m.chunkDuration += time.Since(chunkStart)

		// Append to buffer
		fileIdx := len(buf.files)
		buf.files = append(buf.files, pendingFile{
			path:     cf.path,
			relPath:  relPath,
			mtime:    cf.mtime,
			fileHash: fmt.Sprintf("%x", sha256.Sum256(data)),
			lang:     chunker.DetectLanguage(cf.path),
			chunks:   chunks,
			texts:    texts,
		})
		for j := range texts {
			buf.texts = append(buf.texts, texts[j])
			buf.textMap = append(buf.textMap, textRef{fileIdx: fileIdx, chunkIdx: j})
		}

		// Flush when buffer is full or on the last candidate
		if len(buf.texts) >= cfg.BatchSize || i == len(candidates)-1 {
			if err := flushAndStore(&buf, ctx, st, embedder, cfg, onProgress, m); err != nil {
				break // context cancelled
			}
			onProgress(fmt.Sprintf("Progress: %d/%d files indexed, %d chunks, %d errors",
				m.filesProcessed, len(candidates), m.totalChunks, m.errors))
			buf.reset()
		}
	}

	elapsed := time.Since(startTime).Seconds()
	walkS := walkDuration.Seconds()
	chunkS := m.chunkDuration.Seconds()
	embedS := m.embedDuration.Seconds()
	dbS := m.dbDuration.Seconds()

	// Structured summary line to stderr
	log.Printf("[index] %d files (%d skipped) %d chunks %d batches | total=%.1fs walk=%.1fs chunk=%.1fs embed=%.1fs db=%.1fs | model=%s batch=%d",
		m.filesProcessed, filesSkipped, m.totalChunks, m.totalBatches,
		elapsed, walkS, chunkS, embedS, dbS,
		cfg.EmbeddingModel, cfg.BatchSize)

	onProgress(fmt.Sprintf("Indexing complete: %d files, %d chunks, %.1fs elapsed", m.filesProcessed, m.totalChunks, elapsed))

	// Append to persistent JSONL benchmark file
	writeBenchmark(root, m.filesProcessed, filesSkipped, m.errors, m.totalChunks, m.totalBatches,
		elapsed, walkS, chunkS, embedS, dbS, cfg.EmbeddingModel, cfg.BatchSize)

	// Record the run
	st.RecordIndexRun(ctx, root, m.filesProcessed, filesSkipped, m.errors, m.errorDetails)

	return models.IndexResult{
		FilesProcessed: m.filesProcessed,
		FilesSkipped:   filesSkipped,
		Errors:         m.errors,
		ErrorDetails:   m.errorDetails,
		Elapsed:        elapsed,
		TotalChunks:    m.totalChunks,
		TotalBatches:   m.totalBatches,
		WalkDuration:   walkS,
		ChunkDuration:  chunkS,
		EmbedDuration:  embedS,
		DBDuration:     dbS,
		Model:          cfg.EmbeddingModel,
		BatchSize:      cfg.BatchSize,
	}, nil
}

// flushAndStore embeds all buffered texts (sub-batches in parallel) and writes the
// buffered files to the store. Returns a non-nil error only on context cancellation.
func flushAndStore(buf *flushBuffer, ctx context.Context, st store.Store, embedder *embeddings.Client, cfg config.Config, onProgress ProgressFunc, m *indexMetrics) error {
	if len(buf.texts) == 0 {
		return nil
	}

	// Split the buffer into sub-batches of cfg.BatchSize.
	type batchRange struct{ start, end int }
	var ranges []batchRange
	for i := 0; i < len(buf.texts); i += cfg.BatchSize {
		end := i + cfg.BatchSize
		if end > len(buf.texts) {
			end = len(buf.texts)
		}
		ranges = append(ranges, batchRange{i, end})
	}
	m.totalBatches += len(ranges)

	concurrency := cfg.EmbedConcurrency
	if concurrency < 1 {
		concurrency = 1
	}

	allEmbeddings := make([][]float32, len(buf.texts))
	embedStart := time.Now()

	// Embed sub-batches concurrently; mu guards the shared error metrics.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)
	var mu sync.Mutex
	var done int

	for _, r := range ranges {
		r := r
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			mu.Lock()
			done++
			n := done
			mu.Unlock()
			onProgress(fmt.Sprintf("Embedding batch %d/%d (%d chunks)...", n, len(ranges), r.end-r.start))

			batch, err := embedder.EmbedBatch(buf.texts[r.start:r.end])
			if err != nil {
				log.Printf("[index] batch %d failed (%v), retrying individually", n, err)
				for j := r.start; j < r.end; j++ {
					single, err2 := embedder.EmbedBatch(buf.texts[j : j+1])
					if err2 != nil {
						ref := buf.textMap[j]
						mu.Lock()
						m.errors++
						m.errorDetails = append(m.errorDetails, models.ErrorDetail{
							File:  buf.files[ref.fileIdx].relPath,
							Error: fmt.Sprintf("chunk embed failed: %v", err2),
						})
						mu.Unlock()
						continue
					}
					allEmbeddings[j] = single[0]
				}
				return nil
			}
			copy(allEmbeddings[r.start:r.end], batch)
			return nil
		})
	}
	err := g.Wait()
	m.embedDuration += time.Since(embedStart)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}

	// Distribute embeddings back to their owning files.
	for i, emb := range allEmbeddings {
		ref := buf.textMap[i]
		pf := &buf.files[ref.fileIdx]
		if pf.embeddings == nil {
			pf.embeddings = make([][]float32, len(pf.chunks))
		}
		pf.embeddings[ref.chunkIdx] = emb
	}

	// Build store records (chunks with a nil embedding are skipped by the store).
	fileRecords := make([]store.FileWithChunks, 0, len(buf.files))
	for fi := range buf.files {
		pf := &buf.files[fi]
		chunks := make([]store.ChunkRecord, 0, len(pf.chunks))
		for j, c := range pf.chunks {
			var emb []float32
			if pf.embeddings != nil {
				emb = pf.embeddings[j]
			}
			chunks = append(chunks, store.ChunkRecord{
				ChunkIndex: j,
				StartLine:  c.StartLine,
				EndLine:    c.EndLine,
				ChunkType:  c.ChunkType,
				SymbolName: c.SymbolName,
				Content:    c.Content,
				Embedding:  emb,
			})
		}
		fileRecords = append(fileRecords, store.FileWithChunks{
			Path:         pf.path,
			LastModified: pf.mtime,
			FileHash:     pf.fileHash,
			Language:     pf.lang,
			Chunks:       chunks,
		})
	}

	dbStart := time.Now()
	storedFiles, storedChunks, errs := st.StoreFiles(ctx, fileRecords)
	m.dbDuration += time.Since(dbStart)

	m.filesProcessed += storedFiles
	m.totalChunks += storedChunks
	m.errors += len(errs)
	m.errorDetails = append(m.errorDetails, errs...)

	return nil
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
