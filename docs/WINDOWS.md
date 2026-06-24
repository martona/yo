# Windows PowerShell

`yo` supports Windows PowerShell 5.1 and PowerShell 7+. PowerShell 7+ is
recommended.

## Install From Release

Download the right zip:

- [Windows x64](https://github.com/martona/yo/releases/latest/download/yo-windows-amd64.zip)
- [Windows Arm64](https://github.com/martona/yo/releases/latest/download/yo-windows-arm64.zip)

Or from PowerShell:

```powershell
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
$zip = Join-Path $env:TEMP "yo-windows-$arch.zip"
$dir = Join-Path $env:LOCALAPPDATA "yo"

Invoke-WebRequest "https://github.com/martona/yo/releases/latest/download/yo-windows-$arch.zip" -OutFile $zip
New-Item -ItemType Directory -Force $dir | Out-Null
Expand-Archive $zip -DestinationPath $dir -Force
& "$dir\yo.exe" --setup
```

`yo --setup` can add the binary directory to your user `PATH`, upgrade PSReadLine
for your account if needed, add the shell integration to `$PROFILE`, and configure
an API key.

Open a fresh PowerShell window, then run:

```powershell
yo --check
```

## Manual Setup

Add this to `$PROFILE`:

```powershell
if (Get-Command yo -ErrorAction SilentlyContinue) { yo --init powershell | Out-String | iex }
```

Set an API key:

```powershell
[Environment]::SetEnvironmentVariable("ANTHROPIC_API_KEY", "sk-ant-...", "User")
# or OPENAI_API_KEY for OpenAI
```

Open a fresh shell and run `yo --check`.

## Build From Source

With a [Go](https://go.dev/dl/) 1.26+ toolchain:

```powershell
go build -o yo.exe ./cmd/yo
.\yo.exe --setup
```

For development checks:

```powershell
go test ./...
go vet ./...
```

## Verify A Release Binary

Windows release binaries are Authenticode-signed:

```powershell
Get-AuthenticodeSignature .\yo.exe
```

You can also verify the release zip with GitHub provenance:

```powershell
gh attestation verify yo-windows-amd64.zip --repo martona/yo
```
