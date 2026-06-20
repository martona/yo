// SPDX-License-Identifier: GPL-3.0-or-later

// Package llm turns a natural-language query into a normalized Result by talking
// to a provider (Anthropic, OpenAI, ...). Each provider speaks its own API but
// returns the same Result shape.
package llm

// Result is the normalized outcome of a query, emitted by cmd/yo as one JSON
// line. The shell snippet switches on Type: "command" -> prefill, "chat" ->
// print, "error" -> show the message.
type Result struct {
	Type        string `json:"type"` // "command" | "chat" | "error"
	Command     string `json:"command,omitempty"`
	Explanation string `json:"explanation,omitempty"`
	Response    string `json:"response,omitempty"`
	Message     string `json:"message,omitempty"` // populated when Type == "error"
}
