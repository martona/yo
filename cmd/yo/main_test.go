// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/martona/yo/internal/llm"
)

func setOutputFormat(t *testing.T, format string) {
	t.Helper()
	old := outputFormat
	outputFormat = format
	t.Cleanup(func() { outputFormat = old })
}

// PowerShell commands routinely contain &, >, < — these must survive as literal
// characters, not Go's default JSON HTML escapes.
func TestEncodeResultNoHTMLEscape(t *testing.T) {
	setOutputFormat(t, "json")

	var buf bytes.Buffer
	r := llm.Result{Type: "command", Command: `Get-Process | Where-Object CPU -gt 10 & echo done > out.txt`, Explanation: "x"}
	if err := encodeResult(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, escaped := range []string{"\\u0026", "\\u003e", "\\u003c"} {
		if strings.Contains(out, escaped) {
			t.Fatalf("output is HTML-escaped (%s): %s", escaped, out)
		}
	}
	if !strings.Contains(out, "&") || !strings.Contains(out, ">") {
		t.Fatalf("expected literal & and >: %s", out)
	}
}

// scriptedProvider returns canned results in order, recording each query.
type scriptedProvider struct {
	queries []string
	results []llm.Result
}

func (s *scriptedProvider) Generate(_ context.Context, query string) (llm.Result, error) {
	s.queries = append(s.queries, query)
	return s.results[len(s.queries)-1], nil
}

func (s *scriptedProvider) Request(string) ([]byte, error) { return nil, nil }

// A command tool call with an empty command field is a known model failure mode
// (the command text leaks into the explanation as stray tool-call markup);
// generate must re-prompt once with a corrective note rather than prefill nothing.
func TestGenerateRetriesEmptyCommand(t *testing.T) {
	noThinking = true
	t.Cleanup(func() { noThinking = false })

	t.Run("re-prompts once and uses the retry result", func(t *testing.T) {
		p := &scriptedProvider{results: []llm.Result{
			{Type: "command", Command: "", Explanation: "leaked markup", InputTokens: 10, OutputTokens: 1},
			{Type: "command", Command: "df -h", Explanation: "disk usage", InputTokens: 20, OutputTokens: 2},
		}}
		res, err := generate(p, "how full is the disk")
		if err != nil {
			t.Fatal(err)
		}
		if len(p.queries) != 2 {
			t.Fatalf("expected 2 provider calls, got %d", len(p.queries))
		}
		if !strings.HasPrefix(p.queries[1], "how full is the disk") || !strings.Contains(p.queries[1], "[retry]") {
			t.Errorf("retry query should be the original plus a corrective note:\n%s", p.queries[1])
		}
		if res.Command != "df -h" {
			t.Errorf("expected the retry's command, got %q", res.Command)
		}
		if res.InputTokens != 30 || res.OutputTokens != 3 {
			t.Errorf("usage should cover both calls, got in=%d out=%d", res.InputTokens, res.OutputTokens)
		}
	})

	t.Run("errors when the retry is empty too", func(t *testing.T) {
		p := &scriptedProvider{results: []llm.Result{
			{Type: "command", Command: ""},
			{Type: "command", Command: "   "}, // whitespace-only is equally unusable
		}}
		if _, err := generate(p, "q"); err == nil {
			t.Fatal("expected an error after two empty commands")
		}
		if len(p.queries) != 2 {
			t.Fatalf("expected exactly 2 provider calls (one retry), got %d", len(p.queries))
		}
	})

	t.Run("does not retry good commands or chat", func(t *testing.T) {
		for _, res := range []llm.Result{
			{Type: "command", Command: "ls"},
			{Type: "chat", Response: "hello"},
		} {
			p := &scriptedProvider{results: []llm.Result{res}}
			if _, err := generate(p, "q"); err != nil {
				t.Fatal(err)
			}
			if len(p.queries) != 1 {
				t.Fatalf("%s result should not trigger a retry, got %d calls", res.Type, len(p.queries))
			}
		}
	})
}

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"":                         "''",
		"plain":                    "'plain'",
		"what's here":              "'what'\\''s here'",
		"$(touch /tmp/yo-bad)":     "'$(touch /tmp/yo-bad)'",
		"line one\nline two":       "'line one\nline two'",
		"`touch /tmp/yo-bad`":      "'`touch /tmp/yo-bad`'",
		"semi; pipe | redirect >":  "'semi; pipe | redirect >'",
		`printf '%s\n' "$SHELL"`:   `'printf '\''%s\n'\'' "$SHELL"'`,
		`backslash stays literal\`: `'backslash stays literal\'`,
	}
	for in, want := range tests {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEncodeResultShellAssignments(t *testing.T) {
	setOutputFormat(t, "sh")

	var buf bytes.Buffer
	r := llm.Result{
		Type:         "command",
		Command:      `printf '%s\n' "$(touch /tmp/yo-bad)"; echo done`,
		Explanation:  "line one\nline two",
		Pending:      true,
		State:        "abc123",
		PrefillSpace: true,
	}
	if err := encodeResult(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"YO_RESULT_TYPE='command'\n",
		`YO_RESULT_COMMAND='printf '\''%s\n'\'' "$(touch /tmp/yo-bad)"; echo done'` + "\n",
		"YO_RESULT_EXPLANATION='line one\nline two'\n",
		"YO_RESULT_RESPONSE=''\n",
		"YO_RESULT_MESSAGE=''\n",
		"YO_RESULT_PENDING='1'\n",
		"YO_RESULT_STATE='abc123'\n",
		"YO_RESULT_PREFILL_SPACE='1'\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("shell output missing %q:\n%s", want, out)
		}
	}
}

func TestDryRunUsesZshPromptProfile(t *testing.T) {
	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcessMain", "--", "--dry-run", "list files here")
	cmd.Env = append(os.Environ(),
		"YO_HELPER_MAIN=1",
		"HOME="+home,
		"YO_SHELL=zsh",
		"SHELL=/bin/zsh",
		"ZELLIJ=",
		"TMUX=",
		"ANTHROPIC_API_KEY=",
		"OPENAI_API_KEY=",
		"XAI_API_KEY=",
		"GEMINI_API_KEY=",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run --dry-run failed: %v\n%s", err, out)
	}
	body := string(out)
	for _, want := range []string{
		"zsh prompt on a Unix-like system",
		"POSIX/Unix shell command",
		"list files here",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, body)
		}
	}
	for _, leak := range []string{"PowerShell prompt on Windows", "Get-ChildItem"} {
		if strings.Contains(body, leak) {
			t.Fatalf("dry-run output leaked PowerShell prompt %q:\n%s", leak, body)
		}
	}
}

// TestForgetClearsSessionMemory runs `yo --forget` in a subprocess pointed at a
// throwaway temp dir, seeded with a session file, and asserts the file is gone and
// the count is reported. --forget needs no key or network.
func TestForgetClearsSessionMemory(t *testing.T) {
	tmp := t.TempDir()
	store := filepath.Join(tmp, "yo")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(store, "sess-demo.json")
	if err := os.WriteFile(seed, []byte(`[{"q":"x","t":"chat","r":"y"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcessMain", "--", "--forget")
	cmd.Env = append(os.Environ(),
		"YO_HELPER_MAIN=1",
		"TMPDIR="+tmp, // os.TempDir() on macOS/Linux
		"TMP="+tmp,    // and on Windows
		"TEMP="+tmp,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("yo --forget failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "forgot 1 saved session") {
		t.Fatalf("unexpected --forget output:\n%s", out)
	}
	if _, err := os.Stat(seed); !os.IsNotExist(err) {
		t.Fatalf("session file should be gone, stat err = %v", err)
	}
}

func TestHelperProcessMain(t *testing.T) {
	if os.Getenv("YO_HELPER_MAIN") != "1" {
		return
	}
	args := []string{"yo"}
	for i, arg := range os.Args {
		if arg == "--" {
			args = append(args, os.Args[i+1:]...)
			break
		}
	}
	os.Args = args
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	main()
	os.Exit(0)
}
