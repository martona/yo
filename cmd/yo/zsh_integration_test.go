// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/martona/yo/shell"
)

func requireZsh(t *testing.T) string {
	t.Helper()
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not found on PATH")
	}
	return zsh
}

func writeZshSnippet(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "yo.zsh")
	if err := os.WriteFile(path, []byte(shell.Zsh), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runZsh(t *testing.T, script string) string {
	t.Helper()
	zsh := requireZsh(t)
	cmd := exec.Command(zsh, "-f")
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zsh script failed: %v\n%s", err, out)
	}
	return string(out)
}

func runInteractiveZsh(t *testing.T, script string) string {
	t.Helper()
	zsh := requireZsh(t)
	cmd := exec.Command(zsh, "-f", "-i", "-c", script)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("interactive zsh script failed: %v\n%s", err, out)
	}
	return string(out)
}

func TestSnippetForShellZsh(t *testing.T) {
	got, ok := snippetForShell("zsh")
	if !ok {
		t.Fatal("snippetForShell(zsh) did not report success")
	}
	if got != shell.Zsh {
		t.Fatal("snippetForShell(zsh) did not return the embedded zsh snippet")
	}
	if got, ok := snippetForShell("bash"); ok || got != "" {
		t.Fatalf("snippetForShell(bash) = %q, %v; want unsupported", got, ok)
	}
}

func TestZshSnippetParses(t *testing.T) {
	zsh := requireZsh(t)
	cmd := exec.Command(zsh, "-n")
	cmd.Stdin = strings.NewReader(shell.Zsh)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("embedded zsh snippet did not parse: %v\n%s", err, out)
	}
}

func TestZshSnippetResultHandlingAndContinuation(t *testing.T) {
	dir := t.TempDir()
	snippet := writeZshSnippet(t, dir)
	logPath := filepath.Join(dir, "fake.log")
	fake := filepath.Join(dir, "fake-yo")
	if err := os.WriteFile(fake, []byte(`#!/bin/sh
{
  printf 'args:'
  for arg do printf '[%s]' "$arg"; done
  printf '\nYO_RAN:%s\nYO_STATE:%s\n' "$YO_RAN" "$YO_STATE"
} >> "$YO_FAKE_LOG"
case " $* " in
  *" --continue "*)
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
    ;;
  *)
    cat <<'OUT'
YO_RESULT_TYPE='command'
YO_RESULT_COMMAND='echo generated'
YO_RESULT_EXPLANATION='try this first'
YO_RESULT_RESPONSE=''
YO_RESULT_MESSAGE=''
YO_RESULT_PENDING='1'
YO_RESULT_STATE='state-1'
YO_RESULT_PREFILL_SPACE='0'
OUT
    ;;
esac
`), 0o755); err != nil {
		t.Fatal(err)
	}

	out := runZsh(t, `
export YO_BIN=`+shellQuote(fake)+`
export YO_FAKE_LOG=`+shellQuote(logPath)+`
source `+shellQuote(snippet)+`
yo what next
print -r -- "armed=$_YO_ARMED seen=$_YO_SEEN_PROMPT state=$YO_STATE"
_yo_precmd
_yo_preexec ' echo edited-command'
false
_yo_precmd
print -r -- "after=$_YO_ARMED state=$YO_STATE ran=$YO_RAN"
`)

	for _, want := range []string{
		"try this first\n",
		"armed=1 seen=0 state=state-1\n",
		"done after continuation\n",
		"after=0 state= ran=\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("zsh output missing %q:\n%s", want, out)
		}
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logData)
	for _, want := range []string{
		"args:[--shell][zsh][--output][sh]",
		"args:[--continue][--exit][1][--shell][zsh][--output][sh]",
		"YO_RAN:echo edited-command\n",
		"YO_STATE:state-1\n",
	} {
		if !strings.Contains(log, want) {
			t.Fatalf("fake binary log missing %q:\n%s", want, log)
		}
	}
}

func TestZshSnippetRawLineRewriteAndSafeEval(t *testing.T) {
	dir := t.TempDir()
	snippet := writeZshSnippet(t, dir)
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

	out := runZsh(t, `
source `+shellQuote(snippet)+`
rewritten="$(_yo_rewrite_line 'yo what does (echo hi | wc -c) mean')"
print -r -- "rewritten=$rewritten"
words=(${(z)${rewritten#yo }})
print -r -- "word-count=$#words"
print -r -- "query=${(Q)words[1]}"
print -r -- "flag=$(_yo_rewrite_line 'yo --check')"
print -r -- "quoted=$(_yo_rewrite_line "yo 'already quoted'")"
_yo_apply_result `+shellQuote(result)+`
[[ ! -e `+shellQuote(badPath)+` ]] || exit 42
`)

	for _, want := range []string{
		"word-count=1\n",
		"query=what does (echo hi | wc -c) mean\n",
		"flag=yo --check\n",
		"quoted=yo 'already quoted'\n",
		"$(touch " + badPath + ")\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("zsh output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "rewritten=yo what does") {
		t.Fatalf("raw line was not rewritten:\n%s", out)
	}
}

func TestZshSnippetResourcingIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	snippet := writeZshSnippet(t, dir)

	out := runInteractiveZsh(t, `
source `+shellQuote(snippet)+`
source `+shellQuote(snippet)+`
integer pc=0 pe=0
for f in $precmd_functions; do [[ $f == _yo_precmd ]] && (( pc++ )); done
for f in $preexec_functions; do [[ $f == _yo_preexec ]] && (( pe++ )); done
print -r -- "hooks=$pc/$pe"
print -r -- "accept=${widgets[accept-line]}"
print -r -- "break=${widgets[send-break]}"
`)

	for _, want := range []string{
		"hooks=1/1\n",
		"accept=user:_yo_accept_line\n",
		"break=user:_yo_send_break\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("interactive zsh output missing %q:\n%s", want, out)
		}
	}
}
