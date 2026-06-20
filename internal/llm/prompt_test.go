// SPDX-License-Identifier: GPL-3.0-or-later
package llm

import (
	"strings"
	"testing"
)

func TestWithTerminalContext(t *testing.T) {
	if got := WithTerminalContext("do x", ""); got != "do x" {
		t.Errorf("empty scrollback should pass the query through, got %q", got)
	}
	got := WithTerminalContext("why did it fail", "$ build\nerror: boom")
	for _, want := range []string{"why did it fail", "error: boom", "terminal context", "[request]"} {
		if !strings.Contains(got, want) {
			t.Errorf("result missing %q:\n%s", want, got)
		}
	}
}
