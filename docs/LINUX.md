# Linux

`yo` supports Linux with `bash` (4.2+) and `zsh`. The release binary is fully
static (no glibc/musl requirement, no runtime dependencies), so it runs on any
Linux distribution of the right architecture.

## Quick Install

```sh
curl -fsSL https://github.com/martona/yo/releases/latest/download/install.sh | bash
yo --setup
```

The installer detects your architecture and package manager (apt / dnf / zypper /
pacman) and installs the native package; with no supported package manager it drops
the static binary into `~/.local/bin`. Then `yo --setup` wires the integration into
your `~/.bashrc` / `~/.zshrc` and configures an API key. Open a fresh terminal and
run `yo --check`.

It is **strongly** recommended to run `yo` under `tmux` or `zellij` for the valuable
context (screen buffer) that they provide. `yo` works without this, but without
screen context it's basically blind -- it sees only each command's exit code, not its
output.

## Install A Package Manually

Download the package for your distro (all install `yo` to `/usr/bin/yo`), then run the
install command from the directory you downloaded it to, followed by `yo --setup`:

| Distro | Download | Install |
|---|---|---|
| Debian / Ubuntu | [x64](https://github.com/martona/yo/releases/latest/download/yo-linux-amd64.deb) · [arm64](https://github.com/martona/yo/releases/latest/download/yo-linux-arm64.deb) | `sudo apt install ./yo-linux-*.deb` |
| Fedora / RHEL | [x64](https://github.com/martona/yo/releases/latest/download/yo-linux-amd64.rpm) · [arm64](https://github.com/martona/yo/releases/latest/download/yo-linux-arm64.rpm) | `sudo dnf install ./yo-linux-*.rpm` |
| openSUSE | (the `.rpm` above) | `sudo zypper install --allow-unsigned-rpm ./yo-linux-*.rpm` |
| Arch | [x64](https://github.com/martona/yo/releases/latest/download/yo-linux-amd64.pkg.tar.zst) · [arm64](https://github.com/martona/yo/releases/latest/download/yo-linux-arm64.pkg.tar.zst) | `sudo pacman -U ./yo-linux-*.pkg.tar.zst` |

The packages declare no dependencies and are unsigned (integrity is via
`SHA256SUMS.txt` and GitHub attestation, not repo GPG).

## Other Install Methods

Bare static binary:

```sh
case "$(uname -m)" in
  x86_64|amd64)  yo_arch=amd64 ;;
  aarch64|arm64) yo_arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
mkdir -p "$HOME/.local/bin"
curl -fsSL -o "$HOME/.local/bin/yo" "https://github.com/martona/yo/releases/latest/download/yo-linux-$yo_arch"
chmod +x "$HOME/.local/bin/yo"
export PATH="$HOME/.local/bin:$PATH"
yo --setup
```

With a [Go](https://go.dev/dl/) 1.26+ toolchain:

```sh
go install github.com/martona/yo/cmd/yo@latest
yo --setup
```

## Manual bash Setup

Add this to `~/.bashrc`:

```bash
if command -v yo >/dev/null 2>&1; then eval "$(yo --init bash)"; fi
```

bash integration needs bash 4.2+ with Readline (the default on every modern
distribution). Set an API key:

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
# or OPENAI_API_KEY / XAI_API_KEY / GEMINI_API_KEY for the other providers
```

Open a fresh shell and run `yo --check`.

## Manual zsh Setup

Add this to `${ZDOTDIR:-$HOME}/.zshrc`:

```zsh
if command -v yo >/dev/null 2>&1; then eval "$(yo --init zsh)"; fi
```

Set an API key as above, open a fresh shell, and run `yo --check`.

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
bash -n shell/yo.bash
go run ./cmd/yo --init bash | bash -n
```

## Verify A Release Binary

Linux artifacts are not code-signed (Linux has no equivalent of Authenticode /
Developer ID), but every release asset is GitHub build-provenance attested and listed
in `SHA256SUMS.txt`:

```sh
gh attestation verify yo-linux-amd64.deb --repo martona/yo
sha256sum -c SHA256SUMS.txt --ignore-missing
```
