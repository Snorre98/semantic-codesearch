package embeddings

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// BaseURL returns the Ollama endpoint this client talks to.
func (c *Client) BaseURL() string { return c.baseURL }

// Model returns the configured embedding model.
func (c *Client) Model() string { return c.model }

// Ping checks that the Ollama server is reachable via GET /api/version.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/version", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("ollama unreachable at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// Tags returns the names of the models currently pulled into Ollama (GET /api/tags).
func (c *Client) Tags(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode tags: %w", err)
	}
	names := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

// HasModel reports whether model is among Ollama's pulled models, tolerating the
// implicit ":latest" tag (so "nomic-embed-text" matches "nomic-embed-text:latest").
func (c *Client) HasModel(ctx context.Context, model string) (bool, error) {
	names, err := c.Tags(ctx)
	if err != nil {
		return false, err
	}
	return modelInList(model, names), nil
}

func modelInList(model string, names []string) bool {
	want := strings.TrimSuffix(model, ":latest")
	for _, n := range names {
		if n == model || strings.TrimSuffix(n, ":latest") == want {
			return true
		}
	}
	return false
}

// Pull downloads model into Ollama (POST /api/pull), streaming status lines to
// onProgress (which may be nil) as they arrive.
func (c *Client) Pull(ctx context.Context, model string, onProgress func(string)) error {
	body, err := json.Marshal(map[string]any{"model": model, "stream": true})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("pull %s: %w", model, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	last := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		}
		if json.Unmarshal([]byte(line), &msg) != nil {
			continue
		}
		if msg.Error != "" {
			return fmt.Errorf("pull %s: %s", model, msg.Error)
		}
		if onProgress != nil && msg.Status != "" && msg.Status != last {
			onProgress(msg.Status)
			last = msg.Status
		}
	}
	return scanner.Err()
}

// ProbeDim embeds a short probe string with the given model and returns the
// resulting vector's dimension — used by doctor to verify a model's output
// matches the fixed schema dimension before indexing against it.
func (c *Client) ProbeDim(ctx context.Context, model, text string) (int, error) {
	if text == "" {
		text = "dimension probe"
	}
	body, err := json.Marshal(embedRequest{Model: model, Input: []string{text}})
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("probe embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode embed response: %w", err)
	}
	if len(result.Embeddings) == 0 || len(result.Embeddings[0]) == 0 {
		return 0, fmt.Errorf("model %q returned no embedding", model)
	}
	return len(result.Embeddings[0]), nil
}
