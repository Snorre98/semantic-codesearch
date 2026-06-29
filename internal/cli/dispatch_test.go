package cli

import "testing"

func TestDispatchExitCodes(t *testing.T) {
	// Isolate the registry under a temp dir for commands that touch it.
	t.Setenv("MCP_CS_SQLITE_DIR", t.TempDir())

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"help"}, 0},
		{"unknown command", []string{"bogus"}, 2},
		{"list empty", []string{"list"}, 0},
		{"list json empty", []string{"list", "--json"}, 0},
		{"list bad flag", []string{"list", "--nope"}, 2},
		{"info missing arg", []string{"info"}, 1},
		{"remove missing arg", []string{"remove"}, 1},
		{"pull missing arg", []string{"pull"}, 1},
		{"mcp print native", []string{"mcp"}, 0},
		{"mcp print docker", []string{"mcp", "--docker"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Dispatch(tc.args); got != tc.want {
				t.Errorf("Dispatch(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}
