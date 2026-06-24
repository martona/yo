// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
