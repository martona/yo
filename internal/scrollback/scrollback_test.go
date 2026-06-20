// SPDX-License-Identifier: GPL-3.0-or-later
package scrollback

import "testing"

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

func TestCaptureNotInZellij(t *testing.T) {
	t.Setenv("ZELLIJ", "")
	if got := Capture(100); got != "" {
		t.Errorf("expected empty when not in zellij, got %q", got)
	}
}
