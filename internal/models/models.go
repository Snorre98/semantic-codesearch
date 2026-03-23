package models

// CodeChunk represents a chunk of source code extracted from a file.
type CodeChunk struct {
	Content    string
	StartLine  int
	EndLine    int
	ChunkType  string // "function", "class", "block", "method"
	SymbolName string // empty when none
}

// SearchResult represents a code search result with relevance score.
type SearchResult struct {
	FilePath   string
	StartLine  int
	EndLine    int
	Snippet    string
	SymbolName string
	ChunkType  string
	Score      float64
}

// IndexResult summarizes an indexing run.
type IndexResult struct {
	FilesProcessed int
	FilesSkipped   int
	Errors         int
	ErrorDetails   []ErrorDetail
	Elapsed        float64
	TotalChunks    int
	TotalBatches   int
	WalkDuration   float64
	ChunkDuration  float64
	EmbedDuration  float64
	DBDuration     float64
	Model          string
	BatchSize      int
}

// ErrorDetail records a per-file indexing error.
type ErrorDetail struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

// IndexStatus reports the current state of the search index.
type IndexStatus struct {
	TotalFiles    int
	TotalChunks   int
	LastIndexTime *string
	LastDirectory *string
	LastErrors    []map[string]any
}
