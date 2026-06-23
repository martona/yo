// SPDX-License-Identifier: GPL-3.0-or-later
package scrollback

import (
	"context"
	"testing"
)

func TestStripANSI(t *testing.T) {
	cases := map[string]string{
		"\x1b[31mred\x1b[0m text":        "red text",   // CSI color codes
		"\x1b[1;32mbold\x1b[0m":          "bold",       // multi-param CSI
		"\x1b]0;window title\x07prompt$": "prompt$",    // OSC title + BEL
		"plain text":                     "plain text", // untouched
		"a\tb\nc":                        "a\tb\nc",    // tab/newline kept
		"bell\x07here":                   "bellhere",   // stray control dropped
	}
	for in, want := range cases {
		if got := stripANSI(in); got != want {
			t.Errorf("stripANSI(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd\ne\n\n   \n", 3); got != "c\nd\ne" {
		t.Errorf("lastLines (cap+trim) = %q", got)
	}
	if got := lastLines("only\nthree\nlines", 10); got != "only\nthree\nlines" {
		t.Errorf("lastLines (under cap) = %q", got)
	}
	if got := lastLines("   \n\n  ", 5); got != "" {
		t.Errorf("lastLines (all blank) = %q, want empty", got)
	}
}

func TestCaptureZellijNotInZellij(t *testing.T) {
	// Tested at the zellij-source level: Capture() now has a second source (the
	// Windows console buffer), so a bare Capture() outside zellij is no longer
	// deterministically empty on Windows.
	t.Setenv("ZELLIJ", "")
	if got := captureZellij(); got != "" {
		t.Errorf("expected empty zellij capture when not in zellij, got %q", got)
	}
}

func TestCaptureTmuxNotInTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	if got := captureTmux(10); got != "" {
		t.Errorf("expected empty tmux capture when not in tmux, got %q", got)
	}
}

func TestCaptureTmuxFromCommandOutput(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	t.Setenv("TMUX", "/tmp/tmux-test")

	oldCommandOutput := commandOutput
	t.Cleanup(func() { commandOutput = oldCommandOutput })
	commandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name != "tmux" {
			t.Fatalf("command name = %q, want tmux", name)
		}
		wantArgs := []string{"capture-pane", "-p", "-S", "-2"}
		if len(args) != len(wantArgs) {
			t.Fatalf("args = %#v, want %#v", args, wantArgs)
		}
		for i := range wantArgs {
			if args[i] != wantArgs[i] {
				t.Fatalf("args = %#v, want %#v", args, wantArgs)
			}
		}
		return []byte("old\n\x1b[31mkeep1\x1b[0m\nkeep2\n"), nil
	}

	if got := Capture(2); got != "keep1\nkeep2" {
		t.Fatalf("Capture via fake tmux = %q, want %q", got, "keep1\nkeep2")
	}
}
