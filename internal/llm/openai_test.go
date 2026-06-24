// SPDX-License-Identifier: GPL-3.0-or-later
package llm

import (
	"strings"
	"testing"
)

func TestParseOpenAI(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		status  int
		want    Result
		wantErr string
	}{
		{
			name:   "command after a reasoning item",
			body:   `{"output":[{"type":"reasoning","id":"r1"},{"type":"function_call","name":"command","arguments":"{\"command\":\"Get-ChildItem -Recurse\",\"explanation\":\"lists files\"}"}],"error":null}`,
			status: 200,
			want:   Result{Type: "command", Command: "Get-ChildItem -Recurse", Explanation: "lists files"},
		},
		{
			name:   "command pending (multi-step)",
			body:   `{"output":[{"type":"function_call","name":"command","arguments":"{\"command\":\"choco install foo\",\"explanation\":\"install\",\"pending\":true}"}],"error":null}`,
			status: 200,
			want:   Result{Type: "command", Command: "choco install foo", Explanation: "install", Pending: true},
		},
		{
			name:   "chat",
			body:   `{"output":[{"type":"function_call","name":"chat","arguments":"{\"response\":\"A pipe passes output.\"}"}],"error":null}`,
			status: 200,
			want:   Result{Type: "chat", Response: "A pipe passes output."},
		},
		{
			name:   "usage reported",
			body:   `{"output":[{"type":"function_call","name":"command","arguments":"{\"command\":\"ls\",\"explanation\":\"x\"}"}],"usage":{"input_tokens":123,"output_tokens":45},"error":null}`,
			status: 200,
			want:   Result{Type: "command", Command: "ls", Explanation: "x", InputTokens: 123, OutputTokens: 45},
		},
		{
			name:    "error body (non-null)",
			body:    `{"error":{"message":"invalid api key"}}`,
			status:  401,
			wantErr: "invalid api key",
		},
		{
			name:    "no function_call",
			body:    `{"output":[{"type":"message"}],"error":null}`,
			status:  200,
			wantErr: "no command or chat",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOpenAI([]byte(tt.body), tt.status)
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
