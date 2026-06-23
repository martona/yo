# yo - zsh integration for the `yo` LLM command assistant.
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Install: add this to your ~/.zshrc so the integration loads every session,
# version-locked to the binary (it is emitted by `yo --init`, so it never goes stale):
#     if command -v yo >/dev/null 2>&1; then eval "$(yo --init zsh)"; fi
# (For development you can also source this file directly: source /path/to/yo/shell/yo.zsh)
# Then just type:
#     yo list every pdf modified this week
#
# Requires the yo binary on PATH (or set YO_BIN to its full path) and your API key
# in the standard env var: ANTHROPIC_API_KEY or OPENAI_API_KEY.

if [[ -z ${ZSH_VERSION-} ]]; then
    print -u2 "yo: zsh integration can only be sourced by zsh."
    return 1 2>/dev/null || exit 1
fi

# Session id for cross-call memory: a stable per-shell id (PID + random suffix so a
# reused PID cannot inherit a closed shell's history). Set once; survives re-sourcing.
# Clear it (YO_SESSION='') or set "memory false" in ~/.yoconf to disable memory.
if [[ -z ${YO_SESSION-} ]]; then
    export YO_SESSION="${$}-${RANDOM}${RANDOM}"
fi

typeset -g _YO_ARMED=0
typeset -g _YO_SEEN_PROMPT=0
typeset -g _YO_RAN_SINCE_ARM=0
typeset -g _YO_LAST_RAN=''

_yo_bin() {
    emulate -L zsh

    if [[ -n ${YO_BIN-} ]]; then
        print -r -- "$YO_BIN"
        return 0
    fi

    if [[ -n ${commands[yo]-} ]]; then
        print -r -- "${commands[yo]}"
        return 0
    fi

    local p
    p="$(whence -p yo 2>/dev/null)" || return 1
    [[ -n $p ]] || return 1
    print -r -- "$p"
}

_yo_width() {
    emulate -L zsh
    local w="${COLUMNS:-80}"
    [[ $w == <-> && $w -gt 1 ]] || w=80
    print -r -- "$w"
}

_yo_color_enabled() {
    [[ -z ${NO_COLOR-} && -t 1 ]]
}

_yo_info() {
    emulate -L zsh
    if _yo_color_enabled; then
        # ANSI 90 ("bright black") is often too dark on macOS dark themes, while
        # 37 reads like normal text. Use a 256-color mid gray for secondary text.
        printf '\033[38;5;246m%s\033[0m\n' "$1"
    else
        print -r -- "$1"
    fi
}

_yo_error() {
    emulate -L zsh
    if _yo_color_enabled; then
        printf '\033[31m%s\033[0m\n' "$1"
    else
        print -u2 -r -- "$1"
    fi
}

_yo_clear_continuation() {
    emulate -L zsh
    typeset -g _YO_ARMED=0
    typeset -g _YO_SEEN_PROMPT=0
    typeset -g _YO_RAN_SINCE_ARM=0
    typeset -g _YO_LAST_RAN=''
    export YO_STATE=''
    export YO_RAN=''
}

_yo_prefill() {
    emulate -L zsh
    local cmd="$1"

    [[ -n $cmd ]] || return 0
    # -z pushes onto zsh's editable buffer stack; -r is essential because zsh's
    # print builtin otherwise interprets escapes such as \n before queueing.
    print -r -z -- "$cmd"
}

_yo_arm_continuation() {
    emulate -L zsh
    local state="$1"
    local seen_prompt="${2:-0}"

    export YO_STATE="$state"
    export YO_RAN=''
    typeset -g _YO_ARMED=1
    typeset -g _YO_SEEN_PROMPT="$seen_prompt"
    typeset -g _YO_RAN_SINCE_ARM=0
    typeset -g _YO_LAST_RAN=''
}

_yo_clear_result_vars() {
    emulate -L zsh
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
    emulate -L zsh
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
    emulate -L zsh
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
    emulate -L zsh
    local bin result rc

    bin="$(_yo_bin)" || {
        _yo_error "yo: binary not found; put it on PATH or set YO_BIN to its full path."
        return 1
    }

    result="$("$bin" --shell zsh --output sh --width "$(_yo_width)" "$@")"
    rc=$?
    _yo_apply_result "$result" 0
    return $rc
}

yo() {
    emulate -L zsh
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

_yo_quote_query() {
    emulate -L zsh
    print -r -- "${(qq)1}"
}

_yo_is_single_quoted_token() {
    emulate -L zsh
    local q="$1"
    local -a words

    [[ $q == \'* && $q == *\' ]] || return 1
    words=(${(z)q})
    (( $#words == 1 ))
}

_yo_rewrite_line() {
    emulate -L zsh
    setopt EXTENDED_GLOB

    local line="$1"
    local query quoted

    if [[ $line == (#b)[[:space:]]#yo[[:space:]]##(*) ]]; then
        query="$match[1]"
        if [[ $query == -* ]]; then
            print -r -- "$line"
            return
        fi
        if _yo_is_single_quoted_token "$query"; then
            print -r -- "$line"
            return
        fi
        quoted="$(_yo_quote_query "$query")"
        print -r -- "yo $quoted"
        return
    fi

    print -r -- "$line"
}

_yo_accept_line() {
    emulate -L zsh
    local rewritten

    rewritten="$(_yo_rewrite_line "$BUFFER")"
    if [[ $rewritten != "$BUFFER" ]]; then
        BUFFER="$rewritten"
        CURSOR=${#BUFFER}
    fi
    zle _yo_orig_accept_line
}

_yo_send_break() {
    emulate -L zsh

    if [[ $_YO_ARMED == 1 ]]; then
        _yo_clear_continuation
    fi
    zle _yo_orig_send_break
}

_yo_preexec() {
    emulate -L zsh
    setopt EXTENDED_GLOB
    local cmd="$1"

    if [[ $_YO_ARMED == 1 && $_YO_SEEN_PROMPT == 1 ]]; then
        cmd="${cmd##[[:space:]]#}"
        typeset -g _YO_LAST_RAN="$cmd"
        typeset -g _YO_RAN_SINCE_ARM=1
        export YO_RAN="$cmd"
    fi
}

_yo_precmd() {
    local last_status=$?
    emulate -L zsh
    local bin result

    if [[ $_YO_ARMED != 1 ]]; then
        return $last_status
    fi

    if [[ $_YO_SEEN_PROMPT != 1 ]]; then
        typeset -g _YO_SEEN_PROMPT=1
        return $last_status
    fi

    if [[ $_YO_RAN_SINCE_ARM != 1 ]]; then
        _yo_clear_continuation
        return $last_status
    fi

    typeset -g _YO_ARMED=0
    bin="$(_yo_bin)" || {
        _yo_error "yo: binary not found; cannot continue."
        _yo_clear_continuation
        return $last_status
    }

    result="$("$bin" --continue --exit "$last_status" --shell zsh --output sh --width "$(_yo_width)")"
    export YO_RAN=''
    _yo_apply_result "$result" 1
    return $last_status
}

_yo_install_zle() {
    emulate -L zsh

    [[ -o interactive ]] || return 0
    zmodload zsh/zle 2>/dev/null || return 0

    if [[ -z ${widgets[_yo_orig_accept_line]-} &&
          ${widgets[accept-line]-} != user:_yo_accept_line ]]; then
        zle -A accept-line _yo_orig_accept_line
    fi
    zle -N accept-line _yo_accept_line

    if [[ -z ${widgets[_yo_orig_send_break]-} &&
          ${widgets[send-break]-} != user:_yo_send_break ]]; then
        zle -A send-break _yo_orig_send_break
    fi
    zle -N send-break _yo_send_break
}

_yo_install_hooks() {
    emulate -L zsh

    [[ -o interactive ]] || return 0
    autoload -Uz add-zsh-hook
    add-zsh-hook -d preexec _yo_preexec 2>/dev/null
    add-zsh-hook -d precmd _yo_precmd 2>/dev/null
    add-zsh-hook preexec _yo_preexec
    add-zsh-hook precmd _yo_precmd
}

_yo_install_zle
_yo_install_hooks
