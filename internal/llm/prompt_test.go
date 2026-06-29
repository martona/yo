// SPDX-License-Identifier: GPL-3.0-or-later
package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/martona/yo/internal/config"
)

func TestDetectCommandProfile(t *testing.T) {
	t.Setenv("YO_SHELL", "zsh")
	got := DetectCommandProfile()
	if got.Family != profilePOSIX || got.Shell != "zsh" {
		t.Fatalf("YO_SHELL=zsh profile = %+v", got)
	}

	t.Setenv("YO_SHELL", "pwsh")
	got = DetectCommandProfile()
	if got.Family != profilePowerShell {
		t.Fatalf("YO_SHELL=pwsh profile = %+v", got)
	}

	t.Setenv("YO_SHELL", "")
	t.Setenv("SHELL", "/bin/bash")
	got = DetectCommandProfile()
	if got.Family != profilePOSIX || got.Shell != "bash" {
		t.Fatalf("SHELL=/bin/bash profile = %+v", got)
	}
}

func TestEnvironmentLineInPrompt(t *testing.T) {
	// The system prompt must state the user's OS and shell so the model picks
	// OS-appropriate tools; precise versions come from the shell integration via
	// YO_OS / YO_SHELL_VERSION, with the OS family as the fallback.
	t.Run("uses YO_OS and YO_SHELL_VERSION when set", func(t *testing.T) {
		t.Setenv("YO_OS", "macOS 14.5")
		t.Setenv("YO_SHELL_VERSION", "5.9")
		got := environmentLine(posixProfile("zsh"))
		for _, want := range []string{"macOS 14.5", "zsh 5.9"} {
			if !strings.Contains(got, want) {
				t.Errorf("environment line missing %q: %s", want, got)
			}
		}
		if strings.Contains(got, "lsblk") || strings.Contains(got, "diskutil") {
			t.Errorf("environment line should not enumerate per-OS tools: %s", got)
		}
	})

	t.Run("falls back to the OS family without YO_OS", func(t *testing.T) {
		t.Setenv("YO_OS", "")
		t.Setenv("YO_SHELL_VERSION", "")
		got := environmentLine(posixProfile("bash"))
		if !strings.Contains(got, osLabel()) {
			t.Errorf("environment line missing OS fallback %q: %s", osLabel(), got)
		}
		// No version supplied -> bare shell family, no trailing version number.
		if !strings.Contains(got, "shell bash") {
			t.Errorf("environment line missing bare shell family: %s", got)
		}
	})

	// It is woven into the assembled system prompt, not just a helper.
	t.Setenv("YO_OS", "Ubuntu 24.04 LTS")
	t.Setenv("YO_SHELL_VERSION", "5.2.21")
	if p := anthropicSystemPrompt("m", posixProfile("bash")); !strings.Contains(p, "Ubuntu 24.04 LTS") || !strings.Contains(p, "bash 5.2.21") {
		t.Errorf("anthropic prompt missing environment line:\n%s", p)
	}
	if p := openaiSystemPrompt("m", "openai", posixProfile("bash")); !strings.Contains(p, "Ubuntu 24.04 LTS") || !strings.Contains(p, "bash 5.2.21") {
		t.Errorf("openai prompt missing environment line:\n%s", p)
	}
}

func TestPromptProfilesSplitPowerShellAndPOSIX(t *testing.T) {
	ps := powerShellProfile()
	posix := posixProfile("zsh")

	psPrompt := openaiSystemPrompt("model", "openai", ps)
	for _, want := range []string{"PowerShell prompt on Windows", "Get-PSDrive", "Get-ChildItem"} {
		if !strings.Contains(psPrompt, want) {
			t.Errorf("PowerShell prompt missing %q:\n%s", want, psPrompt)
		}
	}
	if strings.Contains(psPrompt, "POSIX/Unix") || strings.Contains(psPrompt, "ls -la") {
		t.Errorf("PowerShell prompt leaked POSIX examples:\n%s", psPrompt)
	}

	posixPrompt := openaiSystemPrompt("model", "openai", posix)
	for _, want := range []string{"zsh prompt on a Unix-like system", "POSIX/Unix shell commands", "df -h", "ls -la"} {
		if !strings.Contains(posixPrompt, want) {
			t.Errorf("POSIX prompt missing %q:\n%s", want, posixPrompt)
		}
	}
	for _, leak := range []string{"Get-PSDrive", "Get-ChildItem", "PowerShell prompt on Windows"} {
		if strings.Contains(posixPrompt, leak) {
			t.Errorf("POSIX prompt leaked %q:\n%s", leak, posixPrompt)
		}
	}
}

func TestProviderRequestUsesDetectedProfile(t *testing.T) {
	t.Setenv("YO_SHELL", "zsh")

	p := newOpenAI(config.Config{Model: "test-model"})
	body, err := p.Request("what is here")
	if err != nil {
		t.Fatal(err)
	}

	var req openaiRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.Instructions, "POSIX/Unix shell commands") {
		t.Fatalf("request did not use POSIX prompt:\n%s", req.Instructions)
	}
	if len(req.Tools) == 0 || !strings.Contains(req.Tools[0].Description, "POSIX/Unix shell command") {
		t.Fatalf("request did not use POSIX tool description: %+v", req.Tools)
	}
}

func TestWithTerminalContext(t *testing.T) {
	if got := WithTerminalContext("do x", ""); got != "do x" {
		t.Errorf("empty scrollback should pass the query through, got %q", got)
	}
	got := WithTerminalContext("why did it fail", "$ build\nerror: boom")
	for _, want := range []string{"why did it fail", "error: boom", "terminal context", "[request]"} {
		if !strings.Contains(got, want) {
			t.Errorf("result missing %q:\n%s", want, got)
		}
	}
}
