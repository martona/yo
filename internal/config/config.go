// SPDX-License-Identifier: GPL-3.0-or-later

// Package config loads yo's per-invocation settings from ~/.yoconf and the
// provider key files, tolerating the UTF-16/BOM encodings Windows tools emit.
package config

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// Config holds the per-invocation settings, read fresh on every run (like
// yoshell re-reads ~/.yoconf each time).
type Config struct {
	Provider string
	Model    string
	Key      string
	BaseURL  string
}

// providerDefaults holds the per-provider model and key-file fallbacks.
type providerDefaults struct {
	model   string
	keyFile string // relative to the home dir
}

// defaults mirror yoshell's conventions; override either via ~/.yoconf.
var defaults = map[string]providerDefaults{
	"anthropic": {model: "claude-sonnet-4-6", keyFile: ".anthropickey"},
	"openai":    {model: "gpt-5.2", keyFile: ".openaikey"},
}

// inferOrder is the key-file probe order when ~/.yoconf names no provider.
var inferOrder = []string{"anthropic", "openai"}

// Load reads ~/.yoconf (if present), resolves the provider, then fills in the
// default model and the key (from the provider's key file) when not already set.
// A missing key is NOT fatal here — it's checked at call time (Ready) so that
// previews work without a key.
func Load() (Config, error) {
	var cfg Config

	home, err := os.UserHomeDir()
	if err != nil {
		return cfg, fmt.Errorf("cannot determine home directory: %w", err)
	}

	if err := readYoconf(filepath.Join(home, ".yoconf"), &cfg); err != nil {
		return cfg, err
	}

	if cfg.Provider == "" {
		cfg.Provider = inferProvider(home)
	}

	d, ok := defaults[cfg.Provider]
	if !ok {
		return cfg, fmt.Errorf("provider %q not supported (use \"anthropic\" or \"openai\")", cfg.Provider)
	}
	if cfg.Model == "" {
		cfg.Model = d.model
	}
	if cfg.Key == "" {
		// Key-file fallback (single line). yoshell enforces 0600 on Unix; skipped
		// on Windows (v0.1 target). Decoded first to tolerate UTF-16/BOM files.
		if data, err := os.ReadFile(filepath.Join(home, d.keyFile)); err == nil {
			cfg.Key = cleanKey(decodeText(data))
		}
	}
	return cfg, nil
}

// inferProvider picks a provider from whichever key file exists, in a stable
// order; defaults to anthropic when none is found.
func inferProvider(home string) string {
	for _, p := range inferOrder {
		if _, err := os.Stat(filepath.Join(home, defaults[p].keyFile)); err == nil {
			return p
		}
	}
	return "anthropic"
}

// Ready reports whether the config is usable for a live API call. Besides the
// empty check it validates the key charset, turning the cryptic net/http
// "invalid header field value" into an actionable message if a stray byte
// survives decoding.
func (c Config) Ready() error {
	if c.Key == "" {
		keyFile := "~/.anthropickey"
		if d, ok := defaults[c.Provider]; ok {
			keyFile = "~/" + d.keyFile
		}
		return fmt.Errorf("no API key: set `key` in ~/.yoconf or write your key to %s", keyFile)
	}
	for i := 0; i < len(c.Key); i++ {
		if c.Key[i] < 0x21 || c.Key[i] > 0x7e { // API keys are printable ASCII
			return fmt.Errorf("API key contains an invalid character - check your key file for stray whitespace or an unexpected encoding")
		}
	}
	return nil
}

// cleanKey keeps only the first line and trims surrounding whitespace. Encoding
// concerns (UTF-16, BOM) are handled earlier by decodeText.
func cleanKey(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// decodeText decodes raw file bytes to a string, tolerating the encodings that
// Windows tools emit by default: a UTF-8 BOM, or UTF-16 (LE/BE, with or without
// a BOM — PowerShell 5.1 redirection and Notepad's "Unicode" both produce this).
// Plain UTF-8 passes through unchanged.
func decodeText(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return string(b[3:]) // UTF-8 BOM
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return decodeUTF16(b[2:], binary.LittleEndian)
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return decodeUTF16(b[2:], binary.BigEndian)
	case len(b) >= 2 && len(b)%2 == 0 && bytes.IndexByte(b, 0) >= 0:
		// No BOM, but NUL bytes in even-length data → UTF-16LE (Windows default).
		// Real UTF-8 text never contains a NUL byte.
		return decodeUTF16(b, binary.LittleEndian)
	default:
		return string(b)
	}
}

func decodeUTF16(b []byte, order binary.ByteOrder) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1] // drop a dangling byte rather than fail
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = order.Uint16(b[i*2:])
	}
	return string(utf16.Decode(u))
}

// readYoconf parses a yoshell-style config: one `directive value` per line,
// `#` comments, blank lines ignored. A missing file is fine. Unknown directives
// are ignored so the file stays forward-compatible. Bytes are decoded first so a
// UTF-16 config file works too.
func readYoconf(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	for _, raw := range strings.Split(decodeText(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexAny(line, " \t")
		if idx < 0 {
			continue // directive with no value
		}
		directive := strings.ToLower(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		switch directive {
		case "provider":
			cfg.Provider = strings.ToLower(value)
		case "model":
			cfg.Model = value
		case "key":
			cfg.Key = cleanKey(value)
		case "base_url":
			cfg.BaseURL = value
		}
	}
	return nil
}
