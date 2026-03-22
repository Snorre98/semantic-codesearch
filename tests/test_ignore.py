from mcp_code_search.ignore import is_binary, ALWAYS_IGNORE


def test_is_binary():
    assert is_binary("photo.png") is True
    assert is_binary("data.pdf") is True
    assert is_binary("code.py") is False
    assert is_binary("readme.md") is False


def test_always_ignore_includes_common_dirs():
    assert ".git/" in ALWAYS_IGNORE
    assert "node_modules/" in ALWAYS_IGNORE
    assert "__pycache__/" in ALWAYS_IGNORE
