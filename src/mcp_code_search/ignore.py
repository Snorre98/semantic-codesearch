from __future__ import annotations

from pathlib import Path

import pathspec

ALWAYS_IGNORE = [
    ".git/",
    "node_modules/",
    "__pycache__/",
    ".venv/",
    "venv/",
    ".env",
    "*.pyc",
    "*.pyo",
    "*.so",
    "*.dylib",
    "*.dll",
    "*.exe",
    "*.lock",
    "package-lock.json",
    "yarn.lock",
]

BINARY_EXTENSIONS = frozenset({
    ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".webp",
    ".pdf", ".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".rar",
    ".exe", ".dll", ".so", ".dylib", ".bin",
    ".woff", ".woff2", ".ttf", ".eot", ".otf",
    ".mp3", ".mp4", ".wav", ".avi", ".mov", ".flv",
    ".o", ".a", ".lib", ".class", ".jar",
    ".sqlite", ".db",
})


def is_binary(path: str) -> bool:
    return Path(path).suffix.lower() in BINARY_EXTENSIONS


def build_ignore_spec(directory: str) -> pathspec.GitIgnoreSpec:
    """Build a GitIgnoreSpec from .gitignore files in the directory tree."""
    patterns = list(ALWAYS_IGNORE)
    root = Path(directory)

    # Collect .gitignore files from root down
    for gitignore_path in sorted(root.rglob(".gitignore")):
        try:
            patterns.extend(gitignore_path.read_text().splitlines())
        except OSError:
            continue

    return pathspec.GitIgnoreSpec.from_lines(patterns)


def should_ignore(path: str, base_dir: str, spec: pathspec.GitIgnoreSpec) -> bool:
    """Check if a path should be ignored relative to base_dir."""
    rel = str(Path(path).relative_to(base_dir))
    return spec.match_file(rel)
