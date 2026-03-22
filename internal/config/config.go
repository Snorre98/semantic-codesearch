package config

import (
	"fmt"
	"os"
	"strconv"
)

const EmbeddingDimensions = 768

type Config struct {
	PGHost         string
	PGPort         int
	PGDatabase     string
	PGUser         string
	PGPassword     string
	OllamaBaseURL  string
	EmbeddingModel string
	MaxFileSizeKB  int
	BatchSize      int
}

func Load() Config {
	return Config{
		PGHost:         envOr("MCP_CS_PG_HOST", "localhost"),
		PGPort:         envOrInt("MCP_CS_PG_PORT", 5433),
		PGDatabase:     envOr("MCP_CS_PG_DATABASE", "codesearch"),
		PGUser:         envOr("MCP_CS_PG_USER", "codesearch"),
		PGPassword:     envOr("MCP_CS_PG_PASSWORD", "codesearch"),
		OllamaBaseURL:  envOr("MCP_CS_OLLAMA_URL", "http://localhost:11434"),
		EmbeddingModel: envOr("MCP_CS_EMBED_MODEL", "nomic-embed-text"),
		MaxFileSizeKB:  envOrInt("MCP_CS_MAX_FILE_KB", 512),
		BatchSize:      envOrInt("MCP_CS_BATCH_SIZE", 50),
	}
}

func (c Config) PGConnString() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		c.PGUser, c.PGPassword, c.PGHost, c.PGPort, c.PGDatabase)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
