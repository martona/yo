#!/usr/bin/env bash
#
# yo installer for Linux
# ----------------------
# Usage:   curl -fsSL https://github.com/martona/yo/releases/latest/download/install.sh | bash
# Source:  https://github.com/martona/yo
#
# Installs yo, the LLM command assistant for your shell. If your distro uses a
# supported package manager (apt / dnf / zypper / pacman) it installs the native
# package; otherwise it drops the static binary into ~/.local/bin. yo is a fully
# static binary with no runtime dependencies (no glibc/musl requirement), so it runs
# anywhere. After installing, run `yo --setup` to wire the shell integration into your
# bash/zsh profile and set your API key.

set -eu

BASE="https://github.com/martona/yo/releases/latest/download"

# ---- pretty, noisy output ---------------------------------------------------
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  esc=$(printf '\033')
  C_CYAN="${esc}[36m"; C_GREEN="${esc}[32m"; C_RED="${esc}[31m"; C_DIM="${esc}[90m"; C_RESET="${esc}[0m"
else
  C_CYAN=; C_GREEN=; C_RED=; C_DIM=; C_RESET=
fi
step() { printf '%s==>%s %s\n' "$C_CYAN"  "$C_RESET" "$1"; }
ok()   { printf '%s==>%s %s\n' "$C_GREEN" "$C_RESET" "$1"; }
note() { printf '    %s%s%s\n' "$C_DIM"   "$1" "$C_RESET"; }
die()  {
  printf '%sx%s   %s\n' "$C_RED" "$C_RESET" "$1" >&2
  printf '    %sManual install: https://github.com/martona/yo/blob/master/docs/LINUX.md%s\n' "$C_DIM" "$C_RESET" >&2
  exit 1
}

printf '\n  %syo%s - an LLM command assistant for your shell\n' "$C_CYAN" "$C_RESET"
printf '  %shttps://github.com/martona/yo%s\n\n' "$C_DIM" "$C_RESET"

# ---- temp workspace + cleanup ----------------------------------------------
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# ---- run privileged commands whether or not we're already root -------------
if [ "$(id -u)" -eq 0 ]; then SUDO=""; else SUDO="sudo"; fi

# ---- download helper (curl or wget) ----------------------------------------
download() { # url dest
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$2" "$1"
  else
    die "Need curl or wget to download files."
  fi
}

# ---- 1. architecture (bail if unsupported) ---------------------------------
step "Detecting your architecture..."
machine="$(uname -m)"
case "$machine" in
  x86_64|amd64)  arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "Unsupported architecture '$machine'. yo ships for x86_64 and aarch64." ;;
esac
note "$machine -> $arch"

# ---- 2. pick a supported package manager -----------------------------------
# (No C-library check: yo is statically linked with no runtime deps, so it runs on
# any Linux of the right architecture -- glibc, musl/Alpine, old or new.)
step "Looking for a supported package manager..."
pm=""; file=""
if   command -v apt-get >/dev/null 2>&1; then pm=apt;    file="yo-linux-$arch.deb"
elif command -v dnf     >/dev/null 2>&1; then pm=dnf;    file="yo-linux-$arch.rpm"
elif command -v zypper  >/dev/null 2>&1; then pm=zypper; file="yo-linux-$arch.rpm"
elif command -v pacman  >/dev/null 2>&1; then pm=pacman; file="yo-linux-$arch.pkg.tar.zst"
fi

if [ -n "$pm" ]; then
  # ---- 3a. install the native package -------------------------------------
  note "found: $pm"
  pkg="$TMP/$file"
  step "Downloading $file ..."
  note "$BASE/$file"
  download "$BASE/$file" "$pkg"
  note "$(du -h "$pkg" | cut -f1) downloaded"

  step "Installing with $pm..."
  # The packages are unsigned by design (integrity is via Sigstore attestation +
  # SHA256SUMS, not repo GPG). Installing a *local* file needs no special flags: dnf
  # defaults to localpkg_gpgcheck=0, pacman to LocalFileSigLevel=Optional, and apt
  # doesn't GPG-check local .debs. zypper is the exception -- in --non-interactive mode
  # it defaults the "install unsigned?" prompt to no, so it needs --allow-unsigned-rpm.
  case "$pm" in
    apt)
      export DEBIAN_FRONTEND=noninteractive
      # Make the temp dir + package world-readable so apt's unprivileged _apt user can
      # read them (otherwise apt prints a scary but harmless "couldn't be accessed"
      # note and falls back to running the fetch as root anyway).
      chmod a+rx "$TMP" 2>/dev/null || true
      chmod a+r "$pkg" 2>/dev/null || true
      $SUDO apt-get install -y "$pkg"
      ;;
    dnf)
      $SUDO dnf install -y "$pkg"
      ;;
    zypper)
      $SUDO zypper --non-interactive install --allow-unsigned-rpm "$pkg"
      ;;
    pacman)
      $SUDO pacman -U --noconfirm "$pkg"
      ;;
  esac

  printf '\n'
  if command -v yo >/dev/null 2>&1; then
    ok "yo installed: $(command -v yo)"
  else
    ok "yo installed."
  fi
else
  # ---- 3b. no supported PM: drop the static binary in ~/.local/bin --------
  note "none found (apt / dnf / zypper / pacman)"
  bin="yo-linux-$arch"
  dest="$HOME/.local/bin"
  step "Downloading the static binary ($arch)..."
  note "$BASE/$bin"
  mkdir -p "$dest"
  download "$BASE/$bin" "$dest/yo"
  chmod +x "$dest/yo"
  [ -n "${SUDO_USER:-}" ] && chown "$SUDO_USER" "$dest/yo" 2>/dev/null || true

  printf '\n'
  ok "Saved $dest/yo"
  case ":$PATH:" in
    *":$dest:"*) ;;
    *) note "$dest is not on your PATH yet; 'yo --setup' (below) can add it." ;;
  esac
fi

# ---- next steps -------------------------------------------------------------
printf '\n'
note "Next, wire yo into your shell and set your API key:"
note "    yo --setup"
note "(or set ANTHROPIC_API_KEY / OPENAI_API_KEY yourself, then add the snippet:"
note "     echo 'command -v yo >/dev/null && eval \"\$(yo --init bash)\"' >> ~/.bashrc)"
printf '\n'
