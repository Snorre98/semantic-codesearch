#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "==> Starting Postgres (pgvector)..."
docker compose -f "$PROJECT_DIR/docker-compose.yml" up -d

echo "==> Waiting for Postgres to be healthy..."
until docker inspect --format='{{.State.Health.Status}}' mcp-code-search-db 2>/dev/null | grep -q healthy; do
    sleep 1
done
echo "==> Postgres is ready."

echo "==> Checking Ollama..."
if ! command -v ollama &>/dev/null; then
    echo "Ollama not found. Installing via Homebrew..."
    brew install ollama
fi

if ! curl -sf http://localhost:11434/api/tags >/dev/null 2>&1; then
    echo "Ollama is not running. Starting it..."
    ollama serve &
    sleep 3
fi

echo "==> Pulling nomic-embed-text model..."
ollama pull nomic-embed-text

echo "==> Installing Python package (editable)..."
python3 -m pip install -e "$PROJECT_DIR"

echo ""
echo "============================================"
echo "  Setup complete!"
echo ""
echo "  Register in Claude Code:"
echo "    claude mcp add code-search -e PYTHONUNBUFFERED=1 -- mcp-code-search"
echo ""
echo "  Or add to .mcp.json:"
echo '    {"mcpServers":{"code-search":{"command":"mcp-code-search","env":{"PYTHONUNBUFFERED":"1"}}}}'
echo "============================================"
