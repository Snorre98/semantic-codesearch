package cli

import (
	"strings"
	"testing"
)

func TestRegistrationArgsCodexUsesEnvFlag(t *testing.T) {
	args := registrationArgs("/tmp/mcp-code-search", true, "codex")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--env MCP_CS_BACKEND=postgres") {
		t.Fatalf("codex registration args missing --env form: %v", args)
	}
	if strings.Contains(joined, "-e MCP_CS_BACKEND=postgres") {
		t.Fatalf("codex registration args unexpectedly used -e form: %v", args)
	}
}

func TestCodexTOMLIncludesEnvBlockForDocker(t *testing.T) {
	toml := codexTOML("/tmp/mcp-code-search", true)
	if !strings.Contains(toml, "[mcp_servers.code-search]") {
		t.Fatalf("missing server block:\n%s", toml)
	}
	if !strings.Contains(toml, `command = "/tmp/mcp-code-search"`) {
		t.Fatalf("missing command:\n%s", toml)
	}
	if !strings.Contains(toml, "[mcp_servers.code-search.env]") {
		t.Fatalf("missing env block:\n%s", toml)
	}
	if !strings.Contains(toml, `MCP_CS_BACKEND = "postgres"`) {
		t.Fatalf("missing postgres env:\n%s", toml)
	}
}
