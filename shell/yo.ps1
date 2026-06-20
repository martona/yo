# yo - PowerShell integration for the `yo` LLM command assistant.
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Install: dot-source this from your $PROFILE, AFTER any custom prompt setup:
#     . "C:\path\to\yo\shell\yo.ps1"
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
# Note: you are typing a real PowerShell command line, so quote any request that
# contains shell metacharacters (| > < & ; etc.):
#     yo 'what does | do in powershell'

# Decode yo.exe's UTF-8 stdout correctly (and render Unicode in chat replies).
# PowerShell decodes a native command's output using [Console]::OutputEncoding,
# which defaults to the legacy OEM code page; without this, multi-byte UTF-8
# (e.g. emoji) arrives as mojibake. No-BOM UTF-8; guarded for restricted hosts.
try { [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false) } catch {}

function Get-YoBin {
    if ($env:YO_BIN) { return $env:YO_BIN }
    if (Get-Command yo.exe -ErrorAction SilentlyContinue) { return 'yo.exe' }
    return $null
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
            Write-Host $r.response
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

    # stdout = one JSON line; stderr = the transient "thinking..." indicator.
    $json = & $bin @args
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
    if ($lastId -le $global:YoBaseline) { return }  # nothing has run since we armed

    # The user ran the prefilled step -> advance the sequence.
    $global:YoArmed = $false
    $bin = Get-YoBin
    if (-not $bin) { return }
    $code = if ($ok) { 0 } else { 1 }
    $json = & $bin --continue --exit $code   # inherits $env:YO_STATE
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
