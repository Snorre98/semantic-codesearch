package embeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"semantic-codesearch/internal/config"
)

// stubOllama spins up a fake Ollama server with the given handlers.
func stubOllama(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewClient(config.Config{OllamaBaseURL: srv.URL, EmbeddingModel: "nomic-embed-text"})
}

func TestPing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"version": "0.1.0"})
	})
	if err := stubOllama(t, mux).Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestTagsAndHasModel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"models": []map[string]string{
				{"name": "nomic-embed-text:latest"},
				{"name": "llama3:8b"},
			},
		})
	})
	c := stubOllama(t, mux)

	names, err := c.Tags(context.Background())
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 models, got %v", names)
	}

	// ":latest" tolerance: configured "nomic-embed-text" matches the tagged name.
	if has, _ := c.HasModel(context.Background(), "nomic-embed-text"); !has {
		t.Error("HasModel should match nomic-embed-text against nomic-embed-text:latest")
	}
	if has, _ := c.HasModel(context.Background(), "mistral"); has {
		t.Error("HasModel should not match an absent model")
	}
}

func TestProbeDim(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/embed", func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		json.NewDecoder(r.Body).Decode(&req)
		// Return a 768-dim vector regardless of input.
		vec := make([]float32, config.EmbeddingDimensions)
		json.NewEncoder(w).Encode(embedResponse{Embeddings: [][]float32{vec}})
	})
	dim, err := stubOllama(t, mux).ProbeDim(context.Background(), "nomic-embed-text", "")
	if err != nil {
		t.Fatalf("ProbeDim: %v", err)
	}
	if dim != config.EmbeddingDimensions {
		t.Errorf("ProbeDim = %d, want %d", dim, config.EmbeddingDimensions)
	}
}

func TestProbeDimMismatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/embed", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(embedResponse{Embeddings: [][]float32{make([]float32, 1024)}})
	})
	dim, err := stubOllama(t, mux).ProbeDim(context.Background(), "big-model", "")
	if err != nil {
		t.Fatalf("ProbeDim: %v", err)
	}
	if dim == config.EmbeddingDimensions {
		t.Error("expected a non-768 dimension to surface the mismatch")
	}
}

func TestPingUnreachable(t *testing.T) {
	c := NewClient(config.Config{OllamaBaseURL: "http://127.0.0.1:0", EmbeddingModel: "x"})
	if err := c.Ping(context.Background()); err == nil {
		t.Error("Ping should fail against an unreachable server")
	}
}
