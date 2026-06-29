package server

import "testing"

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
