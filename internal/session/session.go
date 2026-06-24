// SPDX-License-Identifier: GPL-3.0-or-later

// Package session persists a small, per-shell history of yo exchanges so that
// independent `yo` calls have cross-call context (what you asked, what was
// suggested or run). It is the durable companion to scrollback: scrollback is the
// raw screen where a multiplexer is present; this is the structured intent thread,
// available everywhere.
//
// Storage is a per-session JSON file under the OS temp dir, keyed by a session id
// the shell supplies ($env:YO_SESSION). Records are deliberately NOT redacted:
// everything stored here has already been sent to the LLM -- your query; the
// model's own offered command or chat; for multi-step, the executed command, which
// already rides offered-vs-executed -- so memory adds no new LLM exposure, only
// disk persistence, bounded to an ephemeral per-session temp file. (This holds
// because storage is temp-only and never includes command OUTPUT. If either
// changes, real secrets could land on disk and this decision must be revisited.)
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/martona/yo/internal/llm"
)

const (
	maxExchanges = 12                 // keep at most this many recent exchanges
	maxChars     = 8000               // and drop oldest until the rendered block fits
	maxChat      = 300                // truncate a stored chat response to this many chars
	staleAfter   = 7 * 24 * time.Hour // sweep session files older than this
)

// Exchange is one recorded yo interaction.
type Exchange struct {
	Query    string     `json:"q"`
	Type     string     `json:"t"`           // "command" | "chat"
	Response string     `json:"r,omitempty"` // chat answer (truncated to maxChat)
	Steps    []llm.Step `json:"s,omitempty"` // command steps: 1 for standalone, N for multi-step
}

// Recent returns the stored exchanges for session id, oldest first (nil if id is
// empty or there is no store).
func Recent(id string) []Exchange {
	if id == "" {
		return nil
	}
	d, err := dir()
	if err != nil {
		return nil
	}
	return load(sessionPath(d, id))
}

// Append records ex for session id (no-op when id is empty, the query is empty, or
// the type is not command/chat). It caps the store and, on a session's first write,
// sweeps stale session files.
func Append(id string, ex Exchange) {
	if id == "" || ex.Query == "" || (ex.Type != "command" && ex.Type != "chat") {
		return
	}
	d, err := dir()
	if err != nil {
		return
	}
	path := sessionPath(d, id)
	if _, statErr := os.Stat(path); statErr != nil {
		sweep(d) // first write for this session -> tidy up orphans from closed shells
	}
	if ex.Type == "chat" && len(ex.Response) > maxChat {
		ex.Response = ex.Response[:maxChat] + "..."
	}
	exs := capExchanges(append(load(path), ex))
	if data, err := json.Marshal(exs); err == nil {
		_ = os.WriteFile(path, data, 0o600)
	}
}

// Render formats exchanges into a compact plain-text history block (empty when
// there are none). The framing for the model is added by llm.WithSessionMemory.
func Render(exs []Exchange) string {
	if len(exs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, ex := range exs {
		fmt.Fprintf(&b, "- asked %q -> ", ex.Query)
		switch ex.Type {
		case "chat":
			fmt.Fprintf(&b, "answered: %s", ex.Response)
		case "command":
			for i, st := range ex.Steps {
				if i > 0 {
					b.WriteString("; ")
				}
				cmd, verb := st.Command, "suggested"
				if st.Executed != "" {
					cmd, verb = st.Executed, "ran"
				}
				fmt.Fprintf(&b, "%s: %s", verb, cmd)
				if st.Exit != nil {
					fmt.Fprintf(&b, " (exit %d)", *st.Exit)
				}
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// capExchanges keeps the most recent maxExchanges, then drops oldest until the
// rendered size is under maxChars.
func capExchanges(exs []Exchange) []Exchange {
	if len(exs) > maxExchanges {
		exs = exs[len(exs)-maxExchanges:]
	}
	for len(exs) > 1 && len(Render(exs)) > maxChars {
		exs = exs[1:]
	}
	return exs
}

func dir() (string, error) {
	d := filepath.Join(os.TempDir(), "yo")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

// Path returns the session-store directory without creating it, so `yo --uninstall`
// can report and clear it.
func Path() string {
	return filepath.Join(os.TempDir(), "yo")
}

// Clear removes all session files (and the directory if that leaves it empty).
// No-op (returns nil) when the store does not exist. For `yo --uninstall`.
func Clear() error {
	d := Path()
	entries, err := os.ReadDir(d)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "sess-") {
			_ = os.Remove(filepath.Join(d, e.Name()))
		}
	}
	_ = os.Remove(d) // best-effort: only succeeds if it's now empty
	return nil
}

func sessionPath(d, id string) string {
	return filepath.Join(d, "sess-"+sanitize(id)+".json")
}

// sanitize keeps a session id filename-safe.
func sanitize(id string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, id)
}

func load(path string) []Exchange {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var exs []Exchange
	if json.Unmarshal(data, &exs) != nil {
		return nil
	}
	return exs
}

// sweep removes session files not modified within staleAfter.
func sweep(d string) {
	entries, err := os.ReadDir(d)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-staleAfter)
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "sess-") {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(d, e.Name()))
		}
	}
}
