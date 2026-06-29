package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// errf prints a message to stderr and returns exit code 1.
func errf(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	return 1
}

// printJSON writes v as indented JSON to stdout and returns exit code 0.
func printJSON(v any) int {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errf("error: %v", err)
	}
	fmt.Fprintln(os.Stdout, string(b))
	return 0
}

// confirm prompts on stderr and returns true only if the user types y/yes.
// With --force callers skip this entirely.
func confirm(prompt string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// humanSize renders a byte count as a compact human-readable string.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// yesno renders a boolean as a short yes/no marker.
func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
