// SPDX-License-Identifier: GPL-3.0-or-later
package llm

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// A model can emit more than one tool call in a single response. The observed
// failure shape is chat-then-command: the model commits to chat, changes its
// mind while writing the response, and appends the command it now intends to
// prefill. The command must win regardless of order, and the dropped call must
// show up in the debug trace rather than vanish silently.
func TestResolveToolCalls(t *testing.T) {
	cmd := func(c string) toolCall {
		return toolCall{name: toolCommand, args: json.RawMessage(fmt.Sprintf(`{"command":%q,"explanation":"x"}`, c))}
	}
	chat := func(r string) toolCall {
		return toolCall{name: toolChat, args: json.RawMessage(fmt.Sprintf(`{"response":%q}`, r))}
	}

	tests := []struct {
		name  string
		calls []toolCall
		want  Result
	}{
		{"chat then command: command wins", []toolCall{chat("prefilling now..."), cmd("df -h")},
			Result{Type: "command", Command: "df -h", Explanation: "x", InputTokens: 1, OutputTokens: 2}},
		{"command then chat: command wins", []toolCall{cmd("df -h"), chat("hi")},
			Result{Type: "command", Command: "df -h", Explanation: "x", InputTokens: 1, OutputTokens: 2}},
		{"two commands: first wins", []toolCall{cmd("df -h"), cmd("ls")},
			Result{Type: "command", Command: "df -h", Explanation: "x", InputTokens: 1, OutputTokens: 2}},
		{"two chats: first wins", []toolCall{chat("first"), chat("second")},
			Result{Type: "chat", Response: "first", InputTokens: 1, OutputTokens: 2}},
		{"unknown tool names are ignored", []toolCall{{name: "bogus", args: json.RawMessage(`{}`)}, chat("hi")},
			Result{Type: "chat", Response: "hi", InputTokens: 1, OutputTokens: 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveToolCalls(tt.calls, 1, 2)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}

	t.Run("no usable call is an error", func(t *testing.T) {
		if _, err := resolveToolCalls(nil, 0, 0); err == nil || !strings.Contains(err.Error(), "no command or chat") {
			t.Fatalf("want no-command-or-chat error, got %v", err)
		}
	})

	t.Run("dropped calls are traced via Debugf", func(t *testing.T) {
		var traced []string
		old := Debugf
		Debugf = func(format string, args ...any) { traced = append(traced, fmt.Sprintf(format, args...)) }
		t.Cleanup(func() { Debugf = old })

		if _, err := resolveToolCalls([]toolCall{chat("hi"), cmd("ls")}, 0, 0); err != nil {
			t.Fatal(err)
		}
		if len(traced) != 1 || !strings.Contains(traced[0], "2 tool calls") || !strings.Contains(traced[0], "honoring the command call") {
			t.Fatalf("expected a dropped-call trace, got %v", traced)
		}

		traced = nil
		if _, err := resolveToolCalls([]toolCall{cmd("ls")}, 0, 0); err != nil {
			t.Fatal(err)
		}
		if len(traced) != 0 {
			t.Fatalf("single call should not trace, got %v", traced)
		}
	})
}
