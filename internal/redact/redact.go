// SPDX-License-Identifier: GPL-3.0-or-later

// Package redact scrubs secrets from outbound text (the terminal scrollback we
// fold into a query) before it crosses the wire to the LLM. Detection is
// gitleaks' engine running its full default ruleset, which is EMBEDDED in the
// binary (config.DefaultConfig) -- no config file ships with us. Redaction
// itself is just replacing each found secret with a labeled placeholder.
//
// This is defense-in-depth, not perfection. gitleaks is tuned to avoid false
// positives (it will, e.g., ignore a lone AWS access-key id with no secret
// beside it), so a missed secret is possible -- but that is the same exposure
// that existed before any redaction. Over-redaction, which would mangle the
// context we send the model, is the failure we most want to avoid, so erring
// toward gitleaks' precision is deliberate.
package redact

import (
	"sort"
	"strings"

	"github.com/spf13/viper"
	"github.com/zricethezav/gitleaks/v8/config"
	"github.com/zricethezav/gitleaks/v8/detect"
)

// Result is the outcome of a redaction pass.
type Result struct {
	Text  string   // input with each secret replaced by "[REDACTED:<kind>]"
	Count int      // number of distinct secrets replaced
	Kinds []string // distinct gitleaks rule ids that matched, sorted (for UX)
}

// Redactor scrubs secrets from a string. It is an interface so the backend
// (gitleaks today) can be swapped without touching callers.
type Redactor interface {
	Redact(s string) Result
}

type gitleaksRedactor struct{ d *detect.Detector }

// New builds a gitleaks-backed redactor from the embedded default config.
func New() (Redactor, error) {
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(strings.NewReader(config.DefaultConfig)); err != nil {
		return nil, err
	}
	var vc config.ViperConfig
	if err := v.Unmarshal(&vc); err != nil {
		return nil, err
	}
	cfg, err := vc.Translate()
	if err != nil {
		return nil, err
	}
	return &gitleaksRedactor{d: detect.NewDetector(cfg)}, nil
}

// Redact replaces every detected secret with "[REDACTED:<rule-id>]" and reports
// how many distinct secrets were scrubbed and which rule kinds matched.
func (g *gitleaksRedactor) Redact(s string) Result {
	res := Result{Text: s}
	if s == "" {
		return res
	}
	seenSecret := make(map[string]bool)
	seenKind := make(map[string]bool)
	for _, f := range g.d.DetectString(s) {
		if f.Secret == "" || seenSecret[f.Secret] {
			continue
		}
		seenSecret[f.Secret] = true
		res.Text = strings.ReplaceAll(res.Text, f.Secret, "[REDACTED:"+f.RuleID+"]")
		res.Count++
		if !seenKind[f.RuleID] {
			seenKind[f.RuleID] = true
			res.Kinds = append(res.Kinds, f.RuleID)
		}
	}
	sort.Strings(res.Kinds)
	return res
}
