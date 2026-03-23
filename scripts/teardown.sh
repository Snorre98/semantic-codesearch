#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "==> Stopping all containers and removing volumes..."
docker compose -f "$PROJECT_DIR/docker-compose.yml" --profile ollama down -v

echo "==> Removing MCP server image..."
docker rmi mcp-code-search:latest 2>/dev/null && echo "    Removed." || echo "    Not found, skipping."

echo "==> Removing Claude Code MCP registration..."
claude mcp remove code-search 2>/dev/null && echo "    Removed." || echo "    Not registered, skipping."

echo ""
echo "============================================"
echo "  Teardown complete!"
echo ""
echo "  Removed:"
echo "    - Postgres container + data volume"
echo "    - Ollama container + model volume (if any)"
echo "    - Docker network"
echo "    - MCP server image"
echo "    - Claude Code MCP registration"
echo ""
echo "  Run ./scripts/setup.sh to start fresh."
echo "============================================"
