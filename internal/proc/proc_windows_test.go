// SPDX-License-Identifier: GPL-3.0-or-later
//go:build windows

package proc

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestNoConsoleSetsCreateNoWindow(t *testing.T) {
	cmd := exec.Command("cmd")
	NoConsole(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("CREATE_NO_WINDOW not set: %+v", cmd.SysProcAttr)
	}
}

func TestNoConsolePreservesExistingAttrs(t *testing.T) {
	cmd := exec.Command("cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000010} // CREATE_NEW_CONSOLE stand-in
	NoConsole(cmd)
	if cmd.SysProcAttr.CreationFlags&0x00000010 == 0 {
		t.Fatal("existing creation flags were clobbered")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatal("CREATE_NO_WINDOW not OR-ed in")
	}
}
