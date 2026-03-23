#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "==> Building MCP server Docker image..."
docker compose -f "$PROJECT_DIR/docker-compose.yml" build mcp-server

echo ""
echo "============================================"
echo "  Rebuild complete!"
echo ""
echo "  Restart your Claude Code session to pick"
echo "  up the new image, then re-index:"
echo ""
echo "    index_codebase(\"/path/to/project\")"
echo "============================================"
