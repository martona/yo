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
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/martona/yo/internal/config"
	"github.com/martona/yo/internal/llm"
	"github.com/martona/yo/internal/redact"
	"github.com/martona/yo/internal/scrollback"
	"github.com/martona/yo/internal/session"
	"github.com/martona/yo/internal/textwrap"
	"github.com/martona/yo/shell"
)

// scrollbackMaxLines caps how much recent terminal output we fold into a query
// when running inside a multiplexer (zellij).
const scrollbackMaxLines = 200

// displayWidth is the column width for wrapping prose output (explanation, chat,
// error message), supplied by the shell via --width; 0 disables wrapping. Wrapping
// lives in the binary so every shell integration shares one implementation -- the
// snippet only has to report its terminal width.
var displayWidth int

// version is the binary version, set via -ldflags "-X main.version=<tag>" in CI;
// "dev" for local/un-tagged builds (with a runtime/debug BuildInfo fallback).
var version = "dev"

func main() {
	dryRun := flag.Bool("dry-run", false, "print the assembled API request to stdout and exit (no network or key needed)")
	check := flag.Bool("check", false, "validate config and the API key (no network), then exit")
	cont := flag.Bool("continue", false, "continuation step; reads $env:YO_STATE (used by the shell integration)")
	exitCode := flag.Int("exit", 0, "exit code of the just-run command (with --continue)")
	dumpSB := flag.Bool("scrollback", false, "print the captured terminal scrollback and exit (debug)")
	width := flag.Int("width", 0, "wrap prose output to this column width (0 = no wrap; set by the shell integration)")
	versionFlag := flag.Bool("version", false, "print the version and exit")
	configFlag := flag.Bool("config", false, "show the resolved configuration and exit")
	initFlag := flag.String("init", "", "print the shell integration for <shell> (powershell) and exit")
	flag.Usage = usage
	flag.Parse()
	displayWidth = *width

	switch {
	case *versionFlag:
		fmt.Println(versionString())
		return
	case *initFlag != "":
		runInit(*initFlag)
		return
	case *configFlag:
		runConfig()
		return
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
	rawQuery := query // the user's actual ask, before any context augmentation

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

	// Opportunistic terminal context (redacted): fold recent screen output into the
	// query so "why did that fail?" works. No-op outside zellij.
	query = withScrollback(query)

	// Cross-call memory: prepend a compact history of recent yo exchanges (no-op when
	// disabled or empty). Applied after scrollback so the framing nests to a single
	// [request]. The exchange itself is recorded after we have a result, below.
	if cfg.Memory {
		query = llm.WithSessionMemory(query, session.Render(session.Recent(os.Getenv("YO_SESSION"))))
	}

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

	// A pending command opens a fresh continuation chain (stored under the RAW
	// query, not the context-augmented one). A terminal result -- a chat or a
	// non-pending command -- is instead recorded to session memory now.
	if res.Type == "command" && res.Pending {
		st := &llm.State{Query: rawQuery, Steps: []llm.Step{{Command: res.Command, Explanation: res.Explanation}}}
		if enc, err := st.Encode(); err == nil {
			res.State = enc
		}
	} else if cfg.Memory {
		recordResult(rawQuery, res)
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
	st.SetLastExecuted(os.Getenv("YO_RAN")) // the command the user actually ran (edits included)
	query := st.ContinuationQuery(exitCode)

	// Opportunistic terminal context (redacted): by the time --continue fires, the
	// just-run step's command and output are on screen, so folding the capture in
	// lets the model react to real output, not just the exit code. No-op outside
	// zellij; symmetric with the initial query path in main().
	query = withScrollback(query)

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
	// chat, or a non-pending command, leaves res.State empty -> snippet clears
	// YO_STATE. That also ends the chain, so record the completed task to memory.
	if cfg.Memory && !(res.Type == "command" && res.Pending) {
		session.Append(os.Getenv("YO_SESSION"), session.Exchange{
			Query: st.Query, Type: "command", Steps: st.Steps,
		})
	}
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

// withScrollback folds redacted terminal scrollback into the query. Capture is a
// no-op (empty) outside zellij, so there we return the query untouched and never
// even build the redactor. When there is output, secrets are scrubbed before it
// leaves the machine; if any were found, a one-line note goes to stderr (stdout is
// the JSON contract). Fails closed: if the redactor cannot be built we drop the
// scrollback rather than send it raw.
func withScrollback(query string) string {
	raw := scrollback.Capture(scrollbackMaxLines)
	if raw == "" {
		return query
	}
	r, err := redact.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "yo: redaction unavailable (%v); skipping terminal context\n", err)
		return query
	}
	res := r.Redact(raw)
	if res.Count > 0 {
		noun := "secrets"
		if res.Count == 1 {
			noun = "secret"
		}
		fmt.Fprintf(os.Stderr, "yo: redacted %d %s (%s)\n", res.Count, noun, strings.Join(res.Kinds, ", "))
	}
	return llm.WithTerminalContext(query, res.Text)
}

// recordResult appends a terminal (non-pending) exchange -- a standalone command or
// a chat -- to session memory. Callers gate on cfg.Memory; Append itself no-ops on an
// empty session id or an unrecordable type.
func recordResult(query string, res llm.Result) {
	ex := session.Exchange{Query: query, Type: res.Type}
	switch res.Type {
	case "command":
		ex.Steps = []llm.Step{{Command: res.Command, Explanation: res.Explanation}}
	case "chat":
		ex.Response = res.Response
	default:
		return
	}
	session.Append(os.Getenv("YO_SESSION"), ex)
}

// usage prints curated help (set as flag.Usage, so it also drives -h / --help).
func usage() {
	fmt.Print(`yo - natural-language command assistant for PowerShell.

Usage:
  yo <natural language>     Get a command prefilled at your prompt, or a chat answer.
  yo --init powershell      Print the shell integration (for your $PROFILE).
  yo --version              Print the version.
  yo --check                Validate config and the API key (no network).
  yo --config               Show the resolved configuration.
  yo --dry-run <text>       Print the assembled API request (no key or network).

One-time setup:
  Add to your $PROFILE:
      if (Get-Command yo -ErrorAction SilentlyContinue) { yo --init powershell | Out-String | iex }
  Set an API key:
      $env:ANTHROPIC_API_KEY = "sk-ant-..."   (or OPENAI_API_KEY)

Config file: ~/.yoconf (provider, model, key, base_url, memory).
Safety: nothing runs until you read the command and press Enter.
`)
}

// versionString resolves the build version: the -ldflags value if set, else the
// module version or short VCS revision from build info, else "dev".
func versionString() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				rev := s.Value
				if len(rev) > 12 {
					rev = rev[:12]
				}
				return "dev+" + rev
			}
		}
	}
	return version
}

// runInit prints the embedded shell-integration snippet for the named shell, to be
// sourced from the user's profile (e.g. `yo --init powershell | Out-String | iex`).
func runInit(shellName string) {
	switch strings.ToLower(shellName) {
	case "powershell", "pwsh":
		fmt.Print(shell.PowerShell)
	default:
		fmt.Fprintf(os.Stderr, "yo: unknown shell %q (supported: powershell)\n", shellName)
		os.Exit(2)
	}
}

// runConfig prints the resolved configuration (no network, no key required).
func runConfig() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	key := "not set"
	if cfg.Key != "" {
		key = fmt.Sprintf("set (%d chars)", len(cfg.Key))
	}
	mem := "off"
	if cfg.Memory {
		mem = "on"
	}
	yoconf := "not found"
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".yoconf")
		if _, statErr := os.Stat(p); statErr == nil {
			yoconf = p
		}
	}
	fmt.Printf("provider: %s\nmodel:    %s\nkey:      %s\nmemory:   %s\nyoconf:   %s\n",
		cfg.Provider, cfg.Model, key, mem, yoconf)
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
	mem := "off"
	if cfg.Memory {
		mem = "on"
	}
	fmt.Printf("OK  provider=%s  model=%s  memory=%s  key=%d chars (decoded & valid)\n", cfg.Provider, cfg.Model, mem, len(cfg.Key))
}

// emit writes a Result as one JSON line to stdout. Errors are emitted in the
// same shape so the snippet can parse every outcome uniformly.
func emit(r llm.Result) {
	// Wrap prose for display only. This runs after any continuation State has been
	// encoded from the raw fields (see main/runContinue), so wrapped text never
	// leaks into the model's replayed context. Command is never wrapped -- it is
	// prefilled onto the line editor as a single runnable line.
	r.Explanation = textwrap.Wrap(r.Explanation, displayWidth)
	r.Response = textwrap.Wrap(r.Response, displayWidth)
	r.Message = textwrap.Wrap(r.Message, displayWidth)
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
