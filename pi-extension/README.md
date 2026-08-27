# mcp-code-search Pi Extension

Bridges [mcp-code-search](https://github.com/your-org/semantic-codesearch) into Pi as native Pi tools, no MCP bridge required.

## Quick Start

```bash
# 1. Build the binary
cd semantic-codesearch
go build -o mcp-code-search .

# 2. Load the extension
pi -e ./pi-extension/index.ts
```

Or install persistently:

```bash
cp pi-extension/index.ts ~/.pi/agent/extensions/codesearch.ts
```

Then just run `pi` — the tools are available automatically.

## Tools Added

| Tool | Description |
|------|-------------|
| `index_codebase(directory)` | Index a codebase for semantic search |
| `search_code(query, limit?, codebase?, all?)` | Natural language code search |
| `index_status()` | List all indexed codebases |

## Configuration

Set `MCP_CS_BINARY` env var if the binary is not co-located or on PATH:

```bash
export MCP_CS_BINARY=/custom/path/mcp-code-search
```

Standard `MCP_CS_*` env vars from mcp-code-search also apply (e.g., `MCP_CS_EMBED_MODEL`, `MCP_CS_OLLAMA_URL`).

## How It Works

The extension spawns `mcp-code-search serve` as a subprocess, discovers its tools via JSON-RPC `tools/list`, and registers each MCP tool as a native Pi tool. No MCP SDK, no HTTP server, no bridge — just direct subprocess communication.

## Install as a Pi Package

```bash
pi install git:github.com/your-org/semantic-codesearch
pi config   # enable the extension
```
