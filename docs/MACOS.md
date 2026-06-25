# macOS zsh / Homebrew bash

`yo` supports macOS with `zsh` and with a modern Homebrew-installed `bash`.
Apple's system `/bin/bash` is 3.2 and is not supported; bash integration needs
bash 4.2+ with Readline.

## Install With Homebrew

```sh
brew install martona/tap/yo
yo --setup
```

`yo --setup` offers to add the shell integration to both supported POSIX shell profiles
(`${ZDOTDIR:-$HOME}/.zshrc` for zsh, `$HOME/.bashrc` for bash), regardless of
which shell launched setup, and can configure an API key. Open a fresh terminal,
then run:

```sh
yo --check
```

It is **strongly** recommended to run `yo` under `tmux` or `zellij` for the valuable context (screen buffer) that they provide. `yo` works without this, but without screen context it's basically blind.

## Manual Release Install

```sh
case "$(uname -m)" in
  arm64) yo_arch=arm64 ;;
  x86_64) yo_arch=amd64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

tmp="$(mktemp -d)"
curl -fsSL -o "$tmp/yo.zip" "https://github.com/martona/yo/releases/latest/download/yo-macos-$yo_arch.zip"
mkdir -p "$HOME/.local/bin"
mkdir -p "$tmp/pkg"
ditto -x -k "$tmp/yo.zip" "$tmp/pkg"
install -m 0755 "$tmp/pkg/yo" "$HOME/.local/bin/yo"
export PATH="$HOME/.local/bin:$PATH"
yo --setup
rm -rf "$tmp"
```

## Manual zsh Setup

Add this to `~/.zshrc`:

```zsh
if command -v yo >/dev/null 2>&1; then eval "$(yo --init zsh)"; fi
```

Set an API key:

```zsh
export ANTHROPIC_API_KEY="sk-ant-..."
# or OPENAI_API_KEY for OpenAI
```

Open a fresh shell and run `yo --check`.

## Manual bash Setup

Install a modern bash if needed:

```sh
brew install bash
```

Add this to `~/.bashrc`:

```bash
if command -v yo >/dev/null 2>&1; then eval "$(yo --init bash)"; fi
```

If your bash starts as a login shell and does not read `~/.bashrc`, source it
from `~/.bash_profile`:

```bash
[[ -r ~/.bashrc ]] && source ~/.bashrc
```

Open a fresh Homebrew bash shell and run `yo --check`.

## Build From Source

With a [Go](https://go.dev/dl/) 1.26+ toolchain:

```sh
go build -o yo ./cmd/yo
./yo --setup
```

For development checks:

```sh
go test ./...
go vet ./...
zsh -n shell/yo.zsh
go run ./cmd/yo --init zsh | zsh -n
bash -n shell/yo.bash
go run ./cmd/yo --init bash | bash -n
```

## Verify A Release Binary

macOS release binaries are Developer ID-signed and notarized:

```sh
codesign -dv --verbose=4 ./yo
spctl --assess --type execute --verbose ./yo
```

You can also verify the release zip with GitHub provenance:

```sh
gh attestation verify yo-macos-arm64.zip --repo martona/yo
```
