// SPDX-License-Identifier: GPL-3.0-or-later
//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32ConMode    = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32ConMode.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32ConMode.NewProc("SetConsoleMode")
)

// saveConsoleMode snapshots the console's current output mode and returns a restore
// function. For INTERACTIVE children only: they need the console (so proc.NoConsole
// is not an option), but may leave its output mode flipped on exit -- e.g. pwsh
// enabling VT with newline auto-return disabled, which staircases every bare-\n
// line printed afterwards. Returns a no-op when not attached to a console.
func saveConsoleMode() func() {
	name, err := syscall.UTF16PtrFromString("CONOUT$")
	if err != nil {
		return func() {}
	}
	h, err := syscall.CreateFile(name,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		return func() {}
	}
	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode))); r == 0 {
		syscall.CloseHandle(h)
		return func() {}
	}
	return func() {
		_, _, _ = procSetConsoleMode.Call(uintptr(h), uintptr(mode))
		_ = syscall.CloseHandle(h)
	}
}
