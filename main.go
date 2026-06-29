package main

import (
	"os"

	"semantic-codesearch/internal/cli"
)

func main() {
	os.Exit(cli.Dispatch(os.Args[1:]))
}
