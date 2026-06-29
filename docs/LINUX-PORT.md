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

#### Phase 0 Result (2026-06-28)

Run via `scripts/phase0-linux-check.sh` on an Ubuntu 24.04 VM (Go 1.26.0, bash
5.2). **All 14 checks PASS.** Confirmed: static amd64+arm64 builds (`file` →
"statically linked", `ldd` → "not a dynamic executable"), native run
(`--version`/`--help`/`--init bash`/`--check`), `gofmt`/`vet`/`go test ./...` green,
and bash/zsh snippet parse.

Two issues surfaced and were handled:

- **Real bug, fixed:** `shell/yo.bash`'s non-bash guard used bash's `[[ ... ]]`,
  which dash (Linux `/bin/sh`) cannot evaluate — so the guard failed *open* and
  dash fell through into bash-only syntax and errored. macOS never showed this
  because its `/bin/sh` is bash. Fixed to POSIX `[ -z "${BASH_VERSION:-}" ]`, so a
  non-bash shell returns before parsing anything bash-specific. This fixes
  `TestBashSnippetNonBashQuietlyNoops` on Linux and the real "sourced from a POSIX
  shell" behavior.
- **Environment only (not a code issue):** building over an SMB mount tripped Go's
  VCS stamping (`git status` exit 128, "dubious ownership"). The de-risk script
  passes `-buildvcs=false`; CI uses a clean checkout and stamps normally.

Note: the committed `go.mod` go directive is `go 1.26.0` (three-part canonical
form); a half-completed Go reinstall on the VM transiently rewrote it to `go 1.26`,
since restored. No implication for CI (setup-go reads `go.mod`), release, or
packaging (static binary; end-user Go version irrelevant).

### Phase 1 — Fix the Linux setup fallback bug

[`cmd/yo/setup.go`](../cmd/yo/setup.go) `detectSetupShell()` falls back to
`"powershell"` whenever `$SHELL`/`YO_SHELL` is unrecognized and `GOOS != "darwin"`
— so on Linux with an unset/odd `$SHELL`, `yo --setup` tries the (absent)
PowerShell host and fails. Add a `runtime.GOOS == "linux"` (or general non-Windows)
fallback to `"bash"`. Add a test case alongside the existing detection tests.

#### Phase 1 Result (2026-06-28)

Fixed `setupTargetShell()` in `cmd/yo/setup.go`: the non-Windows fallback (unset/
unrecognized `$SHELL`, no `YO_SHELL`) now returns `"bash"` instead of `"powershell"`.
Windows still returns `"powershell"` from the early guard; darwin still defaults to
`"zsh"`. Added `TestSetupTargetShell` (fallback-per-OS + explicit-hint-wins).
Verified on Windows: build + tests green, gofmt clean. The `bash` fallback branch is
asserted by the test on the (future) Linux CI runner.

### Phase 2 — Linux CI (`linux-ci.yml`)

Mirror `macos-ci.yml`: checkout, `setup-go`, gofmt/vet/test, then:
- `bash -n shell/yo.bash` + `go run ./cmd/yo --init bash | bash -n` (closes the
  bash no-CI-coverage gap; Ubuntu runners ship bash 5.x — the real ≥4.2 path macOS
  CI structurally can't exercise);
- also lint zsh here (Linux has zsh too) for a second opinion;
- build `linux/amd64` + `linux/arm64` static binaries.

This phase alone is a meaningful win independent of packaging.

#### Phase 2 Result (2026-06-28)

Added `.github/workflows/linux-ci.yml` (mirrors `macos-ci.yml`): on
`ubuntu-latest`, runs gofmt/vet/`go test ./...`, a **bash** snippet smoke
(`bash -n shell/yo.bash` + `yo --init bash | bash -n` under bash 5.x — the
first-ever CI coverage of the bash adapter, and the real >= 4.2 path macOS CI
can't exercise), a zsh snippet smoke (zsh is apt-installed first -- it is NOT on the Ubuntu image
by default; yo targets zsh on Linux too), and a
static `linux/amd64`+`arm64` build guarded by a `file ... statically linked`
assertion. Validated locally: YAML parses, bash smoke passes, cross-builds are
statically linked. Real Linux CI signal lands on push.

### Phase 3 — Packaging layer (nfpm + script)

- Add [`packaging/nfpm.yaml`](../packaging/nfpm.yaml) (adapted; no deps; GPLv3;
  `/usr/bin/yo`; bundle `LICENSE`/`NOTICE`/`THIRD-PARTY-LICENSES.txt` into
  `/usr/share/doc/yo/`).
- Add `scripts/package_linux.sh` (new `scripts/` dir; adapted from clipp). Produces
  `.deb` + `.rpm` + `.pkg.tar.zst` from one built binary.
- Locally runnable for shape-testing a dev build.

#### Phase 3 Result (2026-06-28)

Added `packaging/nfpm.yaml` and `scripts/package_linux.sh` (cribbed from clipp).
nfpm config is simplified: **no `depends:`/`overrides:` at all** (static binary),
`license: GPL-3.0-or-later`, binary → `/usr/bin/yo`, and license/provenance docs
globbed from a script-staged dir → `/usr/share/doc/yo/`. The script keeps clipp's
proven machinery (auto-fetch pinned nfpm, envsubst/perl template render, tolerant
per-packager loop, local-disk staging for the SMB-mount hazard) and adds doc
staging (LICENSE/NOTICE/README always; THIRD-PARTY-LICENSES.txt when present) and a
leading-`v` strip on the version. Validated locally: script parses (`bash -n`),
rendered nfpm config is valid YAML mapping binary + docs correctly.

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

#### Phase 4 Result (2026-06-28)

Added `package` + `package-test` jobs to `linux-ci.yml`. `package` builds static
amd64+arm64 binaries, runs `package_linux.sh` for both, and stages canonical
`yo-linux-$arch.{deb,rpm,pkg.tar.zst}` names (the same renaming the release will
use) as an artifact. `package-test` is a 7-entry matrix that, per distro, downloads
the artifact and **`docker run`s the install + smoke** on the host (so the test
container needs only its package manager + bash, not node): `dpkg -i` / `rpm -i` /
`pacman -U`, then `command -v yo`, `yo --version`, `yo --help`, and
`yo --init bash | bash -n`. Coverage: debian:12, ubuntu:20.04, fedora, almalinux:9,
archlinux on amd64; debian:12 + fedora on arm64 (native `ubuntu-24.04-arm`, no
QEMU). Because the package has zero deps, every install is offline — no apt/dnf
repo refresh. **Swapped rockylinux:9 → almalinux:9** (more reliable Docker Hub
library image; both are RHEL-compatible).

Two corrections after the first real run:

- **Dropped the `sed SigLevel=Never` for Arch** — unnecessary: Arch's default
  `LocalFileSigLevel = Optional` already permits unsigned local `pacman -U`
  installs (the stricter `SigLevel` is for repository packages). Arch installs
  unsigned local packages out of the box (same reason makepkg/AUR works).
- **License-doc check moved from install-time to package-build-time.** The first
  run failed the on-disk `/usr/share/doc/yo/LICENSE` check on ubuntu:20.04 and
  archlinux only — because those minimal base images deliberately strip
  `/usr/share/doc/*` at install (Ubuntu via dpkg `path-exclude`, Arch via pacman
  `NoExtract`); debian:12/fedora/almalinux keep docs and passed. Proof it was the
  container, not the package: ubuntu:20.04 and debian:12 install the *identical*
  `.deb`. So the GPLv3 invariant (the **package** ships the license) is now asserted
  container-independently in the `package` job — `dpkg-deb -c` / `rpm -qlp` /
  `tar --zstd -tf` each grepped for `usr/share/doc/yo/LICENSE`, both arches, all
  three formats — and the install smoke no longer checks on-disk docs. Also dropped
  `yo --check` from the smoke (it reports shell-profile wiring, not install health).

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

#### Phase 5 Result (2026-06-28)

Added a single `build-linux` job to `_release.yml` (`needs: notices`) and added it
to `publish`'s `needs`. **Simplification vs. the plan/clipp:** no per-arch matrix
and no native ARM runner — one `ubuntu-latest` job cross-builds amd64+arm64 (static
Go) and cross-packages both (arch-agnostic nfpm). The job: `go test`, static build
with release ldflags (`-s -w -X main.version=<tag>`, `-trimpath`) + a static-link
guard, downloads the `third-party-licenses` artifact, runs `package_linux.sh` per
arch, and stages version-less assets as `release-linux`: the three native packages
`yo-linux-$arch.{deb,rpm,pkg.tar.zst}` plus the **bare binary** `yo-linux-$arch`
(clipp parity). (An initial `.tar.gz` was dropped per author — unused; the license
rides in the release's LICENSE + SHA256SUMS and the native packages.) A release-specific assertion confirms the generated
THIRD-PARTY-LICENSES.txt rode into each deb + tarball (linux-ci already proves
LICENSE/NOTICE on every push). No signing. Validated locally: YAML parses, both
arches build static+stripped, tarball carries all docs, TPL assertion passes; the
nfpm step is proven by the green Phase 4 install-test matrix (same script).

First draft-release dispatch (v0.3.0) exposed one bug — **in the assertion, not the
packaging** (nfpm produced all six packages correctly and bundled the TPL): a
`<lister> | grep -q PATTERN` check SIGPIPEs the lister under the runner's default
`pipefail` (grep -q closes the pipe at first match, the lister fails its next
write), so it falsely reported the tarball "missing THIRD-PARTY-LICENSES.txt". It is
position-dependent, which is why the structurally-identical linux-ci checks happened
to pass. Fixed in both workflows by capturing each listing into a var and matching
with a pipe-free `case "$list" in *PATTERN*)`.

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

#### Interactive finding (2026-06-28): bash adapter rewritten to the zsh model

First real Linux bash test showed `yo <query>` corrupting the prompt line (binary's
transient `thinking...` / chat output overwriting it). **Latent bug, not introduced
by the port.** Root cause: bash has no `print -z`, so the original adapter did the
binary call + prefill *in place* inside the `bind -x` widget (`READLINE_LINE` +
`redraw-current-line`), which fights readline's redisplay whenever anything prints.
(A blind `printf '\n'` patch was tried and reverted — it desynced readline and
*erased* the typed line.)

**Fix: ported the zsh model to bash** (proven first with a throwaway
`scripts/spike-bash-prefill.sh`, validated interactively on the VM):

- The accept-line hook rewrites `yo <q>` → `yo '<q>'` (correct single-quote escaper)
  and **accepts** it, so the typed line is preserved and metacharacters are safe —
  exactly like zsh. The `yo` function then runs as an ordinary command and prints
  output cleanly below.
- The editable command is placed on the next prompt via the terminal's **DSR**
  reply (`ESC[5n` → bound `ESC[0n`), the bash analogue of `print -z`. The kernel's
  echo of the reply (a stray `^[[0n` leak in the canonical-mode gap before readline)
  is suppressed with `stty -echo`, restored in `PROMPT_COMMAND`.
- Continuation is driven by `PROMPT_COMMAND` (bash analogue of zsh `precmd`): a
  seen-prompt step records a history baseline; the next prompt detects whether a
  command ran (history advanced) and forwards `--exit` + the executed command
  (`YO_RAN` via `fc -ln -1`), or cancels (bare Enter / Ctrl-C / cleared line).

Go tests rewritten for the new structure (rewrite/quote, chat vs command+arm,
continuation fires vs cancels); whole suite + `bash -n` + `yo --init bash | bash -n`
green. **Known limitation:** `prefill_space true` is incompatible with the
history-based ran-detection on bash (a leading-space command is dropped by
`HISTCONTROL=ignorespace`, so continuation can't see it ran) — a DEBUG-trap
(preexec) capture would be needed for that combo; deferred. **Status: implemented;
needs a final interactive VM pass (initial/chat/continuation/cancel/metachars).**

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
- **Loose download format.** Bare `yo-linux-$arch` binary only (clipp parity:
  `curl … -o yo && chmod +x`). A `.tar.gz` was tried but dropped as unused — the
  license is conveyed by the release's own LICENSE + SHA256SUMS and the native
  packages, matching clipp's bare-binary approach.
- **arm64 test depth.** Recommend lighter smoke on arm64 (build is native, but
  full distro-matrix duplication there adds time for little extra signal).
