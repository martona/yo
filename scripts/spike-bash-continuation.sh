# Continuation spike: reproduce the multi-step loop/lockup in ISOLATION, using the
# REAL shell/yo.bash but a deterministic fake binary (no network, no model). This
# exercises the precmd-driven continuation + DSR re-prefill path that the first spike
# did NOT cover.
#
#   source scripts/spike-bash-continuation.sh
#
# Then type:   yo go
# Expected, step by step (each command should WAIT for you to press Enter):
#   1. prompt prefilled with `echo STEP-ONE`  -> press Enter -> prints STEP-ONE
#   2. prompt prefilled with `echo STEP-TWO`  -> press Enter -> prints STEP-TWO
#   3. prints "all done." and returns to a normal prompt (no prefill)
#
# BUG to watch for: steps auto-fire without waiting for Enter, the same command
# repeats, or the prompt locks up (had to Ctrl-C). After the run, inspect the call
# log to see exactly how many times the fake was invoked and with what:
#   cat "$YO_FAKE_LOG"
#
# Undo:  yo_unspike_cont   (or just open a fresh shell)

if [ -z "${BASH_VERSION:-}" ]; then echo "spike: bash only"; return 1 2>/dev/null || exit 1; fi
case $- in *i*) ;; *) echo "spike: source me in an INTERACTIVE bash"; return 1 2>/dev/null || exit 1;; esac

_yo_cont_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

# Deterministic fake `yo`: returns two pending command steps, then a terminal chat.
# Logs every invocation (initial vs --continue, exit code, YO_RAN, step) so a runaway
# loop is visible as repeated/extra calls.
export YO_FAKE_STATE="$(mktemp -t yo-cont-state-XXXXXX)"
export YO_FAKE_LOG="$(mktemp -t yo-cont-log-XXXXXX)"
: > "$YO_FAKE_LOG"
_yo_cont_fake="$(mktemp -t yo-cont-fake-XXXXXX)"
cat > "$_yo_cont_fake" <<'FAKE'
#!/usr/bin/env bash
is_continue=0; exitc=""
prev="0"
for ((i=1;i<=$#;i++)); do
  a="${!i}"
  [ "$a" = "--continue" ] && is_continue=1
  [ "$a" = "--exit" ] && { j=$((i+1)); exitc="${!j}"; }
done
[ "$is_continue" = 0 ] && echo 0 > "$YO_FAKE_STATE"
step="$(cat "$YO_FAKE_STATE" 2>/dev/null || echo 0)"
printf 'CALL continue=%s exit=%s ran=<%s> step=%s\n' "$is_continue" "$exitc" "$YO_RAN" "$step" >> "$YO_FAKE_LOG"
case "$step" in
  0) printf "%s\n" "YO_RESULT_TYPE='command'" "YO_RESULT_COMMAND='echo STEP-ONE'" \
       "YO_RESULT_EXPLANATION='step 1 of 2'" "YO_RESULT_PENDING='1'" "YO_RESULT_STATE='s1'"
     echo 1 > "$YO_FAKE_STATE" ;;
  1) printf "%s\n" "YO_RESULT_TYPE='command'" "YO_RESULT_COMMAND='echo STEP-TWO'" \
       "YO_RESULT_EXPLANATION='step 2 of 2'" "YO_RESULT_PENDING='1'" "YO_RESULT_STATE='s2'"
     echo 2 > "$YO_FAKE_STATE" ;;
  *) printf "%s\n" "YO_RESULT_TYPE='chat'" "YO_RESULT_RESPONSE='all done.'" "YO_RESULT_PENDING='0'" ;;
esac
FAKE
chmod +x "$_yo_cont_fake"
export YO_BIN="$_yo_cont_fake"

# Load the REAL adapter under test.
source "$_yo_cont_root/shell/yo.bash"

yo_unspike_cont() {
    bind '"\C-m": accept-line' 2>/dev/null
    bind '"\C-j": accept-line' 2>/dev/null
    bind -r '\C-x\C-y' 2>/dev/null
    bind -r '\C-x\C-m' 2>/dev/null
    bind -r '\e[0n' 2>/dev/null
    stty echo 2>/dev/null
    PROMPT_COMMAND="${PROMPT_COMMAND//_yo_precmd;/}"
    PROMPT_COMMAND="${PROMPT_COMMAND//_yo_precmd/}"
    rm -f "$YO_FAKE_STATE" "$YO_FAKE_LOG" "$_yo_cont_fake" 2>/dev/null
    unset YO_BIN YO_FAKE_STATE YO_FAKE_LOG
    echo "continuation spike removed (restart the shell for a fully clean slate)."
}

cat <<EOF
continuation spike loaded (fake yo, no network).

  Type:  yo go
  Then press Enter on each prefilled command in turn. Expected:
    echo STEP-ONE  (Enter -> STEP-ONE)
    echo STEP-TWO  (Enter -> STEP-TWO)
    all done.

  If it loops / repeats a command / locks up (Ctrl-C to escape), then afterwards:
    cat "\$YO_FAKE_LOG"
  and paste it -- each line is one fake invocation (continue/exit/ran/step).

  Undo:  yo_unspike_cont
EOF
