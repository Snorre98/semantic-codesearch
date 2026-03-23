#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "==> Building MCP server Docker image..."
docker compose -f "$PROJECT_DIR/docker-compose.yml" build mcp-server

# Detect whether Ollama is already running on the host.
USE_HOST_OLLAMA=false
if curl -sf http://localhost:11434/api/tags >/dev/null 2>&1; then
    USE_HOST_OLLAMA=true
    echo "==> Detected Ollama running on the host — using host Ollama (GPU-accelerated)."
fi

if [ "$USE_HOST_OLLAMA" = true ]; then
    echo "==> Starting Postgres..."
    docker compose -f "$PROJECT_DIR/docker-compose.yml" up -d postgres

    echo "==> Waiting for Postgres to be healthy..."
    until docker inspect --format='{{.State.Health.Status}}' mcp-code-search-db 2>/dev/null | grep -q healthy; do
        sleep 1
    done
    echo "==> Postgres is ready."

    echo "==> Pulling nomic-embed-text model (host Ollama)..."
    ollama pull nomic-embed-text
else
    echo "==> Starting Postgres and Ollama (Docker)..."
    docker compose -f "$PROJECT_DIR/docker-compose.yml" --profile ollama up -d

    echo "==> Waiting for Postgres to be healthy..."
    until docker inspect --format='{{.State.Health.Status}}' mcp-code-search-db 2>/dev/null | grep -q healthy; do
        sleep 1
    done
    echo "==> Postgres is ready."

    echo "==> Waiting for Ollama to be healthy..."
    until docker inspect --format='{{.State.Health.Status}}' mcp-code-search-ollama 2>/dev/null | grep -q healthy; do
        sleep 1
    done
    echo "==> Ollama is ready."

    echo "==> Pulling nomic-embed-text model (Docker Ollama)..."
    docker compose -f "$PROJECT_DIR/docker-compose.yml" exec ollama ollama pull nomic-embed-text
fi

echo ""
echo "============================================"
echo "  Setup complete!"
echo ""
echo "  Register in Claude Code:"
echo ""
if [ "$USE_HOST_OLLAMA" = true ]; then
    echo "    claude mcp add code-search -- \\"
    echo "      docker run -i --rm \\"
    echo "      --network codesearch \\"
    echo "      --add-host=host.docker.internal:host-gateway \\"
    echo "      -e MCP_CS_PG_HOST=postgres \\"
    echo "      -e MCP_CS_PG_PORT=5432 \\"
    echo "      -e MCP_CS_OLLAMA_URL=http://host.docker.internal:11434 \\"
    echo "      -v \$PWD:\$PWD:ro \\"
    echo "      mcp-code-search:latest"
else
    echo "    claude mcp add code-search -- \\"
    echo "      docker run -i --rm \\"
    echo "      --network codesearch \\"
    echo "      -e MCP_CS_PG_HOST=postgres \\"
    echo "      -e MCP_CS_PG_PORT=5432 \\"
    echo "      -v \$PWD:\$PWD:ro \\"
    echo "      mcp-code-search:latest"
fi
echo ""
echo "  Replace \$PWD with the path to the project you want to index."
echo "  To index multiple projects, add multiple -v flags:"
echo ""
echo "      -v /path/to/project-a:/path/to/project-a:ro \\"
echo "      -v /path/to/project-b:/path/to/project-b:ro \\"
echo ""
echo "============================================"
