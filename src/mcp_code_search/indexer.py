from __future__ import annotations

import hashlib
import os
import time
from pathlib import Path

from mcp_code_search.chunker import chunk_file, detect_language, format_chunk_for_embedding
from mcp_code_search.config import Config
from mcp_code_search.db import (
    get_connection,
    get_file_if_unchanged,
    insert_chunks,
    record_index_run,
    upsert_file,
)
from mcp_code_search.embeddings import EmbeddingClient
from mcp_code_search.ignore import build_ignore_spec, is_binary, should_ignore
from mcp_code_search.models import IndexResult


def index_directory(directory: str, config: Config, embedder: EmbeddingClient) -> IndexResult:
    """Walk a directory, chunk files, embed, and store in pgvector."""
    root = Path(directory).resolve()
    if not root.is_dir():
        raise ValueError(f"Not a directory: {root}")

    spec = build_ignore_spec(str(root))
    max_bytes = config.max_file_size_kb * 1024

    files_processed = 0
    files_skipped = 0
    errors = 0
    error_details: list[dict] = []
    start_time = time.time()

    with get_connection(config) as conn:
        for dirpath, dirnames, filenames in os.walk(root):
            # Filter out ignored directories in-place
            dirnames[:] = [
                d for d in dirnames
                if not should_ignore(os.path.join(dirpath, d), str(root), spec)
            ]

            for fname in filenames:
                fpath = os.path.join(dirpath, fname)

                if should_ignore(fpath, str(root), spec):
                    continue
                if is_binary(fpath):
                    continue

                try:
                    stat = os.stat(fpath)
                    if stat.st_size > max_bytes:
                        continue
                    mtime = stat.st_mtime

                    # Incremental: skip unchanged files
                    existing_id = get_file_if_unchanged(conn, fpath, mtime)
                    if existing_id is not None:
                        files_skipped += 1
                        continue

                    content = Path(fpath).read_text(errors="replace")
                    chunks = chunk_file(content, fpath)
                    if not chunks:
                        continue

                    # Prepare texts for batch embedding
                    texts = [format_chunk_for_embedding(c, fpath) for c in chunks]

                    # Embed in batches
                    all_embeddings: list[list[float]] = []
                    for i in range(0, len(texts), config.batch_size):
                        batch = texts[i : i + config.batch_size]
                        all_embeddings.extend(embedder.embed_batch(batch))

                    # Compute file hash
                    file_hash = hashlib.sha256(content.encode()).hexdigest()
                    lang = detect_language(fpath)

                    # Store
                    file_id = upsert_file(conn, fpath, mtime, file_hash, lang)
                    chunk_records = [
                        {
                            "chunk_index": idx,
                            "start_line": c.start_line,
                            "end_line": c.end_line,
                            "chunk_type": c.chunk_type,
                            "symbol_name": c.symbol_name,
                            "content": c.content,
                            "embedding": emb,
                        }
                        for idx, (c, emb) in enumerate(zip(chunks, all_embeddings))
                    ]
                    insert_chunks(conn, file_id, chunk_records)
                    conn.commit()
                    files_processed += 1

                except Exception as e:
                    errors += 1
                    error_details.append({"file": fpath, "error": str(e)})
                    conn.rollback()

        # Record the run
        record_index_run(
            conn, str(root), files_processed, files_skipped, errors, error_details
        )

    elapsed = time.time() - start_time
    return IndexResult(
        files_processed=files_processed,
        files_skipped=files_skipped,
        errors=errors,
        error_details=error_details,
        elapsed=elapsed,
    )
