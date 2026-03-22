# mcp-code-search

MCP server that gives Claude Code semantic code search over any codebase, running 100% locally.

**Stack:** Postgres + pgvector | Ollama (nomic-embed-text) | Go (mcp-go)

## Quick Start

### Prerequisites

- Docker

### Setup

```bash
git clone <this-repo>
cd semantic-codesearch
./scripts/setup.sh
```

This will:
1. Build the MCP server Docker image
2. Start Postgres + pgvector (and Ollama in Docker if no host Ollama is detected)
3. Pull the `nomic-embed-text` embedding model

### Register in Claude Code

```bash
claude mcp add code-search -- \
  docker run -i --rm \
  --network codesearch \
  -e MCP_CS_PG_HOST=postgres \
  -e MCP_CS_PG_PORT=5432 \
  -v /path/to/project:/path/to/project:ro \
  mcp-code-search:latest
```

Mount only the directories you want to index. To index multiple projects, add multiple `-v` flags:

```bash
claude mcp add code-search -- \
  docker run -i --rm \
  --network codesearch \
  -e MCP_CS_PG_HOST=postgres \
  -e MCP_CS_PG_PORT=5432 \
  -v /path/to/project-a:/path/to/project-a:ro \
  -v /path/to/project-b:/path/to/project-b:ro \
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
        "-e", "MCP_CS_PG_HOST=postgres",
        "-e", "MCP_CS_PG_PORT=5432",
        "-v", "/path/to/project:/path/to/project:ro",
        "mcp-code-search:latest"
      ]
    }
  }
}
```

## Performance: GPU acceleration

By default, `setup.sh` runs Ollama in Docker (CPU only on macOS). For faster indexing on Apple Silicon, install Ollama on the host so it can use the Metal GPU:

```bash
brew install ollama
ollama serve   # leave running in a separate terminal
./scripts/setup.sh   # auto-detects host Ollama and skips the Docker container
```

The setup script prints the correct `claude mcp add` command for whichever mode it detects. To manually override an existing registration to use host Ollama:

```bash
claude mcp add code-search -- \
  docker run -i --rm \
  --network codesearch \
  --add-host=host.docker.internal:host-gateway \
  -e MCP_CS_PG_HOST=postgres \
  -e MCP_CS_PG_PORT=5432 \
  -e MCP_CS_OLLAMA_URL=http://host.docker.internal:11434 \
  -v /path/to/project:/path/to/project:ro \
  mcp-code-search:latest
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
| `MCP_CS_OLLAMA_URL` | `http://ollama:11434` | Ollama API URL |
| `MCP_CS_EMBED_MODEL` | `nomic-embed-text` | Embedding model |
| `MCP_CS_MAX_FILE_KB` | `512` | Max file size to index (KB) |
| `MCP_CS_BATCH_SIZE` | `50` | Embedding batch size |

When running in Docker, the setup overrides `MCP_CS_PG_HOST=postgres` and `MCP_CS_PG_PORT=5432` so the container can reach Postgres via the Docker network. By default Ollama runs in Docker Compose on the same network, so the default `MCP_CS_OLLAMA_URL` works without overrides. If using a host-installed Ollama, pass `MCP_CS_OLLAMA_URL=http://host.docker.internal:11434`.

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
> index_codebase("/Users/snorresaether/Documents/Jobb/Fikse2")
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
> index_codebase("/Users/snorresaether/Documents/Jobb/Fikse2")
Indexed 3 files (211 unchanged, 0 errors) in 4.1s
```

### Check index state

```
> index_status()
```

### Multiple codebases

All indexed codebases share the same database. File paths in results tell you which project a match is from:

```
> index_codebase("/Users/snorresaether/Documents/Jobb/Fikse2")
> index_codebase("/Users/snorresaether/Documents/Liv/Projects/semantic-codesearch")
> search_code("vector similarity search")  # searches across both
```

### Reset everything

```bash
docker compose --profile ollama down -v   # wipes all indexed data
docker compose --profile ollama up -d     # fresh start
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
  -e MCP_CS_PG_HOST=postgres \
  -e MCP_CS_PG_PORT=5432 \
  -v /path/to/codebase:/path/to/codebase:ro \
  mcp-code-search:latest index /path/to/codebase
```

## Development

```bash
# Start infrastructure (include --profile ollama for Docker Ollama)
docker compose --profile ollama up -d

# Build locally (requires Go 1.22+ and a C compiler for tree-sitter)
go build -o mcp-code-search .

# Run tests
go test ./...
```

### Build requirements

The chunker uses [tree-sitter](https://tree-sitter.github.io/) via CGo for AST-based code parsing. This requires a C compiler (gcc/clang) at build time. On macOS this is included with Xcode Command Line Tools; on Linux install `build-essential` or equivalent. The Docker build handles this automatically.

### Supported languages (tree-sitter)

Go, Python, JavaScript, TypeScript, Rust, Java, C, C++, Ruby, PHP, C#, Swift, Kotlin, Scala, Shell. Unsupported file types fall back to fixed-size chunking.