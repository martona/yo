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

_YO_ARMED=0
_YO_LAST_PREFILL_SPACE=0
_YO_FINISH_KEY='"\C-x\C-m"'

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
    _YO_LAST_PREFILL_SPACE=0
    export YO_STATE=''
    export YO_RAN=''
}

_yo_arm_continuation() {
    export YO_STATE="$1"
    export YO_RAN=''
    _YO_ARMED=1
    _YO_LAST_PREFILL_SPACE="${2:-0}"
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

_yo_set_readline() {
    READLINE_LINE="$1"
    READLINE_POINT=${#READLINE_LINE}
}

_yo_clear_readline() {
    _yo_set_readline ''
}

_yo_apply_result_to_readline() {
    local result="$1"
    local cmd

    if ! _yo_eval_result "$result"; then
        _yo_clear_readline
        return 1
    fi

    case "$YO_RESULT_TYPE" in
        command)
            [[ -n $YO_RESULT_EXPLANATION ]] && _yo_info "$YO_RESULT_EXPLANATION"
            cmd="$YO_RESULT_COMMAND"
            if [[ $YO_RESULT_PREFILL_SPACE == 1 ]]; then
                cmd=" $cmd"
            fi
            _yo_set_readline "$cmd"
            if [[ $YO_RESULT_PENDING == 1 ]]; then
                _yo_arm_continuation "$YO_RESULT_STATE" "$YO_RESULT_PREFILL_SPACE"
            else
                _yo_clear_continuation
            fi
            ;;
        chat)
            [[ -n $YO_RESULT_RESPONSE ]] && _yo_info "$YO_RESULT_RESPONSE"
            _yo_clear_readline
            _yo_clear_continuation
            ;;
        error)
            _yo_error "yo: $YO_RESULT_MESSAGE"
            _yo_clear_readline
            _yo_clear_continuation
            ;;
        *)
            _yo_error "yo: unexpected response type '$YO_RESULT_TYPE'"
            _yo_clear_readline
            _yo_clear_continuation
            return 1
            ;;
    esac
}

_yo_apply_result_to_stdout() {
    local result="$1"

    if ! _yo_eval_result "$result"; then
        return 1
    fi

    case "$YO_RESULT_TYPE" in
        command)
            [[ -n $YO_RESULT_EXPLANATION ]] && _yo_info "$YO_RESULT_EXPLANATION"
            printf '%s\n' "$YO_RESULT_COMMAND"
            ;;
        chat)
            [[ -n $YO_RESULT_RESPONSE ]] && _yo_info "$YO_RESULT_RESPONSE"
            ;;
        error)
            _yo_error "yo: $YO_RESULT_MESSAGE"
            return 1
            ;;
        *)
            _yo_error "yo: unexpected response type '$YO_RESULT_TYPE'"
            return 1
            ;;
    esac
}

_yo_bind_finish() {
    local action="$1"
    if [[ ${_YO_TEST_NO_BIND-} == 1 ]]; then
        _YO_TEST_FINISH="$action"
        return 0
    fi
    bind "$_YO_FINISH_KEY: $action" 2>/dev/null || true
}

_yo_finish_accept() {
    _yo_bind_finish accept-line
}

_yo_finish_redraw() {
    _yo_bind_finish redraw-current-line
}

_yo_extract_query() {
    local line="$1"

    [[ $line =~ ^[[:space:]]*yo[[:space:]]+([^[:space:]].*)$ ]] || return 1
    printf '%s\n' "${BASH_REMATCH[1]}"
}

_yo_history_record() {
    local ran="$1"

    if [[ $_YO_LAST_PREFILL_SPACE == 1 && $ran == ' '* ]]; then
        return 0
    fi
    history -s -- "$ran" 2>/dev/null || true
}

_yo_run_pending_line() {
    local ran="$1"
    local sent="$ran"
    local status result bin

    if [[ -z ${ran//[[:space:]]/} ]]; then
        _yo_clear_continuation
        _yo_clear_readline
        _yo_finish_accept
        return 0
    fi

    if [[ $_YO_LAST_PREFILL_SPACE == 1 && $sent == ' '* ]]; then
        sent="${sent# }"
    fi

    _yo_history_record "$ran"
    printf '\n'
    eval -- "$ran"
    status=$?

    if (( status == 130 )); then
        _yo_clear_continuation
        _yo_clear_readline
        _yo_finish_redraw
        return 130
    fi

    bin="$(_yo_bin)" || {
        _yo_error "yo: binary not found; cannot continue."
        _yo_clear_continuation
        _yo_clear_readline
        _yo_finish_redraw
        return "$status"
    }

    _YO_ARMED=0
    export YO_RAN="$sent"
    result="$("$bin" --continue --exit "$status" --shell bash --output sh --width "$(_yo_width)")"
    export YO_RAN=''
    _yo_apply_result_to_readline "$result"
    _yo_finish_redraw
    return "$status"
}

_yo_readline_enter() {
    local line="$READLINE_LINE"
    local query result bin

    if query="$(_yo_extract_query "$line")"; then
        if [[ $query == -* ]]; then
            _yo_finish_accept
            return 0
        fi

        _yo_clear_continuation
        bin="$(_yo_bin)" || {
            _yo_error "yo: binary not found; put it on PATH or set YO_BIN to its full path."
            _yo_clear_readline
            _yo_finish_redraw
            return 1
        }

        result="$("$bin" --shell bash --output sh --width "$(_yo_width)" "$query")"
        _yo_apply_result_to_readline "$result"
        _yo_finish_redraw
        return 0
    fi

    if [[ $_YO_ARMED == 1 ]]; then
        _yo_run_pending_line "$line"
        return $?
    fi

    _yo_finish_accept
}

_yo_prompt_command() {
    local status=$?

    if [[ $_YO_ARMED == 1 ]]; then
        _yo_clear_continuation
    fi
    return "$status"
}

_yo_install_prompt_command() {
    [[ $- == *i* ]] || return 0

    case ";${PROMPT_COMMAND-};" in
        *";_yo_prompt_command;"*) ;;
        ";") PROMPT_COMMAND="_yo_prompt_command" ;;
        *) PROMPT_COMMAND="_yo_prompt_command; $PROMPT_COMMAND" ;;
    esac
}

_yo_install_readline() {
    [[ $- == *i* ]] || return 0

    bind -x '"\C-x\C-y": _yo_readline_enter'
    _yo_finish_accept
    bind '"\C-m": "\C-x\C-y\C-x\C-m"'
    bind '"\C-j": "\C-x\C-y\C-x\C-m"'
}

yo() {
    local bin result

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

    # Fallback for shells without an active Readline hook. It cannot prefill, so
    # print the command instead of silently dropping it.
    result="$("$bin" --shell bash --output sh --width "$(_yo_width)" "$@")"
    _yo_apply_result_to_stdout "$result"
}

_yo_install_readline
_yo_install_prompt_command
