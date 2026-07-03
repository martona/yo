// SPDX-License-Identifier: GPL-3.0-or-later
package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Debugf, when set by the caller (cmd/yo wires it to its dbg tracer), receives
// one-line parse diagnostics -- currently just dropped tool calls, so a model
// response carrying more than one call is visible in yo's debug output instead
// of being discarded silently. Default is a no-op.
var Debugf = func(format string, args ...any) {}

// toolCall is one raw tool invocation from a provider response: the tool name
// plus its JSON-encoded arguments, before selection and decoding. Each provider
// extracts these from its own wire format; selection policy is shared.
type toolCall struct {
	name string
	args json.RawMessage
}

// resolveToolCalls picks which tool call to honor and decodes it into a Result.
// Providers force at least one call (tool_choice any/required), but a model can
// emit several. The shape observed in the wild is chat followed by command: the
// model routes to chat, changes its mind while writing the response
// ("prefilling that now..."), and appends the command call it can no longer
// fold into the already-committed chat. So a command call wins over chat
// regardless of order -- the prompt's own tie-break ("when in doubt, ALWAYS
// choose command") -- and among commands the first wins. Anything dropped is
// traced via Debugf.
func resolveToolCalls(calls []toolCall, inTok, outTok int) (Result, error) {
	chosen := -1
	for i, c := range calls {
		if c.name == toolCommand {
			chosen = i
			break
		}
		if c.name == toolChat && chosen == -1 {
			chosen = i
		}
	}
	if chosen == -1 {
		return Result{}, fmt.Errorf("model returned no command or chat")
	}
	if len(calls) > 1 {
		names := make([]string, len(calls))
		for i, c := range calls {
			names[i] = c.name
		}
		Debugf("<- %d tool calls in one response (%s); honoring the %s call",
			len(calls), strings.Join(names, ", "), calls[chosen].name)
	}

	c := calls[chosen]
	if c.name == toolCommand {
		var in struct {
			Command     string `json:"command"`
			Explanation string `json:"explanation"`
			Pending     bool   `json:"pending"`
		}
		if err := json.Unmarshal(c.args, &in); err != nil {
			return Result{}, fmt.Errorf("bad command tool input: %w", err)
		}
		return Result{Type: "command", Command: in.Command, Explanation: in.Explanation, Pending: in.Pending, InputTokens: inTok, OutputTokens: outTok}, nil
	}
	var in struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(c.args, &in); err != nil {
		return Result{}, fmt.Errorf("bad chat tool input: %w", err)
	}
	return Result{Type: "chat", Response: in.Response, InputTokens: inTok, OutputTokens: outTok}, nil
}
