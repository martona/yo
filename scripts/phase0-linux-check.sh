#!/usr/bin/env bash
# Phase 0 Linux de-risk check (see docs/LINUX-PORT.md).
#
# Run from the repo root on a Linux box (Ubuntu VM):
#     bash scripts/phase0-linux-check.sh
#
# Confirms the assumptions the Linux port rests on, BEFORE any CI/packaging YAML:
#   1. Static cross-compile for amd64 + arm64 (no dynamic deps, not even glibc).
#   2. The host-arch binary actually RUNS natively (--version / --help / --check).
#   3. go test / gofmt / go vet are green on Linux.
#   4. The bash (and zsh) snippets parse under this distro's shells.
#
# Runs every check, reports PASS/FAIL per check, and exits non-zero if any failed.
# Touches nothing in the repo except a throwaway ./phase0-build/ dir (cleaned up).

set -uo pipefail

REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

BUILD_DIR="$REPO_ROOT/phase0-build"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"
trap 'rm -rf "$BUILD_DIR"' EXIT

PASS=0
FAIL=0
declare -a RESULTS

ok()   { echo "  [PASS] $1"; RESULTS+=("PASS  $1"); PASS=$((PASS+1)); }
bad()  { echo "  [FAIL] $1"; RESULTS+=("FAIL  $1"); FAIL=$((FAIL+1)); }
info() { echo "  ..... $1"; }
hdr()  { echo; echo "=== $1 ==="; }

# ---------------------------------------------------------------------------
hdr "Environment"
if ! command -v go >/dev/null 2>&1; then
    echo "  [FATAL] 'go' is not on PATH. Install Go (>= the go.mod version) and retry." >&2
    exit 2
fi
info "go:   $(go version)"
info "bash: $(bash --version | head -n1)"
# Bash >= 4.2 is the supported floor; Ubuntu ships 5.x.
bash_major="${BASH_VERSINFO[0]}"; bash_minor="${BASH_VERSINFO[1]}"
if (( bash_major > 4 || (bash_major == 4 && bash_minor >= 2) )); then
    ok "bash >= 4.2 (${bash_major}.${bash_minor})"
else
    bad "bash >= 4.2 required (found ${bash_major}.${bash_minor})"
fi
HOST_ARCH="$(go env GOARCH)"
info "host GOARCH: $HOST_ARCH"

# ---------------------------------------------------------------------------
hdr "1. Static cross-compile (amd64 + arm64)"
for arch in amd64 arm64; do
    out="$BUILD_DIR/yo-linux-$arch"
    # -buildvcs=false: this script is often run over an SMB/cross-owned mount where
    # Go's automatic VCS stamping (`git status`) fails with exit 128 ("dubious
    # ownership"). Version stamping is irrelevant to a de-risk build; CI uses a clean
    # checkout and stamps normally.
    if CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -buildvcs=false -trimpath -ldflags "-s -w" -o "$out" ./cmd/yo; then
        if command -v file >/dev/null 2>&1; then
            desc="$(file -b "$out")"
            info "$arch: $desc"
            if echo "$desc" | grep -q "statically linked"; then
                ok "$arch builds and is statically linked"
            else
                bad "$arch built but 'file' does not report 'statically linked'"
            fi
        else
            ok "$arch builds (no 'file' tool; static-link verified separately via ldd / from Windows)"
        fi
    else
        bad "$arch build failed"
    fi
done
# ldd should refuse a static binary ("not a dynamic executable").
host_bin="$BUILD_DIR/yo-linux-$HOST_ARCH"
if [[ -x "$host_bin" ]]; then
    ldd_out="$(ldd "$host_bin" 2>&1 || true)"
    info "ldd: $ldd_out"
    if echo "$ldd_out" | grep -qiE "not a dynamic executable|statically linked"; then
        ok "ldd confirms no dynamic dependencies"
    else
        bad "ldd shows dynamic deps (expected a fully static binary)"
    fi
fi

# ---------------------------------------------------------------------------
hdr "2. Native run of the host-arch binary"
if [[ -x "$host_bin" ]]; then
    if ver="$("$host_bin" --version 2>&1)"; then
        ok "yo --version runs natively ($ver)"
    else
        bad "yo --version failed to run: $ver"
    fi
    if "$host_bin" --help >/dev/null 2>&1; then
        ok "yo --help exits 0"
    else
        bad "yo --help did not exit 0"
    fi
    if snip="$("$host_bin" --init bash 2>&1)" && [[ -n "$snip" ]] && printf '%s\n' "$snip" | bash -n; then
        ok "yo --init bash emits a parseable snippet"
    else
        bad "yo --init bash output is empty or does not parse under bash -n"
    fi
    # --check validates config offline; give it a dummy key so config is 'ready'.
    if ANTHROPIC_API_KEY="sk-ant-phase0-dummy" "$host_bin" --check >/dev/null 2>&1; then
        ok "yo --check passes offline with a dummy key"
    else
        info "yo --check did not exit 0 with a dummy key (inspect manually; not necessarily fatal):"
        ANTHROPIC_API_KEY="sk-ant-phase0-dummy" "$host_bin" --check 2>&1 | sed 's/^/        /' || true
        bad "yo --check (dummy key, offline) — review output above"
    fi
else
    bad "no host-arch binary to run"
fi

# ---------------------------------------------------------------------------
hdr "3. gofmt / go vet / go test on Linux"
bad_fmt="$(gofmt -l . 2>/dev/null)"
if [[ -z "$bad_fmt" ]]; then
    ok "gofmt clean"
else
    bad "gofmt needs running on: $bad_fmt"
fi
if go vet ./... 2>&1 | sed 's/^/        /'; then
    ok "go vet clean"
else
    bad "go vet reported problems"
fi
if go test ./... 2>&1 | sed 's/^/        /'; then
    ok "go test ./... green"
else
    bad "go test ./... failed"
fi

# ---------------------------------------------------------------------------
hdr "4. Shell snippet parse"
if bash -n shell/yo.bash 2>&1 | sed 's/^/        /'; then
    ok "bash -n shell/yo.bash"
else
    bad "shell/yo.bash failed bash -n"
fi
if command -v zsh >/dev/null 2>&1; then
    if zsh -n shell/yo.zsh 2>&1 | sed 's/^/        /'; then
        ok "zsh -n shell/yo.zsh"
    else
        bad "shell/yo.zsh failed zsh -n"
    fi
    if "$host_bin" --init zsh 2>/dev/null | zsh -n; then
        ok "yo --init zsh parses under zsh -n"
    else
        bad "yo --init zsh did not parse under zsh -n"
    fi
else
    info "zsh not installed; skipping zsh snippet checks"
fi

# ---------------------------------------------------------------------------
hdr "Summary"
for r in "${RESULTS[@]}"; do echo "  $r"; done
echo
echo "  PASS: $PASS    FAIL: $FAIL"
if (( FAIL > 0 )); then
    echo "  >>> Phase 0 found problems. Fix before proceeding to Phase 1+."
    exit 1
fi
echo "  >>> Phase 0 clean. Assumptions hold; safe to proceed with the port."
