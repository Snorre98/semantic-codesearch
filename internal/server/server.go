package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/embeddings"
	"semantic-codesearch/internal/indexer"
	"semantic-codesearch/internal/models"
	"semantic-codesearch/internal/store"
)

// sqliteBackend reports whether the configured backend is the per-codebase SQLite one.
func sqliteBackend(cfg config.Config) bool {
	return cfg.Backend == "" || cfg.Backend == "sqlite"
}

// Run starts the MCP server over stdio.
func Run(cfg config.Config) error {
	embedder := embeddings.NewClient(cfg)

	s := server.NewMCPServer(
		"Code Search",
		"1.0.0",
		server.WithInstructions("Use index_codebase to index a project before searching. Use search_code for natural language queries over indexed code. Multiple codebases are supported; pass `codebase` to target one or `all=true` to search every indexed codebase."),
	)

	// index_codebase tool
	s.AddTool(
		mcp.NewTool("index_codebase",
			mcp.WithDescription("Index a codebase directory for semantic search. Walks the directory, respects .gitignore, chunks source files intelligently, generates embeddings via Ollama, and stores them. Supports incremental re-indexing — unchanged files are skipped. With the SQLite backend each codebase gets its own database file."),
			mcp.WithString("directory",
				mcp.Required(),
				mcp.Description("Absolute path to the codebase directory to index."),
			),
		),
		func(toolCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			directory, _ := args["directory"].(string)
			if directory == "" {
				return mcp.NewToolResultError("directory parameter is required"), nil
			}

			st, err := store.Open(toolCtx, cfg, directory)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			defer st.Close()

			if err := store.GuardModel(toolCtx, cfg, st, directory); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}

			onProgress := func(msg string) {
				notification := mcp.NewLoggingMessageNotification(mcp.LoggingLevelInfo, "indexer", msg)
				s.SendLogMessageToClient(toolCtx, notification)
			}

			result, err := indexer.IndexDirectory(toolCtx, directory, cfg, st, embedder, onProgress)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}

			// Record this codebase's metadata so search/all can find it.
			if status, serr := st.Status(toolCtx); serr == nil {
				store.RecordCodebase(toolCtx, cfg, st, directory, status.TotalFiles, status.TotalChunks, time.Now())
			}

			msg := fmt.Sprintf("Indexed %d files (%d unchanged, %d errors), %d chunks, %d batches in %.1fs | walk=%.1fs chunk=%.1fs embed=%.1fs db=%.1fs | model=%s batch=%d",
				result.FilesProcessed, result.FilesSkipped, result.Errors,
				result.TotalChunks, result.TotalBatches, result.Elapsed,
				result.WalkDuration, result.ChunkDuration, result.EmbedDuration, result.DBDuration,
				result.Model, result.BatchSize)
			return mcp.NewToolResultText(msg), nil
		},
	)

	// search_code tool
	s.AddTool(
		mcp.NewTool("search_code",
			mcp.WithDescription("Search indexed code using natural language. Location-first: returns the top matches as file path, line range, symbol, type, score, and a compact snippet preview (~3 lines / 160 chars) — use these to Read the exact lines. Set verbose=true for the full 500-char snippet."),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Natural language description of the code you're looking for."),
			),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of results to return (default 6)."),
			),
			mcp.WithString("codebase",
				mcp.Description("Absolute path of a specific indexed codebase to search (SQLite backend). Defaults to the most recently indexed codebase."),
			),
			mcp.WithBoolean("all",
				mcp.Description("Search across every indexed codebase and merge results (SQLite backend)."),
			),
			mcp.WithBoolean("verbose",
				mcp.Description("When true, return the full 500-char snippet per result instead of the compact ~3-line/160-char preview. Default false."),
			),
		),
		func(toolCtx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			query, _ := args["query"].(string)
			if query == "" {
				return mcp.NewToolResultError("query parameter is required"), nil
			}
			limit := 6
			if l, ok := args["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			codebase, _ := args["codebase"].(string)
			all, _ := args["all"].(bool)
			verbose, _ := args["verbose"].(bool)

			queryEmbedding, err := embedder.EmbedSingle(query)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}

			results, err := search(toolCtx, cfg, queryEmbedding, limit, codebase, all)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}

			out := make([]map[string]any, len(results))
			for i, r := range results {
				var snippet string
				if verbose {
					snippet = r.Snippet
					if len(snippet) > 500 {
						snippet = snippet[:500]
					}
				} else {
					snippet = trimSnippet(r.Snippet, 3, 160)
				}
				out[i] = map[string]any{
					"file":    r.FilePath,
					"lines":   fmt.Sprintf("%d-%d", r.StartLine, r.EndLine),
					"score":   math.Round(r.Score*10000) / 10000,
					"symbol":  r.SymbolName,
					"type":    r.ChunkType,
					"snippet": snippet,
				}
			}
			b, _ := json.Marshal(out)
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	// index_status tool
	s.AddTool(
		mcp.NewTool("index_status",
			mcp.WithDescription("Show the current state of the code search index. Returns total files indexed, total chunks, last index time, and any errors. With the SQLite backend, lists all indexed codebases."),
		),
		func(toolCtx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			out, err := status(toolCtx, cfg)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}
			b, _ := json.Marshal(out)
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	return server.ServeStdio(s)
}

// trimSnippet returns a compact preview of s: at most maxLines lines and maxChars
// chars (whichever comes first), with common leading indentation stripped so the
// preview reads flush-left. A trailing ellipsis marks content that was cut.
func trimSnippet(s string, maxLines, maxChars int) string {
	lines := strings.Split(s, "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}

	// Compute the minimum leading whitespace across non-blank kept lines.
	minIndent := -1
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		indent := len(ln) - len(strings.TrimLeft(ln, " \t"))
		if minIndent == -1 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent > 0 {
		for i, ln := range lines {
			if len(ln) >= minIndent {
				lines[i] = ln[minIndent:]
			} else {
				lines[i] = strings.TrimLeft(ln, " \t")
			}
		}
	}

	out := strings.Join(lines, "\n")

	// Cap to maxChars, counting runes so we never split a multi-byte char.
	if maxChars > 0 {
		runes := []rune(out)
		if len(runes) > maxChars {
			out = string(runes[:maxChars])
			truncated = true
		}
	}

	if truncated {
		out += "…"
	}
	return out
}

// search runs a vector search against one codebase (default/selected) or, when all
// is set, across every registered codebase, merging by score.
func search(ctx context.Context, cfg config.Config, embedding []float32, limit int, codebase string, all bool) ([]models.SearchResult, error) {
	if !sqliteBackend(cfg) {
		st, err := store.Open(ctx, cfg, "")
		if err != nil {
			return nil, err
		}
		defer st.Close()
		return st.Search(ctx, embedding, limit, store.SearchFilters{})
	}

	reg, err := store.LoadRegistry(cfg)
	if err != nil {
		return nil, err
	}

	// Only search codebases embedded with the currently configured model — mixing
	// vector spaces returns garbage. reg.List() already excludes deprecated ones.
	var roots []string
	switch {
	case all:
		for _, e := range reg.List() {
			if ok, _ := store.MatchesConfig(e, cfg); ok {
				roots = append(roots, e.Root)
			}
		}
		if len(roots) == 0 {
			return nil, fmt.Errorf("no indexed codebase matches the configured model %q; re-embed with `rebuild --reembed`", cfg.EmbeddingModel)
		}
	case codebase != "":
		if e, ok := reg.Get(codebase); ok {
			if ok, reason := store.MatchesConfig(e, cfg); !ok {
				return nil, fmt.Errorf("codebase %q %s; re-embed with `rebuild --reembed`", e.Root, reason)
			}
		}
		roots = []string{codebase}
	default:
		if e, ok := mostRecentMatching(reg, cfg); ok {
			roots = []string{e.Root}
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("no indexed codebase found; run index_codebase first")
	}

	var merged []models.SearchResult
	for _, root := range roots {
		st, err := store.Open(ctx, cfg, root)
		if err != nil {
			return nil, err
		}
		res, serr := st.Search(ctx, embedding, limit, store.SearchFilters{})
		st.Close()
		if serr != nil {
			return nil, serr
		}
		merged = append(merged, res...)
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Score > merged[j].Score })
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

// status returns index status: per-codebase for SQLite, a single object for Postgres.
func status(ctx context.Context, cfg config.Config) (any, error) {
	if !sqliteBackend(cfg) {
		st, err := store.Open(ctx, cfg, "")
		if err != nil {
			return nil, err
		}
		defer st.Close()
		s, err := st.Status(ctx)
		if err != nil {
			return nil, err
		}
		return statusMap(s), nil
	}

	reg, err := store.LoadRegistry(cfg)
	if err != nil {
		return nil, err
	}
	entries := reg.List()
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].LastIndexed > entries[j].LastIndexed })
	codebases := make([]map[string]any, 0, len(entries))
	totalFiles, totalChunks := 0, 0
	for _, e := range entries {
		codebases = append(codebases, map[string]any{
			"root":         e.Root,
			"db_file":      e.DBFile,
			"files":        e.Files,
			"chunks":       e.Chunks,
			"last_indexed": e.LastIndexed,
			"model":        e.Model,
		})
		totalFiles += e.Files
		totalChunks += e.Chunks
	}
	return map[string]any{
		"backend":      "sqlite",
		"codebases":    codebases,
		"total_files":  totalFiles,
		"total_chunks": totalChunks,
	}, nil
}

func statusMap(s models.IndexStatus) map[string]any {
	return map[string]any{
		"total_files":     s.TotalFiles,
		"total_chunks":    s.TotalChunks,
		"last_index_time": s.LastIndexTime,
		"last_directory":  s.LastDirectory,
		"errors":          s.LastErrors,
	}
}

// mostRecentMatching returns the most recently indexed active codebase whose
// embedding model matches the current config, skipping mismatched ones.
func mostRecentMatching(reg *store.Registry, cfg config.Config) (store.RegistryEntry, bool) {
	entries := reg.List()
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].LastIndexed > entries[j].LastIndexed })
	for _, e := range entries {
		if ok, _ := store.MatchesConfig(e, cfg); ok {
			return e, true
		}
	}
	return store.RegistryEntry{}, false
}
