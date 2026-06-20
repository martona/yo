// SPDX-License-Identifier: GPL-3.0-or-later

// Package scrollback captures recent terminal output for context, opportunistically
// via the zellij multiplexer (the only candidate on Windows). It is best-effort:
// outside zellij, or on any failure, Capture returns "" and the caller proceeds
// without context. No secret redaction yet — that's a later, separable layer.
package scrollback

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Capture returns recent terminal output when running inside zellij, stripped of
// ANSI escapes and trimmed to the last maxLines. Returns "" when not in zellij or
// on any error (never fatal).
func Capture(maxLines int) string {
	if os.Getenv("ZELLIJ") == "" {
		return "" // opportunistic: only when inside a zellij session
	}

	tmp, err := os.CreateTemp("", "yo-scrollback-*.txt")
	if err != nil {
		return ""
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// zellij action dump-screen --full --path <PATH>: writes the resolved screen
	// plus scrollback to PATH (the path is a --path option, not positional). We
	// omit --ansi, so the dump is plain text (already collapsed to final cells).
	if err := exec.CommandContext(ctx, "zellij", "action", "dump-screen", "--full", "--path", path).Run(); err != nil {
		return ""
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return lastLines(stripANSI(string(data)), maxLines)
}

// ansiRe matches CSI (ESC [ ... final byte) and OSC (ESC ] ... BEL) escape
// sequences — the common cases in a terminal dump.
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b\\][^\x07]*\x07")

// stripANSI removes ANSI escape sequences and stray control characters (keeping
// tabs and newlines), so the model sees clean text.
func stripANSI(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1 // drop other control chars
		}
		return r
	}, s)
}

// lastLines trims trailing blank space (zellij pads the dump with empty screen
// rows) and returns at most the last max lines.
func lastLines(s string, max int) string {
	s = strings.TrimRight(s, " \t\r\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if max > 0 && len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	return strings.Join(lines, "\n")
}
