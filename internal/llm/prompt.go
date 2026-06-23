// SPDX-License-Identifier: GPL-3.0-or-later
//
// System prompts and tool descriptions, adapted from yoshell's (GPLv3) prompt
// design. yoshell's base prompt is in bashline.c and its tool definitions in
// readline-8.2.13/yo.c. The descriptions are shared across providers; each
// provider renders them into its own tool/schema format and pairs them with the
// right system prompt (minimal for Anthropic, worked-example heavy for
// OpenAI-style models, which over-use chat otherwise).
package llm

import (
	"fmt"
	"strings"
)

// Shared tool surface (names, descriptions, field docs).
const (
	toolCommand = "command"
	toolChat    = "chat"

	descExplainFld = "Brief explanation of what this command does, shown to " +
		"the user before the command"
	descChat = "Respond with text ONLY when there is genuinely nothing to run -- " +
		"greetings, opinions, or conceptual/explanatory answers. If your reply " +
		"would contain, recommend, or describe ANY command the user could run, do " +
		"NOT use this tool: use the command tool and prefill it instead. " +
		"Describing a runnable command here instead of prefilling it is a failure."
	descChatFld = "Your text response to the user"
)

func descCommandBias(profile CommandProfile) string {
	return "CRITICAL: if the request can be satisfied by running " +
		"something, you MUST use this tool, never chat -- including installs, " +
		"removals, and config changes. You propose; the user disposes: the " +
		"command is prefilled and runs only when they press Enter, so reviewing " +
		"is their job and caution is never a reason to withhold a command or to " +
		"merely describe one in prose. If you already know the command (even from " +
		"prior context), prefill it -- do not explain it instead, and never ask " +
		"\"want me to prefill that?\"; just prefill. Favor the simplest command " +
		"that does the job -- one the user could learn and reuse -- over an " +
		"intricate one-liner that computes the exact answer; when precision would " +
		"otherwise need a long or multi-stage pipeline, prefer a simple, well-known " +
		"command with pending=true and read its output to answer. " + profile.ScriptLimit
}

func descPending(profile CommandProfile) string {
	return "Set to true when you need to see this command's output " +
		"before you can answer or decide the next action -- either one step of a " +
		"sequence, or a single investigative command (" + profile.PendingFieldExample + ") " +
		"whose result you must read first. After the user runs it you " +
		"will receive its terminal output and can then give the next command or " +
		"answer with the chat tool. Set false on the final or only step."
}

// multiStep guidance is appended to both system prompts. It covers both the
// sequential-steps case and yoshell's "investigate first" case -- run a
// diagnostic with pending=true, read its output, then answer or continue.
func multiStep(profile CommandProfile) string {
	return "MULTI-STEP & INVESTIGATE-FIRST: Set pending=true and issue ONE " +
		"command at a time whenever a task has sequential steps, OR when you must " +
		"see a command's output before you can answer or choose the next action " +
		"(e.g. " + profile.MultiStepExample + "). After each pending command you receive " +
		"its terminal output and exit code; reply with the next command, or with the " +
		"chat tool to give the user the answer once you have it. Set pending=false on " +
		"the last or only step. Do NOT chain steps with && or ; -- one command per step."
}

// diagnostics is appended to both system prompts. yoshell fetches scrollback on
// demand via a tool; yo instead injects it as a [terminal context] block when it
// can be captured, so the model must be told to READ that block for "why did it
// fail" questions -- and, since yo has no on-demand fetch, never to fall back to
// asking the user to paste output.
const diagnostics = "DIAGNOSTICS: When the user asks why something failed or what " +
	"went wrong (\"why did that fail\", \"what happened\", \"that didn't work\", or " +
	"mentions an error), recent terminal output is provided above as a " +
	"[terminal context] block whenever it could be captured -- read it and answer " +
	"from it; do NOT ask the user what they were doing. NEVER ask the user to " +
	"paste logs, output, or errors. If no terminal context is present, prefill a " +
	"command that surfaces the problem, or answer from what you know -- but never " +
	"ask for a paste."

// anthropicSystemPrompt is intentionally minimal — Anthropic's tool descriptions
// carry the command-vs-chat routing on their own.
func anthropicSystemPrompt(model string, profile CommandProfile) string {
	return fmt.Sprintf(`You are powered by %s (provider: anthropic).

You are a command assistant for a %s. The user is at an
interactive prompt; any command you generate is prefilled at their prompt for
them to review, edit, or run -- nothing executes until they press Enter.

%s Prefer the simplest, most reusable command that answers the request --
ideally one worth remembering -- not an intricate one-liner built to compute the
exact answer. Use the command tool whenever the request is best answered by
running something; use the chat tool only when no command is needed.

If the user asks a question that has an obvious command as an answer, you
must use the command tool; you can elaborate in the explanation field.

Do not ask "want me to prefill that command"; just do it if it might be useful.

Do not use markdown formatting in plain-text response blocks; the text you output
will be rendered on a terminal.

%s

%s`, model, profile.PromptDescription, profile.CommandGuidance, multiStep(profile), diagnostics)
}

// openaiSystemPrompt biases hard toward commands with worked examples, because
// (per yoshell) Responses-API models over-use chat for things a shell assistant
// should answer with a command.
func openaiSystemPrompt(model string, profile CommandProfile) string {
	return fmt.Sprintf(`You are powered by %s (provider: openai).

You are a command assistant for a %s. The user is at an interactive prompt; any
command you generate is prefilled for them to review, edit, or run -- nothing
executes until they press Enter.

%s Prefer the simplest, most reusable command that answers the request -- ideally
one worth remembering -- not an intricate one-liner built to compute the exact
answer.

If the user asks a question that has an obvious command as an answer, you
must use the command tool; you can elaborate in the explanation field.

Do not ask "want me to prefill that command"; just do it if it might be useful.

Do not use markdown formatting in plain-text response blocks; the text you output
will be rendered on a terminal.

When in doubt between the command and chat tools, ALWAYS choose command.
Use chat ONLY for greetings/casual conversation or abstract conceptual
questions. If the request can be answered by running something on this
machine, you MUST use the command tool. Examples that MUST use command:
%s

%s

%s`, model, profile.PromptDescription, profile.CommandGuidance, formatExamples(profile.OpenAIExamples), multiStep(profile), diagnostics)
}

func formatExamples(examples []CommandExample) string {
	var b strings.Builder
	for _, ex := range examples {
		fmt.Fprintf(&b, "- %q -> %s\n", ex.Request, ex.Command)
	}
	return strings.TrimRight(b.String(), "\n")
}

// WithTerminalContext prepends recent terminal output to the query as context,
// framed (yoshell-style) so the model treats it as past/completed output and uses
// it only when relevant. Returns the query unchanged when scrollback is empty.
func WithTerminalContext(query, scrollback string) string {
	if scrollback == "" {
		return query
	}
	return fmt.Sprintf("[terminal context] Recent output from the user's "+
		"terminal, most recent at the bottom. These are "+
		"COMPLETED commands from the PAST; any prompts shown "+
		"were already handled. Ignore stray escape-code "+
		"artifacts and focus on real output. This is general "+
		"terminal history, not necessarily about the request -- "+
		"use it only if it helps answer what follows. It is context for the "+
		"command you prefill, not a reason to answer in prose instead of "+
		"prefilling.\n\n```\n%s\n```\n\n[request] %s", scrollback, query)
}

// WithSessionMemory prepends a compact history of recent yo exchanges to the query,
// framed as background continuity (what the user has been doing this session) so the
// model uses it only to resolve references, not as the current ask. Returns the
// query unchanged when history is empty. It is prepend-only (no [request] marker of
// its own), so wrapping a WithTerminalContext result yields a single [request]:
// history block, then terminal block, then the request.
func WithSessionMemory(query, history string) string {
	if history == "" {
		return query
	}
	return fmt.Sprintf("[recent yo history] Your earlier exchanges with this user "+
		"in the current shell session, oldest first. This is BACKGROUND for "+
		"continuity only (e.g. resolving \"that file\" or \"the previous one\"); it "+
		"is NOT the current request, which follows below. Use it to inform the "+
		"command you prefill (e.g. what \"it\" or \"that\" refers to), not as a "+
		"reason to answer in prose instead of prefilling a command.\n\n%s\n%s", history, query)
}
