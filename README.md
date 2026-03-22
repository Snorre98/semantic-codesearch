# mcp-code-search

MCP server that gives Claude Code semantic code search over any codebase, running 100% locally.

**Stack:** Postgres + pgvector | Ollama (nomic-embed-text) | Python (FastMCP)

## Quick Start

### Prerequisites

- Docker
- Python 3.10+
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
3. Install the Python package in editable mode

### Register in Claude Code

```bash
claude mcp add code-search -e PYTHONUNBUFFERED=1 -- mcp-code-search
```

Or add to your project's `.mcp.json`:

```json
{
  "mcpServers": {
    "code-search": {
      "command": "mcp-code-search",
      "env": {
        "PYTHONUNBUFFERED": "1"
      }
    }
  }
}
```

## Tools

### `index_codebase(directory)`

Index a codebase for semantic search. Walks the directory, respects `.gitignore`, chunks files intelligently (AST for Python, heuristics for other languages, fixed-size fallback), and stores embeddings in pgvector.

Supports **incremental re-indexing** — unchanged files are skipped automatically.

### `search_code(query, limit=10)`

Natural language search over indexed code. Returns top matches with file path, line range, relevance score, and code snippet.

### `index_status()`

Shows total files/chunks indexed, last index time, and any errors.

## Configuration

All via environment variables (defaults work out of the box):

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

## Development

```bash
# Start infrastructure
docker compose up -d

# Install in editable mode with dev deps
pip install -e ".[dev]"

# Run tests
pytest
```
