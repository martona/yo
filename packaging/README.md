# Packaging

Manifests for distributing `yo` via package managers. Windows manifests consume
the **signed per-arch zips** already published on GitHub Releases
(`yo-windows-{amd64,arm64}.zip`) plus `SHA256SUMS.txt`; they do not rebuild
anything. macOS release zips (`yo-macos-{arm64,amd64}.zip`) are Developer
ID-signed and notarized in the release workflow. Linux native packages
(`.deb`/`.rpm`/Arch) are built here by [`nfpm.yaml`](nfpm.yaml) in the release
workflow.

---

## winget — [`winget/`](winget/)

Three manifests (`version`, `installer`, `locale`) for `martona.yo`, modeling the zip
as a **nested portable** exe. Two-step:

**1. First submission (creates the package).** Submit the seed manifests once, via
either:
- `wingetcreate submit --token <PAT> packaging/winget/`, or
- a manual PR placing them at `manifests/m/martona/yo/0.2.0/` in
  [`microsoft/winget-pkgs`](https://github.com/microsoft/winget-pkgs).

Automated validation runs (schema, hash match, URL reachability, a sandbox install
test, SmartScreen) and a moderator merges. The binaries being Authenticode-signed is
what clears the reputation checks — no notability judgment involved.

**2. Ongoing (auto-bump).** [`.github/workflows/winget.yml`](../.github/workflows/winget.yml)
opens the version-bump PR on each published release. To enable it:
- Add a repo secret **`WINGET_TOKEN`**: a classic PAT with `public_repo` scope whose
  account has a fork of `microsoft/winget-pkgs`.
- Until that secret exists the workflow no-ops cleanly (it won't fail your releases).
- **Verify** the `vedantmgoyal9/winget-releaser` action's current owner/version/inputs
  against its README before relying on it — it has been renamed/transferred over time.

The action uses `wingetcreate update`, which preserves the portable/zip structure from
the seed and only swaps version + URLs + hashes — so the seed in step 1 defines the
shape once.

**Uninstall.** A portable/zip package has no uninstall hook: `winget uninstall
martona.yo` removes the `yo` command alias but leaves yo's token-usage file at
`%AppData%\yo\usage.json`. Running a cleanup command on uninstall would require
repackaging as a full installer (Inno/NSIS/MSI) -- deliberately avoided (single
portable binary; MSIX was skipped for the same reason). To clear yo's state, run
`yo --uninstall` before `winget uninstall`. The leftover is a few hundred bytes of
integer counters -- harmless if left.

---

## macOS / Homebrew

`yo` ships as a Homebrew **formula** in the separate tap repo
[`martona/homebrew-tap`](https://github.com/martona/homebrew-tap) (`Formula/yo.rb`) --
`brew install martona/tap/yo`. The formula installs the signed + notarized macOS
release binary (`yo-macos-{arm64,amd64}.zip`) and prints `yo --setup` as the
post-install step.

**Ongoing (auto-bump).** [`.github/workflows/homebrew-tap.yml`](../.github/workflows/homebrew-tap.yml)
dispatches the tap's reusable bump workflow when a release is published. To
enable it, add a fine-grained GitHub token as the `HOMEBREW_TAP_TOKEN` secret in
this repo. Scope the token to `martona/homebrew-tap` with **Contents: Read and
write**. Until that secret exists the workflow no-ops cleanly (it won't fail your
releases). The tap-side workflow reads the release's `SHA256SUMS.txt`, updates
`Formula/yo.rb`, audits it, and commits the bump to the tap.

**Uninstall.** Like winget's portable package, a Homebrew *formula* has no
uninstall hook -- `brew uninstall yo` removes the binary but leaves yo's
token-usage file at `~/Library/Application Support/yo/`. (A *cask* could clear it
via a `zap` stanza, but this tap is formula-shaped and converting just for that
isn't worth it.) To clear yo's state, run `yo --uninstall` before
`brew uninstall yo`; the leftover is a few hundred bytes of integer counters --
harmless if left.

The zip archives are submitted to Apple's notary service after the binary inside
is Developer ID-signed with hardened runtime. Command-line zip archives are not
stapled like `.app` bundles or `.pkg` installers, but the notary acceptance is
bound to the submitted archive and Gatekeeper can validate the signed binary.

---

## Linux

`yo` ships native Linux packages built in the release workflow by **nfpm**
([`nfpm.yaml`](nfpm.yaml), driven by
[`scripts/package_linux.sh`](../scripts/package_linux.sh)): one static binary in,
three packages out -- `yo-linux-<arch>.{deb,rpm,pkg.tar.zst}` for `amd64` and `arm64`.
Because yo is built `CGO_ENABLED=0`, the binary is fully static and the packages
declare **no dependencies**. They are unsigned (integrity is via `SHA256SUMS.txt` +
GitHub attestation, not repo GPG) -- Linux has no Authenticode/Developer ID
equivalent. The release also publishes the bare static binary (`yo-linux-<arch>`) and
a one-line installer ([`scripts/install.sh`](../scripts/install.sh)) at the stable
`releases/latest/download/install.sh` URL.

There is no hosted apt/dnf repo. Users install via the `curl ... | bash` installer
(it picks apt/dnf/zypper/pacman, or drops the binary), a downloaded package,
`go install`, or Homebrew on Linux. The install-test matrix in
[`linux-ci.yml`](../.github/workflows/linux-ci.yml) verifies each package installs and
runs in a clean distro container on every push.

**Homebrew on Linux** uses the same tap formula (`brew install martona/tap/yo`); it
works once the formula references the Linux release URLs -- a tap-repo change, not
wired here yet.

**Possible future:** an AUR `PKGBUILD` and/or a hosted repo. The `.pkg.tar.zst` is
already a release asset, so an AUR `bin` package is mostly a `PKGBUILD` away.

---

## Refreshing hashes manually

```powershell
# real per-asset SHA256 for any tag (version-less asset names -> stable filenames):
(Invoke-WebRequest "https://github.com/martona/yo/releases/download/<tag>/SHA256SUMS.txt" -UseBasicParsing).Content
```
The release's `SHA256SUMS.txt` is the source of truth (it hashes the signed zips).
