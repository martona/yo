// SPDX-License-Identifier: GPL-3.0-or-later

// Package textwrap word-wraps prose to a column width for terminal display. It
// lives in the binary so every shell integration shares one implementation; a
// shell snippet only needs to report its terminal width (via `yo --width`).
package textwrap

import "strings"

// Wrap word-wraps s to width columns. A run of non-whitespace (a "word") moves
// whole to the next line rather than being split; a word longer than width is
// hard-broken only because it cannot otherwise fit. Newlines already in s are
// preserved (paragraphs, lists and blank lines survive); other whitespace within
// a line collapses to single spaces. width <= 0, or empty s, returns s unchanged.
// Widths are counted in runes, not bytes, so multi-byte text wraps sensibly.
func Wrap(s string, width int) string {
	if width <= 0 || s == "" {
		return s
	}
	var out []string
	for _, srcLine := range strings.Split(s, "\n") {
		line := strings.TrimSpace(srcLine)
		if line == "" {
			out = append(out, "")
			continue
		}
		cur := ""
		curLen := 0
		for _, word := range strings.Fields(line) {
			wr := []rune(word)
			// Hard-split a word too long to ever fit on one line by itself.
			for len(wr) > width {
				if curLen > 0 {
					out = append(out, cur)
					cur, curLen = "", 0
				}
				out = append(out, string(wr[:width]))
				wr = wr[width:]
			}
			word, wl := string(wr), len(wr)
			switch {
			case curLen == 0:
				cur, curLen = word, wl
			case curLen+1+wl <= width:
				cur += " " + word
				curLen += 1 + wl
			default:
				out = append(out, cur)
				cur, curLen = word, wl
			}
		}
		if curLen > 0 {
			out = append(out, cur)
		}
	}
	return strings.Join(out, "\n")
}
