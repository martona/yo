// SPDX-License-Identifier: GPL-3.0-or-later

// Package usage keeps a small, persistent tally of LLM token usage so that `yo
// --tokens` can report how many input/output tokens have been spent -- for the
// current shell session (keyed by $env:YO_SESSION) and all-time (global, cleared
// with `yo --tokens-reset`). Every provider reports token counts in its API
// response, so collecting them is free; we only persist the running sums.
//
// Storage is a single JSON file -- global counters plus a per-session map -- under
// the OS user-config dir, so the all-time total survives reboots (unlike the
// temp-dir session store) until an explicit reset. It holds only integer counts, a
// session id, and timestamps -- no query text or command output -- so it is not
// sensitive. Set $env:YO_USAGE_DIR to relocate it (also used by tests).
package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// staleSession bounds the per-session map: a session's tally is forgotten once it
// has been idle this long (its shell is surely closed), mirroring the session
// store's sweep so the file cannot grow without bound.
const staleSession = 7 * 24 * time.Hour

// Totals is an input/output token pair.
type Totals struct {
	In  int64
	Out int64
}

type sessionEntry struct {
	In   int64  `json:"in"`
	Out  int64  `json:"out"`
	Seen string `json:"seen"` // RFC3339; for pruning idle sessions
}

type store struct {
	GlobalIn  int64                   `json:"global_in"`
	GlobalOut int64                   `json:"global_out"`
	Since     string                  `json:"since"` // RFC3339; when the global counter started
	Sessions  map[string]sessionEntry `json:"sessions"`
}

// Add folds one call's usage into the session (when id is non-empty) and global
// tallies, then persists. No-op when both counts are zero -- a failed or empty
// call contributes nothing.
func Add(id string, in, out int) {
	if in <= 0 && out <= 0 {
		return
	}
	path, err := storePath()
	if err != nil {
		return
	}
	s := load(path)
	now := time.Now()
	if s.Since == "" {
		s.Since = now.Format(time.RFC3339)
	}
	s.GlobalIn += int64(in)
	s.GlobalOut += int64(out)
	if id != "" {
		if s.Sessions == nil {
			s.Sessions = map[string]sessionEntry{}
		}
		e := s.Sessions[id]
		e.In += int64(in)
		e.Out += int64(out)
		e.Seen = now.Format(time.RFC3339)
		s.Sessions[id] = e
	}
	prune(&s, now)
	save(path, s)
}

// Report returns the session totals for id, the global totals, and the time the
// global counter started (zero time if it never has).
func Report(id string) (sess Totals, global Totals, since time.Time) {
	path, err := storePath()
	if err != nil {
		return
	}
	s := load(path)
	global = Totals{In: s.GlobalIn, Out: s.GlobalOut}
	if e, ok := s.Sessions[id]; ok {
		sess = Totals{In: e.In, Out: e.Out}
	}
	if t, err := time.Parse(time.RFC3339, s.Since); err == nil {
		since = t
	}
	return
}

// ResetGlobal zeros the all-time counter and restamps its start to now. Per-session
// tallies are left intact -- they age out on their own.
func ResetGlobal() {
	path, err := storePath()
	if err != nil {
		return
	}
	s := load(path)
	s.GlobalIn, s.GlobalOut = 0, 0
	s.Since = time.Now().Format(time.RFC3339)
	save(path, s)
}

// prune drops session entries idle longer than staleSession (or with an unparseable
// timestamp), keeping the file bounded as shells come and go.
func prune(s *store, now time.Time) {
	cutoff := now.Add(-staleSession)
	for id, e := range s.Sessions {
		if t, err := time.Parse(time.RFC3339, e.Seen); err != nil || t.Before(cutoff) {
			delete(s.Sessions, id)
		}
	}
}

// baseDir resolves the store directory (without creating it): $YO_USAGE_DIR if set,
// else the OS user-config dir, else the temp dir as a last resort.
func baseDir() string {
	if d := os.Getenv("YO_USAGE_DIR"); d != "" {
		return d
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "yo")
	}
	return filepath.Join(os.TempDir(), "yo")
}

func storePath() (string, error) {
	base := baseDir()
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(base, "usage.json"), nil
}

// Path returns the resolved usage-file location without creating anything, so
// `yo --uninstall` can report and remove it.
func Path() string {
	return filepath.Join(baseDir(), "usage.json")
}

// Remove deletes the usage file, and the enclosing "yo" directory if that leaves
// it empty. No-op (returns nil) when the file is already gone. For `yo --uninstall`.
func Remove() error {
	p := Path()
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	if d := filepath.Dir(p); filepath.Base(d) == "yo" {
		_ = os.Remove(d) // best-effort: only succeeds if it's now empty
	}
	return nil
}

func load(path string) store {
	var s store
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

// save writes the store (best-effort; same direct-write approach as the session
// store). The file is small, so a concurrent reader is extremely unlikely to catch
// a partial write, and load() tolerates a bad parse by returning a zero store.
func save(path string, s store) {
	if data, err := json.Marshal(s); err == nil {
		_ = os.WriteFile(path, data, 0o600)
	}
}
