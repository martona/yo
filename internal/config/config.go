// SPDX-License-Identifier: GPL-3.0-or-later

// Package config loads yo's per-invocation settings from ~/.yoconf and the
// standard provider API-key environment variables.
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
	Memory   bool // cross-call session memory; default on, "memory false" in ~/.yoconf disables
	Debug    bool // trace LLM request/response scaffolding to stderr; off by default ("debug true" or $env:YO_DEBUG)
	// PrefillSpace prefixes prefilled commands with a leading space so history tools
	// that skip space-prefixed lines (e.g. Atuin) don't record them, while PowerShell's
	// Get-History still does. Off by default ("prefill_space true"). NOTE: on bash/zsh,
	// HISTCONTROL=ignorespace would also drop the command from the shell's OWN history
	// (not just Atuin), and any history-based continuation capture would miss it -- a
	// future Unix port must capture via preexec/DEBUG, not `fc`.
	PrefillSpace bool
}

// providerDefaults holds the per-provider model and the environment variable
// that supplies the API key.
type providerDefaults struct {
	model  string
	envKey string
}

// defaults: models mirror yoshell's conventions (override via ~/.yoconf); the
// key env vars are the ecosystem standard used by the official SDKs.
var defaults = map[string]providerDefaults{
	"anthropic": {model: "claude-opus-4-8", envKey: "ANTHROPIC_API_KEY"},
	"openai":    {model: "gpt-5.5", envKey: "OPENAI_API_KEY"},
}

// inferOrder is the probe order when ~/.yoconf names no provider.
var inferOrder = []string{"anthropic", "openai"}

// Load reads ~/.yoconf (if present), resolves the provider, then fills in the
// default model and the API key (from the provider's env var) when not already
// set in ~/.yoconf. A missing key is NOT fatal here — it's checked at call time
// (Ready) so that previews work without a key.
func Load() (Config, error) {
	var cfg Config
	cfg.Memory = true // default on; ~/.yoconf "memory false" disables

	home, err := os.UserHomeDir()
	if err != nil {
		return cfg, fmt.Errorf("cannot determine home directory: %w", err)
	}

	if err := readYoconf(filepath.Join(home, ".yoconf"), &cfg); err != nil {
		return cfg, err
	}

	if cfg.Provider == "" {
		cfg.Provider = inferProvider()
	}

	d, ok := defaults[cfg.Provider]
	if !ok {
		return cfg, fmt.Errorf("provider %q not supported (use \"anthropic\" or \"openai\")", cfg.Provider)
	}
	if cfg.Model == "" {
		cfg.Model = d.model
	}
	if cfg.Key == "" {
		// Standard env var (e.g. ANTHROPIC_API_KEY). cleanKey trims any stray
		// whitespace; an env var is already a decoded OS string.
		cfg.Key = cleanKey(os.Getenv(d.envKey))
	}
	// $env:YO_DEBUG overrides the yoconf `debug` directive, for quick per-session
	// toggling without editing the file (truthy = on; blank/0/false/off/no = off).
	if v, ok := os.LookupEnv("YO_DEBUG"); ok {
		cfg.Debug = truthy(v)
	}
	return cfg, nil
}

// truthy interprets a config flag value: blank, 0, false, off, and no are false;
// anything else is true. Used for the default-off `debug` directive (memory keeps
// its own default-on parsing).
func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// inferProvider picks a provider from whichever key env var is set, in a stable
// order; defaults to anthropic when none is set.
func inferProvider() string {
	for _, p := range inferOrder {
		if os.Getenv(defaults[p].envKey) != "" {
			return p
		}
	}
	return "anthropic"
}

// Ready reports whether the config is usable for a live API call. Besides the
// empty check it validates the key charset, turning the cryptic net/http
// "invalid header field value" into an actionable message.
func (c Config) Ready() error {
	if c.Key == "" {
		envKey := "ANTHROPIC_API_KEY"
		if d, ok := defaults[c.Provider]; ok {
			envKey = d.envKey
		}
		return fmt.Errorf("no API key: set the %s environment variable (or `key` in ~/.yoconf)", envKey)
	}
	for i := 0; i < len(c.Key); i++ {
		if c.Key[i] < 0x21 || c.Key[i] > 0x7e { // API keys are printable ASCII
			return fmt.Errorf("API key contains an invalid character - check %s for stray whitespace", c.Provider)
		}
	}
	return nil
}

// cleanKey keeps only the first line and trims surrounding whitespace.
func cleanKey(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// decodeText decodes raw file bytes to a string, tolerating the encodings that
// Windows tools emit by default: a UTF-8 BOM, or UTF-16 (LE/BE, with or without
// a BOM — PowerShell 5.1 redirection and Notepad's "Unicode" both produce this).
// Used for ~/.yoconf, which a Windows editor may well have saved as UTF-16.
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
		case "memory":
			switch strings.ToLower(value) {
			case "false", "0", "off", "no":
				cfg.Memory = false
			default:
				cfg.Memory = true
			}
		case "debug":
			cfg.Debug = truthy(value)
		case "prefill_space":
			cfg.PrefillSpace = truthy(value)
		}
	}
	return nil
}
