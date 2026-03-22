from __future__ import annotations

from contextlib import contextmanager
from typing import Generator

import psycopg
from pgvector.psycopg import register_vector
from psycopg.rows import dict_row

from mcp_code_search.config import Config
from mcp_code_search.models import IndexStatus, SearchResult


def _connect(config: Config) -> psycopg.Connection:
    conn = psycopg.connect(config.pg_conninfo, row_factory=dict_row)
    register_vector(conn)
    return conn


@contextmanager
def get_connection(config: Config) -> Generator[psycopg.Connection, None, None]:
    conn = _connect(config)
    try:
        yield conn
    finally:
        conn.close()


def get_file_if_unchanged(
    conn: psycopg.Connection, file_path: str, last_modified: float
) -> int | None:
    """Return file id if the file exists and hasn't changed, else None."""
    row = conn.execute(
        "SELECT id FROM indexed_files WHERE file_path = %s AND last_modified = %s",
        (file_path, last_modified),
    ).fetchone()
    return row["id"] if row else None


def upsert_file(
    conn: psycopg.Connection,
    file_path: str,
    last_modified: float,
    file_hash: str,
    language: str | None,
) -> int:
    """Insert or update a file record, deleting old chunks via CASCADE."""
    # Delete existing record (cascades to chunks)
    conn.execute("DELETE FROM indexed_files WHERE file_path = %s", (file_path,))
    row = conn.execute(
        """INSERT INTO indexed_files (file_path, last_modified, file_hash, language)
           VALUES (%s, %s, %s, %s) RETURNING id""",
        (file_path, last_modified, file_hash, language),
    ).fetchone()
    return row["id"]


def insert_chunks(
    conn: psycopg.Connection,
    file_id: int,
    chunks: list[dict],
) -> None:
    """Batch insert chunks with embeddings."""
    conn.executemany(
        """INSERT INTO code_chunks
           (file_id, chunk_index, start_line, end_line, chunk_type, symbol_name, content, embedding)
           VALUES (%s, %s, %s, %s, %s, %s, %s, %s::vector)""",
        [
            (
                file_id,
                c["chunk_index"],
                c["start_line"],
                c["end_line"],
                c["chunk_type"],
                c["symbol_name"],
                c["content"],
                str(c["embedding"]),
            )
            for c in chunks
        ],
    )


def search_similar(
    config: Config, embedding: list[float], limit: int = 10
) -> list[SearchResult]:
    """Find the most similar code chunks by cosine similarity."""
    with get_connection(config) as conn:
        rows = conn.execute(
            """SELECT
                f.file_path,
                c.start_line,
                c.end_line,
                c.content,
                c.symbol_name,
                c.chunk_type,
                1 - (c.embedding <=> %s::vector) AS score
            FROM code_chunks c
            JOIN indexed_files f ON f.id = c.file_id
            ORDER BY c.embedding <=> %s::vector
            LIMIT %s""",
            (str(embedding), str(embedding), limit),
        ).fetchall()

    return [
        SearchResult(
            file_path=r["file_path"],
            start_line=r["start_line"],
            end_line=r["end_line"],
            snippet=r["content"],
            symbol_name=r["symbol_name"],
            chunk_type=r["chunk_type"],
            score=r["score"],
        )
        for r in rows
    ]


def record_index_run(
    conn: psycopg.Connection,
    directory: str,
    files_processed: int,
    files_skipped: int,
    errors: int,
    error_details: list[dict],
) -> None:
    conn.execute(
        """INSERT INTO index_runs
           (directory, finished_at, files_processed, files_skipped, errors, error_details)
           VALUES (%s, NOW(), %s, %s, %s, %s::jsonb)""",
        (directory, files_processed, files_skipped, errors, str(error_details).replace("'", '"') if error_details else "[]"),
    )
    conn.commit()


def get_index_status(config: Config) -> IndexStatus:
    with get_connection(config) as conn:
        files_row = conn.execute("SELECT COUNT(*) AS cnt FROM indexed_files").fetchone()
        chunks_row = conn.execute("SELECT COUNT(*) AS cnt FROM code_chunks").fetchone()
        last_run = conn.execute(
            "SELECT directory, finished_at, error_details FROM index_runs ORDER BY id DESC LIMIT 1"
        ).fetchone()

    return IndexStatus(
        total_files=files_row["cnt"],
        total_chunks=chunks_row["cnt"],
        last_index_time=str(last_run["finished_at"]) if last_run else None,
        last_directory=last_run["directory"] if last_run else None,
        last_errors=last_run["error_details"] if last_run else [],
    )
