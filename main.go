package main

import (
	"log"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/server"
)

func main() {
	cfg := config.Load()
	if err := server.Run(cfg); err != nil {
		log.Fatal(err)
	}
}
