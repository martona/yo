// SPDX-License-Identifier: GPL-3.0-or-later
//go:build !windows

package scrollback

// consoleCapture has no portable equivalent off Windows: on macOS/Linux the screen
// lives in the terminal emulator's private memory, unreachable without a multiplexer
// (which Capture already handles via zellij/tmux). Always returns "".
func consoleCapture(maxLines int) string { return "" }
