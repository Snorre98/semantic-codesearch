# mcp-code-search

MCP server that gives Claude Code and Codex semantic code search over any codebase, running 100% locally.

**Default stack:** SQLite + [sqlite-vec](https://github.com/asg017/sqlite-vec) (embedded, no server) | Ollama | Go (mcp-go)
**Optional backend:** Postgres + pgvector (Docker)

By default the tool is a self-contained native binary: embeddings are stored in a plain SQLite file per codebase under `~/.codesearch/`. No Docker, no database server. Postgres + pgvector remains available as a selectable backend.

## Quick Start (native, SQLite)

### Prerequisites

- Go 1.25+ and a C compiler (for the tree-sitter chunker — see [Build requirements](#build-requirements))
- [Ollama](https://ollama.com) running locally with an embedding model

```bash
brew install ollama        # or see ollama.com
ollama serve               # leave running
ollama pull nomic-embed-text   # 768-dim embedding model (or embeddinggemma)
```

### Build & register

```bash
git clone <this-repo>
cd semantic-codesearch
go build -o mcp-code-search .

# Print the exact registration command + config snippet for this binary:
./mcp-code-search mcp
# ...or register it directly:
./mcp-code-search mcp install

# For Codex:
./mcp-code-search mcp --client codex
./mcp-code-search mcp install --client codex
```

That's it. The server creates `~/.codesearch/` on first index. No containers or ports.

Run `./mcp-code-search doctor` to verify Ollama is reachable, the model is pulled, and its
output dimension matches the schema before you start indexing.

## Tools

### `index_codebase(directory)`

Index a codebase for semantic search. Walks the directory, respects `.gitignore`, chunks files using tree-sitter AST parsing for accurate function/class/method extraction across 14+ languages, and stores embeddings. Each codebase gets **its own SQLite database file** (`~/.codesearch/<name>-<hash>.db`).

Supports **incremental re-indexing** — unchanged files (by mtime) are skipped automatically.

#### Excluding files from search (`.codesearchignore`)

Indexing respects `.gitignore`, skips binary/oversized files, and also honors an optional
**`.codesearchignore`** file (gitignore syntax) at the repo root or any subdirectory. Use it
to keep noisy or stale content out of semantic search without touching Git — for example, to
exclude markdown docs:

```gitignore
# .codesearchignore
*.md            # skip all markdown
!ARCHITECTURE.md   # ...but keep this one
docs/legacy/    # skip a whole stale directory
```

Rules apply to **future** indexing. To drop chunks for files an updated rule now excludes,
re-index: `mcp-code-search rebuild <root>`.

### `search_code(query, limit=10, codebase?, all?)`

Natural language search over indexed code. Returns top matches with file path, line range, relevance score, and code snippet.

- Defaults to the **most recently indexed** codebase.
- `codebase` — absolute path of a specific indexed codebase to search.
- `all=true` — search across **every** indexed codebase and merge results by score.

### `index_status()`

Lists all indexed codebases with file/chunk counts and last-index time.

## Multiple codebases

Each codebase is isolated in its own DB file, tracked in `~/.codesearch/registry.json`:

```
> index_codebase("/Users/me/projects/api")
> index_codebase("/Users/me/projects/web")
> search_code("vector similarity search")              # most recent (web)
> search_code("auth middleware", codebase="/Users/me/projects/api")
> search_code("retry logic", all=true)                  # both, merged by score
```

To forget a codebase, use `mcp-code-search remove <root>` (see the CLI reference below) — do
not hand-edit `registry.json`.

## Command reference (CLI)

The same binary is both the MCP server and a management CLI. Run `mcp-code-search help` for
the full list. Commands take `--json` where machine-readable output is useful, and prompt
before destructive actions (`--force` to skip).

| Command | What it does |
|---|---|
| `serve` | Run the MCP stdio server (default when no command). |
| `index <dir> [--reembed]` | Index a codebase. Refuses if it was indexed with a different model unless `--reembed`. |
| `list [--all] [--json]` | List indexed codebases (root, size on disk, files/chunks, model, **matches?**, last indexed, status). `--all` includes deprecated. |
| `info <root> [--json]` | Detailed stats for one codebase. |
| `remove <root> [--force]` | Deprecate a codebase (alias `forget`). **Keeps the data**; reclaim it later with `prune --purge`. |
| `rebuild <root> [--reembed] [--force]` | Re-index. Plain = drop + full re-index in place; `--reembed` archives the old index and re-embeds with the current model. |
| `prune [--purge] [--force] [--json]` | Deprecate stale codebases (missing root or DB). `--purge` permanently deletes all deprecated data. |
| `doctor [--json]` | Health check: Ollama reachable, model pulled, output dimension == 768, registry health. Alias `check`. |
| `pull <model>` | Pull an embedding model into Ollama and verify its dimension. |
| `mcp [--client codex] [--docker]` / `mcp install [--client codex] [--docker]` | Print (or run) the registration command for Claude or Codex, plus the matching `.mcp.json` or `config.toml` snippet, for native or Docker/Postgres mode. |

### Managing & deprecating codebases

Codebases move through a simple lifecycle so embeddings are never silently destroyed:

```
active ──remove / model switch / prune──► deprecated ──prune --purge──► gone
```

- **active** — searchable, listed by default.
- **deprecated** — excluded from search and from `list` (use `list --all` to see them). The
  data is retained: for SQLite the `.db` is **archived** (renamed to
  `<name>-deprecated-<model>-<ts>.db`), for Postgres the rows are flagged.
- **purge** — `prune --purge` deletes deprecated data for good.

### Switching embedding models

A codebase embedded with model A **cannot** be searched with model B — the vectors live in a
different space. The registry records each codebase's model and dimension, and the tool
refuses to mix them:

```bash
# After changing MCP_CS_EMBED_MODEL, a plain re-index is refused:
mcp-code-search index /path/to/repo
#   error: codebase "…" indexed with model "nomic-embed-text" but configured model is
#   "embeddinggemma"; re-embed with `rebuild --reembed` to switch models

# Opt in explicitly — the old index is archived as deprecated, then re-embedded:
mcp-code-search rebuild /path/to/repo --reembed
```

`serve` applies the same rule: searches skip (or refuse) codebases whose model no longer
matches the configured one.

## Configuration

All via environment variables (defaults work out of the box):

| Variable | Default | Description |
|---|---|---|
| `MCP_CS_BACKEND` | `sqlite` | Storage backend: `sqlite` or `postgres` |
| `MCP_CS_SQLITE_DIR` | `~/.codesearch` | Directory holding per-codebase DB files |
| `MCP_CS_OLLAMA_URL` | `http://localhost:11434` | Ollama API URL |
| `MCP_CS_EMBED_MODEL` | `nomic-embed-text` | Embedding model (must output 768 dims) |
| `MCP_CS_EMBED_CONCURRENCY` | `4` | Embedding sub-batches embedded in parallel |
| `MCP_CS_MAX_FILE_KB` | `512` | Max file size to index (KB) |
| `MCP_CS_BATCH_SIZE` | `50` | Embedding batch size |
| `MCP_CS_PG_HOST` | `localhost` | Postgres host (postgres backend) |
| `MCP_CS_PG_PORT` | `5433` | Postgres port (postgres backend) |
| `MCP_CS_PG_DATABASE` | `codesearch` | Database name (postgres backend) |
| `MCP_CS_PG_USER` | `codesearch` | Database user (postgres backend) |
| `MCP_CS_PG_PASSWORD` | `codesearch` | Database password (postgres backend) |

> The embedding dimension is fixed at **768** (`config.EmbeddingDimensions`). Use a 768-dim model such as `nomic-embed-text` or `embeddinggemma`.

## Architecture

```
┌─────────────┐     stdio/JSON-RPC     ┌──────────────┐
│ Claude/Codex │◄──────────────────────►│  MCP Server  │
└──────────────┘                        └──────┬───────┘
                                               │
                                    ┌──────────┼──────────┐
                                    ▼                     ▼
                         ┌────────────────────┐   ┌─────────────┐
                         │ SQLite + sqlite-vec │   │   Ollama     │
                         │  (one .db per repo) │   │  (embeddings)│
                         └────────────────────┘   └─────────────┘
```

Indexing pipeline: walk → tree-sitter chunk → **parallel** embed (Ollama) → batched write (one transaction per flush under WAL).

## Manual indexing (CLI)

```bash
go run . index /path/to/codebase
# or
./mcp-code-search index /path/to/codebase
```

### Indexing speed

The bottleneck is Ollama embedding (now parallelized via `MCP_CS_EMBED_CONCURRENCY`); SQLite writes are effectively free (local, WAL).

| Codebase size | Files | Chunks (est.) | Time (est.) |
|---|---|---|---|
| Small project | ~30 | ~160 | 5-10s |
| Typical app | ~200 | ~1,500 | 30-90s |
| Large repo | ~2,000 | ~15,000 | 5-15 min |

Re-indexing after small changes takes seconds regardless of total codebase size.

## Optional: Postgres + pgvector backend (Docker)

The original containerized backend is still available. Set `MCP_CS_BACKEND=postgres` and bring up the stack:

```bash
docker compose --profile ollama up -d     # Postgres (+ optional Ollama)
MCP_CS_BACKEND=postgres go run . index /path/to/codebase
```

In this mode all codebases share one database (path-namespaced) and `MCP_CS_SQLITE_DIR`/`codebase`/`all` are ignored. The Docker registration commands and GPU notes from prior versions still apply; the container passes `MCP_CS_PG_HOST=postgres`/`MCP_CS_PG_PORT=5432`. Reset with `docker compose --profile ollama down -v`.

The management CLI (`list`/`info`/`remove`/`rebuild`/`prune`) works here too: per-codebase
metadata lives in a `codebases` table (created automatically on connect, also in
`docker/init.sql`). Deprecation flags rows and excludes them from search until `prune --purge`
deletes them. Generate the registration with `mcp-code-search mcp --docker`.

## Development

```bash
go build -o mcp-code-search .
go test ./...
```

### Build requirements

The chunker uses [tree-sitter](https://tree-sitter.github.io/) via CGo for AST-based parsing, which requires a C compiler (gcc/clang) at build time — included with Xcode Command Line Tools on macOS, `build-essential` on Linux. The SQLite + sqlite-vec store itself is pure Go (`modernc.org/sqlite` + `modernc.org/sqlite/vec`), so no SQLite system library or extension file is needed.

### Supported languages (tree-sitter)

Go, Python, JavaScript, TypeScript, Rust, Java, C, C++, Ruby, PHP, C#, Swift, Kotlin, Scala, Shell. Unsupported file types fall back to fixed-size chunking.
