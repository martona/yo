// SPDX-License-Identifier: GPL-3.0-or-later
package redact

import (
	"strings"
	"testing"
)

func newOrFatal(t *testing.T) Redactor {
	t.Helper()
	r, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func TestRedactsKnownSecrets(t *testing.T) {
	r := newOrFatal(t)
	in := strings.Join([]string{
		"export GITHUB_TOKEN=ghp_aB3dE6gH9jK2mN5pQ8rS1tU4vW7xY0zAbCdE",
		`api_key = "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6"`,
		"slack xoxb-2444040403-2444040404-aBcDeFgHiJkLmNoPqRsTuVwX",
	}, "\n")
	res := r.Redact(in)
	if res.Count < 3 {
		t.Errorf("expected >=3 secrets redacted, got %d (kinds=%v)", res.Count, res.Kinds)
	}
	// The raw secret material must not survive.
	for _, leak := range []string{"ghp_aB3dE6gH", "a1b2c3d4e5f6", "xoxb-2444040403"} {
		if strings.Contains(res.Text, leak) {
			t.Errorf("secret leaked through redaction: %q still present in:\n%s", leak, res.Text)
		}
	}
	if !strings.Contains(res.Text, "[REDACTED:") {
		t.Errorf("expected [REDACTED:...] placeholders, got:\n%s", res.Text)
	}
}

func TestKeepsBenignText(t *testing.T) {
	r := newOrFatal(t)
	in := "find . -name '*.go' | xargs grep TODO\nuuid 550e8400-e29b-41d4-a716-446655440000\n"
	res := r.Redact(in)
	if res.Count != 0 {
		t.Errorf("benign text should not trigger redaction, got %d (kinds=%v):\n%s", res.Count, res.Kinds, res.Text)
	}
	if res.Text != in {
		t.Errorf("benign text was modified:\n got %q\nwant %q", res.Text, in)
	}
}

func TestEmptyInput(t *testing.T) {
	r := newOrFatal(t)
	if res := r.Redact(""); res.Text != "" || res.Count != 0 {
		t.Errorf("empty input: got %+v", res)
	}
}

// BenchmarkNew measures detector construction cost (paid once per invocation,
// only when there is scrollback to scan).
func BenchmarkNew(b *testing.B) {
	for i := 0; i < b.N; i++ {
		if _, err := New(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRedact measures a scan over ~200 lines of mixed text.
func BenchmarkRedact(b *testing.B) {
	r, err := New()
	if err != nil {
		b.Fatal(err)
	}
	s := strings.Repeat("some log line with a token ghp_aB3dE6gH9jK2mN5pQ8rS1tU4vW7xY0zAbCdE here\n", 200)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Redact(s)
	}
}
