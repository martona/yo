// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/martona/yo/shell"
)

func requireModernBash(t *testing.T) string {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found on PATH")
	}
	out, err := exec.Command(bash, "-c", "printf '%s.%s\\n' \"${BASH_VERSINFO[0]}\" \"${BASH_VERSINFO[1]}\"").Output()
	if err != nil {
		t.Fatalf("checking bash version: %v", err)
	}
	parts := strings.Split(strings.TrimSpace(string(out)), ".")
	if len(parts) != 2 {
		t.Fatalf("unexpected bash version output: %q", out)
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	if major < 4 || (major == 4 && minor < 2) {
		t.Skipf("bash %d.%d found; bash integration requires 4.2+", major, minor)
	}
	return bash
}

func writeBashSnippet(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "yo.bash")
	if err := os.WriteFile(path, []byte(shell.Bash), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runBash(t *testing.T, script string) string {
	t.Helper()
	bash := requireModernBash(t)
	cmd := exec.Command(bash, "--noprofile", "--norc")
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash script failed: %v\n%s", err, out)
	}
	return strings.ReplaceAll(string(out), "\r", "")
}

func TestSnippetForShellBash(t *testing.T) {
	got, ok := snippetForShell("bash")
	if !ok {
		t.Fatal("snippetForShell(bash) did not report success")
	}
	if got != shell.Bash {
		t.Fatal("snippetForShell(bash) did not return the embedded bash snippet")
	}
}

func TestBashSnippetParses(t *testing.T) {
	bash := requireModernBash(t)
	cmd := exec.Command(bash, "-n")
	cmd.Stdin = strings.NewReader(shell.Bash)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("embedded bash snippet did not parse: %v\n%s", err, out)
	}
}

func TestBashSnippetUnsupportedBashQuietlyNoops(t *testing.T) {
	dir := t.TempDir()
	snippet := filepath.Join(dir, "yo-old-bash.bash")
	body := strings.Replace(shell.Bash, "BASH_VERSINFO[0] < 4", "1", 1)
	if err := os.WriteFile(snippet, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out := runBash(t, `
source `+shellQuote(snippet)+`
if declare -F _yo_rewrite_buffer >/dev/null; then echo installed; fi
echo after
`)

	if out != "after\n" {
		t.Fatalf("unsupported bash should quietly no-op, got:\n%s", out)
	}
}

func TestBashSnippetNonBashQuietlyNoops(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not found on PATH")
	}
	dir := t.TempDir()
	snippet := writeBashSnippet(t, dir)
	cmd := exec.Command("sh", "-c", ". "+shellQuote(snippet)+"; echo after")
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("non-bash source failed: %v\n%s", err, out)
	}
	if got := strings.ReplaceAll(string(out), "\r", ""); got != "after\n" {
		t.Fatalf("non-bash should quietly no-op, got:\n%s", got)
	}
}

// fakeBashYo writes a fake `yo` binary: it logs its args + relevant env, and emits a
// command+pending result on the initial call, a terminal chat on the --continue call.
func fakeBashYo(t *testing.T, dir, logPath string) string {
	t.Helper()
	fake := filepath.Join(dir, "fake-yo")
	if err := os.WriteFile(fake, []byte(`#!/bin/sh
is_continue=0
for arg do
  [ "$arg" = "--continue" ] && is_continue=1
done
{
  printf 'args:'
  for arg do printf '[%s]' "$arg"; done
  printf '\nYO_RAN:%s\nYO_STATE:%s\n' "$YO_RAN" "$YO_STATE"
} >> "$YO_FAKE_LOG"
if [ "$is_continue" = 1 ]; then
  cat <<'OUT'
YO_RESULT_TYPE='chat'
YO_RESULT_RESPONSE='done after continuation'
YO_RESULT_PENDING='0'
OUT
else
  cat <<'OUT'
YO_RESULT_TYPE='command'
YO_RESULT_COMMAND='printf first'
YO_RESULT_EXPLANATION='try this first'
YO_RESULT_PENDING='1'
YO_RESULT_STATE='state-1'
OUT
fi
`), 0o755); err != nil {
		t.Fatal(err)
	}
	return fake
}

// Initial query: the accept-line rewrite quotes metacharacters; the yo function emits
// a command + pending and arms; the PROMPT_COMMAND-driven continuation then fires once
// a command has run (history advanced) and forwards the exit code + executed command.
func TestBashSnippetQueryAndContinuation(t *testing.T) {
	dir := t.TempDir()
	snippet := writeBashSnippet(t, dir)
	logPath := filepath.Join(dir, "fake.log")
	fake := fakeBashYo(t, dir, logPath)

	out := runBash(t, `
export YO_BIN=`+shellQuote(fake)+`
export YO_FAKE_LOG=`+shellQuote(logPath)+`
source `+shellQuote(snippet)+`
# Stub the interactive-only prefill so the state machine is exercised without a tty.
_yo_prefill() { printf 'PREFILL:<%s>\n' "$1"; }

# Accept-line rewrite quotes a metacharacter-laden query to one literal arg.
READLINE_LINE='yo what does (echo hi | wc -c) mean; echo bad'
_yo_rewrite_buffer
printf 'rewrite=<%s>\n' "$READLINE_LINE"

# The yo function runs (as accept-line would run it) -> command + pending -> armed.
_yo_invoke 'what does (echo hi | wc -c) mean; echo bad'
printf 'invoke armed=%s state=%s seen=%s\n' "$_YO_ARMED" "$YO_STATE" "$_YO_SEEN_PROMPT"

# Prompt cycle: first prompt marks seen; then the user submits the prefilled command
# (the accept-line hook flags it ran), and the next prompt drives the continuation.
_yo_precmd
printf 'p1 seen=%s ran=%s\n' "$_YO_SEEN_PROMPT" "$_YO_RAN_SINCE_ARM"
READLINE_LINE='printf first'
_yo_rewrite_buffer
printf 'submit ran=%s last=<%s>\n' "$_YO_RAN_SINCE_ARM" "$_YO_LAST_RAN"
_yo_precmd
printf 'p2 armed=%s\n' "$_YO_ARMED"
`)

	for _, want := range []string{
		"rewrite=<yo 'what does (echo hi | wc -c) mean; echo bad'>\n",
		"try this first\n",
		"PREFILL:<printf first>\n",
		"invoke armed=1 state=state-1 seen=0\n",
		"p1 seen=1 ran=0\n",
		"submit ran=1 last=<printf first>\n",
		"done after continuation\n",
		"p2 armed=0\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("bash output missing %q:\n%s", want, out)
		}
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logData)
	for _, want := range []string{
		"args:[--shell][bash][--output][sh]",
		"[what does (echo hi | wc -c) mean; echo bad]",
		"args:[--continue][--exit][0][--shell][bash][--output][sh]",
		"YO_RAN:printf first\n",
		"YO_STATE:state-1\n",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("fake binary log missing %q:\n%s", want, log)
		}
	}
}

// A debug-flag line (yo --check ...) is left untouched by the rewrite; a chat result's
// payload is never command-substituted (eval of single-quoted assignments is inert);
// and a continuation with no command run is cancelled.
func TestBashSnippetFlagsBypassAndSafeEval(t *testing.T) {
	dir := t.TempDir()
	snippet := writeBashSnippet(t, dir)
	badPath := filepath.Join(dir, "bad")
	result := strings.Join([]string{
		"YO_RESULT_TYPE='chat'",
		"YO_RESULT_RESPONSE='$(touch " + badPath + ")'",
		"YO_RESULT_PENDING='0'",
	}, "\n")

	out := runBash(t, `
source `+shellQuote(snippet)+`
_yo_prefill() { :; }

# Debug-flag query: rewrite leaves it alone (handled by normal arg parsing).
READLINE_LINE='yo --check | cat'
_yo_rewrite_buffer
printf 'flag-line=<%s>\n' "$READLINE_LINE"

# Chat payload with a command substitution must NOT execute.
_yo_apply_result `+shellQuote(result)+` 0
[[ ! -e `+shellQuote(badPath)+` ]] || exit 42

# Armed, but a bare Enter (empty line) submits nothing -> cancel on the next prompt.
_yo_arm_continuation 'state-2' 1
READLINE_LINE=''
_yo_rewrite_buffer
_yo_precmd
printf 'cancelled armed=%s state=%s\n' "$_YO_ARMED" "$YO_STATE"
`)

	for _, want := range []string{
		"flag-line=<yo --check | cat>\n",
		"$(touch " + badPath + ")\n",
		"cancelled armed=0 state=\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("bash output missing %q:\n%s", want, out)
		}
	}
}
