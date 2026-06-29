package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"semantic-codesearch/cmd/index"
	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/manage"
	"semantic-codesearch/internal/store"
)

// cmdList handles `list [--all] [--json]`.
func cmdList(args []string) int {
	fs := newFlagSet("list")
	all := fs.Bool("all", false, "include deprecated codebases")
	asJSON := fs.Bool("json", false, "output JSON")
	if _, code, ok := parse(fs, args); !ok {
		return code
	}
	cfg := config.Load()
	ctx := context.Background()
	mgr, err := manage.NewManager(ctx, cfg)
	if err != nil {
		return errf("%v", err)
	}
	defer mgr.Close()

	list, err := mgr.List(ctx, *all)
	if err != nil {
		return errf("%v", err)
	}
	if *asJSON {
		return printJSON(list)
	}
	if len(list) == 0 {
		fmt.Fprintln(os.Stderr, "no codebases indexed yet")
		return 0
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ROOT\tFILES\tCHUNKS\tSIZE\tMODEL\tMATCHES\tLAST INDEXED\tSTATUS")
	for _, c := range list {
		status := c.Status
		if c.Reason != "" {
			status = fmt.Sprintf("%s (%s)", c.Status, c.Reason)
		}
		size := "-"
		if c.SizeBytes > 0 {
			size = humanSize(c.SizeBytes)
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\t%s\t%s\t%s\n",
			c.Root, c.Files, c.Chunks, size, c.Model, yesno(c.ModelMatches), c.LastIndexed, status)
	}
	tw.Flush()
	return 0
}

// cmdInfo handles `info <root> [--json]`.
func cmdInfo(args []string) int {
	fs := newFlagSet("info")
	asJSON := fs.Bool("json", false, "output JSON")
	pos, code, ok := parse(fs, args)
	if !ok {
		return code
	}
	if len(pos) < 1 {
		return errf("usage: mcp-code-search info <root> [--json]")
	}
	cfg := config.Load()
	ctx := context.Background()
	mgr, err := manage.NewManager(ctx, cfg)
	if err != nil {
		return errf("%v", err)
	}
	defer mgr.Close()

	cb, err := mgr.Info(ctx, pos[0])
	if err != nil {
		return errf("%v", err)
	}
	if *asJSON {
		return printJSON(cb)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	row := func(k, v string) { fmt.Fprintf(tw, "%s:\t%s\n", k, v) }
	row("root", cb.Root)
	row("status", cb.Status)
	if cb.Reason != "" {
		row("deprecated", fmt.Sprintf("%s (%s)", cb.DeprecatedAt, cb.Reason))
	}
	if cb.DBFile != "" {
		row("db file", cb.DBFile)
		row("db exists", yesno(cb.DBExists))
		row("size on disk", humanSize(cb.SizeBytes))
	}
	row("model", cb.Model)
	row("dims", fmt.Sprintf("%d", cb.Dims))
	row("matches config", yesno(cb.ModelMatches))
	if cb.MatchReason != "" {
		row("mismatch", cb.MatchReason)
	}
	row("files", fmt.Sprintf("%d", cb.Files))
	row("chunks", fmt.Sprintf("%d", cb.Chunks))
	row("last indexed", cb.LastIndexed)
	row("root exists", yesno(cb.OnDisk))
	tw.Flush()
	return 0
}

// cmdRemove handles `remove|forget <root> [--force]`.
func cmdRemove(args []string) int {
	fs := newFlagSet("remove")
	force := fs.Bool("force", false, "skip confirmation")
	pos, code, ok := parse(fs, args)
	if !ok {
		return code
	}
	if len(pos) < 1 {
		return errf("usage: mcp-code-search remove <root> [--force]")
	}
	root := pos[0]
	cfg := config.Load()
	ctx := context.Background()
	mgr, err := manage.NewManager(ctx, cfg)
	if err != nil {
		return errf("%v", err)
	}
	defer mgr.Close()

	if !*force && !confirm(fmt.Sprintf("Deprecate codebase %q? Its embeddings are kept until `prune --purge`.", root)) {
		fmt.Fprintln(os.Stderr, "aborted")
		return 1
	}
	if err := mgr.Deprecate(ctx, root, store.ReasonRemoved); err != nil {
		return errf("%v", err)
	}
	fmt.Fprintf(os.Stderr, "deprecated %q (data retained; run `prune --purge` to reclaim disk)\n", root)
	return 0
}

// cmdRebuild handles `rebuild <root> [--reembed] [--force]`.
func cmdRebuild(args []string) int {
	fs := newFlagSet("rebuild")
	reembed := fs.Bool("reembed", false, "archive the old index and re-embed with the current model")
	force := fs.Bool("force", false, "skip confirmation")
	pos, code, ok := parse(fs, args)
	if !ok {
		return code
	}
	if len(pos) < 1 {
		return errf("usage: mcp-code-search rebuild <root> [--reembed] [--force]")
	}
	root := pos[0]
	cfg := config.Load()
	ctx := context.Background()

	if *reembed {
		if !*force && !confirm(fmt.Sprintf("Re-embed %q with model %q? The old index is archived as deprecated.", root, cfg.EmbeddingModel)) {
			fmt.Fprintln(os.Stderr, "aborted")
			return 1
		}
		if err := deprecateForReembed(cfg, root); err != nil {
			return errf("%v", err)
		}
		if err := index.Run(root); err != nil {
			return errf("%v", err)
		}
		return 0
	}

	// Plain rebuild: drop the existing data and re-index in place with the same
	// model. GuardModel inside index.Run still protects against a stale mismatch.
	if !*force && !confirm(fmt.Sprintf("Drop and re-index %q from scratch?", root)) {
		fmt.Fprintln(os.Stderr, "aborted")
		return 1
	}
	mgr, err := manage.NewManager(ctx, cfg)
	if err != nil {
		return errf("%v", err)
	}
	if err := mgr.DropData(ctx, root); err != nil {
		mgr.Close()
		return errf("%v", err)
	}
	mgr.Close()
	if err := index.Run(root); err != nil {
		return errf("%v", err)
	}
	return 0
}

// cmdPrune handles `prune [--purge] [--force] [--json]`.
func cmdPrune(args []string) int {
	fs := newFlagSet("prune")
	purge := fs.Bool("purge", false, "also permanently delete deprecated codebases' data")
	force := fs.Bool("force", false, "skip confirmation")
	asJSON := fs.Bool("json", false, "output JSON")
	if _, code, ok := parse(fs, args); !ok {
		return code
	}
	cfg := config.Load()
	ctx := context.Background()
	mgr, err := manage.NewManager(ctx, cfg)
	if err != nil {
		return errf("%v", err)
	}
	defer mgr.Close()

	repaired, err := mgr.Prune(ctx)
	if err != nil {
		return errf("%v", err)
	}

	var purged []manage.Codebase
	if *purge {
		if !*force && !confirm("Permanently delete all deprecated codebases' data?") {
			fmt.Fprintln(os.Stderr, "aborted purge (stale entries were still deprecated)")
		} else {
			purged, err = mgr.Purge(ctx)
			if err != nil {
				return errf("%v", err)
			}
		}
	}

	if *asJSON {
		if repaired == nil {
			repaired = []manage.Codebase{}
		}
		if purged == nil {
			purged = []manage.Codebase{}
		}
		return printJSON(map[string]any{"deprecated": repaired, "purged": purged})
	}
	fmt.Fprintf(os.Stderr, "deprecated %d stale codebase(s)", len(repaired))
	if *purge {
		fmt.Fprintf(os.Stderr, ", purged %d deprecated codebase(s)", len(purged))
	}
	fmt.Fprintln(os.Stderr)
	for _, c := range repaired {
		fmt.Fprintf(os.Stderr, "  deprecated: %s\n", c.Root)
	}
	for _, c := range purged {
		fmt.Fprintf(os.Stderr, "  purged: %s\n", c.Root)
	}
	return 0
}
