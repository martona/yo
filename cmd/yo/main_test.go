// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/martona/yo/internal/llm"
)

// PowerShell commands routinely contain &, >, < — these must survive as literal
// characters, not Go's default JSON HTML escapes.
func TestEncodeResultNoHTMLEscape(t *testing.T) {
	var buf bytes.Buffer
	r := llm.Result{Type: "command", Command: `Get-Process | Where-Object CPU -gt 10 & echo done > out.txt`, Explanation: "x"}
	if err := encodeResult(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, escaped := range []string{"\\u0026", "\\u003e", "\\u003c"} {
		if strings.Contains(out, escaped) {
			t.Fatalf("output is HTML-escaped (%s): %s", escaped, out)
		}
	}
	if !strings.Contains(out, "&") || !strings.Contains(out, ">") {
		t.Fatalf("expected literal & and >: %s", out)
	}
}
