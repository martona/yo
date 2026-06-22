# SPDX-License-Identifier: GPL-3.0-or-later
# yo setup - invoked by `yo --setup` (the binary runs this under pwsh). Reads the
# binary path from $env:YO_SETUP_BIN; $env:YO_SETUP_UNINSTALL=1 removes the wiring.
# Idempotent, and confirm-before-change: every step that would modify something
# (user PATH, PSReadLine, $PROFILE, an env var) is previewed and asked about first.
# Declining a step never aborts setup -- it just skips that one and moves on.

$bin       = $env:YO_SETUP_BIN
$uninstall = $env:YO_SETUP_UNINSTALL -eq '1'
$initLine  = 'if (Get-Command yo -ErrorAction SilentlyContinue) { yo --init powershell | Out-String | iex }'
$marker    = 'yo --init powershell'

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

# Read-Secret reads a hidden value: -MaskInput on PowerShell 7.1+, else a
# SecureString (Windows PowerShell 5.1 / 7.0) converted back to plain text.
function Read-Secret($prompt) {
    if ((Get-Command Read-Host).Parameters.ContainsKey('MaskInput')) {
        return Read-Host $prompt -MaskInput
    }
    $sec = Read-Host $prompt -AsSecureString
    if (-not $sec -or $sec.Length -eq 0) { return '' }
    $b = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($sec)
    try { return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($b) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($b) }
}

if ($uninstall) {
    Step "Removing yo from your PowerShell profile"
    if ((Test-Path $PROFILE) -and ((Get-Content $PROFILE -Raw) -match [regex]::Escape($marker))) {
        Info "I can remove the yo integration line from:"
        Info "    $PROFILE"
        if (Confirm "Remove it?") {
            $kept = Get-Content $PROFILE | Where-Object {
                $_ -notmatch [regex]::Escape($marker) -and $_ -notmatch '^# yo - LLM command assistant$'
            }
            Set-Content -Path $PROFILE -Value $kept
            Good "removed the integration line from $PROFILE"
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

Write-Host ""
Write-Host "yo setup -- I'll ask before each change. Press Enter to accept, 'n' to skip." -ForegroundColor Cyan
Write-Host "Skipping a step is fine; setup keeps going." -ForegroundColor DarkGray

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

# 3. profile wiring
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
        Add-Content -Path $PROFILE -Value "`n# yo - LLM command assistant`n$initLine"
        Good "added the integration line to $PROFILE"
    } else {
        Warn "skipped -- add 'yo --init powershell | Out-String | iex' to your profile yourself"
    }
}

# 4. API key
Step "Checking for an API key"
if ($env:ANTHROPIC_API_KEY -or $env:OPENAI_API_KEY) {
    Good "an API key is set in your environment"
} else {
    Warn "no ANTHROPIC_API_KEY or OPENAI_API_KEY found"
    Info "Which provider do you want to configure?"
    Info "    1) Anthropic (Claude)"
    Info "    2) OpenAI (GPT)"
    $choice = (Read-Host "    Choose [1/2] (Enter to skip)").Trim().ToLower()
    $prov = switch ($choice) {
        '1'         { 'anthropic' }
        'anthropic' { 'anthropic' }
        '2'         { 'openai' }
        'openai'    { 'openai' }
        default     { '' }
    }
    if ($prov) {
        $envVar = if ($prov -eq 'openai') { 'OPENAI_API_KEY' } else { 'ANTHROPIC_API_KEY' }
        $key = Read-Secret "    Paste your $prov API key (Enter to skip)"
        if ($key) {
            [Environment]::SetEnvironmentVariable($envVar, $key, 'User')
            Set-Item "env:$envVar" $key
            Good "$envVar set (user scope; available in new shells)"
        } else {
            Warn "skipped -- set $envVar yourself when ready"
        }
    } else {
        Warn "skipped -- set ANTHROPIC_API_KEY or OPENAI_API_KEY when ready"
    }
}

# 5. validate
Step "Validating configuration"
if ($bin -and (Test-Path $bin)) { & $bin --check } else { & yo --check }

Write-Host ""
Write-Host "Setup complete. Open a new shell (or run the init line now), then try:  yo list files over 100mb" -ForegroundColor Cyan
