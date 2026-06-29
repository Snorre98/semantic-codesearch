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

// Codebase lifecycle states recorded in RegistryEntry.Status. An empty string is
// treated as StatusActive for backward compatibility with older registry files.
const (
	StatusActive     = "active"
	StatusDeprecated = "deprecated"
)

// Deprecation reasons recorded in RegistryEntry.DeprecatedReason.
const (
	ReasonRemoved     = "removed"      // user ran remove/forget
	ReasonModelSwitch = "model-switch" // archived by rebuild --reembed
	ReasonStale       = "stale"        // root path or DB file went missing
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

	// Lifecycle. Status is "" (active) for active entries; deprecated entries live
	// in Registry.Deprecated and carry StatusDeprecated plus the fields below.
	Status           string `json:"status,omitempty"`
	DeprecatedAt     string `json:"deprecated_at,omitempty"`
	DeprecatedReason string `json:"deprecated_reason,omitempty"`
}

// Active reports whether the entry is in the active state.
func (e RegistryEntry) Active() bool {
	return e.Status == "" || e.Status == StatusActive
}

// Registry tracks every codebase's DB file in the central SQLite directory.
// It is a small JSON file; single-user, so no locking beyond atomic rename.
//
// Active codebases live in Entries (keyed by absolute root). Deprecated DBs —
// archived by remove/forget, a model switch, or prune — live in Deprecated and
// retain their embeddings on disk until an explicit purge deletes them.
type Registry struct {
	dir        string
	Entries    map[string]RegistryEntry `json:"entries"`
	Deprecated []RegistryEntry          `json:"deprecated,omitempty"`
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
		Entries    map[string]RegistryEntry `json:"entries"`
		Deprecated []RegistryEntry          `json:"deprecated"`
	}
	if json.Unmarshal(data, &on) == nil {
		if on.Entries != nil {
			r.Entries = on.Entries
		}
		r.Deprecated = on.Deprecated
	}
	return r, nil
}

func (r *Registry) path() string { return filepath.Join(r.dir, "registry.json") }

// Dir returns the central SQLite directory backing this registry.
func (r *Registry) Dir() string { return r.dir }

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

// DeprecatedDBPath returns an archive path for a soon-to-be-deprecated DB file so
// that the canonical DBPath is freed for a future re-index of the same root. The
// model and a caller-supplied timestamp keep archives distinct.
func (r *Registry) DeprecatedDBPath(dbFile, model string, when time.Time) string {
	base := strings.TrimSuffix(filepath.Base(dbFile), ".db")
	modelSlug := slugUnsafe.ReplaceAllString(model, "-")
	if modelSlug == "" {
		modelSlug = "unknown"
	}
	name := fmt.Sprintf("%s-deprecated-%s-%d.db", base, modelSlug, when.Unix())
	return filepath.Join(r.dir, name)
}

// Upsert records/updates an active codebase entry and persists the registry.
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
		Status:      StatusActive,
	}
	return r.save()
}

// List returns all active codebases.
func (r *Registry) List() []RegistryEntry {
	out := make([]RegistryEntry, 0, len(r.Entries))
	for _, e := range r.Entries {
		out = append(out, e)
	}
	return out
}

// ListDeprecated returns all deprecated (archived) codebase entries.
func (r *Registry) ListDeprecated() []RegistryEntry {
	out := make([]RegistryEntry, len(r.Deprecated))
	copy(out, r.Deprecated)
	return out
}

// Get returns the active entry for an exact root, and whether one was found.
func (r *Registry) Get(root string) (RegistryEntry, bool) {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	e, ok := r.Entries[abs]
	return e, ok
}

// MoveToDeprecated removes the active entry for root and records it as deprecated
// with the given archive DB file path, reason, and timestamp. The caller is
// responsible for any on-disk file move/rename to archiveDBFile beforehand.
func (r *Registry) MoveToDeprecated(root, archiveDBFile, reason string, when time.Time) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	e, ok := r.Entries[abs]
	if !ok {
		return fmt.Errorf("no active codebase registered for %q", abs)
	}
	delete(r.Entries, abs)
	e.DBFile = archiveDBFile
	e.Status = StatusDeprecated
	e.DeprecatedReason = reason
	e.DeprecatedAt = when.UTC().Format(time.RFC3339)
	r.Deprecated = append(r.Deprecated, e)
	return r.save()
}

// RemoveDeprecated drops the deprecated entry whose DBFile matches dbFile (used by
// purge after the archive file has been deleted). It is a no-op if none match.
func (r *Registry) RemoveDeprecated(dbFile string) error {
	out := r.Deprecated[:0]
	for _, e := range r.Deprecated {
		if e.DBFile != dbFile {
			out = append(out, e)
		}
	}
	r.Deprecated = out
	return r.save()
}

// Resolve returns the active entry whose root best matches dir (exact root, or the
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

// MatchesConfig reports whether a codebase embedded as recorded in e can be
// searched with the currently configured embedding model. A mismatch means the
// stored vectors live in a different vector space (different model and/or
// dimension) and searching would silently return garbage. The returned reason is
// empty on a match and human-readable otherwise.
func MatchesConfig(e RegistryEntry, cfg config.Config) (bool, string) {
	if e.Model != "" && e.Model != cfg.EmbeddingModel {
		return false, fmt.Sprintf("indexed with model %q but configured model is %q", e.Model, cfg.EmbeddingModel)
	}
	if e.Dims != 0 && e.Dims != config.EmbeddingDimensions {
		return false, fmt.Sprintf("indexed at %d dims but configured dimension is %d", e.Dims, config.EmbeddingDimensions)
	}
	return true, ""
}

func (r *Registry) save() error {
	data, err := json.MarshalIndent(struct {
		Entries    map[string]RegistryEntry `json:"entries"`
		Deprecated []RegistryEntry          `json:"deprecated,omitempty"`
	}{Entries: r.Entries, Deprecated: r.Deprecated}, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path())
}
