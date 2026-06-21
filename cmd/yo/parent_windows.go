// SPDX-License-Identifier: GPL-3.0-or-later
//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	modkernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess                = modkernel32.NewProc("OpenProcess")
	procQueryFullProcessImageNameW = modkernel32.NewProc("QueryFullProcessImageNameW")
	procCloseHandle                = modkernel32.NewProc("CloseHandle")
)

const processQueryLimitedInformation = 0x1000

// parentShell returns the full path of the parent process when it is a PowerShell
// host (pwsh.exe or powershell.exe), else "". `yo --setup` runs under it so it
// configures the profile of the shell the user actually invoked from -- including
// Windows PowerShell 5.1, not whichever host happens to be on PATH.
func parentShell() string {
	ppid := os.Getppid()
	if ppid <= 0 {
		return ""
	}
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(ppid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)

	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if r, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r == 0 {
		return ""
	}
	switch strings.ToLower(filepath.Base(syscall.UTF16ToString(buf[:size]))) {
	case "pwsh.exe", "powershell.exe":
		return syscall.UTF16ToString(buf[:size])
	}
	return ""
}
