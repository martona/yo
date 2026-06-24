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
