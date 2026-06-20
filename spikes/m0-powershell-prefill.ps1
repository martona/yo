# M0 spike — PowerShell next-prompt prefill
# =========================================
# Question this answers: after a function (standing in for the `yo` binary call)
# has ALREADY returned, can we place text onto the *next* prompt's edit buffer,
# editable, so the user just presses Enter to run it?
#
# yoshell got this for free inside readline (rl_startup_hook). PowerShell gives
# us no clean "set the next line" hook, and [PSConsoleReadLine]::Insert() only
# works while the editor's input loop is live. So we test two mechanisms.
#
# Key idea for the cross-moment handoff: use an ENVIRONMENT VARIABLE as the
# channel. It's process-local (no file, auto-gone when the shell exits) and —
# unlike $global:/$script: vars — it is visible across the runspace boundary
# that OnIdle -Action blocks run in. This is the whole v0.1 "state" question:
# one ephemeral value, carried in $env:, no SQLite, no dotfiles.
#
# HOW TO RUN
#   1. Open a FRESH interactive PowerShell 7 window (not VS Code's integrated
#      terminal for the first try — use a plain pwsh console).
#   2. Dot-source this file:   . .\spikes\m0-powershell-prefill.ps1
#   3. Try Approach A:         yo-spike find every pdf
#         -> EXPECT: at the next prompt, the line is pre-filled with a
#            Get-ChildItem command, cursor at end, fully editable. Press Enter
#            to run, or edit first, or Ctrl-C to discard.
#   4. If A does nothing, try Approach B: press  Ctrl+g  at an empty prompt.
#   5. When done:             yo-spike-cleanup
#
# REPORT BACK
#   - Approach A: did the command appear on the next prompt? editable? did it
#     appear exactly once (not repeatedly while idle)? any errors?
#   - Approach B: did Ctrl+g prefill the line?
#   - Which terminal(s) did you try (Windows Terminal, conhost, VS Code)?

Write-Host "M0 prefill spike loaded. Commands: yo-spike <text> | Ctrl+g | yo-spike-cleanup" -ForegroundColor DarkCyan

# Stand-in for "the binary returned a command". v0.1's real binary will print
# JSON; here we just hardcode a command so we test ONLY the prefill mechanism.
function script:Get-FakeCommand {
    param([string]$Query)
    'Get-ChildItem -Recurse -Filter *.pdf | Where-Object LastWriteTime -gt (Get-Date).AddDays(-7)'
}

# ---------------------------------------------------------------------------
# Approach A — OnIdle event + $env channel  (keeps the `yo <text>`<Enter> UX)
# ---------------------------------------------------------------------------
# The function sets $env:YO_PENDING and returns. On the next idle (i.e. once the
# next prompt is up and waiting), the handler injects it and clears the channel.

function yo-spike {
    param([Parameter(ValueFromRemainingArguments = $true)] $Words)
    $cmd = Get-FakeCommand -Query ($Words -join ' ')
    $env:YO_PENDING = $cmd
    Write-Host "yo-spike: queued for the next prompt -> $cmd" -ForegroundColor DarkGray
}

# Remove any prior registration so the file is safe to re-dot-source.
Get-EventSubscriber -SourceIdentifier 'PowerShell.OnIdle' -ErrorAction SilentlyContinue |
    Where-Object { $_.Action.Name -like '*YoSpike*' -or $_.SourceObject -eq $null } |
    ForEach-Object { Unregister-Event -SubscriptionId $_.SubscriptionId -ErrorAction SilentlyContinue }

$null = Register-EngineEvent -SourceIdentifier 'PowerShell.OnIdle' -Action {
    if ($env:YO_PENDING) {
        $c = $env:YO_PENDING
        Remove-Item Env:\YO_PENDING -ErrorAction SilentlyContinue   # consume once
        try {
            [Microsoft.PowerShell.PSConsoleReadLine]::Insert($c)
        } catch {
            # If this throws, OnIdle's runspace can't reach the live editor and
            # Approach A is out — fall back to Approach B.
            [Console]::Error.WriteLine("yo-spike OnIdle Insert failed: $($_.Exception.Message)")
        }
    }
}

# ---------------------------------------------------------------------------
# Approach B — PSReadLine key handler  (proven; trades the typed-command UX
# for a keypress). This is how Atuin/PSFzf inject text, and runs in-editor so
# Insert() is guaranteed to work.
# ---------------------------------------------------------------------------
Set-PSReadLineKeyHandler -Chord 'Ctrl+g' -BriefDescription 'yo-spike-prefill' -ScriptBlock {
    $cmd = Get-FakeCommand -Query 'demo'
    [Microsoft.PowerShell.PSConsoleReadLine]::Insert($cmd)
}

function yo-spike-cleanup {
    Get-EventSubscriber -SourceIdentifier 'PowerShell.OnIdle' -ErrorAction SilentlyContinue |
        ForEach-Object { Unregister-Event -SubscriptionId $_.SubscriptionId -ErrorAction SilentlyContinue }
    Remove-Item Env:\YO_PENDING -ErrorAction SilentlyContinue
    try { Remove-PSReadLineKeyHandler -Chord 'Ctrl+g' } catch {}
    Write-Host "yo-spike: cleaned up (OnIdle handler, Ctrl+g, env channel)." -ForegroundColor DarkCyan
}
