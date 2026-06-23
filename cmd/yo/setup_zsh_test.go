// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"path/filepath"
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
