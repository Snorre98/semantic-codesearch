package server

import (
	"strings"
	"testing"

	"semantic-codesearch/internal/models"
)

func TestTrimSnippet(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		maxLines int
		maxChars int
		want     string
	}{
		{
			name:     "short input passes through unchanged",
			in:       "func foo() {}",
			maxLines: 3,
			maxChars: 160,
			want:     "func foo() {}",
		},
		{
			name:     "truncates to maxLines with ellipsis",
			in:       "line1\nline2\nline3\nline4\nline5",
			maxLines: 3,
			maxChars: 160,
			want:     "line1\nline2\nline3…",
		},
		{
			name:     "strips common leading indentation",
			in:       "\t\tif x {\n\t\t\treturn y\n\t\t}",
			maxLines: 3,
			maxChars: 160,
			want:     "if x {\n\treturn y\n}",
		},
		{
			name:     "caps to maxChars with ellipsis",
			in:       "abcdefghij",
			maxLines: 3,
			maxChars: 5,
			want:     "abcde…",
		},
		{
			name:     "blank lines ignored when computing indent",
			in:       "    a\n\n    b",
			maxLines: 3,
			maxChars: 160,
			want:     "a\n\nb",
		},
		{
			name:     "char cap counts runes, not bytes",
			in:       "héllo wörld",
			maxLines: 3,
			maxChars: 5,
			want:     "héllo…",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := trimSnippet(tc.in, tc.maxLines, tc.maxChars)
			if got != tc.want {
				t.Errorf("trimSnippet(%q, %d, %d) = %q, want %q", tc.in, tc.maxLines, tc.maxChars, got, tc.want)
			}
		})
	}
}

func TestRenderSearchMarkdown(t *testing.T) {
	meta := markdownMeta{
		Query:     "auth middleware",
		Roots:     "/repo",
		Model:     "nomic-embed-text",
		Timestamp: "2026-06-29T00:00:00Z",
		Count:     1,
	}
	results := []models.SearchResult{
		{
			FilePath:   "internal/auth/mw.go",
			StartLine:  10,
			EndLine:    20,
			Snippet:    "func Auth() {\n\treturn\n}",
			SymbolName: "Auth",
			ChunkType:  "function",
			Score:      0.8765,
		},
	}

	cases := []struct {
		name     string
		meta     markdownMeta
		title    string
		notes    string
		results  []models.SearchResult
		wantSubs []string
		denySubs []string
	}{
		{
			name:    "full doc with notes",
			meta:    meta,
			title:   "My Memory",
			notes:   "Key entrypoints below.",
			results: results,
			wantSubs: []string{
				"# My Memory\n",
				"- **Query:** auth middleware\n",
				"- **Model:** nomic-embed-text\n",
				"- **Generated:** 2026-06-29T00:00:00Z\n",
				"## Summary\n\nKey entrypoints below.\n",
				"### [internal/auth/mw.go:10-20](internal/auth/mw.go#L10-L20)\n",
				"Score: 0.8765 · Symbol: Auth · Type: function\n",
				"```go\nfunc Auth() {\n\treturn\n}\n```",
			},
		},
		{
			name:     "title defaults to query, no notes",
			meta:     meta,
			results:  results,
			wantSubs: []string{"# auth middleware\n"},
			denySubs: []string{"## Summary"},
		},
		{
			name:     "zero results",
			meta:     markdownMeta{Query: "nope", Roots: "/repo", Model: "m", Timestamp: "2026-06-29T00:00:00Z", Count: 0},
			results:  nil,
			wantSubs: []string{"## Results\n\nNo matches found.\n"},
			denySubs: []string{"###", "```"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderSearchMarkdown(tc.meta, tc.title, tc.notes, tc.results)
			for _, sub := range tc.wantSubs {
				if !strings.Contains(got, sub) {
					t.Errorf("output missing %q\n--- got ---\n%s", sub, got)
				}
			}
			for _, sub := range tc.denySubs {
				if strings.Contains(got, sub) {
					t.Errorf("output unexpectedly contains %q\n--- got ---\n%s", sub, got)
				}
			}
		})
	}
}

func TestResolveMarkdownPath(t *testing.T) {
	cases := []struct {
		name       string
		outputPath string
		query      string
		want       string
	}{
		{"md file passes through", "/proj/docs/notes.md", "anything", "/proj/docs/notes.md"},
		{"uppercase md passes through", "/proj/NOTES.MD", "anything", "/proj/NOTES.MD"},
		{"directory derives slug", "/proj/docs", "Auth Middleware!", "/proj/docs/auth-middleware.md"},
		{"non-md path treated as dir", "/proj/docs/sub", "x y", "/proj/docs/sub/x-y.md"},
		{"empty query falls back", "/proj/docs", "!!!", "/proj/docs/search.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveMarkdownPath(tc.outputPath, tc.query); got != tc.want {
				t.Errorf("resolveMarkdownPath(%q, %q) = %q, want %q", tc.outputPath, tc.query, got, tc.want)
			}
		})
	}
}

func TestSlugifyLengthCap(t *testing.T) {
	long := strings.Repeat("a", 100)
	got := slugify(long)
	if len(got) > 60 {
		t.Errorf("slug length = %d, want <= 60", len(got))
	}
}
