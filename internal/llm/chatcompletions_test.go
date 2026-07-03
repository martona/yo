// SPDX-License-Identifier: GPL-3.0-or-later
package llm

import (
	"strings"
	"testing"
)

func TestParseChat(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		status  int
		want    Result
		wantErr string
	}{
		{
			name:   "command",
			body:   `{"choices":[{"message":{"tool_calls":[{"type":"function","function":{"name":"command","arguments":"{\"command\":\"Get-ChildItem -Recurse\",\"explanation\":\"lists files\"}"}}]}}]}`,
			status: 200,
			want:   Result{Type: "command", Command: "Get-ChildItem -Recurse", Explanation: "lists files"},
		},
		{
			name:   "command pending (multi-step)",
			body:   `{"choices":[{"message":{"tool_calls":[{"function":{"name":"command","arguments":"{\"command\":\"choco install foo\",\"explanation\":\"install\",\"pending\":true}"}}]}}]}`,
			status: 200,
			want:   Result{Type: "command", Command: "choco install foo", Explanation: "install", Pending: true},
		},
		{
			name:   "chat",
			body:   `{"choices":[{"message":{"tool_calls":[{"function":{"name":"chat","arguments":"{\"response\":\"A pipe passes output.\"}"}}]}}]}`,
			status: 200,
			want:   Result{Type: "chat", Response: "A pipe passes output."},
		},
		{
			name:   "usage reported",
			body:   `{"choices":[{"message":{"tool_calls":[{"function":{"name":"command","arguments":"{\"command\":\"ls\",\"explanation\":\"x\"}"}}]}}],"usage":{"prompt_tokens":123,"completion_tokens":45}}`,
			status: 200,
			want:   Result{Type: "command", Command: "ls", Explanation: "x", InputTokens: 123, OutputTokens: 45},
		},
		{
			// Mind-change response: chat first, then the command it now intends
			// to prefill; the command is the intent (resolveToolCalls).
			name:   "chat then command: command wins",
			body:   `{"choices":[{"message":{"tool_calls":[{"function":{"name":"chat","arguments":"{\"response\":\"Prefilling that now.\"}"}},{"function":{"name":"command","arguments":"{\"command\":\"df -h\",\"explanation\":\"disk usage\"}"}}]}}]}`,
			status: 200,
			want:   Result{Type: "command", Command: "df -h", Explanation: "disk usage"},
		},
		{
			name:    "error body (non-null)",
			body:    `{"error":{"message":"invalid api key"}}`,
			status:  401,
			wantErr: "invalid api key",
		},
		{
			name:    "no tool call",
			body:    `{"choices":[{"message":{"content":"hi"}}]}`,
			status:  200,
			wantErr: "no command or chat",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseChat([]byte(tt.body), tt.status)
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
