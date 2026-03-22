from __future__ import annotations

from dataclasses import dataclass, field


@dataclass
class CodeChunk:
    content: str
    start_line: int
    end_line: int
    chunk_type: str  # 'function', 'class', 'block', 'raw'
    symbol_name: str | None = None


@dataclass
class SearchResult:
    file_path: str
    start_line: int
    end_line: int
    snippet: str
    symbol_name: str | None
    chunk_type: str
    score: float


@dataclass
class IndexResult:
    files_processed: int
    files_skipped: int
    errors: int
    error_details: list[dict] = field(default_factory=list)
    elapsed: float = 0.0


@dataclass
class IndexStatus:
    total_files: int
    total_chunks: int
    last_index_time: str | None
    last_directory: str | None
    last_errors: list[dict] = field(default_factory=list)
