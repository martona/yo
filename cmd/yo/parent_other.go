// SPDX-License-Identifier: GPL-3.0-or-later
//go:build !windows

package main

// parentShell is Windows-only; elsewhere runSetup falls back to a pwsh/powershell
// PATH lookup.
func parentShell() string { return "" }
