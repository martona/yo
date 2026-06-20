// SPDX-License-Identifier: GPL-3.0-or-later
package session

import (
	"strings"
	"testing"

	"github.com/martona/yo/internal/llm"
)

// isolate points the store at a throwaway temp dir for the test.
func isolate(t *testing.T) {
	t.Helper()
	d := t.TempDir()
	t.Setenv("TMP", d)
	t.Setenv("TEMP", d)
}

func TestAppendAndRecent(t *testing.T) {
	isolate(t)
	const id = "sess-test-1"
	Append(id, Exchange{Query: "find big logs", Type: "command",
		Steps: []llm.Step{{Command: "Get-ChildItem -Recurse"}}})
	Append(id, Exchange{Query: "what is a process", Type: "chat", Response: "A process is..."})

	got := Recent(id)
	if len(got) != 2 {
		t.Fatalf("want 2 exchanges, got %d", len(got))
	}
	if got[0].Query != "find big logs" || got[1].Type != "chat" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestDisabledAndEmpty(t *testing.T) {
	isolate(t)
	Append("", Exchange{Query: "x", Type: "command", Steps: []llm.Step{{Command: "c"}}}) // no id -> no-op
	if r := Recent(""); r != nil {
		t.Errorf("empty id should yield nil, got %v", r)
	}
	Append("s", Exchange{Query: "", Type: "command"}) // empty query -> no-op
	Append("s", Exchange{Query: "q", Type: "error"})  // non-command/chat -> no-op
	if r := Recent("s"); len(r) != 0 {
		t.Errorf("nothing valid was appended, got %v", r)
	}
}

func TestCapByCount(t *testing.T) {
	isolate(t)
	const id = "cap"
	for i := 0; i < maxExchanges+5; i++ {
		Append(id, Exchange{Query: "q", Type: "command", Steps: []llm.Step{{Command: "c"}}})
	}
	if got := len(Recent(id)); got != maxExchanges {
		t.Errorf("want %d (capped), got %d", maxExchanges, got)
	}
}

func TestRenderShowsOfferedAndExecuted(t *testing.T) {
	exit0 := 0
	exs := []Exchange{
		{Query: "spooler status then start", Type: "command", Steps: []llm.Step{
			{Command: "Get-Service Spoolerr", Executed: "Get-Service Spooler", Exit: &exit0},
			{Command: "Start-Service Spooler", Exit: &exit0},
		}},
		{Query: "explain it", Type: "chat", Response: "The spooler manages print jobs."},
	}
	out := Render(exs)
	for _, want := range []string{
		`asked "spooler status then start"`,
		"ran: Get-Service Spooler (exit 0)", // executed form wins over the suggested typo
		"suggested: Start-Service Spooler (exit 0)",
		"answered: The spooler manages print jobs.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Render missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Spoolerr") {
		t.Errorf("should show executed command, not the suggested typo:\n%s", out)
	}
}

func TestRenderEmpty(t *testing.T) {
	if Render(nil) != "" {
		t.Error("empty history should render empty")
	}
}
