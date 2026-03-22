package main

import (
	"fmt"
	"log"
	"os"

	"semantic-codesearch/cmd/index"
	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/server"
)

func main() {
	sub := ""
	if len(os.Args) > 1 {
		sub = os.Args[1]
	}

	switch sub {
	case "index":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: mcp-code-search index <directory>")
			os.Exit(1)
		}
		if err := index.Run(os.Args[2]); err != nil {
			log.Fatal(err)
		}

	case "", "serve":
		cfg := config.Load()
		if err := server.Run(cfg); err != nil {
			log.Fatal(err)
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", sub)
		fmt.Fprintln(os.Stderr, "usage: mcp-code-search [serve | index <directory>]")
		os.Exit(1)
	}
}
