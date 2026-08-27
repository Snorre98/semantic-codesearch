package ignore

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCodesearchignoreExcludesByExtension(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".codesearchignore"), "*.md\n")
	write(t, filepath.Join(root, "main.go"), "package main")
	write(t, filepath.Join(root, "README.md"), "# stale")
	docs := filepath.Join(root, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(docs, "guide.md"), "# stale nested")

	spec := BuildIgnoreSpec(root)
	cases := map[string]bool{
		filepath.Join(root, "main.go"):   false, // indexed
		filepath.Join(root, "README.md"): true,  // excluded
		filepath.Join(docs, "guide.md"):  true,  // excluded (nested)
	}
	for path, wantIgnored := range cases {
		if got := ShouldIgnore(path, root, spec); got != wantIgnored {
			t.Errorf("ShouldIgnore(%s) = %v, want %v", path, got, wantIgnored)
		}
	}
}

func TestCodesearchignoreNegationAndPaths(t *testing.T) {
	root := t.TempDir()
	// Exclude all markdown except a kept file, and a whole directory.
	write(t, filepath.Join(root, ".codesearchignore"), "*.md\n!KEEP.md\nvendor/\n")
	write(t, filepath.Join(root, "KEEP.md"), "# keep me")
	write(t, filepath.Join(root, "notes.md"), "# drop me")
	vendor := filepath.Join(root, "vendor")
	if err := os.Mkdir(vendor, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(vendor, "lib.go"), "package vendor")

	spec := BuildIgnoreSpec(root)
	if ShouldIgnore(filepath.Join(root, "KEEP.md"), root, spec) {
		t.Error("KEEP.md should be re-included by negation")
	}
	if !ShouldIgnore(filepath.Join(root, "notes.md"), root, spec) {
		t.Error("notes.md should be excluded")
	}
	if !ShouldIgnore(filepath.Join(vendor, "lib.go"), root, spec) {
		t.Error("vendor/ contents should be excluded")
	}
}
