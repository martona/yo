// SPDX-License-Identifier: GPL-3.0-or-later
//go:build !windows

// Package proc holds small helpers for spawning child processes.
package proc

import "os/exec"

// NoConsole is a no-op off Windows: Unix children share the tty by design and
// cannot corrupt the parent's console mode the way a Windows child can (the
// staircase this guards against is a Windows console-mode phenomenon).
func NoConsole(cmd *exec.Cmd) {}
