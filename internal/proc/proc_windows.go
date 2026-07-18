// SPDX-License-Identifier: GPL-3.0-or-later
//go:build windows

// Package proc holds small helpers for spawning child processes.
package proc

import (
	"os/exec"
	"syscall"
)

// createNoWindow is Win32's CREATE_NO_WINDOW: the child console application runs
// with no console at all (it does not inherit ours).
const createNoWindow = 0x08000000

// NoConsole detaches cmd from the parent's console. Diagnostic children (pwsh, the
// WSL bash stub, zellij/tmux) can flip the inherited console's output mode -- e.g.
// enable VT with newline auto-return disabled -- and not restore it on exit, which
// staircases every bare-\n line we print afterwards. A child with no console cannot
// touch ours. Only for children whose stdio is piped or discarded; an interactive
// child needs the console and must not be detached.
func NoConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
