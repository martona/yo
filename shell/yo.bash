# yo - bash integration for the `yo` LLM command assistant.
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Install: add this to your ~/.bashrc so the integration loads every session,
# version-locked to the binary (it is emitted by `yo --init`, so it never goes stale):
#     if command -v yo >/dev/null 2>&1; then eval "$(yo --init bash)"; fi
# (For development you can also source this file directly: source /path/to/yo/shell/yo.bash)
# Then just type:
#     yo list every pdf modified this week
#
# Requires bash 4.2+ with Readline. macOS /bin/bash 3.2 is not supported; install
# a modern bash (for example via Homebrew) and source this from that shell.
#
# How it works (mirrors the zsh adapter, since bash has no `print -z`):
#   * An accept-line hook rewrites a `yo <query>` line into a safely single-quoted
#     `yo '<query>'` and lets readline ACCEPT it -- so the line you typed is preserved
#     on screen and metacharacters never reach the parser, exactly like the zsh path.
#   * The `yo` function then runs as an ordinary command: it calls the binary and
#     prints the explanation/answer cleanly below your line.
#   * For a command result it places the (editable) command on the NEXT prompt using
#     the terminal's DSR reply (ESC[5n -> bound ESC[0n), the bash analogue of zsh's
#     `print -z`. Kernel echo of the reply is suppressed so nothing leaks on screen.
#   * Multi-step continuation is driven by PROMPT_COMMAND (the bash analogue of zsh's
#     precmd): it detects the prefilled command ran, captures its exit code, and asks
#     the binary for the next step.

# Bail out for non-bash shells FIRST, using POSIX `[ ]` (not bash's `[[ ]]`): a shell
# like dash (Linux /bin/sh) has no `[[` keyword, so a `[[`-based guard would fail open
# and let dash fall through into the bash-only syntax below and choke. `[ ]` is POSIX,
# so dash evaluates this correctly and returns before parsing anything bash-specific.
if [ -z "${BASH_VERSION:-}" ]; then
    return 0 2>/dev/null || exit 0
fi

if (( BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 2) )); then
    return 0 2>/dev/null || exit 0
fi

# Session id for cross-call memory: a stable per-shell id (PID + random suffix so a
# reused PID cannot inherit a closed shell's history). Set once; survives re-sourcing.
# Clear it (YO_SESSION='') or set "memory false" in ~/.yoconf to disable memory.
if [[ -z ${YO_SESSION-} ]]; then
    export YO_SESSION="${$}-${RANDOM}${RANDOM}"
fi

# OS + shell version for the model's environment context (the binary falls back to
# its own OS family if these are unset). OS is computed once; the shell version is
# free from $BASH_VERSION. macOS -> sw_vers, Linux -> /etc/os-release, else uname.
if [[ -z ${YO_OS-} ]]; then
    if command -v sw_vers >/dev/null 2>&1; then
        export YO_OS="macOS $(sw_vers -productVersion 2>/dev/null)"
    elif [[ -r /etc/os-release ]]; then
        export YO_OS="$(. /etc/os-release 2>/dev/null && printf '%s' "${PRETTY_NAME:-${NAME:-Linux}}")"
    else
        export YO_OS="$(uname -sr 2>/dev/null)"
    fi
fi
export YO_SHELL_VERSION="${BASH_VERSION%%(*}"

_YO_ARMED=0
_YO_SEEN_PROMPT=0
_YO_RAN_SINCE_ARM=0
_YO_LAST_RAN=''
_YO_RESTORE_ECHO=0
_YO_AT_PROMPT=0

_yo_bin() {
    if [[ -n ${YO_BIN-} ]]; then
        printf '%s\n' "$YO_BIN"
        return 0
    fi

    local p
    p="$(type -P yo 2>/dev/null)" || return 1
    [[ -n $p ]] || return 1
    printf '%s\n' "$p"
}

_yo_width() {
    local w="${COLUMNS:-80}"
    [[ $w =~ ^[0-9]+$ && $w -gt 1 ]] || w=80
    printf '%s\n' "$w"
}

_yo_color_enabled() {
    [[ -z ${NO_COLOR-} && -t 1 ]]
}

_yo_info() {
    if _yo_color_enabled; then
        printf '\033[38;5;246m%s\033[0m\n' "$1"
    else
        printf '%s\n' "$1"
    fi
}

_yo_error() {
    if _yo_color_enabled; then
        printf '\033[31m%s\033[0m\n' "$1"
    else
        printf '%s\n' "$1" >&2
    fi
}

_yo_clear_continuation() {
    _YO_ARMED=0
    _YO_SEEN_PROMPT=0
    _YO_RAN_SINCE_ARM=0
    _YO_LAST_RAN=''
    export YO_STATE=''
    export YO_RAN=''
}

_yo_arm_continuation() {
    export YO_STATE="$1"
    export YO_RAN=''
    _YO_ARMED=1
    _YO_SEEN_PROMPT="${2:-0}"
    _YO_RAN_SINCE_ARM=0
    _YO_LAST_RAN=''
}

# Place an editable command on the NEXT prompt via the terminal's DSR reply, and
# suppress the kernel's echo of that reply so it cannot leak as a stray ^[[0n during
# the canonical-mode gap before readline raws the tty (echo is restored in _yo_precmd).
# If readline is unavailable (non-interactive / no tty), fall back to printing it.
_yo_prefill() {
    local cmd="$1" esc
    [[ -n $cmd ]] || return 0
    esc=${cmd//\\/\\\\}
    esc=${esc//\"/\\\"}
    if bind '"\e[0n": "'"$esc"'"' 2>/dev/null; then
        _YO_RESTORE_ECHO=1
        stty -echo 2>/dev/null
        printf '\e[5n'
    else
        printf '%s\n' "$cmd"
    fi
}

_yo_clear_result_vars() {
    YO_RESULT_TYPE=''
    YO_RESULT_COMMAND=''
    YO_RESULT_EXPLANATION=''
    YO_RESULT_RESPONSE=''
    YO_RESULT_MESSAGE=''
    YO_RESULT_PENDING='0'
    YO_RESULT_STATE=''
    YO_RESULT_PREFILL_SPACE='0'
}

_yo_eval_result() {
    local result="$1"

    _yo_clear_result_vars
    if [[ -z $result ]]; then
        _yo_error "yo: no response from yo."
        _yo_clear_continuation
        return 1
    fi
    eval "$result"
}

_yo_apply_result() {
    local result="$1"
    local seen_prompt="${2:-0}"
    local cmd

    if ! _yo_eval_result "$result"; then
        return 1
    fi

    case "$YO_RESULT_TYPE" in
        command)
            [[ -n $YO_RESULT_EXPLANATION ]] && _yo_info "$YO_RESULT_EXPLANATION"
            cmd="$YO_RESULT_COMMAND"
            [[ $YO_RESULT_PREFILL_SPACE == 1 ]] && cmd=" $cmd"
            _yo_prefill "$cmd"
            if [[ $YO_RESULT_PENDING == 1 ]]; then
                _yo_arm_continuation "$YO_RESULT_STATE" "$seen_prompt"
            else
                _yo_clear_continuation
            fi
            ;;
        chat)
            [[ -n $YO_RESULT_RESPONSE ]] && _yo_info "$YO_RESULT_RESPONSE"
            _yo_clear_continuation
            ;;
        error)
            _yo_error "yo: $YO_RESULT_MESSAGE"
            _yo_clear_continuation
            ;;
        *)
            _yo_error "yo: unexpected response type '$YO_RESULT_TYPE'"
            _yo_clear_continuation
            return 1
            ;;
    esac
}

_yo_invoke() {
    local bin result

    bin="$(_yo_bin)" || {
        _yo_error "yo: binary not found; put it on PATH or set YO_BIN to its full path."
        return 1
    }

    result="$("$bin" --shell bash --output sh --width "$(_yo_width)" "$@")"
    _yo_apply_result "$result" 0
}

yo() {
    local bin

    # A new yo query cancels any in-progress continuation.
    _yo_clear_continuation

    bin="$(_yo_bin)" || {
        _yo_error "yo: binary not found; put it on PATH or set YO_BIN to its full path."
        return 1
    }

    # Bare `yo`: show help rather than making a no-query LLM call.
    if (( $# == 0 )); then
        "$bin" --help
        return $?
    fi

    # Binary-level flags (--dry-run, --check, --scrollback, ...): pass straight
    # through and print raw output; don't parse as a Result or prefill.
    if [[ ${1-} == -* ]]; then
        "$bin" "$@"
        return $?
    fi

    _yo_invoke "$@"
}

# Wrap a query in single quotes, escaping embedded single quotes as '\'' so the whole
# query becomes ONE literal argument. (printf %q would also work but escapes with
# backslashes; single quotes read better and match the zsh adapter's display.)
_yo_single_quote() {
    local s="$1" out="'"
    while [[ $s == *\'* ]]; do
        out+="${s%%\'*}'\\''"
        s="${s#*\'}"
    done
    out+="$s'"
    printf '%s' "$out"
}

# Accept-line hook: rewrite a raw `yo <query>` buffer to `yo '<query>'` before readline
# parses it, so metacharacters ( ) < > & ; | $ survive; the bound finish key then
# accept-lines it, preserving the typed line on screen. Non-`yo` lines are untouched.
# (Only READLINE_LINE is changed here -- a bind-x widget reliably affects the buffer,
# but its ordinary global-variable writes do NOT persist consistently when invoked via
# a macro, which is why continuation's "did a command run" detection lives in a DEBUG
# trap below, not here.)
_yo_rewrite_buffer() {
    local line="$READLINE_LINE" query q

    [[ $line =~ ^[[:space:]]*yo[[:space:]]+([^[:space:]].*)$ ]] || return 0
    query="${BASH_REMATCH[1]}"
    # A query that starts with `-` is a debug-flag call (yo --dry-run ...): leave it
    # for normal argument parsing.
    [[ $query == -* ]] && return 0
    # Already one single-quoted token (e.g. a line recalled from history): idempotent.
    [[ $query == \'*\' ]] && return 0
    q="$(_yo_single_quote "$query")"
    READLINE_LINE="yo $q"
    READLINE_POINT=${#READLINE_LINE}
}

# preexec via the DEBUG trap: fires before each command runs. The first command after
# a prompt (when _YO_AT_PROMPT is set by _yo_mark_prompt) is the user's interactive
# command; if a continuation is armed, record that it ran and what it was. This ties
# detection to ACTUAL command execution -- reliable across bash versions, and immune to
# prefill_space / HISTCONTROL (unlike history) and to bind-x-in-macro quirks (unlike a
# readline widget).
_yo_preexec() {
    [[ $_YO_AT_PROMPT == 1 ]] || return 0
    # Skip yo's own machinery, never treat it as the user's command: the prompt-command
    # functions, and _yo_rewrite_buffer -- the accept-line widget runs as a command via
    # the Enter macro just before the real one, so without this it gets captured instead.
    case "$BASH_COMMAND" in
        _yo_precmd | _yo_mark_prompt | _yo_preexec* | _yo_rewrite_buffer) return 0 ;;
    esac
    _YO_AT_PROMPT=0
    [[ $_YO_ARMED == 1 && $_YO_SEEN_PROMPT == 1 && $_YO_RAN_SINCE_ARM == 0 ]] || return 0
    _YO_RAN_SINCE_ARM=1
    _YO_LAST_RAN="$BASH_COMMAND"
}

# Runs LAST in PROMPT_COMMAND. Two jobs:
#   - Restore echo suppressed by _yo_prefill. Doing it here (rather than at the top of
#     _yo_precmd) means echo is on before readline for BOTH an initial-query prefill
#     and a continuation-step prefill (the latter is set DURING _yo_precmd, after its
#     top would have run), so the prefilled command renders on the next prompt.
#   - Mark that the next command executed is the user's (for the DEBUG-trap preexec).
_yo_mark_prompt() {
    if [[ $_YO_RESTORE_ECHO == 1 ]]; then
        stty echo 2>/dev/null
        _YO_RESTORE_ECHO=0
    fi
    _YO_AT_PROMPT=1
}

# PROMPT_COMMAND hook (bash analogue of zsh precmd). Drives continuation and restores
# echo suppressed by _yo_prefill. Must capture $? first and run before other prompt
# commands (it is prepended), so the exit code is the just-run command's.
_yo_precmd() {
    local last_status=$?

    # Consume the at-prompt flag here: if the user ran a command, the DEBUG trap
    # already cleared it (and set _YO_RAN_SINCE_ARM); if they didn't (bare Enter,
    # Ctrl-C), it is still set, and clearing it now stops this function's own commands
    # from tripping the trap and faking a run.
    _YO_AT_PROMPT=0

    [[ $_YO_ARMED == 1 ]] || return "$last_status"

    # First prompt after arming an initial query: the command is now prefilled and
    # waiting. Mark it seen and wait for the user to run it (the accept-line hook will
    # flag _YO_RAN_SINCE_ARM when they do).
    if [[ $_YO_SEEN_PROMPT != 1 ]]; then
        _YO_SEEN_PROMPT=1
        return "$last_status"
    fi

    # No command was submitted since arming -> the user declined (bare Enter, Ctrl-C,
    # or cleared the line): cancel the sequence.
    if [[ $_YO_RAN_SINCE_ARM != 1 ]]; then
        _yo_clear_continuation
        return "$last_status"
    fi

    # A command ran: fetch the next step with its exit code and the executed command.
    _YO_ARMED=0
    local bin result
    bin="$(_yo_bin)" || {
        _yo_error "yo: binary not found; cannot continue."
        _yo_clear_continuation
        return "$last_status"
    }
    export YO_RAN="$_YO_LAST_RAN"
    result="$("$bin" --continue --exit "$last_status" --shell bash --output sh --width "$(_yo_width)")"
    export YO_RAN=''
    _yo_apply_result "$result" 1
    return "$last_status"
}

_yo_install_readline() {
    [[ $- == *i* ]] || return 0

    bind -x '"\C-x\C-y": _yo_rewrite_buffer' 2>/dev/null
    bind '"\C-x\C-m": accept-line' 2>/dev/null
    bind '"\C-m": "\C-x\C-y\C-x\C-m"' 2>/dev/null
    bind '"\C-j": "\C-x\C-y\C-x\C-m"' 2>/dev/null
}

_yo_install_precmd() {
    [[ $- == *i* ]] || return 0

    # _yo_precmd runs FIRST (captures $? before any other prompt command resets it);
    # _yo_mark_prompt runs LAST (so the next command executed is seen as the user's by
    # the DEBUG trap).
    if [[ ";${PROMPT_COMMAND-};" != *";_yo_precmd;"* ]]; then
        PROMPT_COMMAND="_yo_precmd${PROMPT_COMMAND:+;$PROMPT_COMMAND};_yo_mark_prompt"
    fi
}

_yo_install_preexec() {
    [[ $- == *i* ]] || return 0

    local cur
    cur="$(trap -p DEBUG 2>/dev/null)"
    case "$cur" in
        *_yo_preexec*) return 0 ;;
        "") trap '_yo_preexec' DEBUG ;;
        *)
            # Preserve an existing DEBUG handler (e.g. bash-preexec): run ours, then it.
            # `trap -p` prints: trap -- 'BODY' DEBUG
            _YO_PREV_DEBUG="${cur#trap -- \'}"
            _YO_PREV_DEBUG="${_YO_PREV_DEBUG%\' DEBUG}"
            trap '_yo_preexec; eval "$_YO_PREV_DEBUG"' DEBUG
            ;;
    esac
}

_yo_install_readline
_yo_install_precmd
_yo_install_preexec
