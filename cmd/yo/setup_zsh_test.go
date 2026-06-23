// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestZshProfilePathUsesZDOTDIR(t *testing.T) {
	env := map[string]string{"HOME": "/home/marton", "ZDOTDIR": "/tmp/zsh-dotdir"}
	got, err := zshProfilePathFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/zsh-dotdir", ".zshrc"); got != want {
		t.Fatalf("profile path = %q, want %q", got, want)
	}
}

func TestZshProfilePathFallsBackToHome(t *testing.T) {
	env := map[string]string{"HOME": "/home/marton"}
	got, err := zshProfilePathFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/home/marton", ".zshrc"); got != want {
		t.Fatalf("profile path = %q, want %q", got, want)
	}
}

func TestZshManagedBlockPinsBinaryFallback(t *testing.T) {
	block := zshManagedBlock("/tmp/yo bin/yo")
	for _, want := range []string{
		zshManagedStart,
		`eval "$(yo --init zsh)"`,
		`export YO_BIN='/tmp/yo bin/yo'`,
		`eval "$('/tmp/yo bin/yo' --init zsh)"`,
		zshManagedEnd,
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("managed block missing %q:\n%s", want, block)
		}
	}
}

func TestRemoveZshManagedBlock(t *testing.T) {
	content := "before\n" + zshManagedBlock("/tmp/yo") + "\nafter\n"
	got, removed := removeZshInit(content)
	if !removed {
		t.Fatal("removeZshInit did not report removal")
	}
	if strings.Contains(got, zshInitMarker) || strings.Contains(got, zshManagedStart) {
		t.Fatalf("managed block still present:\n%s", got)
	}
	for _, want := range []string{"before", "after"} {
		if !strings.Contains(got, want) {
			t.Fatalf("kept content missing %q:\n%s", want, got)
		}
	}
}

func TestRemoveZshManagedLine(t *testing.T) {
	content := "before\n" + zshLegacyComment + "\n" + zshInitLine + "\nafter\n"
	got, removed := removeZshInit(content)
	if !removed {
		t.Fatal("removeZshInit did not report removal")
	}
	if want := "before\nafter\n"; got != want {
		t.Fatalf("removeZshInit() = %q, want %q", got, want)
	}
}

func TestSetupRunnerZshWritesProfileAndYoconfThenUninstalls(t *testing.T) {
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
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("YO_SHELL", "zsh")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	var out bytes.Buffer
	runner := newSetupRunner(strings.NewReader("Y\nY\n1\nsk-ant-test\n"), &out, &out)
	if err := runner.installShell("zsh", "/opt/yo/bin/yo"); err != nil {
		t.Fatal(err)
	}
	if err := runner.configureKey(); err != nil {
		t.Fatal(err)
	}

	zshrc := filepath.Join(zdot, ".zshrc")
	zshrcData, err := os.ReadFile(zshrc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{zshManagedStart, zshInitMarker, "YO_BIN=", zshManagedEnd} {
		if !strings.Contains(string(zshrcData), want) {
			t.Fatalf(".zshrc missing %q:\n%s", want, zshrcData)
		}
	}

	yoconf := filepath.Join(home, ".yoconf")
	yoconfData, err := os.ReadFile(yoconf)
	if err != nil {
		t.Fatal(err)
	}
	if want := "provider anthropic\nkey sk-ant-test\n"; string(yoconfData) != want {
		t.Fatalf(".yoconf = %q, want %q", yoconfData, want)
	}
	if info, err := os.Stat(yoconf); runtime.GOOS != "windows" && err == nil && info.Mode().Perm() != 0o600 {
		t.Fatalf(".yoconf mode = %v, want 0600", info.Mode().Perm())
	}

	out.Reset()
	runner = newSetupRunner(strings.NewReader("Y\n"), &out, &out)
	if err := runner.uninstallShell("zsh"); err != nil {
		t.Fatal(err)
	}
	zshrcData, err = os.ReadFile(zshrc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(zshrcData), zshInitMarker) || strings.Contains(string(zshrcData), zshManagedStart) {
		t.Fatalf("uninstall left zsh init marker:\n%s", zshrcData)
	}
	if got, err := os.ReadFile(yoconf); err != nil || string(got) != string(yoconfData) {
		t.Fatalf("uninstall changed .yoconf: got %q err %v", got, err)
	}
}
