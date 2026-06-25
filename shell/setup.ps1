# SPDX-License-Identifier: GPL-3.0-or-later
# PowerShell-specific setup helper, invoked by the Go `yo --setup` runner. Reads
# the binary path from $env:YO_SETUP_BIN; $env:YO_SETUP_UNINSTALL=1 removes the
# profile wiring. Common setup concerns (provider/key prompts and `yo --check`)
# live in Go; this script only handles PowerShell-native pieces: user PATH,
# PSReadLine, and $PROFILE.

$bin       = $env:YO_SETUP_BIN
$uninstall = $env:YO_SETUP_UNINSTALL -eq '1'
$initLine  = 'if (Get-Command yo -ErrorAction SilentlyContinue) { yo --init powershell | Out-String | iex }'
$marker    = 'yo --init powershell'
$managedStart = '# >>> yo initialize >>>'
$managedEnd = '# <<< yo initialize <<<'
$managedComment = '# Added by `yo --setup`; remove with `yo --uninstall`.'
$legacyComment = '# yo - LLM command assistant'

function Step($m) { Write-Host ""; Write-Host "==> $m" -ForegroundColor Cyan }
function Good($m) { Write-Host "    OK  $m" -ForegroundColor Green }
function Warn($m) { Write-Host "    !   $m" -ForegroundColor Yellow }
function Info($m) { Write-Host "    $m" }

# Confirm asks a yes/no question, defaulting to Yes (Enter accepts). It only gates
# the step it guards: a "no" skips that change and setup continues to the next step.
function Confirm($prompt) {
    $ans = (Read-Host "    $prompt [Y/n]").Trim().ToLower()
    return ($ans -eq '' -or $ans -eq 'y' -or $ans -eq 'yes')
}

function ManagedBlock() {
    return ($managedStart, $managedComment, $initLine, $managedEnd) -join [Environment]::NewLine
}

function RemoveYoInit($content) {
    $content = $content -replace "`r`n", "`n"
    $lines = $content -split "`n", -1
    $out = New-Object System.Collections.Generic.List[string]
    $inBlock = $false
    for ($i = 0; $i -lt $lines.Count; $i++) {
        $trimmed = $lines[$i].Trim()
        if ($inBlock) {
            if ($trimmed -eq $managedEnd) { $inBlock = $false }
            continue
        }
        if ($trimmed -eq $managedStart) {
            $inBlock = $true
            continue
        }
        if ($trimmed -eq $legacyComment -and $i + 1 -lt $lines.Count -and $lines[$i + 1].Trim() -eq $initLine) {
            continue
        }
        if ($trimmed -eq $initLine) {
            continue
        }
        $out.Add($lines[$i])
    }
    return ($out -join [Environment]::NewLine)
}

if ($uninstall) {
    Step "Removing yo from your PowerShell profile"
    $raw = Get-Content $PROFILE -Raw -ErrorAction SilentlyContinue
    if ($raw -and ($raw -match [regex]::Escape($marker) -or $raw -match [regex]::Escape($managedStart))) {
        Info "I can remove the yo integration block from:"
        Info "    $PROFILE"
        if (Confirm "Remove it?") {
            $kept = RemoveYoInit $raw
            [System.IO.File]::WriteAllText($PROFILE, $kept)
            Good "removed the integration block from $PROFILE"
        } else {
            Warn "skipped -- left $PROFILE unchanged"
        }
    } else {
        Good "nothing to remove"
    }
    Write-Host ""
    Write-Host "Done. Your ~/.yoconf and the yo binary are untouched."
    return
}

# 1. binary on PATH
Step "Checking the yo binary is on PATH"
if (Get-Command yo -ErrorAction SilentlyContinue) {
    Good "yo resolves on PATH"
} elseif ($bin -and (Test-Path $bin)) {
    $dir = Split-Path $bin
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $userPath) { $userPath = '' }
    if (($userPath -split ';') -notcontains $dir) {
        Info "yo is not on PATH. I can add this folder to your user PATH:"
        Info "    $dir"
        if (Confirm "Add it?") {
            [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ';' + $dir), 'User')
            Good "added $dir to your user PATH (restart your shell to pick it up)"
        } else {
            Warn "skipped -- add $dir to PATH yourself, or invoke yo by full path"
        }
    } else {
        Good "$dir already in your user PATH (restart your shell to pick it up)"
    }
} else {
    Warn "yo is not on PATH and I could not find the binary; add it to PATH yourself"
}

# 2. PowerShell + PSReadLine
Step "Checking PowerShell and PSReadLine"
if ($PSVersionTable.PSVersion.Major -lt 7) {
    Good "Windows PowerShell $($PSVersionTable.PSVersion) -- supported; prefill needs PSReadLine 2.1+"
} else {
    Good "PowerShell $($PSVersionTable.PSVersion)"
}
$psrl = (Get-Module PSReadLine -ListAvailable | Sort-Object Version -Descending | Select-Object -First 1).Version
if ($psrl -and ($psrl -ge [version]'2.1')) {
    Good "PSReadLine $psrl"
} else {
    Info "PSReadLine $psrl looks old; 2.1+ is needed for a clean prefill."
    Info "I can upgrade it now (CurrentUser scope, no admin needed)."
    if (Confirm "Upgrade PSReadLine?") {
        try {
            # Windows PowerShell 5.1 defaults to old TLS for the gallery; force 1.2.
            [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
            Install-Module PSReadLine -Force -SkipPublisherCheck -Scope CurrentUser -ErrorAction Stop
            Good "PSReadLine upgraded -- restart your shell"
        } catch {
            Warn "auto-upgrade failed: $($_.Exception.Message)"
            Warn "run it yourself (an elevated shell may be needed):"
            Warn "    Install-Module PSReadLine -Force -SkipPublisherCheck"
        }
    } else {
        Warn "skipped -- prefill may render doubled on PSReadLine 2.0 until you upgrade"
    }
}

# 3. execution policy. Windows PowerShell defaults to Restricted, which blocks the
#    profile from running AND PSReadLine from loading -- the real failure on 5.x.
#    NB: setup runs under -ExecutionPolicy Bypass, so the *effective* policy here is
#    Bypass and useless; check the scopes a normal interactive session will use.
Step "Checking the PowerShell execution policy"
$cuPol  = Get-ExecutionPolicy -Scope CurrentUser
$lmPol  = Get-ExecutionPolicy -Scope LocalMachine
$future = if ($cuPol -ne 'Undefined') { $cuPol } else { $lmPol }
if ($future -eq 'Undefined' -or $future -in @('Restricted', 'AllSigned')) {
    Info "Execution policy is '$future' -- that blocks your PowerShell profile from"
    Info "running and PSReadLine from loading. RemoteSigned (CurrentUser, no admin) fixes it."
    if (Confirm "Set it to RemoteSigned?") {
        try {
            Set-ExecutionPolicy RemoteSigned -Scope CurrentUser -Force -ErrorAction Stop
            Good "execution policy set to RemoteSigned for your user"
        } catch {
            Warn "couldn't set it: $($_.Exception.Message)"
            Warn "run it yourself:  Set-ExecutionPolicy RemoteSigned -Scope CurrentUser"
        }
    } else {
        Warn "skipped -- your profile line and PSReadLine may not load until you set it"
    }
} else {
    Good "execution policy '$future' allows scripts"
}

# 4. profile wiring
Step "Wiring the integration into your profile"
if ((Get-Content $PROFILE -Raw -ErrorAction SilentlyContinue) -match [regex]::Escape($marker)) {
    Good "already wired in $PROFILE"
} else {
    Info "I can add the yo init line to your profile:"
    Info "    $PROFILE"
    if (-not (Test-Path $PROFILE)) { Info "    (will be created -- it does not exist yet)" }
    if (Confirm "Add it?") {
        if (-not (Test-Path $PROFILE)) {
            $pdir = Split-Path $PROFILE
            if ($pdir -and -not (Test-Path $pdir)) { New-Item -ItemType Directory -Path $pdir -Force | Out-Null }
            New-Item -ItemType File -Path $PROFILE | Out-Null
        }
        $raw = Get-Content $PROFILE -Raw -ErrorAction SilentlyContinue
        $sep = if ($raw -and -not $raw.EndsWith("`n")) { "`n`n" } elseif ($raw) { "`n" } else { "" }
        Add-Content -Path $PROFILE -Value ($sep + (ManagedBlock))
        Good "added the integration block to $PROFILE"
    } else {
        Warn "skipped -- add 'yo --init powershell | Out-String | iex' to your profile yourself"
    }
}
