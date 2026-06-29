# Standalone spike to prove the bash prefill mechanism BEFORE touching shell/yo.bash.
#
# It proves three things, using a fake `demo` command (no LLM, no yo binary):
#   1. The typed line is PRESERVED (not erased) when you press Enter, because we
#      rewrite it to a safely-quoted form and let `accept-line` commit it -- exactly
#      how zsh does it. (This is the "middle ground" that is NOT erasing the line.)
#   2. Shell metacharacters in the query survive (no parsing/execution), because the
#      rewrite single-quotes the whole query before accept-line runs it.
#   3. The model's command can be placed, EDITABLE, on the NEXT prompt via the
#      terminal's DSR/CPR reply (ESC[5n -> bound ESC[0n) -- the bash analogue of
#      zsh's `print -z`. Output (explanation/chat) prints cleanly below, no overwrite.
#
# This is throwaway. It does not import or modify yo. Source it in an INTERACTIVE
# bash, play with it, then close the shell (or run `demo_unspike`) to undo.
#
#   source scripts/spike-bash-prefill.sh
#
# Then try (watch what happens to the line you typed, and the next prompt):
#   demo just testing
#   demo what is (echo hi | wc -c) & also "quotes" and 'apostrophes'?
#   demo chat how are you            # chat path: prints a reply, no prefill
#
# Expected for a command query:
#   $ demo just\ testing                          <- your line, preserved (%q-quoted)
#   [explanation] ...                             <- printed below, clean
#   $ echo "it's a prefilled, editable command"   <- on the NEXT prompt, EDITABLE
#
# (The quoted form uses backslashes -- `printf %q` style -- rather than zsh's single
# quotes. That is a cosmetic choice; what matters here is the line is PRESERVED and
# the query stays one literal argument. The real adapter can pretty-print if wanted.)
#
# Expected for `demo chat ...`: your line preserved, a [chat] reply below, no prefill.

if [ -z "${BASH_VERSION:-}" ]; then
    echo "spike: run under bash." >&2
    return 1 2>/dev/null || exit 1
fi
case $- in
    *i*) ;;
    *) echo "spike: source me in an INTERACTIVE bash: source scripts/spike-bash-prefill.sh" >&2
       return 1 2>/dev/null || exit 1 ;;
esac

# (3) Next-prompt prefill via DSR. Bind the terminal's ESC[0n status reply to a macro
# that "types" $1, then ask for status (ESC[5n). The reply is injected into stdin and
# read by the NEXT prompt's readline, where the macro inserts the (editable) command.
#
# Echo suppression: the ESC[0n reply may arrive during the brief canonical-mode gap
# before readline raws the tty, where the kernel ECHOES it -> a visible ^[[0n leak.
# (The bytes are still buffered and delivered to readline, so the prefill works; the
# leak is purely cosmetic.) We `stty -echo` before asking, and restore echo at the
# next prompt via PROMPT_COMMAND (which also self-heals if readline restored -echo).
demo_prefill_next() {
    local cmd="$1" esc
    # Escape for a readline macro string: backslash first, then double-quote.
    esc=${cmd//\\/\\\\}
    esc=${esc//\"/\\\"}
    bind '"\e[0n": "'"$esc"'"' 2>/dev/null
    stty -echo 2>/dev/null
    printf '\e[5n'
}

# Restore echo on every prompt (idempotent). Runs right before readline starts, so
# the tty readline saves/restores has echo ON, and any -echo we set is undone.
_demo_precmd() { stty echo 2>/dev/null; }
case ";${PROMPT_COMMAND:-};" in
    *";_demo_precmd;"*) ;;
    *) PROMPT_COMMAND="_demo_precmd${PROMPT_COMMAND:+;$PROMPT_COMMAND}" ;;
esac

# The fake "yo": runs as a NORMAL command (via accept-line), so its output lands
# cleanly below the preserved prompt line -- no bind -x in-place repaint.
demo() {
    local query="$*"
    if [[ $query == chat* ]]; then
        printf '\033[38;5;246m[chat] just answering, no command: %s\033[0m\n' "$query"
        return 0
    fi
    printf '\033[38;5;246m[explanation] a command for: %s\033[0m\n' "$query"
    # Deliberately contains BOTH a single quote and double quotes to stress the
    # macro escaping. If this prefills intact and editable, escaping is sound.
    demo_prefill_next "echo \"it's a prefilled, editable command\"  # from: ${query}"
}

# (1)+(2) Accept-line rewrite: quote a `demo <query>` line in place, then accept it.
# Non-`demo` lines pass through untouched. Use `printf %q` to shell-quote the query
# into a single literal argument -- it handles quotes, spaces, and metacharacters and
# round-trips exactly. (A hand-rolled ' -> '\'' replacement via ${//} is bug-prone:
# bash mangles the backslashes in the replacement string.)
_demo_rewrite() {
    local line="$READLINE_LINE" query q
    if [[ $line =~ ^[[:space:]]*demo[[:space:]]+([^[:space:]].*)$ ]]; then
        query="${BASH_REMATCH[1]}"
        printf -v q '%q' "$query"
        READLINE_LINE="demo $q"
        READLINE_POINT=${#READLINE_LINE}
    fi
}

# Enter = run the rewrite widget, then a separate finish key bound to accept-line.
# (The finish key must be distinct from \C-m, or the macro would recurse.)
bind -x '"\C-x\C-z": _demo_rewrite'
bind '"\C-x\C-q": accept-line'
bind '"\C-m": "\C-x\C-z\C-x\C-q"'
bind '"\C-j": "\C-x\C-z\C-x\C-q"'

demo_unspike() {
    bind '"\C-m": accept-line' 2>/dev/null
    bind '"\C-j": accept-line' 2>/dev/null
    bind -r '\C-x\C-z' 2>/dev/null
    bind -r '\C-x\C-q' 2>/dev/null
    bind -r '\e[0n' 2>/dev/null
    stty echo 2>/dev/null
    PROMPT_COMMAND="${PROMPT_COMMAND//_demo_precmd;/}"
    PROMPT_COMMAND="${PROMPT_COMMAND//_demo_precmd/}"
    unset -f demo demo_prefill_next _demo_rewrite _demo_precmd
    echo "spike: removed. (Restart the shell for a fully clean slate.)"
    unset -f demo_unspike
}

cat <<'EOF'
spike loaded. Try these in THIS shell and watch the typed line + the next prompt:

  demo just testing
  demo what is (echo hi | wc -c) & also "quotes" and 'apostrophes'?
  demo chat how are you

What to check:
  * the line you typed STAYS on screen (as a quoted `demo ...`), not erased
  * the [explanation]/[chat] text prints cleanly on the line(s) below it
  * for a command query, the NEXT prompt is pre-filled with an EDITABLE command
    (it contains a ' and " on purpose -- confirm they survived intact)
  * metacharacters ( ) | & " ' in your query do NOT run or mangle anything

Note: assumes emacs keymap (bash default). To undo: demo_unspike
EOF
