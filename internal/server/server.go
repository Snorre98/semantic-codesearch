package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/db"
	"semantic-codesearch/internal/embeddings"
	"semantic-codesearch/internal/indexer"
)

// Run starts the MCP server over stdio.
func Run(cfg config.Config) error {
	ctx := context.Background()

	pool, err := db.NewPool(ctx, cfg)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer pool.Close()

	embedder := embeddings.NewClient(cfg)

	s := server.NewMCPServer(
		"Code Search",
		"1.0.0",
		server.WithInstructions("Use index_codebase to index a project before searching. Use search_code for natural language queries over indexed code."),
	)

	// index_codebase tool
	s.AddTool(
		mcp.NewTool("index_codebase",
			mcp.WithDescription("Index a codebase directory for semantic search. Walks the directory, respects .gitignore, chunks source files intelligently, generates embeddings via Ollama, and stores them in pgvector. Supports incremental re-indexing — unchanged files are skipped."),
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

			onProgress := func(msg string) {
				notification := mcp.NewLoggingMessageNotification(mcp.LoggingLevelInfo, "indexer", msg)
				s.SendLogMessageToClient(toolCtx, notification)
			}

			result, err := indexer.IndexDirectory(toolCtx, directory, cfg, pool, embedder, onProgress)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}

			msg := fmt.Sprintf("Indexed %d files (%d unchanged, %d errors) in %.1fs",
				result.FilesProcessed, result.FilesSkipped, result.Errors, result.Elapsed)
			return mcp.NewToolResultText(msg), nil
		},
	)

	// search_code tool
	s.AddTool(
		mcp.NewTool("search_code",
			mcp.WithDescription("Search indexed code using natural language. Returns the top matching code snippets with file path, line range, and relevance score."),
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Natural language description of the code you're looking for."),
			),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of results to return (default 10)."),
			),
		),
		func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			args := request.GetArguments()
			query, _ := args["query"].(string)
			if query == "" {
				return mcp.NewToolResultError("query parameter is required"), nil
			}

			limit := 10
			if l, ok := args["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}

			queryEmbedding, err := embedder.EmbedSingle(query)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}

			results, err := db.SearchSimilar(ctx, pool, queryEmbedding, limit)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}

			out := make([]map[string]any, len(results))
			for i, r := range results {
				snippet := r.Snippet
				if len(snippet) > 500 {
					snippet = snippet[:500]
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
			mcp.WithDescription("Show the current state of the code search index. Returns total files indexed, total chunks, last index time, and any errors."),
		),
		func(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			status, err := db.GetIndexStatus(ctx, pool)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
			}

			out := map[string]any{
				"total_files":     status.TotalFiles,
				"total_chunks":    status.TotalChunks,
				"last_index_time": status.LastIndexTime,
				"last_directory":  status.LastDirectory,
				"errors":          status.LastErrors,
			}

			b, _ := json.Marshal(out)
			return mcp.NewToolResultText(string(b)), nil
		},
	)

	return server.ServeStdio(s)
}
