// Command yo turns a natural-language request into a PowerShell command (or a
// chat answer) via an LLM, emitting exactly one JSON line on stdout for the
// shell integration snippet to prefill or print. See docs/DESIGN-NOTES.md.
//
// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
)

// Result is the normalized outcome, emitted as exactly one JSON line on stdout.
// The shell snippet switches on Type: "command" → prefill, "chat" → print,
// "error" → show the message.
type Result struct {
	Type        string `json:"type"` // "command" | "chat" | "error"
	Command     string `json:"command,omitempty"`
	Explanation string `json:"explanation,omitempty"`
	Response    string `json:"response,omitempty"`
	Message     string `json:"message,omitempty"` // populated when Type == "error"
}

func main() {
	dryRun := flag.Bool("dry-run", false, "print the assembled API request to stdout and exit (no network or key needed)")
	check := flag.Bool("check", false, "validate config and the API key (decode + charset), no network, then exit")
	flag.Parse()

	if *check {
		runCheck()
		return
	}

	query := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if query == "" {
		// Allow piping: `echo "list pdfs" | yo`
		if b, err := io.ReadAll(os.Stdin); err == nil {
			query = strings.TrimSpace(string(b))
		}
	}
	if query == "" {
		emit(Result{Type: "error", Message: "no query given (usage: yo <natural language>)"})
		os.Exit(2)
	}

	cfg, err := loadConfig()
	if err != nil {
		emit(Result{Type: "error", Message: err.Error()})
		os.Exit(1)
	}

	if *dryRun {
		body, err := buildAnthropicRequest(cfg, query)
		if err != nil {
			emit(Result{Type: "error", Message: err.Error()})
			os.Exit(1)
		}
		os.Stdout.Write(body)
		fmt.Fprintln(os.Stdout)
		return
	}

	if err := cfg.ready(); err != nil {
		emit(Result{Type: "error", Message: err.Error()})
		os.Exit(1)
	}

	// Ctrl-C cancels the in-flight request (M3 polish, cheap to wire now).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Fprint(os.Stderr, "thinking…")
	res, err := callAnthropic(ctx, cfg, query)
	fmt.Fprint(os.Stderr, "\r         \r") // clear the transient indicator
	if err != nil {
		emit(Result{Type: "error", Message: err.Error()})
		os.Exit(1)
	}
	emit(res)
}

// runCheck validates config and the decoded API key without any network call —
// handy when fighting key-file encoding issues. It never prints the key itself.
func runCheck() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	if err := cfg.ready(); err != nil {
		fmt.Fprintln(os.Stderr, "key error:", err)
		os.Exit(1)
	}
	fmt.Printf("OK  provider=%s  model=%s  key=%d chars (decoded & valid)\n", cfg.Provider, cfg.Model, len(cfg.Key))
}

// emit writes a Result as one JSON line to stdout. Errors are emitted in the
// same shape so the snippet can parse every outcome uniformly. HTML escaping is
// off so command strings keep their literal >, <, & (ubiquitous in PowerShell).
func emit(r Result) {
	if err := encodeResult(os.Stdout, r); err != nil {
		fmt.Fprintln(os.Stdout, `{"type":"error","message":"failed to encode result"}`)
	}
}

// encodeResult writes r as one JSON line with HTML escaping off, so command
// strings keep their literal >, <, & (ubiquitous in PowerShell).
func encodeResult(w io.Writer, r Result) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}
