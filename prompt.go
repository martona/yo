// SPDX-License-Identifier: GPL-3.0-or-later
//
// The system prompt and tool descriptions adapt yoshell's (GPLv3) prompt design
// to PowerShell/Windows. yoshell's base prompt lives in bash-5.2.32/bashline.c
// and its tool definitions in readline-8.2.13/yo.c (yo_build_tools_anthropic).
// We keep yoshell's minimal-prompt approach for Anthropic — the tool
// descriptions carry the command-vs-chat routing — and describe the real shell.
package main

import "fmt"

// systemPrompt returns the Anthropic system prompt for the given model.
func systemPrompt(model string) string {
	return fmt.Sprintf(`You are powered by %s (provider: anthropic).

You are a command assistant for PowerShell on Windows. The user is at an interactive PowerShell prompt; any command you generate is prefilled at their prompt for them to review, edit, or run — nothing executes until they press Enter.

Generate idiomatic PowerShell (cmdlets such as Get-ChildItem, Where-Object, Select-String, Select-Object), not bash or cmd. Prefer a single readable pipeline. Use the command tool whenever the request is best answered by running something; use the chat tool only when no command is needed.`, model)
}

// buildTools returns the v0.1 tool set: command and chat. (scrollback and docs
// arrive with later milestones.)
func buildTools() []anthropicTool {
	return []anthropicTool{
		{
			Name:        "command",
			Description: "Generate a PowerShell command for the user to review and execute. The command will be prefilled at the prompt for the user to edit or run.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command":     map[string]any{"type": "string", "description": "The PowerShell command to execute"},
					"explanation": map[string]any{"type": "string", "description": "Brief explanation of what this command does, shown to the user before the command"},
				},
				"required": []string{"command", "explanation"},
			},
		},
		{
			Name:        "chat",
			Description: "Respond with a text message for questions and explanations; use ONLY when no command is needed.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"response": map[string]any{"type": "string", "description": "Your text response to the user"},
				},
				"required": []string{"response"},
			},
		},
	}
}
