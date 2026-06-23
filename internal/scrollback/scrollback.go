// SPDX-License-Identifier: GPL-3.0-or-later

// Package scrollback captures recent terminal output for context. Sources, in
// precedence order: zellij, tmux, and, as a Windows-only fallback, the console
// buffer read directly via the Console API (the visible viewport under Windows
// Terminal/ConPTY, the full buffer under classic conhost). There is no native
// equivalent on macOS/Linux, where the screen lives in the terminal emulator's
// private memory and a multiplexer is the only portable source. Best-effort: on
// any failure Capture returns "" and the caller proceeds without context.
// Redaction is applied separately by the caller.
package scrollback

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var commandOutput = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Capture returns recent terminal output, stripped of ANSI escapes and trimmed to
// the last maxLines. It prefers multiplexers (zellij, then tmux); failing that it
// reads the Windows console buffer (a no-op on other OSes). Returns "" when no
// source is available or on any error (never fatal).
func Capture(maxLines int) string {
	raw := captureZellij()
	if raw == "" {
		raw = captureTmux(maxLines)
	}
	if raw == "" {
		raw = consoleCapture(maxLines) // Windows console buffer; "" elsewhere
	}
	if raw == "" {
		return ""
	}
	return lastLines(stripANSI(raw), maxLines)
}

// captureZellij returns the resolved screen + scrollback from a zellij session, or
// "" when not in zellij or on any failure.
func captureZellij() string {
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
	return string(data)
}

// captureTmux returns the resolved pane text from a tmux session, or "" when not
// in tmux or on any failure.
func captureTmux(maxLines int) string {
	if os.Getenv("TMUX") == "" {
		return "" // opportunistic: only when inside a tmux session
	}

	args := []string{"capture-pane", "-p"}
	if maxLines > 0 {
		args = append(args, "-S", fmt.Sprintf("-%d", maxLines))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := commandOutput(ctx, "tmux", args...)
	if err != nil {
		return ""
	}
	return string(out)
}

// ansiRe matches CSI (ESC [ ... final byte) and OSC (ESC ] ... BEL) escape
// sequences — the common cases in a terminal dump.
var ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]|\x1b\\][^\x07]*\x07")

// stripANSI removes ANSI escape sequences and stray control characters (keeping
// tabs and newlines), so the model sees clean text. (A no-op for console-buffer
// text, which is already resolved cells.)
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

// lastLines trims trailing blank space (the dump is padded with empty screen rows)
// and returns at most the last max lines.
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
