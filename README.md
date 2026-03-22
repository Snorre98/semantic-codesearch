# mcp-code-search

MCP server that gives Claude Code semantic code search over any codebase, running 100% locally.

**Stack:** Postgres + pgvector | Ollama (nomic-embed-text) | Go (mcp-go)

## Quick Start

### Prerequisites

- Docker
- Homebrew (for Ollama install)

### Setup

```bash
git clone <this-repo>
cd semantic-codesearch
./scripts/setup.sh
```

This will:
1. Start Postgres + pgvector via Docker
2. Install Ollama and pull `nomic-embed-text`
3. Build the MCP server Docker image

### Register in Claude Code

```bash
claude mcp add code-search -- \
  docker run -i --rm \
  --network codesearch \
  --add-host=host.docker.internal:host-gateway \
  -e MCP_CS_PG_HOST=postgres \
  -e MCP_CS_PG_PORT=5432 \
  -e MCP_CS_OLLAMA_URL=http://host.docker.internal:11434 \
  -v "$HOME:$HOME:ro" \
  mcp-code-search:latest
```

Or add to your project's `.mcp.json`:

```json
{
  "mcpServers": {
    "code-search": {
      "command": "docker",
      "args": [
        "run", "-i", "--rm",
        "--network", "codesearch",
        "--add-host=host.docker.internal:host-gateway",
        "-e", "MCP_CS_PG_HOST=postgres",
        "-e", "MCP_CS_PG_PORT=5432",
        "-e", "MCP_CS_OLLAMA_URL=http://host.docker.internal:11434",
        "-v", "/Users/USERNAME:/Users/USERNAME:ro",
        "mcp-code-search:latest"
      ]
    }
  }
}
```

## Tools

### `index_codebase(directory)`

Index a codebase for semantic search. Walks the directory, respects `.gitignore`, chunks files using tree-sitter AST parsing for accurate function/class/method extraction across 14+ languages, and stores embeddings in pgvector.

Supports **incremental re-indexing** — unchanged files are skipped automatically.

### `search_code(query, limit=10)`

Natural language search over indexed code. Returns top matches with file path, line range, relevance score, and code snippet.

### `index_status()`

Shows total files/chunks indexed, last index time, and any errors.

## Configuration

All via environment variables (defaults work out of the box for local dev):

| Variable | Default | Description |
|---|---|---|
| `MCP_CS_PG_HOST` | `localhost` | Postgres host |
| `MCP_CS_PG_PORT` | `5433` | Postgres port |
| `MCP_CS_PG_DATABASE` | `codesearch` | Database name |
| `MCP_CS_PG_USER` | `codesearch` | Database user |
| `MCP_CS_PG_PASSWORD` | `codesearch` | Database password |
| `MCP_CS_OLLAMA_URL` | `http://localhost:11434` | Ollama API URL |
| `MCP_CS_EMBED_MODEL` | `nomic-embed-text` | Embedding model |
| `MCP_CS_MAX_FILE_KB` | `512` | Max file size to index (KB) |
| `MCP_CS_BATCH_SIZE` | `50` | Embedding batch size |

When running in Docker, the setup overrides `MCP_CS_PG_HOST=postgres`, `MCP_CS_PG_PORT=5432`, and `MCP_CS_OLLAMA_URL=http://host.docker.internal:11434` so the container can reach Postgres via the Docker network and Ollama on the host.

## Architecture

```
┌─────────────┐     stdio/JSON-RPC     ┌──────────────┐
│ Claude Code  │◄──────────────────────►│  MCP Server  │
└─────────────┘                         └──────┬───────┘
                                               │
                                    ┌──────────┼──────────┐
                                    ▼                     ▼
                            ┌──────────────┐    ┌─────────────┐
                            │   Postgres   │    │   Ollama     │
                            │  + pgvector  │    │  (embeddings)│
                            └──────────────┘    └─────────────┘
```

## Usage

### Index a codebase

```
> index_codebase("/Users/USERNAME/Projects/example-repo")
Indexed 214 files (0 unchanged, 2 errors) in 98.3s
```

### Search with natural language

```
> search_code("where is the authentication middleware")
> search_code("database migration logic")
> search_code("error handling in API routes", 5)
```

### Re-index after changes

Only changed files are re-processed — re-indexing is fast:

```
> index_codebase("/Users/USERNAME/Projects/example-repo")
Indexed 3 files (211 unchanged, 0 errors) in 4.1s
```

### Check index state

```
> index_status()
```

### Multiple codebases

All indexed codebases share the same database. File paths in results tell you which project a match is from:

```
> index_codebase("/Users/USERNAME/Projects/example-repo")
> index_codebase("/Users/USERNAME/Projects/semantic-codesearch")
> search_code("vector similarity search")  # searches across both
```

### Reset everything

```bash
docker compose down -v   # wipes all indexed data
docker compose up -d     # fresh start
```

### Indexing speed estimates

The bottleneck is Ollama embedding. With `nomic-embed-text` on Apple Silicon:

| Codebase size | Files | Chunks (est.) | Time (est.) |
|---|---|---|---|
| Small project | ~15 | ~50 | 5-10s |
| Typical app | ~200 | ~1,500 | 1-3 min |
| Large repo | ~2,000 | ~15,000 | 10-20 min |
| Monorepo (10k+) | ~10,000 | ~60,000 | 45-90 min |

Re-indexing after small changes takes seconds regardless of total codebase size.

## Manual Indexing

You can index codebases directly from the command line without going through the MCP server.

### Local dev

```bash
go run . index /path/to/codebase
```

### Docker

```bash
docker run --rm \
  --network codesearch \
  --add-host=host.docker.internal:host-gateway \
  -e MCP_CS_PG_HOST=postgres \
  -e MCP_CS_PG_PORT=5432 \
  -e MCP_CS_OLLAMA_URL=http://host.docker.internal:11434 \
  -v "$HOME:$HOME:ro" \
  mcp-code-search:latest index /path/to/codebase
```

## Development

```bash
# Start infrastructure
docker compose up -d

# Build locally (requires Go 1.22+ and a C compiler for tree-sitter)
go build -o mcp-code-search .

# Run tests
go test ./...
```

### Build requirements

The chunker uses [tree-sitter](https://tree-sitter.github.io/) via CGo for AST-based code parsing. This requires a C compiler (gcc/clang) at build time. On macOS this is included with Xcode Command Line Tools; on Linux install `build-essential` or equivalent. The Docker build handles this automatically.

### Supported languages (tree-sitter)

Go, Python, JavaScript, TypeScript, Rust, Java, C, C++, Ruby, PHP, C#, Swift, Kotlin, Scala, Shell. Unsupported file types fall back to fixed-size chunking.
