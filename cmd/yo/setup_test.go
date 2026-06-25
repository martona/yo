// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
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

func TestSetupRunnerInstallsAndUninstallsAllPosixProfiles(t *testing.T) {
	oldHost := powerShellSetupHost
	powerShellSetupHost = func() string { return "" }
	defer func() { powerShellSetupHost = oldHost }()

	home := filepath.Join(t.TempDir(), "home")
	zdot := filepath.Join(t.TempDir(), "zdot")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(zdot, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", zdot)
	t.Setenv("SHELL", "/bin/fish")
	t.Setenv("YO_SHELL", "")
	binDir := t.TempDir()
	yoName := "yo"
	if runtime.GOOS == "windows" {
		yoName = "yo.exe"
	}
	if err := os.WriteFile(filepath.Join(binDir, yoName), []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	var out bytes.Buffer
	runner := newSetupRunner(strings.NewReader("Y\nY\n"), &out, &out)
	if err := runner.installShells("/opt/yo/bin/yo"); err != nil {
		t.Fatal(err)
	}

	bashrc := filepath.Join(home, ".bashrc")
	zshrc := filepath.Join(zdot, ".zshrc")
	for _, tc := range []struct {
		path  string
		shell string
	}{
		{bashrc, "bash"},
		{zshrc, "zsh"},
	} {
		data, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{shellManagedStart, shellInitMarker(tc.shell), shellManagedEnd} {
			if !strings.Contains(string(data), want) {
				t.Fatalf("%s missing %q:\n%s", tc.path, want, data)
			}
		}
		if strings.Contains(string(data), "YO_BIN=") {
			t.Fatalf("%s contains YO_BIN fallback:\n%s", tc.path, data)
		}
	}

	out.Reset()
	runner = newSetupRunner(strings.NewReader("Y\nY\n"), &out, &out)
	if err := runner.uninstallShells(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{bashrc, zshrc} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), shellManagedStart) || strings.Contains(string(data), "yo --init") {
			t.Fatalf("uninstall left init block in %s:\n%s", path, data)
		}
	}
}

func TestSetupRunnerCopiesYoAddsLocalBinPathThenWiresProfiles(t *testing.T) {
	oldHost := powerShellSetupHost
	powerShellSetupHost = func() string { return "" }
	defer func() { powerShellSetupHost = oldHost }()

	home := filepath.Join(t.TempDir(), "home")
	zdot := filepath.Join(t.TempDir(), "zdot")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(zdot, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", zdot)
	t.Setenv("PATH", t.TempDir())
	source := filepath.Join(t.TempDir(), "yo-source")
	if err := os.WriteFile(source, []byte("fake yo binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	runner := newSetupRunner(strings.NewReader("Y\nY\nY\nY\nY\n"), &out, &out)
	if err := runner.installShells(source); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(home, ".local", "bin", posixInstallName())
	copiedData, err := os.ReadFile(copied)
	if err != nil {
		t.Fatal(err)
	}
	if string(copiedData) != "fake yo binary" {
		t.Fatalf("copied binary content = %q", copiedData)
	}
	bashrc := filepath.Join(home, ".bashrc")
	zshrc := filepath.Join(zdot, ".zshrc")
	for _, tc := range []struct {
		path  string
		shell string
	}{
		{bashrc, "bash"},
		{zshrc, "zsh"},
	} {
		data, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{shellPathStart, `$HOME/.local/bin`, shellManagedStart, shellInitMarker(tc.shell), shellManagedEnd} {
			if !strings.Contains(string(data), want) {
				t.Fatalf("%s missing %q:\n%s", tc.path, want, data)
			}
		}
		if strings.Contains(string(data), "YO_BIN=") {
			t.Fatalf("%s contains YO_BIN fallback:\n%s", tc.path, data)
		}
	}

	out.Reset()
	runner = newSetupRunner(strings.NewReader("Y\nY\n"), &out, &out)
	if err := runner.uninstallShells(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{bashrc, zshrc} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, unwanted := range []string{shellPathStart, shellManagedStart, "yo --init"} {
			if strings.Contains(string(data), unwanted) {
				t.Fatalf("uninstall left %q in %s:\n%s", unwanted, path, data)
			}
		}
	}
}

func TestSetupRunnerSkipsPosixProfilesWhenCopyDeclined(t *testing.T) {
	oldHost := powerShellSetupHost
	powerShellSetupHost = func() string { return "" }
	defer func() { powerShellSetupHost = oldHost }()

	home := filepath.Join(t.TempDir(), "home")
	zdot := filepath.Join(t.TempDir(), "zdot")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(zdot, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", zdot)
	t.Setenv("PATH", t.TempDir())
	source := filepath.Join(t.TempDir(), "yo-source")
	if err := os.WriteFile(source, []byte("fake yo binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	runner := newSetupRunner(strings.NewReader("n\n"), &out, &out)
	if err := runner.installShells(source); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(home, ".bashrc"), filepath.Join(zdot, ".zshrc")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s should not be written when copy is declined, stat err = %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", posixInstallName())); !os.IsNotExist(err) {
		t.Fatalf("yo should not be copied when copy is declined, stat err = %v", err)
	}
	if !strings.Contains(out.String(), "skipped bash/zsh profile wiring") {
		t.Fatalf("output should explain POSIX profile wiring was skipped:\n%s", out.String())
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
