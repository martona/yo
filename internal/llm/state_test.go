// SPDX-License-Identifier: GPL-3.0-or-later
package llm

import (
	"strings"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	exit0 := 0
	s := &State{
		Query: "set up a thing",
		Steps: []Step{
			{Command: "step one", Explanation: "first", Exit: &exit0},
			{Command: "step two"},
		},
	}
	enc, err := s.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeState(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got.Query != s.Query || len(got.Steps) != 2 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.Steps[0].Command != "step one" || got.Steps[0].Exit == nil || *got.Steps[0].Exit != 0 {
		t.Fatalf("step 0 mismatch: %+v", got.Steps[0])
	}
	if got.Steps[1].Exit != nil {
		t.Fatalf("step 1 should have no exit yet: %+v", got.Steps[1])
	}
}

func TestDecodeStateEmptyAndBad(t *testing.T) {
	if s, err := DecodeState(""); s != nil || err != nil {
		t.Fatalf("empty: want (nil,nil), got (%v,%v)", s, err)
	}
	if _, err := DecodeState("!!!not base64!!!"); err == nil {
		t.Fatal("expected error for corrupt state")
	}
}

func TestAddStepCap(t *testing.T) {
	s := &State{Query: "q"}
	for i := 0; i < maxSteps+5; i++ {
		s.AddStep("cmd", "")
	}
	if len(s.Steps) != maxSteps {
		t.Fatalf("want %d steps, got %d", maxSteps, len(s.Steps))
	}
}

func TestContinuationQuery(t *testing.T) {
	s := &State{Query: "install foo then configure it", Steps: []Step{{Command: "choco install foo"}}}
	s.SetLastExit(0)
	q := s.ContinuationQuery(0)
	for _, want := range []string{"install foo then configure it", "choco install foo", "exit 0", "pending"} {
		if !strings.Contains(q, want) {
			t.Errorf("continuation query missing %q:\n%s", want, q)
		}
	}
}
