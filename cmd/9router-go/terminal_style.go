package main

import (
	"io"
	"os"
	"strings"
)

// terminalColors enables decoration only for an interactive stdout. Redirected
// output, CI logs, NO_COLOR and TERM=dumb always receive plain text.
func terminalColors(out io.Writer) bool {
	if out != os.Stdout || !isTTY(os.Stdout) {
		return false
	}
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled {
		return false
	}
	term := os.Getenv("TERM")
	return term != "" && !strings.EqualFold(term, "dumb")
}

func terminalColor(out io.Writer, text, code string) string {
	if !terminalColors(out) {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}
