// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBashProfilePathFallsBackToHome(t *testing.T) {
	env := map[string]string{"HOME": "/home/marton"}
	got, err := bashProfilePathFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/home/marton", ".bashrc"); got != want {
		t.Fatalf("profile path = %q, want %q", got, want)
	}
}

func TestBashManagedBlockPinsBinaryFallback(t *testing.T) {
	block := bashManagedBlock("/tmp/yo bin/yo")
	for _, want := range []string{
		bashManagedStart,
		`eval "$(yo --init bash)"`,
		`export YO_BIN='/tmp/yo bin/yo'`,
		`eval "$('/tmp/yo bin/yo' --init bash)"`,
		bashManagedEnd,
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("managed block missing %q:\n%s", want, block)
		}
	}
}

func TestRemoveBashManagedBlock(t *testing.T) {
	content := "before\n" + bashManagedBlock("/tmp/yo") + "\nafter\n"
	got, removed := removeBashInit(content)
	if !removed {
		t.Fatal("removeBashInit did not report removal")
	}
	if strings.Contains(got, bashInitMarker) || strings.Contains(got, bashManagedStart) {
		t.Fatalf("managed block still present:\n%s", got)
	}
	for _, want := range []string{"before", "after"} {
		if !strings.Contains(got, want) {
			t.Fatalf("kept content missing %q:\n%s", want, got)
		}
	}
}

func TestSetupRunnerBashWritesProfileThenUninstalls(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("YO_SHELL", "bash")

	var out bytes.Buffer
	runner := newSetupRunner(strings.NewReader("Y\n"), &out, &out)
	if err := runner.installShell("bash", "/opt/yo/bin/yo"); err != nil {
		t.Fatal(err)
	}

	bashrc := filepath.Join(home, ".bashrc")
	bashrcData, err := os.ReadFile(bashrc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{bashManagedStart, bashInitMarker, "YO_BIN=", bashManagedEnd} {
		if !strings.Contains(string(bashrcData), want) {
			t.Fatalf(".bashrc missing %q:\n%s", want, bashrcData)
		}
	}

	out.Reset()
	runner = newSetupRunner(strings.NewReader("Y\n"), &out, &out)
	if err := runner.uninstallShell("bash"); err != nil {
		t.Fatal(err)
	}
	bashrcData, err = os.ReadFile(bashrc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bashrcData), bashInitMarker) || strings.Contains(string(bashrcData), bashManagedStart) {
		t.Fatalf("uninstall left bash init marker:\n%s", bashrcData)
	}
}
