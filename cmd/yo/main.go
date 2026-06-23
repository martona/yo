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
// when a supported scrollback source is available (zellij, tmux, Windows console).
const scrollbackMaxLines = 200

// displayWidth is the column width for wrapping prose output (explanation, chat,
// error message), supplied by the shell via --width; 0 disables wrapping. Wrapping
// lives in the binary so every shell integration shares one implementation -- the
// snippet only has to report its terminal width.
var displayWidth int

// outputFormat controls the machine-readable Result encoding. JSON is the stable
// default used by PowerShell; "sh" is for POSIX-family shell snippets that do not
// have a built-in JSON parser.
var outputFormat = "json"

// version is the binary version, set via -ldflags "-X main.version=<tag>" in CI;
// "dev" for local/un-tagged builds (with a runtime/debug BuildInfo fallback).
var version = "dev"

// debugOn mirrors cfg.Debug for the current run; when set, dbg() traces the
// scaffolding around each LLM call to stderr. Set once, right after config load.
var debugOn bool

func main() {
	dryRun := flag.Bool("dry-run", false, "print the assembled API request to stdout and exit (no network or key needed)")
	check := flag.Bool("check", false, "validate config and the API key (no network), then exit")
	cont := flag.Bool("continue", false, "continuation step; reads $env:YO_STATE (used by the shell integration)")
	exitCode := flag.Int("exit", 0, "exit code of the just-run command (with --continue)")
	dumpSB := flag.Bool("scrollback", false, "print the captured terminal scrollback and exit (debug)")
	width := flag.Int("width", 0, "wrap prose output to this column width (0 = no wrap; set by the shell integration)")
	versionFlag := flag.Bool("version", false, "print the version and exit")
	configFlag := flag.Bool("config", false, "show the resolved configuration and exit")
	initFlag := flag.String("init", "", "print the shell integration for <shell> (powershell or zsh) and exit")
	setupFlag := flag.Bool("setup", false, "install or repair the shell integration (interactive) and exit")
	uninstallFlag := flag.Bool("uninstall", false, "remove the shell integration from your profile and exit")
	outputFlag := flag.String("output", "json", "result output format for shell integrations (json or sh)")
	shellFlag := flag.String("shell", "", "shell profile hint for command generation (powershell, zsh, bash)")
	flag.Usage = usage
	flag.Parse()
	displayWidth = *width
	outputFormat = strings.ToLower(strings.TrimSpace(*outputFlag))
	if *shellFlag != "" {
		os.Setenv("YO_SHELL", *shellFlag)
	}
	if outputFormat != "json" && outputFormat != "sh" {
		fmt.Fprintf(os.Stderr, "yo: unknown --output %q (supported: json, sh)\n", *outputFlag)
		os.Exit(2)
	}

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
	case *setupFlag || *uninstallFlag:
		runSetup(*uninstallFlag)
		return
	case *check:
		runCheck()
		return
	case *cont:
		runContinue(*exitCode, *dryRun)
		return
	case *dumpSB:
		fmt.Fprintf(os.Stderr, "ZELLIJ=%q\n", os.Getenv("ZELLIJ"))
		fmt.Fprintf(os.Stderr, "TMUX=%q\n", os.Getenv("TMUX"))
		out := scrollback.Capture(scrollbackMaxLines)
		fmt.Fprintf(os.Stderr, "captured %d chars\n", len(out))
		fmt.Print(out)
		return
	}

	query := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if query == "" {
		if stdinIsTerminal() {
			// Bare interactive invocation: no query and nothing piped. Show help
			// rather than blocking on a stdin read. (`echo "..." | yo` still works.)
			usage()
			return
		}
		// Piped or redirected stdin: read the query from it (`echo "list pdfs" | yo`).
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
	debugOn = cfg.Debug
	provider, err := llm.New(cfg)
	if err != nil {
		emit(llm.Result{Type: "error", Message: err.Error()})
		os.Exit(1)
	}

	// Opportunistic terminal context (redacted): fold recent screen output into the
	// query so "why did that fail?" works. No-op without a supported source.
	preLen := len(query)
	query = withScrollback(query)
	sbLen := len(query) - preLen

	// Cross-call memory: prepend a compact history of recent yo exchanges (no-op when
	// disabled or empty). Applied after scrollback so the framing nests to a single
	// [request]. The exchange itself is recorded after we have a result, below.
	memLen := 0
	if cfg.Memory {
		preLen = len(query)
		query = llm.WithSessionMemory(query, session.Render(session.Recent(os.Getenv("YO_SESSION"))))
		memLen = len(query) - preLen
	}
	dbg("-> %s/%s  q=%q  [scrollback +%dch, memory +%dch]", cfg.Provider, cfg.Model, rawQuery, sbLen, memLen)

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
	res.PrefillSpace = cfg.PrefillSpace // snippet prefixes the prefill with a space (history hygiene)
	dbgResult(res)

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
	debugOn = cfg.Debug
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
	// lets the model react to real output, not just the exit code. No-op without a
	// supported source; symmetric with the initial query path in main().
	preLen := len(query)
	query = withScrollback(query)
	dbg("-> continue %s/%s  exit=%d steps=%d ran=%q  [scrollback +%dch]",
		cfg.Provider, cfg.Model, exitCode, len(st.Steps), clip(os.Getenv("YO_RAN"), 80), len(query)-preLen)

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
	res.PrefillSpace = cfg.PrefillSpace
	dbgResult(res)

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

// dbg writes a one-line trace to stderr (never stdout -- that's the JSON contract)
// when debug is enabled (`debug true` in ~/.yoconf or $env:YO_DEBUG). It surfaces
// the scaffolding around each LLM call: provider/model, the query, the SIZES of any
// attached context (never the scrollback or command output itself), and the response
// type + pending flag -- enough to see whether the model asked for a continuation.
func dbg(format string, args ...any) {
	if !debugOn {
		return
	}
	fmt.Fprintf(os.Stderr, "yo[debug] "+format+"\n", args...)
}

// dbgResult traces a parsed result's scaffolding: type, the pending flag, and a
// clipped preview of the command or chat text. No-op unless debug is on.
func dbgResult(res llm.Result) {
	switch res.Type {
	case "command":
		dbg("<- command pending=%v  %q", res.Pending, clip(res.Command, 120))
	case "chat":
		dbg("<- chat  %q", clip(res.Response, 120))
	default:
		dbg("<- %s", res.Type)
	}
}

// clip collapses newlines and truncates s to n runes for a one-line debug preview.
func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "..."
	}
	return s
}

// withScrollback folds redacted terminal scrollback into the query. Capture is a
// no-op (empty) without a supported source, so there we return the query untouched
// and never even build the redactor. When there is output, secrets are scrubbed before it
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

// stdinIsTerminal reports whether stdin is an interactive console rather than a pipe
// or a redirected file. A query-less `yo` with an interactive stdin shows help (no
// blocking read); with piped/redirected stdin it reads the query from there. The
// ModeCharDevice heuristic is correct on Windows (console vs pipe vs file); a Unix
// port for bash/zsh may want golang.org/x/term for the rare edge cases.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// usage prints curated help (set as flag.Usage, so it also drives -h / --help).
func usage() {
	fmt.Print(`yo - natural-language command assistant for your shell.

Usage:
  yo <natural language>     Get a command prefilled at your prompt, or a chat answer.
  yo --init powershell      Print the PowerShell integration (for your $PROFILE).
  yo --init zsh             Print the zsh integration (for your ~/.zshrc).
  yo --setup                Install/repair the integration: profile, shell checks, key.
  yo --version              Print the version.
  yo --check                Validate config and the API key (no network).
  yo --config               Show the resolved configuration.
  yo --dry-run <text>       Print the assembled API request (no key or network).

One-time setup:
  PowerShell or macOS zsh:  yo --setup
  Manual PowerShell: add to your $PROFILE -
      if (Get-Command yo -ErrorAction SilentlyContinue) { yo --init powershell | Out-String | iex }
  Manual zsh: add to ~/.zshrc -
      if command -v yo >/dev/null 2>&1; then eval "$(yo --init zsh)"; fi
  Then set ANTHROPIC_API_KEY or OPENAI_API_KEY.

Exit codes:
  0   success.
  1   runtime error (bad config, missing/invalid key, network or API failure).
  2   usage error (no query given, or an unknown --init shell).

Config file: ~/.yoconf (provider, model, key, base_url, memory, debug, prefill_space).
Debug: set "debug true" in ~/.yoconf (or $env:YO_DEBUG) to trace each LLM call's
       request/response scaffolding to stderr.
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
// sourced from the user's profile (e.g. `yo --init powershell | Out-String | iex`,
// or `eval "$(yo --init zsh)"`).
func runInit(shellName string) {
	switch strings.ToLower(shellName) {
	case "powershell", "pwsh":
		fmt.Print(shell.PowerShell)
	case "zsh":
		fmt.Print(shell.Zsh)
	default:
		fmt.Fprintf(os.Stderr, "yo: unknown shell %q (supported: powershell, zsh)\n", shellName)
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
	dbgState := "off"
	if cfg.Debug {
		dbgState = "on"
	}
	psState := "off"
	if cfg.PrefillSpace {
		psState = "on"
	}
	yoconf := "not found"
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".yoconf")
		if _, statErr := os.Stat(p); statErr == nil {
			yoconf = p
		}
	}
	fmt.Printf("provider: %s\nmodel:    %s\nkey:      %s\nmemory:   %s\ndebug:    %s\nprefill:  %s\nyoconf:   %s\n",
		cfg.Provider, cfg.Model, key, mem, dbgState, psState, yoconf)
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
	dbgState := "off"
	if cfg.Debug {
		dbgState = "on"
	}
	fmt.Printf("OK  provider=%s  model=%s  memory=%s  debug=%s  key=%d chars (decoded & valid)\n", cfg.Provider, cfg.Model, mem, dbgState, len(cfg.Key))
}

// emit writes a Result to stdout in the selected machine-readable format. Errors
// are emitted in the same shape so the snippet can parse every outcome uniformly.
func emit(r llm.Result) {
	// Wrap prose for display only. This runs after any continuation State has been
	// encoded from the raw fields (see main/runContinue), so wrapped text never
	// leaks into the model's replayed context. Command is never wrapped -- it is
	// prefilled onto the line editor as a single runnable line.
	r.Explanation = textwrap.Wrap(r.Explanation, displayWidth)
	r.Response = textwrap.Wrap(r.Response, displayWidth)
	r.Message = textwrap.Wrap(r.Message, displayWidth)
	if err := encodeResult(os.Stdout, r); err != nil {
		fallback := llm.Result{Type: "error", Message: "failed to encode result"}
		_ = encodeResult(os.Stdout, fallback)
	}
}

// encodeResult writes r in the currently selected Result encoding.
func encodeResult(w io.Writer, r llm.Result) error {
	switch outputFormat {
	case "sh":
		return encodeResultSh(w, r)
	default:
		return encodeResultJSON(w, r)
	}
}

// encodeResultJSON writes r as one JSON line with HTML escaping off, so command
// strings keep their literal >, <, & (ubiquitous in shell commands).
func encodeResultJSON(w io.Writer, r llm.Result) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}

// encodeResultSh writes r as shell assignments for POSIX-family snippets to eval.
// Every variable is emitted every time so stale values from a prior result cannot
// survive in the caller.
func encodeResultSh(w io.Writer, r llm.Result) error {
	fields := []struct {
		name  string
		value string
	}{
		{"YO_RESULT_TYPE", r.Type},
		{"YO_RESULT_COMMAND", r.Command},
		{"YO_RESULT_EXPLANATION", r.Explanation},
		{"YO_RESULT_RESPONSE", r.Response},
		{"YO_RESULT_MESSAGE", r.Message},
		{"YO_RESULT_PENDING", boolString(r.Pending)},
		{"YO_RESULT_STATE", r.State},
		{"YO_RESULT_PREFILL_SPACE", boolString(r.PrefillSpace)},
	}
	for _, f := range fields {
		if _, err := fmt.Fprintf(w, "%s=%s\n", f.name, shellQuote(f.value)); err != nil {
			return err
		}
	}
	return nil
}

func boolString(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// shellQuote returns a single POSIX-shell token that evaluates to s. Single quotes
// are closed, escaped, and reopened; all other bytes, including newlines and
// command substitutions, stay literal inside the quoted string.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
