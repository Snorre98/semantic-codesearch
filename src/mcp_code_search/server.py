from __future__ import annotations

import os

os.environ.setdefault("PYTHONUNBUFFERED", "1")

from fastmcp import FastMCP

from mcp_code_search.config import Config
from mcp_code_search.db import get_index_status, search_similar
from mcp_code_search.embeddings import EmbeddingClient
from mcp_code_search.indexer import index_directory

mcp = FastMCP(
    name="Code Search",
    instructions="Use index_codebase to index a project before searching. Use search_code for natural language queries over indexed code.",
)
config = Config()
embedder = EmbeddingClient(config)


@mcp.tool
def index_codebase(directory: str) -> str:
    """Index a codebase directory for semantic search.

    Walks the directory, respects .gitignore, chunks source files intelligently,
    generates embeddings via Ollama, and stores them in pgvector.
    Supports incremental re-indexing — unchanged files are skipped.

    Args:
        directory: Absolute path to the codebase directory to index.
    """
    try:
        result = index_directory(directory, config, embedder)
        return (
            f"Indexed {result.files_processed} files "
            f"({result.files_skipped} unchanged, {result.errors} errors) "
            f"in {result.elapsed:.1f}s"
        )
    except Exception as e:
        return f"Error: {e}"


@mcp.tool
def search_code(query: str, limit: int = 10) -> list[dict]:
    """Search indexed code using natural language.

    Returns the top matching code snippets with file path, line range, and relevance score.

    Args:
        query: Natural language description of the code you're looking for.
        limit: Maximum number of results to return (default 10).
    """
    try:
        query_embedding = embedder.embed_single(query)
        results = search_similar(config, query_embedding, limit)
        return [
            {
                "file": r.file_path,
                "lines": f"{r.start_line}-{r.end_line}",
                "score": round(r.score, 4),
                "symbol": r.symbol_name,
                "type": r.chunk_type,
                "snippet": r.snippet[:500],
            }
            for r in results
        ]
    except Exception as e:
        return [{"error": str(e)}]


@mcp.tool
def index_status() -> dict:
    """Show the current state of the code search index.

    Returns total files indexed, total chunks, last index time, and any errors.
    """
    try:
        status = get_index_status(config)
        return {
            "total_files": status.total_files,
            "total_chunks": status.total_chunks,
            "last_index_time": status.last_index_time,
            "last_directory": status.last_directory,
            "errors": status.last_errors,
        }
    except Exception as e:
        return {"error": str(e)}


def run() -> None:
    mcp.run()


if __name__ == "__main__":
    run()
