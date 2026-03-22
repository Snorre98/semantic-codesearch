from __future__ import annotations

import os
from dataclasses import dataclass, field


@dataclass(frozen=True)
class Config:
    pg_host: str = field(default_factory=lambda: os.getenv("MCP_CS_PG_HOST", "localhost"))
    pg_port: int = field(default_factory=lambda: int(os.getenv("MCP_CS_PG_PORT", "5433")))
    pg_database: str = field(default_factory=lambda: os.getenv("MCP_CS_PG_DATABASE", "codesearch"))
    pg_user: str = field(default_factory=lambda: os.getenv("MCP_CS_PG_USER", "codesearch"))
    pg_password: str = field(default_factory=lambda: os.getenv("MCP_CS_PG_PASSWORD", "codesearch"))

    ollama_base_url: str = field(default_factory=lambda: os.getenv("MCP_CS_OLLAMA_URL", "http://localhost:11434"))
    embedding_model: str = field(default_factory=lambda: os.getenv("MCP_CS_EMBED_MODEL", "nomic-embed-text"))
    embedding_dimensions: int = 768

    max_file_size_kb: int = field(default_factory=lambda: int(os.getenv("MCP_CS_MAX_FILE_KB", "512")))
    batch_size: int = field(default_factory=lambda: int(os.getenv("MCP_CS_BATCH_SIZE", "50")))

    @property
    def pg_conninfo(self) -> str:
        return (
            f"host={self.pg_host} port={self.pg_port} "
            f"dbname={self.pg_database} user={self.pg_user} "
            f"password={self.pg_password}"
        )
