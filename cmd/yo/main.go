// Command yo turns a natural-language request into a PowerShell command (or a
// chat answer) via an LLM, emitting exactly one JSON line on stdout for the
// shell integration snippet to prefill or print. See docs/DESIGN-NOTES.md.
//
// Multi-step tasks: a command may come back with "pending":true and a "state"
// blob. The snippet stashes the state in $env:YO_STATE and, after the user runs
// the command, calls `yo --continue --exit <code>` for the next step. The binary
// stays pure: prior state in via $env:YO_STATE, new state out via the result.
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

	"github.com/martona/yo/internal/config"
	"github.com/martona/yo/internal/llm"
	"github.com/martona/yo/internal/scrollback"
)

// scrollbackMaxLines caps how much recent terminal output we fold into a query
// when running inside a multiplexer (zellij).
const scrollbackMaxLines = 200

func main() {
	dryRun := flag.Bool("dry-run", false, "print the assembled API request to stdout and exit (no network or key needed)")
	check := flag.Bool("check", false, "validate config and the API key (no network), then exit")
	cont := flag.Bool("continue", false, "continuation step; reads $env:YO_STATE (used by the shell integration)")
	exitCode := flag.Int("exit", 0, "exit code of the just-run command (with --continue)")
	dumpSB := flag.Bool("scrollback", false, "print the captured terminal scrollback and exit (debug)")
	flag.Parse()

	switch {
	case *check:
		runCheck()
		return
	case *cont:
		runContinue(*exitCode, *dryRun)
		return
	case *dumpSB:
		fmt.Fprintf(os.Stderr, "ZELLIJ=%q\n", os.Getenv("ZELLIJ"))
		out := scrollback.Capture(scrollbackMaxLines)
		fmt.Fprintf(os.Stderr, "captured %d chars\n", len(out))
		fmt.Print(out)
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
		emit(llm.Result{Type: "error", Message: "no query given (usage: yo <natural language>)"})
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		emit(llm.Result{Type: "error", Message: err.Error()})
		os.Exit(1)
	}
	provider, err := llm.New(cfg)
	if err != nil {
		emit(llm.Result{Type: "error", Message: err.Error()})
		os.Exit(1)
	}

	// Opportunistic terminal context: when running inside zellij, fold recent
	// screen output into the query so "why did that fail?" works. No-op otherwise.
	query = llm.WithTerminalContext(query, scrollback.Capture(scrollbackMaxLines))

	if *dryRun {
		body, err := provider.Request(query)
		if err != nil {
			emit(llm.Result{Type: "error", Message: err.Error()})
			os.Exit(1)
		}
		os.Stdout.Write(body)
		fmt.Fprintln(os.Stdout)
		return
	}

	if err := cfg.Ready(); err != nil {
		emit(llm.Result{Type: "error", Message: err.Error()})
		os.Exit(1)
	}

	res, err := generate(provider, query)
	if err != nil {
		emit(llm.Result{Type: "error", Message: err.Error()})
		os.Exit(1)
	}

	// A pending command opens a fresh continuation chain.
	if res.Type == "command" && res.Pending {
		st := &llm.State{Query: query, Steps: []llm.Step{{Command: res.Command, Explanation: res.Explanation}}}
		if enc, err := st.Encode(); err == nil {
			res.State = enc
		}
	}
	emit(res)
}

// runContinue performs the next step of a continuation: it reads the chain from
// $env:YO_STATE, tells the model the previous command's exit code, and returns
// the next command (or a chat). State out via the result for the snippet to
// restash; an empty state means the chain is done.
func runContinue(exitCode int, dryRun bool) {
	st, err := llm.DecodeState(os.Getenv("YO_STATE"))
	if err != nil {
		emit(llm.Result{Type: "error", Message: err.Error()})
		os.Exit(1)
	}
	if st == nil {
		emit(llm.Result{Type: "error", Message: "no continuation in progress"})
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		emit(llm.Result{Type: "error", Message: err.Error()})
		os.Exit(1)
	}
	provider, err := llm.New(cfg)
	if err != nil {
		emit(llm.Result{Type: "error", Message: err.Error()})
		os.Exit(1)
	}

	st.SetLastExit(exitCode)
	query := st.ContinuationQuery(exitCode)

	if dryRun {
		body, err := provider.Request(query)
		if err != nil {
			emit(llm.Result{Type: "error", Message: err.Error()})
			os.Exit(1)
		}
		os.Stdout.Write(body)
		fmt.Fprintln(os.Stdout)
		return
	}

	if err := cfg.Ready(); err != nil {
		emit(llm.Result{Type: "error", Message: err.Error()})
		os.Exit(1)
	}

	res, err := generate(provider, query)
	if err != nil {
		emit(llm.Result{Type: "error", Message: err.Error()})
		os.Exit(1)
	}

	if res.Type == "command" {
		st.AddStep(res.Command, res.Explanation)
		if res.Pending {
			if enc, err := st.Encode(); err == nil {
				res.State = enc
			}
		}
	}
	// chat, or a non-pending command, leaves res.State empty -> snippet clears YO_STATE.
	emit(res)
}

// generate runs the provider call with a thinking indicator and Ctrl-C cancel.
func generate(p llm.Provider, query string) (llm.Result, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	fmt.Fprint(os.Stderr, "thinking...")
	res, err := p.Generate(ctx, query)
	fmt.Fprint(os.Stderr, "\r            \r") // clear the transient indicator
	return res, err
}

// runCheck validates config and the API key without any network call.
func runCheck() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	if err := cfg.Ready(); err != nil {
		fmt.Fprintln(os.Stderr, "key error:", err)
		os.Exit(1)
	}
	fmt.Printf("OK  provider=%s  model=%s  key=%d chars (decoded & valid)\n", cfg.Provider, cfg.Model, len(cfg.Key))
}

// emit writes a Result as one JSON line to stdout. Errors are emitted in the
// same shape so the snippet can parse every outcome uniformly.
func emit(r llm.Result) {
	if err := encodeResult(os.Stdout, r); err != nil {
		fmt.Fprintln(os.Stdout, `{"type":"error","message":"failed to encode result"}`)
	}
}

// encodeResult writes r as one JSON line with HTML escaping off, so command
// strings keep their literal >, <, & (ubiquitous in PowerShell).
func encodeResult(w io.Writer, r llm.Result) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}
