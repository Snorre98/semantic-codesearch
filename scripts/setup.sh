#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "==> Starting Postgres (pgvector)..."
docker compose -f "$PROJECT_DIR/docker-compose.yml" up -d postgres

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

echo "==> Building MCP server Docker image..."
docker compose -f "$PROJECT_DIR/docker-compose.yml" build mcp-server

echo ""
echo "============================================"
echo "  Setup complete!"
echo ""
echo "  Register in Claude Code:"
echo ""
echo "    claude mcp add code-search -- \\"
echo "      docker run -i --rm \\"
echo "      --network codesearch \\"
echo "      --add-host=host.docker.internal:host-gateway \\"
echo "      -e MCP_CS_PG_HOST=postgres \\"
echo "      -e MCP_CS_PG_PORT=5432 \\"
echo "      -e MCP_CS_OLLAMA_URL=http://host.docker.internal:11434 \\"
echo "      -v $HOME:$HOME:ro \\"
echo "      mcp-code-search:latest"
echo ""
echo "============================================"
