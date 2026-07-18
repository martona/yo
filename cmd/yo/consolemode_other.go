// SPDX-License-Identifier: GPL-3.0-or-later
//go:build !windows

package main

// saveConsoleMode is a no-op off Windows; the staircase it guards against is a
// Windows console-mode phenomenon (see consolemode_windows.go).
func saveConsoleMode() func() { return func() {} }
