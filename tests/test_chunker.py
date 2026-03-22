from mcp_code_search.chunker import chunk_file, detect_language


def test_detect_language():
    assert detect_language("foo.py") == "python"
    assert detect_language("bar.ts") == "typescript"
    assert detect_language("baz.unknown") is None


def test_chunk_python_function():
    code = '''
def hello():
    print("hello")

def world():
    print("world")
'''.strip()
    chunks = chunk_file(code, "test.py")
    assert len(chunks) == 2
    assert chunks[0].symbol_name == "hello"
    assert chunks[0].chunk_type == "function"
    assert chunks[1].symbol_name == "world"


def test_chunk_python_class():
    code = '''
class Greeter:
    def greet(self):
        return "hi"

    def farewell(self):
        return "bye"
'''.strip()
    chunks = chunk_file(code, "test.py")
    assert len(chunks) == 1
    assert chunks[0].chunk_type == "class"
    assert chunks[0].symbol_name == "Greeter"


def test_chunk_fixed_size_fallback():
    # Unknown extension -> fixed size chunking
    lines = "\n".join(f"line {i}" for i in range(100))
    chunks = chunk_file(lines, "data.xyz")
    assert len(chunks) >= 2
    assert chunks[0].chunk_type == "block"


def test_chunk_empty_file():
    assert chunk_file("", "test.py") == []
    assert chunk_file("   \n\n  ", "test.py") == []


def test_chunk_javascript_heuristic():
    code = '''function greet(name) {
    return "Hello " + name;
}

function farewell(name) {
    return "Bye " + name;
}'''
    chunks = chunk_file(code, "test.js")
    assert len(chunks) >= 2
    assert chunks[0].symbol_name == "greet"
