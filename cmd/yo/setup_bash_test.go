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

func TestBashManagedBlockUsesPathLookup(t *testing.T) {
	block := bashManagedBlock("/tmp/yo bin/yo")
	for _, want := range []string{
		shellManagedStart,
		`eval "$(yo --init bash)"`,
		shellManagedEnd,
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("managed block missing %q:\n%s", want, block)
		}
	}
	for _, unwanted := range []string{"YO_BIN=", "/tmp/yo bin/yo"} {
		if strings.Contains(block, unwanted) {
			t.Fatalf("managed block contains fallback %q:\n%s", unwanted, block)
		}
	}
}

func TestRemoveBashManagedBlock(t *testing.T) {
	content := "before\n" + bashManagedBlock("/tmp/yo") + "\nafter\n"
	got, removed := removeBashInit(content)
	if !removed {
		t.Fatal("removeBashInit did not report removal")
	}
	if strings.Contains(got, shellInitMarker("bash")) || strings.Contains(got, shellManagedStart) {
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
	for _, want := range []string{shellManagedStart, shellInitMarker("bash"), shellManagedEnd} {
		if !strings.Contains(string(bashrcData), want) {
			t.Fatalf(".bashrc missing %q:\n%s", want, bashrcData)
		}
	}
	if strings.Contains(string(bashrcData), "YO_BIN=") {
		t.Fatalf(".bashrc contains YO_BIN fallback:\n%s", bashrcData)
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
	if strings.Contains(string(bashrcData), shellInitMarker("bash")) || strings.Contains(string(bashrcData), shellManagedStart) {
		t.Fatalf("uninstall left bash init marker:\n%s", bashrcData)
	}
}
