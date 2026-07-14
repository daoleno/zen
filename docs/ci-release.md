# CI release pipeline (signed Android + daemons)

Automated build lives in [`.github/workflows/release-artifacts.yml`](../.github/workflows/release-artifacts.yml).

The separate [`native-libs.yml`](../.github/workflows/native-libs.yml) workflow builds the same pinned `libghostty-vt` C ABI for Android on Linux and as an iOS device/simulator XCFramework on macOS. Normal CI compiles and links an unsigned iOS Simulator app on macOS. Signed iOS archive/export and optional TestFlight upload are isolated in [`ios-release.yml`](../.github/workflows/ios-release.yml); see [iOS CI and release automation](ios-ci-release.md).

## What it does

| Step | Beta tag push or manual rebuild | Publishing tag push / reviewed recovery only |
| --- | --- | --- |
| Checkout selected ref | yes | — |
| `verify-release-identity.sh` | yes | — |
| Linux amd64/arm64 + macOS arm64 daemon archives | yes | — |
| Build release-grade `libghostty` arm64 | yes | — |
| Clean Expo prebuild + signed arm64 APK | yes | — |
| Verify package / version / ABI / Ghostty notice / cert fingerprint | yes | — |
| Stage tree + checksums + signed updater manifest | yes | — |
| Upload workflow artifact | yes | — |
| Upload/replace assets on **existing** GitHub prerelease | **no** | yes |

Pushing one reviewed annotated `vX.Y.Z-beta.N` tag is the canonical build-and-publish event. The workflow checks out that immutable tag, proves strict syntax, version equality, annotated-tag identity, exact HEAD resolution, and ancestry on `origin/main`, then runs the tracked release-identity verifier. Daemon builds and Android/native work run in parallel. A final job combines those verified outputs, creates deterministic archives and checksums, signs and verifies the updater manifest, and uploads one complete workflow artifact. Only then does the write-scoped job create or reuse the matching draft prerelease, upload and verify the exact non-empty asset set, and publish it. A failed build cannot create a public release; a failed first upload can leave only a draft.

## Repository secrets (names only)

Configure under GitHub → Settings → Secrets and variables → Actions. Values are **not** documented and must never be committed or printed in logs.

| Secret name | Purpose |
| --- | --- |
| `ZEN_ANDROID_KEYSTORE_BASE64` | Base64 encoding of the PKCS#12 (or JKS) release keystore binary |
| `ZEN_ANDROID_KEYSTORE_PASSWORD` | Keystore password |
| `ZEN_ANDROID_KEY_ALIAS` | Key alias inside the keystore |
| `ZEN_ANDROID_KEY_PASSWORD` | Key password |
| `ZEN_UPDATE_SIGNING_KEY_BASE64` | Base64 encoding of the Ed25519 private PEM matching `release/zen-update-public-key.pem` |

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
| `ref` | string | — | Exact annotated `vX.Y.Z-beta.N` tag to rebuild |
| `publish` | boolean | `false` | Reviewed recovery authorization to reconcile and publish the matching draft/existing prerelease |

Permissions: validation, build, and aggregation jobs have `contents: read`; only the final gated publish job has `contents: write`.

## Build and publish paths

Prerequisites:

1. The prepared commit already contains the reviewed `app/app.base.json` version and Android `versionCode`, the independent positive unused `app/ios-build.json` build number, and `docs/releases/vX.Y.Z-beta.N.md` release notes.
2. That exact commit is already pushed to `origin/main`.
3. The four `ZEN_ANDROID_*` secrets above are configured on the repository.
4. `ZEN_UPDATE_SIGNING_KEY_BASE64` is configured with the updater manifest key.

Optional build-only validation (workflow artifact; never mutates the release):

```bash
gh workflow run release-artifacts.yml \
  --ref main \
  -f ref=vX.Y.Z-beta.N \
  -f publish=false
```

The normal maintainer action is exactly:

```bash
git tag -a vX.Y.Z-beta.N -m "Zen vX.Y.Z-beta.N"
git push origin vX.Y.Z-beta.N
```

Do not create a GitHub release by hand. For recovery after a failed run, dispatch the exact existing tag with `publish=true` only after reviewing the tag and intended release. Recovery repeats every identity, SHA, main-ancestry, asset, checksum, and signature gate; it can safely resume a matching draft or reconcile an existing prerelease with `--clobber`. It never creates or rewrites a tag and never force-pushes.

## Build-time baseline and optimization

Recent pre-change GitHub runs on July 14, 2026 put the combined release-assets job at about 20–27 minutes warm/cold. Android dominated: clean prebuild plus Gradle took roughly 14–22 minutes, native Ghostty took 2–3 minutes, and all three daemon binaries took about 40 seconds at the end. The old redundant tag-triggered `native-libs` workflow also consumed about 25–35 minutes, including duplicate iOS/Android native builds.

The new graph moves the daemon work beside Android, so the expected assets critical path is Android plus roughly one minute for aggregation/publication: about 16–24 minutes cold and 14–20 minutes warm based on those observed Android ranges. `--build-cache` now explicitly enables Gradle task reuse across clean Expo prebuilds. The Gradle cache key still includes the generated release identity so a new version can save its own task outputs while setup-java restores compatible dependency state from its prefix; an exact-tag recovery can then hit that version's cache. Pinned native source/output caches remain content-addressed and verified after restore. CocoaPods downloads no longer miss only because the independent iOS build number changed. Xcode DerivedData remains uncached because signed, identity-sensitive intermediates are large and the observed evidence does not justify that risk and complexity.

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
- `release-manifest.json.sig`

Daemon archives contain `zen`, `LICENSE`, `NOTICE`, and `TRADEMARKS.md`. Release notes stay in the GitHub Release body; Android third-party notices remain embedded in the APK. This keeps the download list focused without dropping required attribution.

## Local helper scripts

| Script | Role |
| --- | --- |
| `scripts/materialize-android-keystore.sh` | Base64 → temp keystore file (0600); prints path only |
| `scripts/verify-apk-release.sh` | Package/version/ABI/notice/certificate checks |
| `scripts/android-release-apk.sh` | Clean prebuild + signed assembleRelease |
| `scripts/stage-release.sh` | Deterministic stage tree |
| `scripts/sign-release-manifest.sh` | Validate the updater key and sign the exact manifest bytes |
| `scripts/verify-release-identity.sh` | Tracked identity + stage layout |

## Safety

- Secrets never appear in docs, commits, or intentional log lines.
- The updater accepts one public release stream, including prereleases by semantic-version precedence; it has no channel setting.
- The detached Ed25519 signature authenticates manifest version, platform mapping, archive size, and SHA-256 before download installation.
- CI does not read developer home directories (including `~/.zen/release-keys`).
- Manual dispatch publishes only with the reviewed boolean; automatic publication is only a strict annotated beta tag push whose commit is already on `origin/main`.
- Gradle caches only dependency/build state under the runner's Gradle home. Zig/Ghostty source and unsigned native-output keys include the native lock and build/verification inputs; every cache hit still runs release-grade manifest, checksum, ABI, and notice verification.
- No cache path includes a keystore, updater signing key, signed APK, or staged release output.
- Pinned Zig archives are SHA-256 verified on both cache misses and hits and extracted under `$RUNNER_TEMP/zen-tools` (not `/usr/local`). No `sudo` or unverified installers.
