// SPDX-License-Identifier: GPL-3.0-or-later

// Package shell holds the per-shell integration snippets, embedded into the binary
// so `yo --init <shell>` can emit them. Embedding version-locks the snippet to the
// binary: upgrading the exe always emits the matching integration, with no stale
// on-disk copy to drift out of sync.
package shell

import _ "embed"

//go:embed yo.ps1
var PowerShell string
