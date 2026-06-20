# yo — PowerShell integration for the `yo` LLM command assistant.
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Install: dot-source this from your $PROFILE, e.g.
#     . "C:\path\to\yo\shell\yo.ps1"
# Then just type:
#     yo list every pdf modified this week
#
# Requires yo.exe on PATH, or set $env:YO_BIN to its full path.
#
# Note: you are typing a real PowerShell command line, so quote any request that
# contains shell metacharacters (| > < & ; etc.):
#     yo 'what does | do in powershell'
#     yo "find files and redirect with >"

function yo {
    $bin = if ($env:YO_BIN) { $env:YO_BIN }
           elseif (Get-Command yo.exe -ErrorAction SilentlyContinue) { 'yo.exe' }
           else { $null }
    if (-not $bin) {
        Write-Host "yo: yo.exe not found — put it on PATH or set `$env:YO_BIN to its full path." -ForegroundColor Red
        return
    }

    # stdout carries one JSON line; stderr carries the transient "thinking…"
    # indicator, which we let flow straight to the console.
    $json = & $bin @args
    if (-not $json) {
        Write-Host "yo: no response from yo.exe." -ForegroundColor Red
        return
    }

    try {
        $r = $json | ConvertFrom-Json
    } catch {
        Write-Host "yo: could not parse response: $json" -ForegroundColor Red
        return
    }

    switch ($r.type) {
        'command' {
            if ($r.explanation) { Write-Host $r.explanation -ForegroundColor DarkGray }
            # Stash the command; the OnIdle handler below prefills it at the next prompt.
            $env:YO_PENDING = $r.command
        }
        'chat'    { Write-Host $r.response }
        'error'   { Write-Host "yo: $($r.message)" -ForegroundColor Red }
        default   { Write-Host "yo: unexpected response type '$($r.type)'" -ForegroundColor Red }
    }
}

# Prefill at the next prompt. A 'command' response stashes the text in
# $env:YO_PENDING (an env var so it crosses the OnIdle handler's runspace
# boundary); on the next idle - once the fresh prompt's editor is live - we
# Insert() it so it appears prefilled, editable, and runs only when you press
# Enter. Mechanism proven in spikes/m0-powershell-prefill.ps1.
if (-not $global:__yoOnIdleRegistered) {
    $global:__yoOnIdleRegistered = $true
    $null = Register-EngineEvent -SourceIdentifier PowerShell.OnIdle -Action {
        if ($env:YO_PENDING) {
            $cmd = $env:YO_PENDING
            Remove-Item Env:\YO_PENDING -ErrorAction SilentlyContinue
            try { [Microsoft.PowerShell.PSConsoleReadLine]::Insert($cmd) } catch {}
        }
    }
}
