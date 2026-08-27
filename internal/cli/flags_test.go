package cli

import "testing"

// Flags must be honored whether they appear before or after the positional arg —
// Go's flag package stops at the first non-flag, so parse() interleaves.
func TestParseFlagsAfterPositional(t *testing.T) {
	cases := [][]string{
		{"./repo", "--reembed", "--force"},
		{"--reembed", "./repo", "--force"},
		{"--reembed", "--force", "./repo"},
	}
	for _, args := range cases {
		fs := newFlagSet("rebuild")
		reembed := fs.Bool("reembed", false, "")
		force := fs.Bool("force", false, "")
		pos, _, ok := parse(fs, args)
		if !ok {
			t.Fatalf("parse(%v) not ok", args)
		}
		if len(pos) != 1 || pos[0] != "./repo" {
			t.Errorf("parse(%v) positionals = %v, want [./repo]", args, pos)
		}
		if !*reembed || !*force {
			t.Errorf("parse(%v): reembed=%v force=%v, want both true", args, *reembed, *force)
		}
	}
}

func TestParseBadFlag(t *testing.T) {
	fs := newFlagSet("list")
	fs.Bool("json", false, "")
	if _, code, ok := parse(fs, []string{"--nope"}); ok || code != 2 {
		t.Errorf("bad flag: got (code=%d, ok=%v), want (2, false)", code, ok)
	}
}
