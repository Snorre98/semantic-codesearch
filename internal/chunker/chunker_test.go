package chunker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readTestData(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading testdata/%s: %v", name, err)
	}
	return string(data)
}

func TestChunkFile_Go(t *testing.T) {
	content := readTestData(t, "sample.go")
	chunks := ChunkFile(content, "testdata/sample.go")

	if len(chunks) == 0 {
		t.Fatal("expected chunks for Go file, got none")
	}

	// Expect: Buffer (type), NewBuffer (func), Write (method), String (method)
	expectedSymbols := map[string]string{
		"Buffer":        "class",
		"NewBuffer":     "function",
		"Buffer.Write":  "method",
		"Buffer.String": "method",
	}

	found := make(map[string]bool)
	for _, c := range chunks {
		found[c.SymbolName] = true
		if expected, ok := expectedSymbols[c.SymbolName]; ok {
			if c.ChunkType != expected {
				t.Errorf("chunk %q: expected type %q, got %q", c.SymbolName, expected, c.ChunkType)
			}
		}
	}

	for sym := range expectedSymbols {
		if !found[sym] {
			t.Errorf("expected to find chunk with symbol %q", sym)
		}
	}
}

func TestChunkFile_Go_LineNumbers(t *testing.T) {
	content := readTestData(t, "sample.go")
	chunks := ChunkFile(content, "testdata/sample.go")

	for _, c := range chunks {
		if c.StartLine < 1 {
			t.Errorf("chunk %q: StartLine %d < 1", c.SymbolName, c.StartLine)
		}
		if c.EndLine < c.StartLine {
			t.Errorf("chunk %q: EndLine %d < StartLine %d", c.SymbolName, c.EndLine, c.StartLine)
		}
		// Verify content matches the line range
		lines := strings.Split(content, "\n")
		chunkLines := strings.Split(c.Content, "\n")
		if c.StartLine-1 < len(lines) {
			firstLine := strings.TrimSpace(lines[c.StartLine-1])
			chunkFirstLine := strings.TrimSpace(chunkLines[0])
			if firstLine != chunkFirstLine {
				t.Errorf("chunk %q: first line mismatch at line %d\n  expected: %q\n  got:      %q",
					c.SymbolName, c.StartLine, firstLine, chunkFirstLine)
			}
		}
	}
}

func TestChunkFile_Go_Comments(t *testing.T) {
	content := readTestData(t, "sample.go")
	chunks := ChunkFile(content, "testdata/sample.go")

	for _, c := range chunks {
		if c.SymbolName == "Buffer" {
			// The Buffer struct has a 2-line doc comment above it
			if !strings.Contains(c.Content, "Buffer holds bytes") {
				t.Error("Buffer chunk should include leading doc comment")
			}
		}
		if c.SymbolName == "NewBuffer" {
			if !strings.Contains(c.Content, "NewBuffer creates") {
				t.Error("NewBuffer chunk should include leading doc comment")
			}
		}
	}
}

func TestChunkFile_Python(t *testing.T) {
	content := readTestData(t, "sample.py")
	chunks := ChunkFile(content, "testdata/sample.py")

	if len(chunks) == 0 {
		t.Fatal("expected chunks for Python file, got none")
	}

	expectedSymbols := map[string]string{
		"Calculator": "class",
		"fibonacci":  "function",
		"is_prime":   "function",
	}

	found := make(map[string]bool)
	for _, c := range chunks {
		found[c.SymbolName] = true
		if expected, ok := expectedSymbols[c.SymbolName]; ok {
			if c.ChunkType != expected {
				t.Errorf("chunk %q: expected type %q, got %q", c.SymbolName, expected, c.ChunkType)
			}
		}
	}

	for sym := range expectedSymbols {
		if !found[sym] {
			t.Errorf("expected to find chunk with symbol %q", sym)
		}
	}
}

func TestChunkFile_Python_ClassIntact(t *testing.T) {
	content := readTestData(t, "sample.py")
	chunks := ChunkFile(content, "testdata/sample.py")

	for _, c := range chunks {
		if c.SymbolName == "Calculator" {
			// The class is small enough to be one chunk — it should contain methods
			if !strings.Contains(c.Content, "def __init__") {
				t.Error("Calculator chunk should contain __init__ method")
			}
			if !strings.Contains(c.Content, "def add") {
				t.Error("Calculator chunk should contain add method")
			}
			if !strings.Contains(c.Content, "def multiply") {
				t.Error("Calculator chunk should contain multiply method")
			}
		}
	}
}

func TestChunkFile_TypeScript(t *testing.T) {
	content := readTestData(t, "sample.ts")
	chunks := ChunkFile(content, "testdata/sample.ts")

	if len(chunks) == 0 {
		t.Fatal("expected chunks for TypeScript file, got none")
	}

	// Check we find key symbols
	foundSymbols := make(map[string]string)
	for _, c := range chunks {
		if c.SymbolName != "" {
			foundSymbols[c.SymbolName] = c.ChunkType
		}
	}

	expected := map[string]string{
		"Shape":    "class", // interface
		"Circle":   "class",
		"distance": "function",
	}

	for sym, expectedType := range expected {
		if gotType, ok := foundSymbols[sym]; !ok {
			t.Errorf("expected to find chunk with symbol %q", sym)
		} else if gotType != expectedType {
			t.Errorf("chunk %q: expected type %q, got %q", sym, expectedType, gotType)
		}
	}
}

func TestChunkFile_TypeScript_Comments(t *testing.T) {
	content := readTestData(t, "sample.ts")
	chunks := ChunkFile(content, "testdata/sample.ts")

	for _, c := range chunks {
		if c.SymbolName == "Shape" {
			if !strings.Contains(c.Content, "Shape interface") {
				t.Error("Shape chunk should include leading comment")
			}
		}
		if c.SymbolName == "distance" {
			if !strings.Contains(c.Content, "Calculate the distance") {
				t.Error("distance chunk should include leading comment")
			}
		}
	}
}

func TestChunkFile_Fallback(t *testing.T) {
	content := "key: value\nfoo: bar\n"
	chunks := ChunkFile(content, "config.yaml")

	if len(chunks) == 0 {
		t.Fatal("expected fallback chunks for YAML file")
	}
	if chunks[0].ChunkType != "block" {
		t.Errorf("expected block chunk type for fallback, got %q", chunks[0].ChunkType)
	}
}

func TestChunkFile_Empty(t *testing.T) {
	chunks := ChunkFile("", "test.go")
	if chunks != nil {
		t.Error("expected nil for empty content")
	}

	chunks = ChunkFile("   \n  \n  ", "test.py")
	if chunks != nil {
		t.Error("expected nil for whitespace-only content")
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"script.py", "python"},
		{"app.ts", "typescript"},
		{"style.css", "css"},
		{"unknown.xyz", ""},
		{"FILE.GO", "go"},
	}

	for _, tt := range tests {
		got := DetectLanguage(tt.path)
		if got != tt.want {
			t.Errorf("DetectLanguage(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestFormatChunkForEmbedding(t *testing.T) {
	chunk := ChunkFile(readTestData(t, "sample.go"), "testdata/sample.go")
	if len(chunk) == 0 {
		t.Fatal("no chunks")
	}

	formatted := FormatChunkForEmbedding(chunk[0], "testdata/sample.go")
	if !strings.Contains(formatted, "# File: testdata/sample.go") {
		t.Error("formatted output should contain file path")
	}
	if !strings.Contains(formatted, "# Language: go") {
		t.Error("formatted output should contain language")
	}
	if !strings.Contains(formatted, "# Lines:") {
		t.Error("formatted output should contain line numbers")
	}
}

func TestChunkFile_UnsupportedLanguageFallback(t *testing.T) {
	// A file with a recognized extension but no tree-sitter config (e.g., .toml)
	content := "[section]\nkey = \"value\"\n"
	chunks := ChunkFile(content, "config.toml")

	if len(chunks) == 0 {
		t.Fatal("expected fallback chunks for unsupported language")
	}
	if chunks[0].ChunkType != "block" {
		t.Errorf("expected block type for fallback, got %q", chunks[0].ChunkType)
	}
}
