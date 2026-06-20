// SPDX-License-Identifier: GPL-3.0-or-later
package llm

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// maxSteps caps the continuation chain so the base64 state stays small enough to
// ride in an environment variable ($env:YO_STATE). Continuations are short in
// practice; if one runs long we keep the most recent steps.
const maxSteps = 12

// Step is one command issued during a continuation chain.
type Step struct {
	Command     string `json:"c"`
	Explanation string `json:"e,omitempty"`
	Exit        *int   `json:"x,omitempty"` // exit code, once the user has run it
}

// State is the continuation context carried between yo invocations (base64 JSON
// in $env:YO_STATE). It is provider-neutral: the continuation turn is plain
// text, so any provider can pick up a chain and there's no native tool-call
// history to reconstruct. Short JSON keys keep the encoded blob small.
type State struct {
	Query string `json:"q"` // the original request
	Steps []Step `json:"s"`
}

// DecodeState parses base64-JSON state. Empty input yields (nil, nil).
func DecodeState(b64 string) (*State, error) {
	b64 = strings.TrimSpace(b64)
	if b64 == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("corrupt continuation state: %w", err)
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("corrupt continuation state: %w", err)
	}
	return &s, nil
}

// Encode serializes the state to base64 JSON for $env:YO_STATE.
func (s *State) Encode() (string, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// AddStep records a newly issued command, trimming the oldest if over the cap.
func (s *State) AddStep(command, explanation string) {
	s.Steps = append(s.Steps, Step{Command: command, Explanation: explanation})
	if len(s.Steps) > maxSteps {
		s.Steps = s.Steps[len(s.Steps)-maxSteps:]
	}
}

// SetLastExit records the exit code of the most recently run step.
func (s *State) SetLastExit(code int) {
	if n := len(s.Steps); n > 0 {
		s.Steps[n-1].Exit = &code
	}
}

// ContinuationQuery synthesizes the next user turn: the original request, the
// steps run so far with exit codes, and instructions to emit the next command
// (or finish). Plain text -> provider-agnostic.
func (s *State) ContinuationQuery(lastExit int) string {
	var b strings.Builder
	b.WriteString("[continuation] You are guiding a multi-step PowerShell task, one command at a time.\n\n")
	fmt.Fprintf(&b, "Original request: %s\n\n", s.Query)
	b.WriteString("Commands run so far (most recent last):\n")
	for i, st := range s.Steps {
		fmt.Fprintf(&b, "%d. %s", i+1, st.Command)
		if st.Exit != nil {
			fmt.Fprintf(&b, "   -> exit %d", *st.Exit)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "\nThe last command exited with code %d (0 = success). ", lastExit)
	b.WriteString("Provide the NEXT single command via the command tool, with pending=true if more steps remain or pending=false if it is the last. If the task is already complete, or the last step failed and you cannot recover, use the chat tool to explain.")
	return b.String()
}
