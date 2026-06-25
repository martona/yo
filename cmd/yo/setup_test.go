// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/martona/yo/internal/session"
	tokens "github.com/martona/yo/internal/usage"
)

func TestShellIsZsh(t *testing.T) {
	for _, name := range []string{"zsh", "/bin/zsh", "/opt/homebrew/bin/-zsh"} {
		if !shellIsZsh(name) {
			t.Fatalf("shellIsZsh(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"bash", "/bin/pwsh", ""} {
		if shellIsZsh(name) {
			t.Fatalf("shellIsZsh(%q) = true, want false", name)
		}
	}
}

func TestShellIsBash(t *testing.T) {
	for _, name := range []string{"bash", "/bin/bash", "/opt/homebrew/bin/-bash"} {
		if !shellIsBash(name) {
			t.Fatalf("shellIsBash(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"zsh", "/bin/pwsh", ""} {
		if shellIsBash(name) {
			t.Fatalf("shellIsBash(%q) = true, want false", name)
		}
	}
}

func TestParseProviderChoice(t *testing.T) {
	tests := map[string]string{
		"1":         "anthropic",
		"Anthropic": "anthropic",
		"2":         "openai",
		"OPENAI":    "openai",
		"3":         "grok",
		"Grok":      "grok",
		"xai":       "grok",
		"4":         "gemini",
		"Gemini":    "gemini",
		"google":    "gemini",
		"":          "",
		"bogus":     "",
	}
	for in, want := range tests {
		if got := parseProviderChoice(in); got != want {
			t.Fatalf("parseProviderChoice(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRemoveStateFilesDeletesStateKeepsYoconf(t *testing.T) {
	// Isolate every location removeStateFiles touches.
	t.Setenv("YO_USAGE_DIR", t.TempDir())
	tmp := t.TempDir()
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	t.Setenv("TMPDIR", tmp)
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed yo's own state plus a ~/.yoconf that must survive.
	tokens.Add("sess-x", 10, 2)
	session.Append("sess-x", session.Exchange{Query: "q", Type: "chat", Response: "a"})
	yoconf := filepath.Join(home, ".yoconf")
	if err := os.WriteFile(yoconf, []byte("provider openai\nkey sk-keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	s := newSetupRunner(strings.NewReader("y\n"), &out, &out)
	s.removeStateFiles()

	if _, err := os.Stat(tokens.Path()); !os.IsNotExist(err) {
		t.Fatalf("token usage file should be gone, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(session.Path(), "sess-sess-x.json")); !os.IsNotExist(err) {
		t.Fatalf("session cache file should be gone, stat err = %v", err)
	}
	if _, err := os.Stat(yoconf); err != nil {
		t.Fatalf("~/.yoconf must be left in place, got %v", err)
	}
	if !strings.Contains(out.String(), yoconf) {
		t.Fatalf("output should mention the left-behind ~/.yoconf:\n%s", out.String())
	}
}

func TestRemoveStateFilesDeclineKeepsEverything(t *testing.T) {
	t.Setenv("YO_USAGE_DIR", t.TempDir())
	tmp := t.TempDir()
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	t.Setenv("TMPDIR", tmp)
	t.Setenv("HOME", t.TempDir())

	tokens.Add("sess-y", 5, 1)

	var out bytes.Buffer
	s := newSetupRunner(strings.NewReader("n\n"), &out, &out)
	s.removeStateFiles()

	if _, err := os.Stat(tokens.Path()); err != nil {
		t.Fatalf("declining should leave the token usage file in place, got %v", err)
	}
}

func TestUpsertYoconfProviderKeyCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".yoconf")
	if err := upsertYoconfProviderKey(path, "openai", "sk-test"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "provider openai\nkey sk-test\n"; string(data) != want {
		t.Fatalf(".yoconf = %q, want %q", string(data), want)
	}
}

func TestUpsertYoconfProviderKeyReplacesActiveDirectives(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".yoconf")
	initial := "# keep this comment\nprovider anthropic\nmodel custom\nkey old-key\n# key commented-out\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := upsertYoconfProviderKey(path, "openai", "new-key"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{"# keep this comment", "provider openai", "model custom", "key new-key", "# key commented-out"} {
		if !strings.Contains(out, want) {
			t.Fatalf(".yoconf missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "provider anthropic") || strings.Contains(out, "key old-key") {
		t.Fatalf(".yoconf kept stale provider/key:\n%s", out)
	}
}
