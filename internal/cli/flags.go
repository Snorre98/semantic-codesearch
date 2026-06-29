package cli

import (
	"context"
	"flag"
	"strings"

	"semantic-codesearch/internal/config"
	"semantic-codesearch/internal/manage"
	"semantic-codesearch/internal/store"
)

// newFlagSet builds a per-command flag set that reports errors to stderr.
func newFlagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}

// parse runs fs.Parse, allowing flags to appear before or after positional
// arguments (Go's flag package otherwise stops at the first non-flag). It returns
// the collected positionals and (exitCode, ok): on -h, (0, false); on a bad flag,
// (2, false).
func parse(fs *flag.FlagSet, args []string) ([]string, int, bool) {
	var positional []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			if err == flag.ErrHelp {
				return nil, 0, false
			}
			return nil, 2, false
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	return positional, 0, true
}

// deprecateForReembed archives an existing active codebase ahead of a model
// switch. A not-registered codebase is fine (first index) and reported as nil.
func deprecateForReembed(cfg config.Config, dir string) error {
	ctx := context.Background()
	mgr, err := manage.NewManager(ctx, cfg)
	if err != nil {
		return err
	}
	defer mgr.Close()
	if err := mgr.Deprecate(ctx, dir, store.ReasonModelSwitch); err != nil {
		if strings.Contains(err.Error(), "no active codebase") || strings.Contains(err.Error(), "no codebase registered") {
			return nil
		}
		return err
	}
	return nil
}
