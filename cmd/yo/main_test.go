// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
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
