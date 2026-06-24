// SPDX-License-Identifier: GPL-3.0-or-later
package usage

import (
	"testing"
	"time"
)

func TestAddAndReport(t *testing.T) {
	t.Setenv("YO_USAGE_DIR", t.TempDir())

	Add("sess-1", 100, 10)
	Add("sess-1", 50, 5)
	Add("sess-2", 7, 3)

	sess, global, since := Report("sess-1")
	if sess.In != 150 || sess.Out != 15 {
		t.Fatalf("session totals = %+v, want In=150 Out=15", sess)
	}
	if global.In != 157 || global.Out != 18 {
		t.Fatalf("global totals = %+v, want In=157 Out=18", global)
	}
	if since.IsZero() {
		t.Fatalf("since should be set after Add")
	}

	// A different session sees only its own tally, but the same global.
	s2, g2, _ := Report("sess-2")
	if s2.In != 7 || s2.Out != 3 {
		t.Fatalf("sess-2 totals = %+v, want In=7 Out=3", s2)
	}
	if g2.In != 157 || g2.Out != 18 {
		t.Fatalf("sess-2 global = %+v, want In=157 Out=18", g2)
	}
}

func TestAddZeroIsNoop(t *testing.T) {
	t.Setenv("YO_USAGE_DIR", t.TempDir())

	Add("sess-1", 0, 0)
	_, global, since := Report("sess-1")
	if global.In != 0 || global.Out != 0 {
		t.Fatalf("global = %+v, want zero after a no-op Add", global)
	}
	if !since.IsZero() {
		t.Fatalf("since should remain unset after a no-op Add")
	}
}

func TestResetGlobalKeepsSession(t *testing.T) {
	t.Setenv("YO_USAGE_DIR", t.TempDir())

	Add("sess-1", 100, 10)
	ResetGlobal()

	sess, global, _ := Report("sess-1")
	if global.In != 0 || global.Out != 0 {
		t.Fatalf("global after reset = %+v, want zero", global)
	}
	if sess.In != 100 || sess.Out != 10 {
		t.Fatalf("session after reset = %+v, want In=100 Out=10 (unchanged)", sess)
	}
}

func TestEmptySessionIDStillCountsGlobal(t *testing.T) {
	t.Setenv("YO_USAGE_DIR", t.TempDir())

	Add("", 20, 4)
	sess, global, _ := Report("")
	if global.In != 20 || global.Out != 4 {
		t.Fatalf("global = %+v, want In=20 Out=4", global)
	}
	if sess.In != 0 || sess.Out != 0 {
		t.Fatalf("session for empty id = %+v, want zero", sess)
	}
}

func TestPruneDropsStaleSessions(t *testing.T) {
	now := time.Now()
	s := store{Sessions: map[string]sessionEntry{
		"fresh": {In: 1, Seen: now.Format(time.RFC3339)},
		"stale": {In: 1, Seen: now.Add(-staleSession - time.Hour).Format(time.RFC3339)},
		"bad":   {In: 1, Seen: "not-a-time"},
	}}
	prune(&s, now)
	if _, ok := s.Sessions["fresh"]; !ok {
		t.Fatalf("fresh session was pruned")
	}
	if _, ok := s.Sessions["stale"]; ok {
		t.Fatalf("stale session was not pruned")
	}
	if _, ok := s.Sessions["bad"]; ok {
		t.Fatalf("session with an unparseable timestamp was not pruned")
	}
}
