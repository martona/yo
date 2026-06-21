# yo - PowerShell integration for the `yo` LLM command assistant.
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Install: add this to your $PROFILE so the integration loads every session,
# version-locked to the binary (it is emitted by `yo --init`, so it never goes stale):
#     if (Get-Command yo -ErrorAction SilentlyContinue) { yo --init powershell | Out-String | iex }
# (For development you can also dot-source this file directly: . C:\path\to\yo\shell\yo.ps1)
# Then just type:
#     yo list every pdf modified this week
#
# Requires yo.exe on PATH (or set $env:YO_BIN to its full path) and your API key
# in the standard env var: ANTHROPIC_API_KEY or OPENAI_API_KEY.
#
# Multi-step tasks: when the model returns a command with pending=true, yo runs
# it and then, at the next prompt, automatically fetches the next step based on
# that command's exit code, prefilling each in turn. Type a new 'yo ...' to
# cancel a sequence in progress.
#
# You can type shell metacharacters -- ( ) < > & ; | $ " and backtick -- directly
# in a yo question; you do NOT need to quote them. An Enter-key hook (registered at
# the bottom of this file) captures the raw line and single-quotes it before
# PowerShell parses it. Example:
#     yo what does (Get-Process | Where CPU > 50) do
# Requires PSReadLine (pwsh 7+); without it, yo falls back to plain PowerShell
# argument parsing and you must quote metacharacters yourself.

# Decode yo.exe's UTF-8 stdout correctly (and render Unicode in chat replies).
# PowerShell decodes a native command's output using [Console]::OutputEncoding,
# which defaults to the legacy OEM code page; without this, multi-byte UTF-8
# (e.g. emoji) arrives as mojibake. No-BOM UTF-8; guarded for restricted hosts.
try { [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false) } catch {}

# Session id for cross-call memory: a stable per-shell id (PID + random suffix so a
# reused PID cannot inherit a closed shell's history). Set once; survives re-sourcing.
# Clear it ($env:YO_SESSION = '') or set "memory false" in ~/.yoconf to disable memory.
if (-not $env:YO_SESSION) {
    $env:YO_SESSION = "$PID-$([guid]::NewGuid().ToString('N').Substring(0, 8))"
}

function Get-YoBin {
    if ($env:YO_BIN) { return $env:YO_BIN }
    if (Get-Command yo.exe -ErrorAction SilentlyContinue) { return 'yo.exe' }
    return $null
}

# Get-YoWidth reports the console width, passed to the binary via --width so it can
# word-wrap prose output. Wrapping lives in the binary (one implementation shared by
# every shell); the snippet only reports the width. Falls back to 80 off-console.
function Get-YoWidth {
    try { $w = [Console]::WindowWidth; if ($w -gt 1) { return $w } } catch {}
    return 80
}

# Set-YoPrefill schedules a one-shot prefill of $cmd at the next prompt: it stashes
# the command in $env:YO_PENDING, clears any prior OnIdle handler, then registers a
# single handler that inserts the command and unregisters ITSELF after firing once.
# The self-unregister is what fixes PS 5.1 / old PSReadLine, where a persistent
# handler could apply the insert twice. (We pass the command via the env var, not
# -MessageData: $Event.MessageData is null for engine OnIdle events.)
function Set-YoPrefill([string]$cmd) {
    if (-not $cmd) { return }
    $env:YO_PENDING = $cmd
    Get-EventSubscriber -SourceIdentifier 'PowerShell.OnIdle' -ErrorAction SilentlyContinue |
        ForEach-Object { Unregister-Event -SubscriptionId $_.SubscriptionId -ErrorAction SilentlyContinue }
    $null = Register-EngineEvent -SourceIdentifier 'PowerShell.OnIdle' -Action {
        $c = $env:YO_PENDING
        $env:YO_PENDING = ''
        if ($c) { try { [Microsoft.PowerShell.PSConsoleReadLine]::Insert($c) } catch {} }
        Unregister-Event -SubscriptionId $EventSubscriber.SubscriptionId -ErrorAction SilentlyContinue
    }
}

# Invoke-YoResult handles one yo.exe JSON result (shared by `yo` and the
# continuation driver). It prints chat/explanations, stashes the command to
# prefill (via Set-YoPrefill), carries the continuation blob in $env:YO_STATE, and
# sets $global:YoArmed when a multi-step sequence is in progress.
function Invoke-YoResult([string]$json) {
    if (-not $json) {
        Write-Host "yo: no response from yo.exe." -ForegroundColor Red
        $env:YO_STATE = ''; $global:YoArmed = $false
        return
    }
    try {
        $r = $json | ConvertFrom-Json
    } catch {
        Write-Host "yo: could not parse response: $json" -ForegroundColor Red
        $env:YO_STATE = ''; $global:YoArmed = $false
        return
    }
    switch ($r.type) {
        'command' {
            if ($r.explanation) { Write-Host $r.explanation -ForegroundColor DarkGray }
            Set-YoPrefill $r.command
            if ($r.pending) {
                $env:YO_STATE = $r.state
                $global:YoArmed = $true
            } else {
                $env:YO_STATE = ''
                $global:YoArmed = $false
            }
        }
        'chat' {
            Write-Host $r.response -ForegroundColor DarkGray
            $env:YO_STATE = ''; $global:YoArmed = $false
        }
        'error' {
            Write-Host "yo: $($r.message)" -ForegroundColor Red
            $env:YO_STATE = ''; $global:YoArmed = $false
        }
        default {
            Write-Host "yo: unexpected response type '$($r.type)'" -ForegroundColor Red
            $env:YO_STATE = ''; $global:YoArmed = $false
        }
    }
}

function yo {
    # A new yo query cancels any in-progress continuation.
    $global:YoArmed = $false
    $env:YO_STATE = ''

    $bin = Get-YoBin
    if (-not $bin) {
        Write-Host "yo: yo.exe not found; put it on PATH or set `$env:YO_BIN to its full path." -ForegroundColor Red
        return
    }

    # Binary-level debug flags (--dry-run, --check, --scrollback): pass straight
    # through and print raw output; don't parse as a Result or prefill.
    if ($args.Count -gt 0 -and $args[0] -like '-*') {
        & $bin @args
        return
    }

    # stdout = one JSON line; stderr = the transient "thinking..." indicator.
    # --width lets the binary word-wrap its prose output to this terminal.
    $json = & $bin --width (Get-YoWidth) @args
    Invoke-YoResult $json
    if ($global:YoArmed) { $global:YoBaseline = $null }  # baseline captured at the next prompt
}

# Invoke-YoContinuation runs from the prompt function. When a sequence is armed
# and the user has actually run the current step (history advanced past the
# baseline captured when we armed), it fetches the next step using that step's
# success as the exit code.
function Invoke-YoContinuation([bool]$ok) {
    $h = Get-History -Count 1
    $lastId = if ($h) { $h.Id } else { 0 }

    if ($null -eq $global:YoBaseline) {
        $global:YoBaseline = $lastId   # first prompt after arming; wait for a run
        return
    }
    if ($lastId -le $global:YoBaseline) {
        # A fresh prompt with no new history since we armed means the user declined
        # the prefilled step (bare Enter, or Ctrl+C abandoned the line) -> cancel.
        $global:YoArmed = $false
        $env:YO_STATE = ''
        return
    }

    # The user ran the prefilled step -> advance the sequence.
    $global:YoArmed = $false
    $bin = Get-YoBin
    if (-not $bin) { return }
    $code = if ($ok) { 0 } else { 1 }
    # Pass the command the user ACTUALLY ran (edits included) so the model can
    # reconcile its suggestion against reality. Env var, not a flag, so the command's
    # metacharacters need no escaping; inherited by the child like YO_STATE.
    $env:YO_RAN = if ($h) { $h.CommandLine } else { '' }
    $json = & $bin --continue --exit $code --width (Get-YoWidth)   # inherits $env:YO_STATE
    $env:YO_RAN = ''
    Invoke-YoResult $json
    if ($global:YoArmed) { $global:YoBaseline = $lastId }  # re-armed: fire on the next run
}

# Wrap the existing prompt so we can drive continuations. The original is saved
# once, so re-sourcing this file is safe.
if (-not $global:YoOrigPrompt) {
    $global:YoOrigPrompt = $function:prompt
}
function global:prompt {
    $yoOk = $?   # capture the last command's success FIRST, before anything else
    if ($global:YoArmed) {
        try { Invoke-YoContinuation $yoOk } catch { $global:YoArmed = $false }
    }
    if ($global:YoOrigPrompt) { & $global:YoOrigPrompt }
    else { "PS $($ExecutionContext.SessionState.Path.CurrentLocation)> " }
}

# Clear any stale OnIdle prefill handlers from a previous source or the M0 spike,
# so the session starts clean. Prefills are scheduled on demand by Set-YoPrefill
# (one-shot handlers that remove themselves after firing).
Get-EventSubscriber -SourceIdentifier 'PowerShell.OnIdle' -ErrorAction SilentlyContinue |
    ForEach-Object { Unregister-Event -SubscriptionId $_.SubscriptionId -ErrorAction SilentlyContinue }

# Raw-line capture (the readline hook): intercept Enter so a `yo <query>` line is
# single-quoted BEFORE PowerShell parses it. This is the PowerShell analog of
# yoshell's rl_yo_accept_line, which grabs the raw Readline buffer at accept time
# (pre-parse). Pre-parse is the only correct point: metacharacters like ; | < >
# act at parse time, so reading the line inside the `yo` function (via
# $MyInvocation.Line) would be too late -- and unsafe, since `yo a; rm b` would
# already have run `rm b` as its own statement.
#
# On every Enter:
#   1. Read the raw edit buffer (pre-parse) via GetBufferState.
#   2. Rewrite to  yo '<query>'  ONLY when ALL of:
#        - the first token is exactly `yo` followed by a query; AND
#        - the query does not start with `-` (so `yo --dry-run ...` and other
#          debug-flag calls pass through to normal parsing); AND
#        - the query is not already one single-quoted token (so a line recalled
#          from history is not double-wrapped) -- i.e. the hook is idempotent.
#      Embedded single quotes are escaped by doubling (' becomes ''). Inside a
#      single-quoted string nothing else expands, so the query reaches the binary
#      byte-for-byte, internal whitespace included.
#   3. ALWAYS submit via ValidateAndAcceptLine -- the stock Enter binding -- so
#      every non-yo line behaves exactly as before (including multi-line /
#      incomplete-input handling, which ValidateAndAcceptLine performs itself).
#
# This takes over the Enter key globally, so source this file AFTER any module that
# also rebinds Enter (last writer wins). Needs PSReadLine 2.x (pwsh 7+); if it is
# absent the hook is skipped and `yo` falls back to plain argument parsing. The
# rewrite is wrapped so any failure degrades to a plain accept -- never a stuck key.
try {
    Set-PSReadLineKeyHandler -Chord Enter -ScriptBlock {
        param($key, $arg)
        try {
            $line = $null; $cursor = $null
            [Microsoft.PowerShell.PSConsoleReadLine]::GetBufferState([ref]$line, [ref]$cursor)
            # (?s) lets . span a multi-line buffer; (?!-) skips debug-flag calls.
            if ($line -match '(?s)^\s*yo\s+(?!-)(.+)$') {
                $q = $Matches[1]
                if ($q -notmatch "^'([^']|'')*'$") {   # not already one quoted token
                    $esc = "'" + $q.Replace("'", "''") + "'"
                    [Microsoft.PowerShell.PSConsoleReadLine]::Replace(0, $line.Length, "yo $esc")
                }
            }
        } catch {}
        try { [Microsoft.PowerShell.PSConsoleReadLine]::ValidateAndAcceptLine() }
        catch { [Microsoft.PowerShell.PSConsoleReadLine]::AcceptLine() }
    }
} catch {
    Write-Host "yo: Enter hook not installed ($($_.Exception.Message)); quote metacharacters in yo queries." -ForegroundColor DarkYellow
}

# Ctrl+C cancels an armed multi-step sequence (you are declining the prefilled
# step), on top of its normal copy/cancel-line behavior. This handler fires only
# during line editing -- a running command's Ctrl+C is handled by the runtime, not
# here -- and is a no-op when nothing is armed. We cancel only when there is no
# selection, so Ctrl+C-to-copy still just copies.
try {
    Set-PSReadLineKeyHandler -Chord Ctrl+c -ScriptBlock {
        param($key, $arg)
        if ($global:YoArmed) {
            $selStart = -1; $selLen = -1
            try { [Microsoft.PowerShell.PSConsoleReadLine]::GetSelectionState([ref]$selStart, [ref]$selLen) } catch {}
            if ($selLen -le 0) {
                $global:YoArmed = $false
                $env:YO_STATE = ''
            }
        }
        [Microsoft.PowerShell.PSConsoleReadLine]::CopyOrCancelLine()
    }
} catch {}
