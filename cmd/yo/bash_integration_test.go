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

func TestBashSnippetReadlineQueryAndContinuation(t *testing.T) {
	dir := t.TempDir()
	snippet := writeBashSnippet(t, dir)
	logPath := filepath.Join(dir, "fake.log")
	cdDir := filepath.Join(dir, "next")
	if err := os.MkdirAll(cdDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(dir, "fake-yo")
	if err := os.WriteFile(fake, []byte(`#!/bin/sh
is_continue=0
for arg do
  [ "$arg" = "--continue" ] && is_continue=1
done
{
  printf 'args:'
  for arg do printf '[%s]' "$arg"; done
  printf '\nYO_RAN:%s\nYO_STATE:%s\nPWD:%s\nYO_BASH_TEST_VAR:%s\n' "$YO_RAN" "$YO_STATE" "$(pwd)" "$YO_BASH_TEST_VAR"
} >> "$YO_FAKE_LOG"
if [ "$is_continue" = 1 ]; then
  cat <<'OUT'
YO_RESULT_TYPE='chat'
YO_RESULT_COMMAND=''
YO_RESULT_EXPLANATION=''
YO_RESULT_RESPONSE='done after continuation'
YO_RESULT_MESSAGE=''
YO_RESULT_PENDING='0'
YO_RESULT_STATE=''
YO_RESULT_PREFILL_SPACE='0'
OUT
else
  cat <<'OUT'
YO_RESULT_TYPE='command'
YO_RESULT_COMMAND='printf first'
YO_RESULT_EXPLANATION='try this first'
YO_RESULT_RESPONSE=''
YO_RESULT_MESSAGE=''
YO_RESULT_PENDING='1'
YO_RESULT_STATE='state-1'
YO_RESULT_PREFILL_SPACE='0'
OUT
fi
`), 0o755); err != nil {
		t.Fatal(err)
	}

	out := runBash(t, `
export _YO_TEST_NO_BIND=1
export YO_BIN=`+shellQuote(fake)+`
export YO_FAKE_LOG=`+shellQuote(logPath)+`
export YO_TEST_CD=`+shellQuote(cdDir)+`
source `+shellQuote(snippet)+`
START_PWD=$PWD
READLINE_LINE='yo what does (echo hi | wc -c) mean; echo bad'
_yo_readline_enter
printf 'prefill=<%s> armed=%s state=%s finish=%s\n' "$READLINE_LINE" "$_YO_ARMED" "$YO_STATE" "$_YO_TEST_FINISH"
READLINE_LINE='printf first; export YO_BASH_TEST_VAR=ok; cd "$YO_TEST_CD"; false'
_yo_readline_enter
if [[ "$PWD" != "$START_PWD" ]]; then moved=yes; else moved=no; fi
printf 'after=<%s> armed=%s state=%s finish=%s var=%s moved=%s\n' "$READLINE_LINE" "$_YO_ARMED" "$YO_STATE" "$_YO_TEST_FINISH" "$YO_BASH_TEST_VAR" "$moved"
`)

	for _, want := range []string{
		"try this first\n",
		"prefill=<printf first> armed=1 state=state-1 finish=redraw-current-line\n",
		"done after continuation\n",
		"after=<> armed=0 state= finish=redraw-current-line var=ok moved=yes\n",
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
		"args:[--continue][--exit][1][--shell][bash][--output][sh]",
		`YO_RAN:printf first; export YO_BASH_TEST_VAR=ok; cd "$YO_TEST_CD"; false` + "\n",
		"YO_STATE:state-1\n",
		"PWD:",
		"YO_BASH_TEST_VAR:ok\n",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("fake binary log missing %q:\n%s", want, log)
		}
	}
}

func TestBashSnippetFlagsBypassAndSafeEval(t *testing.T) {
	dir := t.TempDir()
	snippet := writeBashSnippet(t, dir)
	badPath := filepath.Join(dir, "bad")
	result := strings.Join([]string{
		"YO_RESULT_TYPE='chat'",
		"YO_RESULT_COMMAND=''",
		"YO_RESULT_EXPLANATION=''",
		"YO_RESULT_RESPONSE='$(touch " + badPath + ")'",
		"YO_RESULT_MESSAGE=''",
		"YO_RESULT_PENDING='0'",
		"YO_RESULT_STATE=''",
		"YO_RESULT_PREFILL_SPACE='0'",
	}, "\n")

	out := runBash(t, `
export _YO_TEST_NO_BIND=1
source `+shellQuote(snippet)+`
READLINE_LINE='yo --check | cat'
_yo_readline_enter
printf 'flag-line=<%s> finish=%s\n' "$READLINE_LINE" "$_YO_TEST_FINISH"
_yo_apply_result_to_readline `+shellQuote(result)+`
[[ ! -e `+shellQuote(badPath)+` ]] || exit 42
_YO_ARMED=1
YO_STATE=state-2
_yo_prompt_command
printf 'cancelled armed=%s state=%s\n' "$_YO_ARMED" "$YO_STATE"
`)

	for _, want := range []string{
		"flag-line=<yo --check | cat> finish=accept-line\n",
		"$(touch " + badPath + ")\n",
		"cancelled armed=0 state=\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("bash output missing %q:\n%s", want, out)
		}
	}
}
