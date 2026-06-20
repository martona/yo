// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseAnthropicResponse(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		status  int
		want    Result
		wantErr string
	}{
		{
			name:   "command",
			body:   `{"type":"message","content":[{"type":"tool_use","name":"command","input":{"command":"Get-ChildItem -Recurse","explanation":"lists files"}}]}`,
			status: 200,
			want:   Result{Type: "command", Command: "Get-ChildItem -Recurse", Explanation: "lists files"},
		},
		{
			name:   "chat",
			body:   `{"type":"message","content":[{"type":"tool_use","name":"chat","input":{"response":"A pipe passes output."}}]}`,
			status: 200,
			want:   Result{Type: "chat", Response: "A pipe passes output."},
		},
		{
			name:   "text block before tool_use",
			body:   `{"type":"message","content":[{"type":"text","text":"thinking"},{"type":"tool_use","name":"command","input":{"command":"Get-Location","explanation":"x"}}]}`,
			status: 200,
			want:   Result{Type: "command", Command: "Get-Location", Explanation: "x"},
		},
		{
			name:    "api error body",
			body:    `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`,
			status:  401,
			wantErr: "invalid x-api-key",
		},
		{
			name:    "no tool_use",
			body:    `{"type":"message","content":[{"type":"text","text":"hi"}]}`,
			status:  200,
			wantErr: "no command or chat",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAnthropicResponse([]byte(tt.body), tt.status)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// PowerShell commands routinely contain &, >, < — these must survive as literal
// characters, not Go's default JSON HTML escapes (&, >, <).
func TestEncodeResultNoHTMLEscape(t *testing.T) {
	var buf bytes.Buffer
	r := Result{Type: "command", Command: `Get-Process | Where-Object CPU -gt 10 & echo done > out.txt`, Explanation: "x"}
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
