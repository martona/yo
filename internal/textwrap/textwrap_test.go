// SPDX-License-Identifier: GPL-3.0-or-later
package textwrap

import (
	"strings"
	"testing"
)

// maxRuneLen returns the longest line of s measured in runes.
func maxRuneLen(s string) int {
	max := 0
	for _, line := range strings.Split(s, "\n") {
		if n := len([]rune(line)); n > max {
			max = n
		}
	}
	return max
}

func TestWrapNeverBreaksWords(t *testing.T) {
	in := "The quick brown fox jumps over the lazy dog and then keeps on running well past the right edge of any sane terminal window."
	out := Wrap(in, 20)
	if max := maxRuneLen(out); max > 20 {
		t.Errorf("a line exceeds width 20 (max=%d):\n%s", max, out)
	}
	// Same words, same order -- only the line breaks changed.
	if got, want := strings.Join(strings.Fields(out), " "), strings.Join(strings.Fields(in), " "); got != want {
		t.Errorf("word content changed:\n got %q\nwant %q", got, want)
	}
}

func TestWrapPreservesParagraphs(t *testing.T) {
	in := "First paragraph, short.\n\nSecond paragraph, long enough that it must reflow across more than one output line at this width."
	out := Wrap(in, 25)
	if !strings.Contains(out, "\n\n") {
		t.Errorf("blank line between paragraphs was not preserved:\n%s", out)
	}
	if max := maxRuneLen(out); max > 25 {
		t.Errorf("a line exceeds width 25 (max=%d):\n%s", max, out)
	}
}

func TestWrapHardSplitsOverlongToken(t *testing.T) {
	in := strings.Repeat("x", 50)
	out := Wrap(in, 20)
	if max := maxRuneLen(out); max > 20 {
		t.Errorf("a chunk exceeds width 20 (max=%d):\n%s", max, out)
	}
	if reassembled := strings.ReplaceAll(out, "\n", ""); reassembled != in {
		t.Errorf("hard-split did not reassemble: got %q want %q", reassembled, in)
	}
}

func TestWrapWordExactlyWidth(t *testing.T) {
	in := strings.Repeat("a", 10) + " " + strings.Repeat("b", 10)
	want := strings.Repeat("a", 10) + "\n" + strings.Repeat("b", 10)
	if out := Wrap(in, 10); out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestWrapNoOpCases(t *testing.T) {
	if got := Wrap("leave this entirely alone", 0); got != "leave this entirely alone" {
		t.Errorf("width 0 should pass through unchanged, got %q", got)
	}
	if got := Wrap("short", -5); got != "short" {
		t.Errorf("negative width should pass through unchanged, got %q", got)
	}
	if got := Wrap("", 80); got != "" {
		t.Errorf("empty input should stay empty, got %q", got)
	}
}

func TestWrapCountsRunes(t *testing.T) {
	// Five 2-byte runes plus a space and one more: must not split a word, and the
	// rune count (not byte count) governs width.
	in := "café café café"
	out := Wrap(in, 9) // "café café" is 9 runes; the third wraps
	if got := out; got != "café café\ncafé" {
		t.Errorf("got %q want %q", got, "café café\\ncafé")
	}
}
