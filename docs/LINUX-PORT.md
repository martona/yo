# Linux Platform Port Plan

This plan brings `yo` to Linux as a first-class platform. Unlike the macOS/zsh
port, **almost none of the application code is new** — bash integration, the POSIX
setup path, and the cross-platform Go core already exist and ship on macOS. What
Linux lacks is *platform plumbing*: CI, release build legs, native packaging, and
docs. The work is mostly CI/YAML + two cribbed files from `../clipp`, not Go.

The reference for Linux release management is **`../clipp`** (`pizlonator`-style
single static binary, comprehensive deb/rpm/arch release pipeline). We reuse its
packaging layer near-verbatim and discard its build layer (see below).

## Current state (what already exists)

- **Go core is already cross-platform.** Platform files use `//go:build !windows`
  ([`internal/scrollback/console_other.go`](../internal/scrollback/console_other.go),
  `cmd/yo/parent_other.go`), so Linux shares macOS's code path. No Linux-specific
  Go source is required.
- **bash integration is built and tested** — [`shell/yo.bash`](../shell/yo.bash),
  `yo --init bash`, full `--setup`/`--uninstall` wiring in
  [`cmd/yo/setup_posix.go`](../cmd/yo/setup_posix.go), plus
  `bash_integration_test.go` / `setup_bash_test.go`. **But it has only ever run on
  macOS Homebrew bash 4.2+** — never on Linux, and it currently has **zero CI
  coverage** (macOS CI lints only zsh).
- **Scrollback on Linux** is the same as macOS: tmux/zellij work; the console
  fallback returns `""`. Nothing to add.
- **Build is CGO-free.** `_release.yml` already builds Windows and macOS with
  `CGO_ENABLED=0`; nothing in `go.mod` pulls cgo.

### The key simplification vs. clipp

clipp's Linux build job is dominated by **glibc-compatibility machinery** (builds
inside a `debian:11`/glibc-2.31 container, hand-installs CMake, runs vcpkg, static-
links libstdc++/libgcc, still carries a `libavahi-client` runtime dep). All of
that is C++-specific.

A `CGO_ENABLED=0 GOOS=linux` Go binary is **fully static with zero dynamic deps —
not even glibc**. So yo:

- builds on plain `ubuntu-latest` / `ubuntu-24.04-arm` with `setup-go` — no
  container, no CMake, no vcpkg;
- produces nfpm packages with an **empty `depends:`** (clipp needs avahi; yo needs
  nothing), which also makes package install-testing trivial and offline.

## Goals

- `yo <text>` works from interactive Linux bash (and zsh) — reusing the existing
  POSIX adapter unchanged.
- Linux CI: build/vet/test + bash & zsh snippet lint + build both arches.
- **Automated package install-testing** across distro families (the part done by
  hand for clipp).
- Release legs producing `linux/amd64` + `linux/arm64` artifacts: native `.deb`,
  `.rpm`, Arch `.pkg.tar.zst`, plus a loose `.tar.gz`.
- Native packaging via nfpm (one config → three formats), cribbed from clipp.
- Docs: `docs/LINUX.md`, README install rows, packaging notes.
- Fix the Linux `--setup` shell-detection fallback bug.

## Non-Goals

- No PTY-proxy / native non-multiplexer scrollback on Linux (multiplexer-only,
  same as macOS — documented, not a regression).
- No AUR auto-submission in this pass (we ship the `.pkg.tar.zst` as a release
  asset; an AUR PKGBUILD is an optional follow-up).
- No code signing (a static binary needs none; `SHA256SUMS.txt` + build-provenance
  attestation cover integrity).
- No merging of the bash/zsh/PowerShell snippets.

## What we reuse from `../clipp`

| clipp artifact | yo action |
| --- | --- |
| `packaging/nfpm.yaml` | **Crib + simplify.** Drop all `overrides`/`depends`/`recommends` (avahi); `license: GPL-3.0-or-later`; `dst: /usr/bin/yo`; add license docs to `/usr/share/doc/yo/`; drop `version_schema: none` (yo is semver). |
| `scripts/package_linux.sh` | **Crib near-verbatim** (`s/clipp/yo/`). Keeps the fetch-pinned-nfpm-if-absent, envsubst/perl template render, tolerant per-packager loop, local-disk staging. |
| `_release.yml` `build-linux` job | Reuse the **shape** (matrix amd64@`ubuntu-latest` / arm64@`ubuntu-24.04-arm`, package step, version-less staged names, upload-artifact). **Delete** the debian:11 container + CMake + vcpkg + readelf/symbol-split steps — replace with `setup-go` + a 3-line `go build`. |
| `publish` job | Already identical in yo. Just add `build-linux` to `needs:` and the loose-binary handling; checksums/attest/gh-release pick up new assets via the `release-*` pattern. |
| `linux-ci.yml` | Reuse the *idea*; body is yo's `macos-ci.yml` with zsh→bash + the package build + the install-test matrix. |
| `packaging/README.md` | Template for the Linux nfpm section folded into yo's existing packaging README. |

## Phases

### Phase 0 — Pre-flight de-risk (do first, ~30 min)

A throwaway local/CI check before writing YAML, to confirm the assumptions:

- `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/yo` then `file yo` →
  *"statically linked"*; repeat `arm64`.
- `go test ./...` green on Linux (exercises `setup_posix.go`, bash/zsh setup tests
  under a Linux `$HOME`).
- `bash -n shell/yo.bash` and `go run ./cmd/yo --init bash | bash -n` under bash
  5.x.

If all pass (expected), the rest is plumbing.

### Phase 1 — Fix the Linux setup fallback bug

[`cmd/yo/setup.go`](../cmd/yo/setup.go) `detectSetupShell()` falls back to
`"powershell"` whenever `$SHELL`/`YO_SHELL` is unrecognized and `GOOS != "darwin"`
— so on Linux with an unset/odd `$SHELL`, `yo --setup` tries the (absent)
PowerShell host and fails. Add a `runtime.GOOS == "linux"` (or general non-Windows)
fallback to `"bash"`. Add a test case alongside the existing detection tests.

### Phase 2 — Linux CI (`linux-ci.yml`)

Mirror `macos-ci.yml`: checkout, `setup-go`, gofmt/vet/test, then:
- `bash -n shell/yo.bash` + `go run ./cmd/yo --init bash | bash -n` (closes the
  bash no-CI-coverage gap; Ubuntu runners ship bash 5.x — the real ≥4.2 path macOS
  CI structurally can't exercise);
- also lint zsh here (Linux has zsh too) for a second opinion;
- build `linux/amd64` + `linux/arm64` static binaries.

This phase alone is a meaningful win independent of packaging.

### Phase 3 — Packaging layer (nfpm + script)

- Add [`packaging/nfpm.yaml`](../packaging/nfpm.yaml) (adapted; no deps; GPLv3;
  `/usr/bin/yo`; bundle `LICENSE`/`NOTICE`/`THIRD-PARTY-LICENSES.txt` into
  `/usr/share/doc/yo/`).
- Add `scripts/package_linux.sh` (new `scripts/` dir; adapted from clipp). Produces
  `.deb` + `.rpm` + `.pkg.tar.zst` from one built binary.
- Locally runnable for shape-testing a dev build.

### Phase 4 — Automated package install-testing (in `linux-ci.yml`)

After Phase 2's build, run `package_linux.sh`, upload the packages as a CI
artifact, then a **container matrix** installs and smoke-tests each — the thing
that was manual for clipp:

| Container | Format | Install |
| --- | --- | --- |
| `debian:12`, `ubuntu:20.04` | `.deb` | `apt-get install -y ./yo*.deb` |
| `fedora:latest`, `rockylinux:9` | `.rpm` | `dnf install -y ./yo*.rpm` |
| `archlinux:latest` | Arch | `pacman -U --noconfirm ./yo*.pkg.tar.zst` |

Per container smoke (all non-interactive):
- `command -v yo` → `/usr/bin/yo`;
- `yo --version` matches the build version;
- `yo --init bash | bash -n` parses under that distro's bash;
- `yo --check` with a dummy `ANTHROPIC_API_KEY` (offline config validation).

Full matrix on amd64; a lighter smoke (one `.deb` + one `.rpm`) on
`ubuntu-24.04-arm` for arm64 (native, no QEMU; the images are multi-arch).
`ubuntu:20.04` doubles as proof the fully-static binary runs on old userland.

**Un-CI-able (stays a manual checklist):** the interactive prefill, continuation
loop, raw-line capture, and Ctrl-C cancellation — they need a real PTY/line
editor, same limitation as every platform.

### Phase 5 — Release legs (`_release.yml`)

Add a `build-linux` job (`needs: notices`):
- matrix `amd64`@`ubuntu-latest`, `arm64`@`ubuntu-24.04-arm`;
- `setup-go`, `go test ./...`, static `go build` with the release ldflags
  (`-s -w -X main.version=<tag>`, `-trimpath`);
- download the `third-party-licenses` artifact (as win/mac do);
- run `package_linux.sh`;
- stage **version-less** assets for stable `latest/download` URLs:
  `yo-linux-$arch.deb`, `yo-linux-$arch.rpm`, `yo-linux-$arch.pkg.tar.zst`, and a
  `yo-linux-$arch.tar.gz` (binary + `LICENSE`/`NOTICE`/`README`/
  `THIRD-PARTY-LICENSES.txt`, paralleling the win/mac zips for GPLv3 compliance on
  the loose download);
- `upload-artifact` as `release-linux`.

`publish`: add `build-linux` to `needs`. The existing `release-*` download pattern,
`SHA256SUMS.txt`, `attest-build-provenance`, and `gh-release` ingest the new assets
with no further change. **No signing job** — not needed for a static binary.

### Phase 6 — Docs, README, Homebrew

- `docs/LINUX.md` (mirror `docs/MACOS.md`): install via `.deb`/`.rpm`/Arch/`.tar.gz`/
  `go install`/Linuxbrew; bash 4.2+ note; the tmux/zellij "run yo inside a
  multiplexer for screen context" nudge; `yo --setup`/`yo --check`.
- `README.md`: add Linux rows to the release-asset table and a `docs/LINUX.md` link.
- `packaging/README.md`: a Linux nfpm section (deb/rpm/arch from one config).
- `yoconf.example` / `docs/DESIGN-NOTES.md`: bump status to include Linux.
- **Homebrew:** the tap auto-bump (`homebrew-tap.yml`) already fires; Linuxbrew
  works once Linux assets exist **provided the tap formula references the Linux
  URLs** — a tap-repo-side follow-up to verify, not a change in this repo.

### Phase 7 — Manual live verification on Linux

Run the interactive checklist on real Linux bash (and a zsh pass): prefill →
edit → run; chat; multi-step continuation; edited-command reconciliation; raw-line
capture with metacharacters; bare-Enter and Ctrl-C cancel; `yo why did that fail`
inside tmux and zellij; no-multiplexer graceful degrade; `prefill_space true`.

## Suggested order

1. Phase 0 de-risk.
2. Phase 1 setup fallback fix (small, isolated, testable).
3. Phase 2 Linux CI (immediate value: first-ever bash CI coverage).
4. Phase 3 packaging files.
5. Phase 4 install-test matrix (builds on 2+3).
6. Phase 5 release legs.
7. Phase 6 docs.
8. Phase 7 manual interactive verification.

Phases 1–4 are landable and useful before any release leg exists. Phase 5 is the
only one that changes shipped artifacts; everything before it is CI/dev-only.

## Open decisions (recommendations inline)

- **Distro matrix breadth.** Recommend the five above (covers deb/rpm/arch + an
  old-userland deb). openSUSE (`zypper`) is optional — yo's zero-dep package
  sidesteps the SUSE soname issue clipp documents, so it's low-value to add.
- **Loose download format.** Recommend `.tar.gz` (carries license docs, Linux
  convention) over a bare binary. Cheap; matches win/mac zip contents.
- **arm64 test depth.** Recommend lighter smoke on arm64 (build is native, but
  full distro-matrix duplication there adds time for little extra signal).
