// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestBashVersionSupported(t *testing.T) {
	tests := map[string]bool{
		"version unknown": false,
		"3.2.57":          false,
		"4.1.17":          false,
		"4.2.0":           true,
		"5.3.0":           true,
		"version 5.3.0":   true,
		"5.3.0(1)-rc1":    true,
	}
	for in, want := range tests {
		if got := bashVersionSupported(in); got != want {
			t.Fatalf("bashVersionSupported(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestProfileWiringStatus(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if got := profileWiringStatus(missing, "yo --init bash"); !strings.Contains(got, "not found") {
		t.Fatalf("missing profile status = %q", got)
	}

	profile := filepath.Join(dir, ".bashrc")
	if err := os.WriteFile(profile, []byte("if command -v yo >/dev/null 2>&1; then eval \"$(yo --init bash)\"; fi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := profileWiringStatus(profile, "yo --init bash"); !strings.Contains(got, "wired") {
		t.Fatalf("wired profile status = %q", got)
	}
	if profileNeedsAttention(profile, "yo --init bash") {
		t.Fatal("wired profile should not need attention")
	}
	if !profileNeedsAttention(profile, "yo --init zsh") {
		t.Fatal("wrong marker should need attention")
	}
}

func TestParsePowerShellDiagnosticOutput(t *testing.T) {
	got := parseKeyValueLines("version=7.4.1\r\nprofile=C:\\Users\\m\\Documents\\PowerShell\\profile.ps1\r\npsreadline=2.3.5\r\n")
	if got["version"] != "7.4.1" || got["psreadline"] != "2.3.5" {
		t.Fatalf("parseKeyValueLines() = %#v", got)
	}
	if !versionAtLeast(got["psreadline"], 2, 1) {
		t.Fatal("PSReadLine 2.3.5 should satisfy 2.1+")
	}
}

func TestDecodeShellOutputUTF16LE(t *testing.T) {
	encoded := utf16.Encode([]rune("version=5.1\r\nprofile=C:\\p.ps1\r\n"))
	data := []byte{0xFF, 0xFE}
	for _, r := range encoded {
		var buf [2]byte
		binary.LittleEndian.PutUint16(buf[:], r)
		data = append(data, buf[:]...)
	}
	got := parseKeyValueLines(decodeShellOutput(data))
	if got["version"] != "5.1" || got["profile"] != `C:\p.ps1` {
		t.Fatalf("decoded PowerShell output = %#v", got)
	}
}

func TestPowerShellPolicyBlocksProfiles(t *testing.T) {
	for _, in := range []string{"", "Undefined", "RemoteSigned", "Unrestricted", "Bypass"} {
		if powerShellPolicyBlocksProfiles(in) {
			t.Fatalf("%q should not block profiles", in)
		}
	}
	for _, in := range []string{"Restricted", "AllSigned"} {
		if !powerShellPolicyBlocksProfiles(in) {
			t.Fatalf("%q should block profiles", in)
		}
	}
}
