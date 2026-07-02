package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
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
	client := fs.String("client", "claude", "registration target: claude or codex")
	docker := fs.Bool("docker", false, "generate registration for the Docker/Postgres backend")
	if _, code, ok := parse(fs, args); !ok {
		return code
	}
	target := strings.ToLower(strings.TrimSpace(*client))
	if target != "claude" && target != "codex" {
		return errf("unsupported MCP client %q (want claude or codex)", *client)
	}

	bin, err := os.Executable()
	if err != nil || bin == "" {
		bin = "mcp-code-search"
	}

	addArgs := registrationArgs(bin, *docker, target)
	if doInstall {
		return runRegistration(target, addArgs)
	}
	return printRegistration(bin, *docker, target, addArgs)
}

// registrationArgs builds the argument vector for `<client> mcp add` (excluding the
// leading client executable).
func registrationArgs(bin string, docker bool, client string) []string {
	args := []string{"mcp", "add", mcpServerName}
	if docker {
		envFlag := "-e"
		if client == "codex" {
			envFlag = "--env"
		}
		for _, kv := range sortedEnvPairs(postgresEnv()) {
			args = append(args, envFlag, kv)
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

func sortedEnvPairs(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, env[k]))
	}
	return pairs
}

func printRegistration(bin string, docker bool, client string, addArgs []string) int {
	mode := "native (SQLite)"
	if docker {
		mode = "Docker/Postgres"
	}
	clientLabel := map[string]string{
		"claude": "Claude Code",
		"codex":  "Codex",
	}[client]
	fmt.Printf("# Register mcp-code-search with %s — %s mode\n\n", clientLabel, mode)
	fmt.Printf("%s %s\n\n", client, shellJoin(addArgs))

	if docker {
		fmt.Println("# Requires the Postgres + Ollama stack to be running:")
		fmt.Println("#   docker compose --profile ollama up -d")
		fmt.Println()
	}

	if client == "claude" {
		fmt.Println("# Or add this to .mcp.json:")
		fmt.Println(mcpJSON(bin, docker))
		return 0
	}

	fmt.Println("# Or add this to ~/.codex/config.toml:")
	fmt.Println(codexTOML(bin, docker))
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

func codexTOML(bin string, docker bool) string {
	lines := []string{
		fmt.Sprintf("[mcp_servers.%s]", mcpServerName),
		fmt.Sprintf("command = %q", bin),
		`args = ["serve"]`,
	}
	if docker {
		lines = append(lines, "", fmt.Sprintf("[mcp_servers.%s.env]", mcpServerName))
		for _, kv := range sortedEnvPairs(postgresEnv()) {
			parts := strings.SplitN(kv, "=", 2)
			lines = append(lines, fmt.Sprintf("%s = %q", parts[0], parts[1]))
		}
	}
	return strings.Join(lines, "\n")
}

func runRegistration(client string, addArgs []string) int {
	if _, err := exec.LookPath(client); err != nil {
		return errf("`%s` CLI not found on PATH; run the printed command manually:\n%s %s", client, client, shellJoin(addArgs))
	}
	fmt.Fprintf(os.Stderr, "running: %s %s\n", client, shellJoin(addArgs))
	cmd := exec.Command(client, addArgs...)
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
