# Packaging

Manifests for distributing `yo` via package managers. Windows manifests consume
the **signed per-arch zips** already published on GitHub Releases
(`yo-windows-{amd64,arm64}.zip`) plus `SHA256SUMS.txt`; they do not rebuild
anything. macOS release zips (`yo-macos-{arm64,amd64}.zip`) are Developer
ID-signed and notarized in the release workflow.

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
post-install step. On a new release, bump `version`, the two per-arch URLs, and
their `sha256` (from the release's `SHA256SUMS.txt`) in that repo.

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

## Refreshing hashes manually

```powershell
# real per-asset SHA256 for any tag (version-less asset names -> stable filenames):
(Invoke-WebRequest "https://github.com/martona/yo/releases/download/<tag>/SHA256SUMS.txt" -UseBasicParsing).Content
```
The release's `SHA256SUMS.txt` is the source of truth (it hashes the signed zips).
