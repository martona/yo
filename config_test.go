// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

func TestCleanKey(t *testing.T) {
	cases := map[string]string{
		"sk-ant-abc":          "sk-ant-abc",
		"sk-ant-abc\n":        "sk-ant-abc",
		"sk-ant-abc\r\n":      "sk-ant-abc",
		"  sk-ant-abc  ":      "sk-ant-abc",
		"sk-ant-abc\nGARBAGE": "sk-ant-abc", // only the first line
	}
	for in, want := range cases {
		if got := cleanKey(in); got != want {
			t.Errorf("cleanKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadyValidatesKey(t *testing.T) {
	bom := string(rune(0xFEFF))
	if err := (Config{Key: "sk-ant-ok_123-XYZ"}).ready(); err != nil {
		t.Errorf("valid key rejected: %v", err)
	}
	for _, k := range []string{"", "sk-ant\nbad", bom + "sk", "sk ant", "sk\tant"} {
		if err := (Config{Key: k}).ready(); err == nil {
			t.Errorf("bad key %q should be rejected", k)
		}
	}
}

func TestDecodeText(t *testing.T) {
	utf16le := func(s string, withBOM bool) []byte {
		var out []byte
		if withBOM {
			out = append(out, 0xFF, 0xFE)
		}
		for _, u := range utf16.Encode([]rune(s)) {
			var p [2]byte
			binary.LittleEndian.PutUint16(p[:], u)
			out = append(out, p[0], p[1])
		}
		return out
	}
	utf16be := func(s string) []byte {
		out := []byte{0xFE, 0xFF}
		for _, u := range utf16.Encode([]rune(s)) {
			var p [2]byte
			binary.BigEndian.PutUint16(p[:], u)
			out = append(out, p[0], p[1])
		}
		return out
	}

	if got := decodeText([]byte("sk-ant-abc")); got != "sk-ant-abc" {
		t.Errorf("utf8: got %q", got)
	}
	if got := decodeText(append([]byte{0xEF, 0xBB, 0xBF}, "sk"...)); got != "sk" {
		t.Errorf("utf8-bom: got %q", got)
	}
	if got := decodeText(utf16le("sk-ant", true)); got != "sk-ant" {
		t.Errorf("utf16le-bom: got %q", got)
	}
	if got := decodeText(utf16le("sk-ant", false)); got != "sk-ant" {
		t.Errorf("utf16le-nobom: got %q", got)
	}
	if got := decodeText(utf16be("sk-ant")); got != "sk-ant" {
		t.Errorf("utf16be-bom: got %q", got)
	}
	// End-to-end: a UTF-16LE key file with trailing CRLF cleans to a valid key.
	if got := cleanKey(decodeText(utf16le("sk-ant-xyz\r\n", true))); got != "sk-ant-xyz" {
		t.Errorf("utf16 key end-to-end: got %q", got)
	}
}
