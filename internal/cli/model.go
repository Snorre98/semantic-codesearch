package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/embeddings"
	"semantic-codesearch/internal/manage"
)

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | fail
	Detail string `json:"detail"`
}

// cmdDoctor handles `doctor|check [--json]`.
func cmdDoctor(args []string) int {
	fs := newFlagSet("doctor")
	asJSON := fs.Bool("json", false, "output JSON")
	if _, code, ok := parse(fs, args); !ok {
		return code
	}
	cfg := config.Load()
	ctx := context.Background()
	embedder := embeddings.NewClient(cfg)

	var checks []check
	hardFail := false
	add := func(name, status, detail string) {
		checks = append(checks, check{Name: name, Status: status, Detail: detail})
		if status == "fail" {
			hardFail = true
		}
	}

	add("backend", "ok", backendName(cfg))
	add("ollama url", "ok", cfg.OllamaBaseURL)

	// 1. Reachability.
	if err := embedder.Ping(ctx); err != nil {
		add("ollama reachable", "fail", err.Error())
		// Skip the model/dim probes that depend on a live server.
		return reportDoctor(checks, hardFail, *asJSON)
	}
	add("ollama reachable", "ok", "responded to /api/version")

	// 2. Configured model pulled?
	if has, err := embedder.HasModel(ctx, cfg.EmbeddingModel); err != nil {
		add("model pulled", "fail", err.Error())
	} else if !has {
		add("model pulled", "fail", fmt.Sprintf("%q not found — run `mcp-code-search pull %s`", cfg.EmbeddingModel, cfg.EmbeddingModel))
	} else {
		add("model pulled", "ok", cfg.EmbeddingModel)

		// 3. Dimension probe (only meaningful if the model is present).
		if dim, err := embedder.ProbeDim(ctx, cfg.EmbeddingModel, ""); err != nil {
			add("embedding dimension", "fail", err.Error())
		} else if dim != config.EmbeddingDimensions {
			add("embedding dimension", "fail",
				fmt.Sprintf("model emits %d dims but schema is fixed at %d — this model is not indexable", dim, config.EmbeddingDimensions))
		} else {
			add("embedding dimension", "ok", fmt.Sprintf("%d", dim))
		}
	}

	// 4. Registry / codebase health.
	if mgr, err := manage.NewManager(ctx, cfg); err != nil {
		add("codebase registry", "warn", err.Error())
	} else {
		defer mgr.Close()
		list, lerr := mgr.List(ctx, true)
		if lerr != nil {
			add("codebase registry", "warn", lerr.Error())
		} else {
			mismatch, missing, deprecated := 0, 0, 0
			for _, c := range list {
				switch {
				case c.Status != "active":
					deprecated++
				case !c.ModelMatches:
					mismatch++
				case !c.OnDisk || !c.DBExists:
					missing++
				}
			}
			detail := fmt.Sprintf("%d active, %d deprecated, %d model-mismatch, %d missing on disk",
				len(list)-deprecated, deprecated, mismatch, missing)
			status := "ok"
			if mismatch > 0 || missing > 0 {
				status = "warn"
			}
			add("codebase registry", status, detail)
		}
	}

	return reportDoctor(checks, hardFail, *asJSON)
}

func reportDoctor(checks []check, hardFail, asJSON bool) int {
	if asJSON {
		printJSON(checks)
		if hardFail {
			return 1
		}
		return 0
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, c := range checks {
		marker := map[string]string{"ok": "✓", "warn": "!", "fail": "✗"}[c.Status]
		fmt.Fprintf(tw, "%s %s\t%s\n", marker, c.Name, c.Detail)
	}
	tw.Flush()
	if hardFail {
		return 1
	}
	return 0
}

// cmdPull handles `pull <model>`.
func cmdPull(args []string) int {
	fs := newFlagSet("pull")
	pos, code, ok := parse(fs, args)
	if !ok {
		return code
	}
	if len(pos) < 1 {
		return errf("usage: mcp-code-search pull <model>")
	}
	model := pos[0]
	cfg := config.Load()
	ctx := context.Background()
	embedder := embeddings.NewClient(cfg)

	fmt.Fprintf(os.Stderr, "pulling %q from %s ...\n", model, cfg.OllamaBaseURL)
	if err := embedder.Pull(ctx, model, func(status string) {
		fmt.Fprintf(os.Stderr, "  %s\n", status)
	}); err != nil {
		return errf("%v", err)
	}

	dim, err := embedder.ProbeDim(ctx, model, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "pulled %q (could not probe dimension: %v)\n", model, err)
		return 0
	}
	if dim != config.EmbeddingDimensions {
		fmt.Fprintf(os.Stderr, "pulled %q, but it emits %d dims and the schema is fixed at %d — not usable for indexing without a schema change\n",
			model, dim, config.EmbeddingDimensions)
		return 0
	}
	fmt.Fprintf(os.Stderr, "pulled %q (%d dims) — ready to use; set MCP_CS_EMBED_MODEL=%s\n", model, dim, model)
	return 0
}

func backendName(cfg config.Config) string {
	if cfg.Backend == "" {
		return "sqlite"
	}
	return cfg.Backend
}
