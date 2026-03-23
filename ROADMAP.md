# Roadmap

## GitHub App — Autonomous PR Agent

A GitHub App that indexes a repo, understands the codebase semantically, and then when an issue comes in (or a user triggers it), an agent uses that understanding to write a PR — with real context about how the codebase works, not just grep-level pattern matching.

The semantic search becomes the agent's memory of the codebase. That's the missing piece in most autonomous coding agents right now — they either dump the whole repo into context (expensive, doesn't scale) or do naive file search (misses relationships).

### What we already have

- Tree-sitter chunking that understands code structure
- Embedding + vector search for "find relevant code"
- Incremental indexing so the index stays fresh

### What we'd need to add

- GitHub App webhook handler (issue opened, comment triggers)
- Agent orchestration layer (plan changes, search for context, write code, create PR)
- Git operations (branch, commit, push from the agent)
- LLM integration for the actual code generation (Claude API)
- Webhook for push events to keep the index updated automatically

### Why this is commercially interesting

- It's not just a search tool anymore — it's an autonomous developer
- GitHub Apps have built-in distribution (marketplace)
- Per-repo or per-org subscription model
- The index is the moat — your agent has deeper codebase understanding than competitors doing naive RAG

### Risk

This is exactly what Copilot Workspace, Devin, Factory, CodeGen, and a dozen well-funded startups are building. The question is whether you can find a niche (specific languages, specific workflows, small teams underserved by enterprise tools) or move faster on the MCP/Claude integration angle that nobody else has.

---

Future extensions for mcp-code-search, roughly ordered by impact and complexity.

## ~~Tree-sitter Chunking~~ (Done)

Implemented. Uses `github.com/smacker/go-tree-sitter` for AST-based chunking across 15 languages.

## SQLite as Default Storage

Postgres+pgvector is overkill for a single-user tool on one machine. Replace with SQLite as the default storage backend — zero setup, no server process, the index is just a file.

**Why:**
- Postgres is the heaviest dependency in the stack — heavier than Ollama. Removing it eliminates Docker as a requirement for most users.
- SQLite ships inside the binary. No connection strings, no `docker compose up`, no port conflicts.
- Single file backup — copy `.codesearch/index.db`, done.
- Read-heavy workload (search) is where SQLite shines.

**Vector search options:**
- `sqlite-vec` — lightweight C extension for vector similarity. No Faiss dependency, easy to embed in a Go binary via CGo.
- `sqlite-vss` — uses Faiss under the hood. More mature, heavier.
- Brute-force scan — for a single repo (<100K chunks), computing cosine similarity across all vectors in memory takes milliseconds. Might not even need a vector index.

**Per-project index:**
- Store the index at `.codesearch/index.db` inside the project directory — same pattern as `.git/`.
- The tool finds it by walking up from the current directory looking for `.codesearch/index.db`. No config, no paths to pass.
- Add `.codesearch/` to `.gitignore` by default since vectors are model-specific. Teams that standardize on the same embedding model could commit it for instant search on clone.
- Solves multi-project scoping for free — each project has its own isolated index. No `project` column needed.
- Deleting a project deletes its index. No orphaned data in a shared database.
- Eliminates the Docker volume mount problem entirely — the binary reads the project directory directly.

**Migration path:**
- Abstract storage behind an interface in `internal/db/` — `Store` interface with `UpsertFile`, `InsertChunks`, `Search`, etc.
- Default to SQLite for local use.
- Keep Postgres as an option for team/server deployments (e.g. the GitHub App) via a config flag.
- This is the single biggest step toward "download binary, run, done."

## Search Filters

Add optional parameters to `search_code`:

- `language` — filter by file language (e.g., `"go"`, `"typescript"`)
- `path` — filter by file path glob (e.g., `"internal/**"`)
- `type` — filter by symbol type (`"function"`, `"class"`, `"method"`)

Requires adding `WHERE` clauses to the similarity query and indexing the `language`/`chunk_type` columns.

## ~~Multi-Project Support~~ (Solved by SQLite per-project index)

Per-project `.codesearch/index.db` gives natural isolation — no `project` column needed. Each repo has its own index. For the GitHub App (shared Postgres), a `project` column would still be needed.

## Auto-Reindex

Use `fsnotify` to watch indexed directories for file changes and reindex in the background. The MCP server would run a watcher goroutine alongside the stdio handler. Changed files get re-chunked and re-embedded automatically.

## Hybrid Search

Combine pgvector cosine similarity with PostgreSQL `tsvector` full-text search. Keyword matches boost semantic results — useful when you know an exact function name but want related code too. Requires adding a `tsvector` column to `code_chunks` and a combined ranking query.

## `find_similar` Tool

New MCP tool: given a file path and line range, find code similar to that snippet. Looks up the existing chunk's embedding and searches against it. No Ollama call needed — pure vector similarity from what's already indexed.

## Cross-File Context in Chunks

Each chunk is currently isolated — a function chunk doesn't know what imports it uses or what types its parameters are. Adding import statements and type definitions as context to the embedding would improve queries like "where is the database connection used." At embedding time, prepend the file's import block and relevant type definitions to each chunk's text.

## Inter-Chunk Relationships

If `funcA` calls `funcB`, we don't capture that. Storing a simple `calls`/`called_by` relationship (even just by string-matching symbol names across chunks) would enable "what calls this function" queries. Requires a new `chunk_references` table or a JSONB column on `code_chunks`.

## Comment Association

File-level and package-level comment blocks at the top of a file don't get associated with the functions below them. These architectural docs end up as orphan "block" chunks. Tree-sitter already captures comment nodes — attach leading doc comments to the nearest top-level symbol, and prepend file-header comments to all chunks in that file.

## Re-Ranking

Return more candidates from pgvector (e.g. top 50), then re-rank with a cross-encoder or a cheap LLM call. The embedding model does fast approximate matching; re-ranking does precise relevance scoring. Could use a small local model or even call the same Ollama instance with a ranking prompt.

## File-Level Summaries

Generate a one-sentence summary of each file and store it alongside the file record. Include the summary in every chunk's embedding context. This way a function in `auth/middleware.go` carries "this file handles HTTP authentication middleware" as background signal, improving relevance for broad queries.

## Scope / Hierarchy Context

A method inside a class should carry the class name in its embedding text. We partially do this via `SymbolName`, but the embedded text doesn't include "this is a method of class X that implements interface Y." Enrich `FormatChunkForEmbedding` to include parent scope, implemented interfaces, and struct/class membership where tree-sitter provides it.

## Web UI

Add an HTTP transport alongside stdio so the search can be used from a browser. A small embedded web server with a search box, results list with syntax highlighting, and links to open files in the editor. Could run on a local port (e.g., `localhost:9876`) when started with a `--http` flag.

