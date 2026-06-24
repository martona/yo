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

	// Pending marks a command as one step of a multi-step task: the snippet runs
	// it, then fires a continuation step. Always false for chat/error.
	Pending bool `json:"pending,omitempty"`
	// State is the base64 continuation blob for the snippet to stash in
	// $env:YO_STATE. Present iff a continuation is active; empty means "clear".
	State string `json:"state,omitempty"`

	// PrefillSpace tells the snippet to prefix the prefilled command with a leading
	// space (history hygiene; see config.Config.PrefillSpace). Only meaningful for
	// Type == "command"; the command string itself is left clean.
	PrefillSpace bool `json:"prefillSpace,omitempty"`

	Message string `json:"message,omitempty"` // populated when Type == "error"

	// InputTokens / OutputTokens carry the provider-reported usage for this call,
	// for local token accounting (yo --tokens). json:"-" keeps them out of the
	// binary<->snippet contract: neither the JSON nor the sh encoder emits them;
	// they are read in-process right after the call and folded into the tally.
	InputTokens  int `json:"-"`
	OutputTokens int `json:"-"`
}
