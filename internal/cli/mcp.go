package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"semantic-codesearch/internal/config"
)

const mcpServerName = "code-search"

// cmdMCP handles `mcp [--docker]` (print) and `mcp install [--docker]` (run).
func cmdMCP(args []string) int {
	doInstall := false
	if len(args) > 0 && args[0] == "install" {
		doInstall = true
		args = args[1:]
	}
	fs := newFlagSet("mcp")
	docker := fs.Bool("docker", false, "generate registration for the Docker/Postgres backend")
	if _, code, ok := parse(fs, args); !ok {
		return code
	}

	bin, err := os.Executable()
	if err != nil || bin == "" {
		bin = "mcp-code-search"
	}

	addArgs := registrationArgs(bin, *docker)
	if doInstall {
		return runRegistration(addArgs)
	}
	return printRegistration(bin, *docker, addArgs)
}

// registrationArgs builds the argument vector for `claude mcp add` (excluding the
// leading "claude").
func registrationArgs(bin string, docker bool) []string {
	args := []string{"mcp", "add", mcpServerName}
	if docker {
		for k, v := range postgresEnv() {
			args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
		}
	}
	return append(args, "--", bin, "serve")
}

// postgresEnv returns the env overrides that select the Postgres backend with the
// compose stack's defaults.
func postgresEnv() map[string]string {
	cfg := config.Load()
	return map[string]string{
		"MCP_CS_BACKEND":     "postgres",
		"MCP_CS_PG_HOST":     cfg.PGHost,
		"MCP_CS_PG_PORT":     fmt.Sprintf("%d", cfg.PGPort),
		"MCP_CS_PG_DATABASE": cfg.PGDatabase,
		"MCP_CS_PG_USER":     cfg.PGUser,
		"MCP_CS_PG_PASSWORD": cfg.PGPassword,
		"MCP_CS_OLLAMA_URL":  cfg.OllamaBaseURL,
	}
}

func printRegistration(bin string, docker bool, addArgs []string) int {
	mode := "native (SQLite)"
	if docker {
		mode = "Docker/Postgres"
	}
	fmt.Printf("# Register mcp-code-search with Claude Code — %s mode\n\n", mode)
	fmt.Printf("claude %s\n\n", shellJoin(addArgs))

	if docker {
		fmt.Println("# Requires the Postgres + Ollama stack to be running:")
		fmt.Println("#   docker compose --profile ollama up -d")
		fmt.Println()
	}

	fmt.Println("# Or add this to .mcp.json:")
	fmt.Println(mcpJSON(bin, docker))
	return 0
}

func mcpJSON(bin string, docker bool) string {
	server := map[string]any{
		"command": bin,
		"args":    []string{"serve"},
	}
	if docker {
		server["env"] = postgresEnv()
	}
	doc := map[string]any{
		"mcpServers": map[string]any{mcpServerName: server},
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	return string(b)
}

func runRegistration(addArgs []string) int {
	if _, err := exec.LookPath("claude"); err != nil {
		return errf("`claude` CLI not found on PATH; run the printed command manually:\nclaude %s", shellJoin(addArgs))
	}
	fmt.Fprintf(os.Stderr, "running: claude %s\n", shellJoin(addArgs))
	cmd := exec.Command("claude", addArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return errf("registration failed: %v", err)
	}
	return 0
}

// shellJoin renders args for display, quoting any token that contains whitespace.
func shellJoin(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t") {
			out[i] = fmt.Sprintf("%q", a)
		} else {
			out[i] = a
		}
	}
	return strings.Join(out, " ")
}
