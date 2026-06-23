# Releasing

Releases are driven by `.github/workflows/release-manual.yml` or by pushing a
`v*` tag. Both call the reusable `.github/workflows/_release.yml` pipeline.

## Assets

The release pipeline publishes:

- `yo-windows-amd64.zip`
- `yo-windows-arm64.zip`
- `yo-macos-amd64.zip`
- `yo-macos-arm64.zip`
- `yo.cdx.json`
- `SHA256SUMS.txt`

Asset names are version-less so package manifests and docs can link to stable
`releases/latest/download/<asset>` URLs. The version is baked into the binary via
`-ldflags "-X main.version=<tag>"`.

## Signing

Windows binaries are signed with Azure Trusted Signing before packaging. macOS
binaries are signed with a Developer ID Application certificate using hardened
runtime, packaged into zips, and submitted to Apple notarization with
`xcrun notarytool --wait`.

macOS command-line zips are not stapled like `.app`, `.dmg`, or `.pkg` artifacts.
The notarization acceptance is still recorded by Apple for the submitted archive,
and Gatekeeper can validate the signed binary after extraction.

## GitHub Configuration

The `release-signing` environment is shared by Windows and macOS release jobs.

Secrets:

- `AZURE_CLIENT_ID`
- `AZURE_TENANT_ID`
- `AZURE_SUBSCRIPTION_ID`
- `APPLE_CERTIFICATE_P12`
- `APPLE_CERTIFICATE_P12_PASSWORD`
- `APPLE_KEYCHAIN_PASSWORD`
- `APPLE_API_KEY_P8`

Variables:

- `ARTIFACT_SIGNING_ENDPOINT`
- `ARTIFACT_SIGNING_ACCOUNT`
- `ARTIFACT_SIGNING_CERTIFICATE_PROFILE`
- `APPLE_CODESIGN_IDENTITY`
- `APPLE_TEAM_ID`
- `APPLE_API_KEY_ID`
- `APPLE_API_ISSUER_ID`

## Verification

Verify provenance with GitHub CLI:

```sh
gh attestation verify yo-macos-arm64.zip --repo martona/yo
```

Verify macOS signing after extracting a release zip:

```sh
codesign -dv --verbose=4 ./yo
spctl --assess --type execute --verbose ./yo
```
