CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS indexed_files (
    id              SERIAL PRIMARY KEY,
    file_path       TEXT NOT NULL UNIQUE,
    last_modified   DOUBLE PRECISION NOT NULL,
    file_hash       TEXT NOT NULL,
    language        TEXT,
    indexed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS code_chunks (
    id              SERIAL PRIMARY KEY,
    file_id         INTEGER NOT NULL REFERENCES indexed_files(id) ON DELETE CASCADE,
    chunk_index     INTEGER NOT NULL,
    start_line      INTEGER NOT NULL,
    end_line        INTEGER NOT NULL,
    chunk_type      TEXT,
    symbol_name     TEXT,
    content         TEXT NOT NULL,
    embedding       VECTOR(768) NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_chunks_embedding
    ON code_chunks USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

CREATE INDEX IF NOT EXISTS idx_chunks_file_id
    ON code_chunks (file_id);

CREATE INDEX IF NOT EXISTS idx_files_path
    ON indexed_files (file_path);

CREATE TABLE IF NOT EXISTS index_runs (
    id              SERIAL PRIMARY KEY,
    directory       TEXT NOT NULL,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at     TIMESTAMPTZ,
    files_processed INTEGER DEFAULT 0,
    files_skipped   INTEGER DEFAULT 0,
    errors          INTEGER DEFAULT 0,
    error_details   JSONB DEFAULT '[]'::jsonb
);
