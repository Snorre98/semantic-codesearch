// Package cli is the command surface for the mcp-code-search binary. It dispatches
// hand-rolled subcommands (no framework) and wires them to the indexer, MCP
// server, the manage package, and the embeddings client.
package cli

import (
	"fmt"
	"os"

	"semantic-codesearch/cmd/index"
	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/server"
)

const usage = `mcp-code-search — semantic code search (MCP server + management CLI)

Usage: mcp-code-search <command> [arguments]

Server & indexing:
  serve                       Run the MCP stdio server (default when no command)
  index <dir> [--reembed]     Index a codebase directory

Codebase management:
  list [--all] [--json]       List indexed codebases
  info <root> [--json]        Show details for one codebase
  remove <root> [--force]     Deprecate a codebase (keeps data; alias: forget)
  rebuild <root> [--reembed]  Re-index a codebase (--reembed to switch models)
  prune [--purge] [--force]   Deprecate stale codebases; --purge deletes deprecated data

Embedding model:
  doctor [--json]             Check Ollama, model, and embedding dimension (alias: check)
  pull <model>                Pull an embedding model into Ollama

MCP registration:
  mcp [--client codex] [--docker]
                              Print the registration command and config snippet
  mcp install [--client codex] [--docker]
                              Run 'claude mcp add' or 'codex mcp add' for this binary

  help                        Show this help

Configuration is via MCP_CS_* environment variables (see README).`

// Dispatch routes a command line (os.Args[1:]) and returns a process exit code.
func Dispatch(args []string) int {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	rest := args[1:]

	switch sub {
	case "", "serve":
		cfg := config.Load()
		if err := server.Run(cfg); err != nil {
			return errf("%v", err)
		}
		return 0

	case "index":
		return cmdIndex(rest)

	case "list":
		return cmdList(rest)
	case "info":
		return cmdInfo(rest)
	case "remove", "forget":
		return cmdRemove(rest)
	case "rebuild":
		return cmdRebuild(rest)
	case "prune":
		return cmdPrune(rest)

	case "doctor", "check":
		return cmdDoctor(rest)
	case "pull":
		return cmdPull(rest)

	case "mcp":
		return cmdMCP(rest)

	case "help", "-h", "--help":
		fmt.Fprintln(os.Stdout, usage)
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n%s\n", sub, usage)
		return 2
	}
}

// cmdIndex handles `index <dir> [--reembed]`.
func cmdIndex(args []string) int {
	fs := newFlagSet("index")
	reembed := fs.Bool("reembed", false, "archive the existing index and re-embed with the current model")
	pos, code, ok := parse(fs, args)
	if !ok {
		return code
	}
	if len(pos) < 1 {
		return errf("usage: mcp-code-search index <directory> [--reembed]")
	}
	dir := pos[0]
	cfg := config.Load()
	if *reembed {
		if err := deprecateForReembed(cfg, dir); err != nil {
			return errf("%v", err)
		}
	}
	if err := index.Run(dir); err != nil {
		return errf("%v", err)
	}
	return 0
}
