# CI release pipeline (signed Android + Linux + macOS)

Automated build lives in [`.github/workflows/release-artifacts.yml`](../.github/workflows/release-artifacts.yml).

The separate [`native-libs.yml`](../.github/workflows/native-libs.yml) workflow builds the same pinned `libghostty-vt` C ABI for Android on Linux and as an iOS device/simulator XCFramework on macOS. It then generates the Expo iOS project, installs the mandatory local pod, and compiles/links an unsigned Apple Silicon simulator build. Physical-device acceptance and signed distribution remain separate release gates.

## What it does

| Step | Always (tag push or dispatch) | Only when `publish=true` |
| --- | --- | --- |
| Checkout selected ref | yes | — |
| `verify-release-identity.sh` | yes | — |
| Linux amd64/arm64 + macOS arm64 daemon archives | yes | — |
| Build release-grade `libghostty` arm64 | yes | — |
| Clean Expo prebuild + signed arm64 APK | yes | — |
| Verify package / version / ABI / Ghostty notice / cert fingerprint | yes | — |
| Stage tree + `SHA256SUMS` + `release-manifest.json` | yes | — |
| Upload workflow artifact | yes | — |
| Upload/replace assets on **existing** GitHub prerelease | **no** | yes |

**Never automatic:** creating tags, converting draft→prerelease, or publishing on ordinary branch pushes. Tag `v*` pushes **build and verify only** (no asset publish).

## Repository secrets (names only)

Configure under GitHub → Settings → Secrets and variables → Actions. Values are **not** documented and must never be committed or printed in logs.

| Secret name | Purpose |
| --- | --- |
| `ZEN_ANDROID_KEYSTORE_BASE64` | Base64 encoding of the PKCS#12 (or JKS) release keystore binary |
| `ZEN_ANDROID_KEYSTORE_PASSWORD` | Keystore password |
| `ZEN_ANDROID_KEY_ALIAS` | Key alias inside the keystore |
| `ZEN_ANDROID_KEY_PASSWORD` | Key password |

Runtime mapping (CI materializes the file, mode `0600`, then shreds it):

| CI env | Source |
| --- | --- |
| `ZEN_ANDROID_KEYSTORE` | Temp path from `scripts/materialize-android-keystore.sh` |
| `ZEN_ANDROID_KEYSTORE_PASSWORD` | secret of same name |
| `ZEN_ANDROID_KEY_ALIAS` | secret of same name |
| `ZEN_ANDROID_KEY_PASSWORD` | secret of same name |

Local maintainer builds continue to use a filesystem path via `ZEN_ANDROID_KEYSTORE` (see [android.md](android.md)); agents must not read `~/.zen/release-keys`.

## Public certificate identity

Release APKs must match the fingerprint published in the current version's tracked release notes (public metadata, not a secret):

```
C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD
```

`scripts/verify-apk-release.sh` enforces this after the build.

## workflow_dispatch inputs

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `ref` | string | empty | Git tag/branch/SHA to build; empty = the branch/tag selected in the Actions UI |
| `publish` | boolean | `false` | If `true`, upload/replace assets on existing prerelease `v<expo.version>` |

Permissions: build job `contents: read`; publish job `contents: write` (only when `publish` is true).

## Example: build and publish an existing prerelease

Prerequisites:

1. The release tag already exists and points at the intended commit.
2. A matching **published prerelease** (not draft) already exists on GitHub.
3. The four `ZEN_ANDROID_*` secrets above are configured on the repository.

Build only (artifact upload; no release mutation):

```bash
gh workflow run release-artifacts.yml \
  --ref main \
  -f ref=vX.Y.Z \
  -f publish=false
```

Build and replace assets on the existing prerelease:

```bash
gh workflow run release-artifacts.yml \
  --ref main \
  -f ref=vX.Y.Z \
  -f publish=true
```

Watch:

```bash
gh run list --workflow=release-artifacts.yml --limit 5
gh run watch
```

## Staged asset names

Under `dist-download/v<version>/` (and the workflow artifact):

- `zen-linux-amd64.tar.gz`
- `zen-linux-arm64.tar.gz`
- `zen-darwin-arm64.tar.gz`
- `zen-android-arm64-v<version>.apk`
- `SHA256SUMS`
- `release-manifest.json`

Daemon archives contain `zen`, `LICENSE`, `NOTICE`, and `TRADEMARKS.md`. Release notes stay in the GitHub Release body; Android third-party notices remain embedded in the APK. This keeps the download list focused without dropping required attribution.

## Local helper scripts

| Script | Role |
| --- | --- |
| `scripts/materialize-android-keystore.sh` | Base64 → temp keystore file (0600); prints path only |
| `scripts/verify-apk-release.sh` | Package/version/ABI/notice/certificate checks |
| `scripts/android-release-apk.sh` | Clean prebuild + signed assembleRelease |
| `scripts/stage-release.sh` | Deterministic stage tree |
| `scripts/verify-release-identity.sh` | Tracked identity + stage layout |

## Safety

- Secrets never appear in docs, commits, or intentional log lines.
- CI does not read developer home directories (including `~/.zen/release-keys`).
- `publish=true` refuses non-prerelease and draft releases; uses `gh release upload --clobber` only.
- Tag pushes do not set `publish`; no unexpected double-publication path from push alone.
- Pinned Zig is downloaded with SHA-256 verification and extracted under `$RUNNER_TEMP/zig-install` (not `/usr/local`); its directory is appended to `$GITHUB_PATH` for later steps. No `sudo` or unverified installers.
