from __future__ import annotations

import ast
import re
from pathlib import Path

from mcp_code_search.models import CodeChunk

LANGUAGE_MAP: dict[str, str] = {
    ".py": "python",
    ".js": "javascript", ".jsx": "javascript",
    ".ts": "typescript", ".tsx": "typescript",
    ".java": "java",
    ".go": "go",
    ".rs": "rust",
    ".c": "c", ".h": "c",
    ".cpp": "cpp", ".hpp": "cpp", ".cc": "cpp",
    ".rb": "ruby",
    ".php": "php",
    ".swift": "swift",
    ".kt": "kotlin",
    ".scala": "scala",
    ".cs": "csharp",
    ".sh": "shell", ".bash": "shell", ".zsh": "shell",
    ".sql": "sql",
    ".md": "markdown",
    ".yaml": "yaml", ".yml": "yaml",
    ".json": "json",
    ".toml": "toml",
    ".xml": "xml",
    ".html": "html", ".htm": "html",
    ".css": "css", ".scss": "scss", ".less": "less",
}

# Regex patterns for function/class detection per language
_BOUNDARY_PATTERNS: dict[str, list[re.Pattern[str]]] = {
    "javascript": [re.compile(r"^(?:export\s+)?(?:async\s+)?function\s+\w+|^(?:export\s+)?(?:const|let|var)\s+\w+\s*=\s*(?:async\s+)?\(|^class\s+\w+")],
    "typescript": [re.compile(r"^(?:export\s+)?(?:async\s+)?function\s+\w+|^(?:export\s+)?(?:const|let|var)\s+\w+\s*=\s*(?:async\s+)?\(|^(?:export\s+)?class\s+\w+|^(?:export\s+)?interface\s+\w+")],
    "java": [re.compile(r"^\s*(?:public|private|protected|static|\s)*(?:class|interface|enum)\s+\w+|^\s*(?:public|private|protected|static|\s)*\w+(?:<[^>]*>)?\s+\w+\s*\(")],
    "go": [re.compile(r"^func\s+|^type\s+\w+\s+(?:struct|interface)")],
    "rust": [re.compile(r"^(?:pub\s+)?(?:async\s+)?fn\s+|^(?:pub\s+)?(?:struct|enum|trait|impl)\s+")],
    "ruby": [re.compile(r"^\s*(?:def|class|module)\s+")],
    "php": [re.compile(r"^\s*(?:public|private|protected|static|\s)*function\s+|^\s*class\s+")],
    "csharp": [re.compile(r"^\s*(?:public|private|protected|internal|static|\s)*(?:class|interface|struct|enum)\s+|^\s*(?:public|private|protected|internal|static|\s)*\w+\s+\w+\s*\(")],
    "kotlin": [re.compile(r"^\s*(?:fun|class|interface|object|data class)\s+")],
    "swift": [re.compile(r"^\s*(?:func|class|struct|enum|protocol)\s+")],
    "scala": [re.compile(r"^\s*(?:def|class|trait|object|case class)\s+")],
}

CHUNK_SIZE = 60
CHUNK_OVERLAP = 10
MAX_CHUNK_LINES = 100


def detect_language(file_path: str) -> str | None:
    return LANGUAGE_MAP.get(Path(file_path).suffix.lower())


def chunk_file(content: str, file_path: str) -> list[CodeChunk]:
    """Chunk a file's content into searchable pieces."""
    if not content.strip():
        return []

    lang = detect_language(file_path)

    if lang == "python":
        chunks = _chunk_python_ast(content)
        if chunks:
            return chunks

    if lang and lang in _BOUNDARY_PATTERNS:
        chunks = _chunk_by_heuristic(content, lang)
        if chunks:
            return chunks

    return _chunk_fixed_size(content)


def _chunk_python_ast(content: str) -> list[CodeChunk]:
    """Chunk Python code using the AST module."""
    try:
        tree = ast.parse(content)
    except SyntaxError:
        return []

    lines = content.splitlines(keepends=True)
    chunks: list[CodeChunk] = []

    for node in ast.iter_child_nodes(tree):
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            start = node.lineno
            end = node.end_lineno or node.lineno
            chunk_text = "".join(lines[start - 1 : end])
            chunk = CodeChunk(
                content=chunk_text,
                start_line=start,
                end_line=end,
                chunk_type="function",
                symbol_name=node.name,
            )
            chunks.append(chunk)
            # Sub-chunk if too large
            if end - start + 1 > MAX_CHUNK_LINES:
                chunks.pop()
                chunks.extend(_sub_chunk(chunk_text, start, "function", node.name))

        elif isinstance(node, ast.ClassDef):
            start = node.lineno
            end = node.end_lineno or node.lineno
            chunk_text = "".join(lines[start - 1 : end])

            if end - start + 1 > MAX_CHUNK_LINES:
                # Chunk individual methods
                for item in ast.iter_child_nodes(node):
                    if isinstance(item, (ast.FunctionDef, ast.AsyncFunctionDef)):
                        m_start = item.lineno
                        m_end = item.end_lineno or item.lineno
                        m_text = "".join(lines[m_start - 1 : m_end])
                        chunks.append(CodeChunk(
                            content=m_text,
                            start_line=m_start,
                            end_line=m_end,
                            chunk_type="method",
                            symbol_name=f"{node.name}.{item.name}",
                        ))
            else:
                chunks.append(CodeChunk(
                    content=chunk_text,
                    start_line=start,
                    end_line=end,
                    chunk_type="class",
                    symbol_name=node.name,
                ))

    # If AST found nothing (e.g., module-level code only), fall back
    if not chunks:
        return []

    return chunks


def _chunk_by_heuristic(content: str, language: str) -> list[CodeChunk]:
    """Chunk code by detecting function/class boundaries with regex."""
    patterns = _BOUNDARY_PATTERNS.get(language, [])
    if not patterns:
        return []

    lines = content.splitlines()
    boundaries: list[int] = []

    for i, line in enumerate(lines):
        for pattern in patterns:
            if pattern.match(line):
                boundaries.append(i)
                break

    if not boundaries:
        return []

    chunks: list[CodeChunk] = []
    for idx, start in enumerate(boundaries):
        end = boundaries[idx + 1] - 1 if idx + 1 < len(boundaries) else len(lines) - 1
        chunk_lines = lines[start : end + 1]
        text = "\n".join(chunk_lines)

        # Try to extract symbol name from first line
        first_line = lines[start].strip()
        name_match = re.search(r"(?:function|def|fn|func|class|interface|struct|trait|enum|type|impl|object|module)\s+(\w+)", first_line)
        symbol_name = name_match.group(1) if name_match else None

        chunk_type = "class" if any(kw in first_line for kw in ("class ", "interface ", "struct ", "trait ", "enum ", "impl ")) else "function"

        chunk = CodeChunk(
            content=text,
            start_line=start + 1,
            end_line=end + 1,
            chunk_type=chunk_type,
            symbol_name=symbol_name,
        )

        if end - start + 1 > MAX_CHUNK_LINES:
            chunks.extend(_sub_chunk(text, start + 1, chunk_type, symbol_name))
        else:
            chunks.append(chunk)

    return chunks


def _chunk_fixed_size(content: str) -> list[CodeChunk]:
    """Fallback: chunk by fixed line count with overlap."""
    lines = content.splitlines()
    if not lines:
        return []

    chunks: list[CodeChunk] = []
    start = 0
    while start < len(lines):
        end = min(start + CHUNK_SIZE, len(lines))
        text = "\n".join(lines[start:end])
        chunks.append(CodeChunk(
            content=text,
            start_line=start + 1,
            end_line=end,
            chunk_type="block",
        ))
        start += CHUNK_SIZE - CHUNK_OVERLAP
        if start >= len(lines):
            break

    return chunks


def _sub_chunk(text: str, start_line: int, chunk_type: str, symbol_name: str | None) -> list[CodeChunk]:
    """Break a large chunk into smaller overlapping pieces."""
    lines = text.splitlines()
    chunks: list[CodeChunk] = []
    start = 0
    while start < len(lines):
        end = min(start + CHUNK_SIZE, len(lines))
        sub_text = "\n".join(lines[start:end])
        chunks.append(CodeChunk(
            content=sub_text,
            start_line=start_line + start,
            end_line=start_line + end - 1,
            chunk_type=chunk_type,
            symbol_name=symbol_name,
        ))
        start += CHUNK_SIZE - CHUNK_OVERLAP
        if start >= len(lines):
            break
    return chunks


def format_chunk_for_embedding(chunk: CodeChunk, file_path: str) -> str:
    """Add context header to a chunk before embedding for better search quality."""
    lang = detect_language(file_path) or "unknown"
    header_parts = [f"# File: {file_path}", f"# Language: {lang}"]
    if chunk.symbol_name:
        header_parts.append(f"# Symbol: {chunk.symbol_name} ({chunk.chunk_type})")
    header_parts.append(f"# Lines: {chunk.start_line}-{chunk.end_line}")
    header = "\n".join(header_parts)
    return f"{header}\n\n{chunk.content}"
