# Roadmap

Future extensions for mcp-code-search, roughly ordered by impact and complexity.

## ~~Tree-sitter Chunking~~ (Done)

Implemented. Uses `github.com/smacker/go-tree-sitter` for AST-based chunking across 15 languages.

## Search Filters

Add optional parameters to `search_code`:

- `language` — filter by file language (e.g., `"go"`, `"typescript"`)
- `path` — filter by file path glob (e.g., `"internal/**"`)
- `type` — filter by symbol type (`"function"`, `"class"`, `"method"`)

Requires adding `WHERE` clauses to the similarity query and indexing the `language`/`chunk_type` columns.

## Multi-Project Support

Add a `project` column to `indexed_files` to scope searches to a specific codebase. Enables indexing multiple repos without cross-contamination in results. `index_codebase` would accept an optional project name, `search_code` would accept an optional project filter.

## Auto-Reindex

Use `fsnotify` to watch indexed directories for file changes and reindex in the background. The MCP server would run a watcher goroutine alongside the stdio handler. Changed files get re-chunked and re-embedded automatically.

## Hybrid Search

Combine pgvector cosine similarity with PostgreSQL `tsvector` full-text search. Keyword matches boost semantic results — useful when you know an exact function name but want related code too. Requires adding a `tsvector` column to `code_chunks` and a combined ranking query.

## `find_similar` Tool

New MCP tool: given a file path and line range, find code similar to that snippet. Looks up the existing chunk's embedding and searches against it. No Ollama call needed — pure vector similarity from what's already indexed.

## Web UI

Add an HTTP transport alongside stdio so the search can be used from a browser. A small embedded web server with a search box, results list with syntax highlighting, and links to open files in the editor. Could run on a local port (e.g., `localhost:9876`) when started with a `--http` flag.

