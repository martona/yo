// SPDX-License-Identifier: GPL-3.0-or-later
package llm

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	profilePowerShell = "powershell"
	profilePOSIX      = "posix"
)

// CommandExample is one worked command-routing example in a system prompt.
type CommandExample struct {
	Request string
	Command string
}

// CommandProfile describes the shell command language the model should emit.
// Providers share this: Anthropic and OpenAI differ in API shape, not in what
// shell the user is sitting in.
type CommandProfile struct {
	Family              string
	Shell               string
	PromptDescription   string
	CommandNoun         string
	CommandTool         string
	CommandField        string
	CommandGuidance     string
	PendingFieldExample string
	MultiStepExample    string
	ScriptLimit         string
	OpenAIExamples      []CommandExample
}

// DetectCommandProfile picks the prompt profile for this invocation. Shell
// integrations should set YO_SHELL explicitly; otherwise we infer from the OS and
// (on Unix) $SHELL. Unknown Unix shells use the POSIX/Unix profile, which is the
// right default for zsh now and bash later.
func DetectCommandProfile() CommandProfile {
	shell := normalizeShell(os.Getenv("YO_SHELL"))
	if shell == "" {
		shell = normalizeShell(filepath.Base(os.Getenv("SHELL")))
	}

	switch shell {
	case "powershell", "pwsh":
		return powerShellProfile()
	case "zsh", "bash", "sh", "ksh", "dash":
		return posixProfile(shell)
	}

	if runtime.GOOS == "windows" {
		return powerShellProfile()
	}
	return posixProfile(shell)
}

func normalizeShell(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, ".exe")
	switch s {
	case "powershell", "powershell_ise":
		return "powershell"
	default:
		return s
	}
}

func powerShellProfile() CommandProfile {
	return CommandProfile{
		Family:              profilePowerShell,
		Shell:               "powershell",
		PromptDescription:   "PowerShell prompt on Windows",
		CommandNoun:         "PowerShell command",
		CommandTool:         "Generate a PowerShell command for the user to review and execute. The command will be prefilled at the prompt for the user to edit or run.",
		CommandField:        "The PowerShell command to execute",
		CommandGuidance:     "Generate idiomatic PowerShell (cmdlets such as Get-ChildItem, Where-Object, Select-String, Select-Object), not bash or cmd.",
		PendingFieldExample: "e.g. Get-Disk, Test-Path, Get-Process",
		MultiStepExample:    `"where is my USB drive mounted" -> Get-Disk / Get-Partition with pending=true, then read the result`,
		ScriptLimit:         "No long here-strings or multi-line scripts.",
		OpenAIExamples: []CommandExample{
			{Request: "what version of powershell do I have", Command: "$PSVersionTable.PSVersion"},
			{Request: "how much free disk space", Command: "Get-PSDrive -PSProvider FileSystem"},
			{Request: "show running processes by memory", Command: "Get-Process | Sort-Object WS -Descending | Select-Object -First 20"},
			{Request: "what's in this folder", Command: "Get-ChildItem"},
		},
	}
}

// osLabel is a human-friendly name for the OS the binary (and thus the user) runs on.
// It is the fallback when the shell integration did not supply a precise YO_OS.
func osLabel() string { return osLabelFor(runtime.GOOS) }

func osLabelFor(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	case "windows":
		return "Windows"
	default:
		return goos
	}
}

func posixProfile(shell string) CommandProfile {
	if shell == "" {
		shell = "POSIX-style shell"
	}
	return CommandProfile{
		Family:              profilePOSIX,
		Shell:               shell,
		PromptDescription:   shell + " prompt on a Unix-like system",
		CommandNoun:         "POSIX/Unix shell command",
		CommandTool:         "Generate a POSIX/Unix shell command for the user to review and execute. The command will be prefilled at the prompt for the user to edit or run.",
		CommandField:        "The POSIX/Unix shell command to execute",
		CommandGuidance:     "Generate portable POSIX/Unix shell commands using standard utilities such as ls, find, grep, sed, awk, ps, df, du, sort, and xargs. Do not generate PowerShell or cmd commands. Prefer portable commands over shell-specific syntax unless the request specifically needs the user's shell.",
		PendingFieldExample: "e.g. pwd, ls -la, df -h, ps -ef",
		MultiStepExample:    `"what is using space here" -> du -sh ./* 2>/dev/null with pending=true, then read the result`,
		ScriptLimit:         "No long here-docs or multi-line shell scripts.",
		OpenAIExamples: []CommandExample{
			{Request: "what shell am I using", Command: `printf '%s\n' "$SHELL"`},
			{Request: "how much free disk space", Command: "df -h"},
			{Request: "show running processes", Command: "ps -ef"},
			{Request: "what's in this folder", Command: "ls -la"},
		},
	}
}
