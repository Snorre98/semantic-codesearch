package store

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"semantic-codesearch/internal/config"
)

// RegistryEntry describes one indexed codebase and its backing DB file.
type RegistryEntry struct {
	Root        string `json:"root"`
	DBFile      string `json:"db_file"`
	Model       string `json:"model"`
	Dims        int    `json:"dims"`
	LastIndexed string `json:"last_indexed"`
	Files       int    `json:"files"`
	Chunks      int    `json:"chunks"`
}

// Registry tracks every codebase's DB file in the central SQLite directory.
// It is a small JSON file; single-user, so no locking beyond atomic rename.
type Registry struct {
	dir     string
	Entries map[string]RegistryEntry `json:"entries"` // keyed by absolute root
}

var slugUnsafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// LoadRegistry reads (or initializes) the registry from cfg.SQLiteDir.
func LoadRegistry(cfg config.Config) (*Registry, error) {
	r := &Registry{dir: cfg.SQLiteDir, Entries: map[string]RegistryEntry{}}
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	data, err := os.ReadFile(r.path())
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	// Tolerate an empty/corrupt file by starting fresh.
	var on struct {
		Entries map[string]RegistryEntry `json:"entries"`
	}
	if json.Unmarshal(data, &on) == nil && on.Entries != nil {
		r.Entries = on.Entries
	}
	return r, nil
}

func (r *Registry) path() string { return filepath.Join(r.dir, "registry.json") }

// DBPath returns the deterministic DB file path for a codebase root. It does not
// require the root to be registered, so indexing works even with no registry yet.
func (r *Registry) DBPath(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	base := slugUnsafe.ReplaceAllString(filepath.Base(abs), "-")
	if base == "" || base == "-" {
		base = "codebase"
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(r.dir, fmt.Sprintf("%s-%x.db", base, sum[:4]))
}

// Upsert records/updates a codebase entry and persists the registry.
func (r *Registry) Upsert(root, model string, dims, files, chunks int, when time.Time) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	r.Entries[abs] = RegistryEntry{
		Root:        abs,
		DBFile:      r.DBPath(abs),
		Model:       model,
		Dims:        dims,
		LastIndexed: when.UTC().Format(time.RFC3339),
		Files:       files,
		Chunks:      chunks,
	}
	return r.save()
}

// List returns all registered codebases.
func (r *Registry) List() []RegistryEntry {
	out := make([]RegistryEntry, 0, len(r.Entries))
	for _, e := range r.Entries {
		out = append(out, e)
	}
	return out
}

// Resolve returns the entry whose root best matches dir (exact root, or the
// longest registered root that is a prefix of dir), and whether one was found.
func (r *Registry) Resolve(dir string) (RegistryEntry, bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if e, ok := r.Entries[abs]; ok {
		return e, true
	}
	var best RegistryEntry
	found := false
	for root, e := range r.Entries {
		if abs == root || strings.HasPrefix(abs, root+string(filepath.Separator)) {
			if !found || len(root) > len(best.Root) {
				best, found = e, true
			}
		}
	}
	return best, found
}

func (r *Registry) save() error {
	data, err := json.MarshalIndent(struct {
		Entries map[string]RegistryEntry `json:"entries"`
	}{Entries: r.Entries}, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path())
}
