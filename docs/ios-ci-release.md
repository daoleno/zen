# iOS CI and release automation

Zen deliberately uses two different Apple artifacts because CI validation and end-user distribution have different signing and trust requirements.

| Path | Trigger | Signing | Artifact | Intended use |
| --- | --- | --- | --- | --- |
| iOS native CI | push to `main` or pull request | none | `Zen.app` for Apple Silicon iOS Simulator | Compile/link validation on macOS; not installable on an iPhone |
| iOS signed production | manual `workflow_dispatch` with `app_identity=production` (default) | `app-store-connect` environment | `Zen` / `com.daoleno.zen` App Store Connect IPA | Canonical formal release path; never automatic |
| iOS signed Preview | pushed annotated `vX.Y.Z-beta.N` tag, or manual `workflow_dispatch` with `app_identity=preview` | `app-store-connect-preview` environment | `Zen` / `com.daoleno.zen.preview` App Store Connect IPA | Automatic beta TestFlight path plus explicit manual recovery/testing |
| Android release | the same beta tag push; manual reviewed recovery/build | Android release keystore | arm64 signed APK | Direct sideload and automated GitHub prerelease attachment |

An unsigned Simulator `.app` is the correct CI product: it proves Expo prebuild, CocoaPods, Swift/Objective-C++, the Ghostty XCFramework, and the final app link without granting CI access to Apple signing credentials. It only runs in a compatible Simulator.

An iPhone build must be signed and provisioned. App Store distribution IPAs are delivered through App Store Connect and TestFlight; attaching one to a GitHub Release would suggest it can be installed like the Android APK, which is not true. The signed workflow stores the verified IPA briefly as a GitHub Actions workflow artifact for diagnostics, but it never attaches the IPA to a GitHub Release.

## Unsigned macOS CI

The `ios-native` job in [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) runs on the Apple Silicon `macos-26` image and:

1. installs the locked Bun workspace;
2. validates the shared Android/iOS native contract;
3. builds and verifies the pinned `libs/ios/GhosttyVt.xcframework` device and Simulator slices;
4. runs a clean Expo iOS prebuild and `pod install`;
5. runs `xcodebuild` for a generic iOS Simulator with `CODE_SIGNING_ALLOWED=NO`;
6. verifies the `.app` bundle identifier and Mach-O executable;
7. uploads the unsigned Simulator `.app` as a seven-day workflow artifact.

This job contains no Apple credentials. A JavaScript-only Expo export remains in the Linux app job, but it is not a substitute for this macOS compile/link job.

The optional [`native-libs.yml`](../.github/workflows/native-libs.yml) workflow uses the same canonical iOS build script and artifact layout only when manually requested. Its redundant beta tag trigger was removed; release workflows build and verify the required native outputs themselves.

## Signed release and TestFlight

The [`.github/workflows/ios-release.yml`](../.github/workflows/ios-release.yml) workflow starts directly from a pushed beta tag. Before Apple credentials are used, it checks out the immutable tag, requires strict `vX.Y.Z-beta.N` syntax and an annotated tag object, proves the tag version equals the tracked Expo version, proves the tag resolves to checked-out HEAD, proves that commit is already on `origin/main`, and runs the tracked identity checks. It derives `CFBundleVersion` from the separate reviewed `app/ios-build.json`, selects only Preview and TestFlight, and enters the protected `app-store-connect-preview` Environment. The uploader helper is checked out separately at the immutable workflow revision so manual recovery of an older app ref still receives reviewed idempotency behavior. An automatic event can never select Production. The workflow has `contents: read` and creates no tag or release.

Manual dispatch remains available for both Preview and Production and for both `artifact-only` and `testflight` destinations. It selects either the existing `app-store-connect` production GitHub Environment or the isolated `app-store-connect-preview` Environment so repository maintainers can add required reviewers or deployment restrictions.

Required dispatch inputs:

| Input | Meaning |
| --- | --- |
| `ref` | Explicit branch, tag, or full commit SHA to check out |
| `build_number` | Manual-only positive `CFBundleVersion`, greater than the builds already uploaded for the selected app identity and marketing version; automatic Preview beta runs use the reviewed `app/ios-build.json` value and do not accept a free-form value |
| `app_identity` | Closed choice: `production` (default) or `preview`; it atomically selects the iOS identity contract, including the bundle ID, artifact name, manifest identity, and protected GitHub Environment |
| `destination` | `artifact-only` (default) or `testflight` |

The identity mapping is defined in `app/iosIdentity.js`; the workflow does not accept a free-form display name or bundle ID. Unset local configuration and the workflow default both resolve to canonical production. Production and Preview both install with `CFBundleDisplayName` set to `Zen`, while Preview keeps the distinct bundle ID `com.daoleno.zen.preview`. Expo's top-level name deliberately remains `Zen`, so Android's normal name/package and the generated `Zen.xcworkspace`/`Zen` scheme cannot change when selecting the iOS Preview identity.

The tracked general/Android prerelease is `0.1.0-beta.11`. iOS derives the App Store-valid numeric marketing version `0.1.0` from that value and writes it to both `CFBundleShortVersionString` and every generated Xcode `MARKETING_VERSION`. Its reviewed monotonic baseline is build `12` in `app/ios-build.json`, independent of Android `versionCode` because App Store Connect history may be ahead. Release preparation must increment that integer above every build already uploaded for the same Apple app and marketing version before creating the next tag. The required positive manual `build_number` overrides it for recovery/testing and is written to both `CFBundleVersion` and every generated `CURRENT_PROJECT_VERSION`. IPA verification and the release manifest use the resolved packaged iOS value.

The workflow fails before building if an input or required signing secret is absent. It also cross-checks that the resolved Expo name, display name, and bundle ID are from the same identity. After building the XCFramework and generated project, it imports signing material into a temporary keychain, validates the provisioning profile's team, selected bundle identifier, expiration, distribution entitlements, and TestFlight eligibility, archives the app, exports exactly one IPA, verifies its display name, bundle ID, marketing version, build number, and code signature, and uploads it with a SHA-256 file and source/build manifest as a 14-day workflow artifact.

When `destination=testflight`, the workflow uploads that same IPA through Apple's official App Store Connect Build Upload API. It creates a build-upload reservation, uploads every Apple-specified byte range to its temporary URL with the returned headers, commits the IPA's streamed MD5 file checksum, and waits for the build upload to become `COMPLETE` or `FAILED`. Preview first looks up both the exact processed build and its marketing-version/build-number upload reservation; if either has completed, a rerun resumes post-processing instead of creating a duplicate upload. An ambiguous identity or an existing incomplete/failed reservation stops for recovery rather than risking another upload. It then waits for the build resource to become `VALID`, records that Zen uses no non-exempt encryption, requires and attaches the exact public `Zen Preview` beta group, and creates the Beta App Review submission only when the build does not already have one. Apple still controls review and tester availability; the workflow does not submit the app for App Review.

If upload succeeds but a later App Store operation fails, the manual `iOS TestFlight post-process` workflow resumes the existing Preview build by marketing/build number without rebuilding or uploading a duplicate IPA. An export-compliance HTTP 409 is accepted only after a fresh exact-build query proves that `usesNonExemptEncryption` already equals the required `false`; every other 409 remains fatal.

The signed release job caches only checksum-pinned Zig/Ghostty sources, verified unsigned Ghostty XCFramework output, and CocoaPods download repositories. Keys include runner architecture, Xcode/CocoaPods toolchain, native lock, and relevant build inputs; verification still runs after every native-output restore. It intentionally does not cache Xcode DerivedData: signed archive intermediates are large and identity-sensitive, and safe invalidation would erase most benefit. Certificates, profiles, keychains, API keys, archives, and signed IPAs are never cached.

Authentication is intentionally limited to an **Individual API Key**. The workflow creates short-lived ES256 JWTs with `sub=user`, `aud=appstoreconnect-v1`, and no `iss` claim, as Apple requires for individual keys. It does not invoke `altool` and does not accept or pass an issuer ID. The presigned binary-upload URLs do not receive the JWT.

Apple Transporter 4.2 also supports uploading an `.ipa` with `-assetFile` and authenticating with the same JWT through `-jwt`. Zen does not use Transporter in GitHub-hosted CI because the official [`macos-15` runner inventory](https://github.com/actions/runner-images/blob/main/images/macos/macos-15-Readme.md) does not guarantee `iTMSTransporter`, and GitHub's [runner-images request to add Transporter](https://github.com/actions/runner-images/issues/13389) was closed as not planned. A probe-only Transporter workflow would therefore fail on valid hosted runners, while noninteractive installation is not a stable runner contract. This is a runner-availability decision, not a claim that Apple lacks an official tool. See Apple's [Transporter User Guide 4.2](https://help.apple.com/itc/transporteruserguide/en.lproj/static.html) and [Upload builds](https://developer.apple.com/help/app-store-connect/manage-builds/upload-builds/).

## GitHub Environments and secrets

Production keeps the existing protected `app-store-connect` Environment. Create a separate protected `app-store-connect-preview` Environment for the friend account. Configure the same generic secret names in each environment; GitHub exposes only the environment selected by `app_identity`. This keeps workflow inputs minimal and prevents Preview values from overwriting or being combined with canonical signing values. Values must be base64-encoded without being committed or printed.

| Secret | Required for artifact-only | Required for TestFlight | Purpose |
| --- | --- | --- | --- |
| `ZEN_APPLE_CERTIFICATE_BASE64` | yes | yes | PKCS#12 containing the Apple Distribution certificate and private key |
| `ZEN_APPLE_CERTIFICATE_PASSWORD` | yes | yes | PKCS#12 password |
| `ZEN_APPLE_PROVISIONING_PROFILE_BASE64` | yes | yes | App Store distribution profile for the environment's exact bundle ID (`com.daoleno.zen` for production; `com.daoleno.zen.preview` for Preview) |
| `ZEN_APPLE_TEAM_ID` | yes | yes | Apple Developer Team identifier for that environment, cross-checked against the profile |
| `ZEN_ASC_KEY_ID` | no | yes | App Store Connect API key ID |
| `ZEN_ASC_API_KEY_BASE64` | no | yes | Base64 form of the Individual API `.p8` private key |

`ZEN_ASC_APP_ID` is a non-secret GitHub Environment variable required for production TestFlight uploads. Preview is pinned in the reviewed workflow to App Store Connect app ID `6790486708`, bundle ID `com.daoleno.zen.preview`, and signing team `HD84J3DJ2B`; the workflow rejects a different Preview team before uploading. Do not put the key, JWT, P12, or passwords in variables.

Apple secrets are scoped to the validation and specific signing/upload steps rather than the whole job. The workflow writes the decoded Individual key only to a mode-`0600` file under `runner.temp`. Its `always()` cleanup step deletes that exact file along with the temporary keychain, certificate, provisioning profile, and installed profile. GitHub still controls runner disposal and workflow-artifact retention.

## Canonical production Apple prerequisites

Before the first run, a maintainer must complete these Apple-side operations:

1. Enroll the owning team in the Apple Developer Program and accept current agreements.
2. Register the explicit App ID `com.daoleno.zen`.
3. Create the matching app record in App Store Connect.
4. Create an Apple Distribution certificate and an App Store provisioning profile for that App ID.
5. Create an Individual App Store Connect API key for a user with permission to upload builds; store only its key ID and `.p8` in the environment secrets, and set the public App Store Connect app ID as `ZEN_ASC_APP_ID` in environment variables.
6. Configure the `app-store-connect` GitHub Environment, preferably with required reviewers.
7. Choose a build number greater than every build already uploaded for the same marketing version.

Certificate/profile rotation is a manual prerequisite. Expired or mismatched material fails with a named validation error before archive/export.

## Temporary friend-account Preview setup

The Preview App ID and app record belong to the friend's Apple team; they do not rename, transfer, or replace the canonical `Zen` record. After the team invitation is accepted, an authorized person on that team must:

1. Confirm the Apple Developer Program membership and current agreements are active. Apple requires the latest agreement before an app record can be created.
2. Register the explicit App ID `com.daoleno.zen.preview` in Certificates, Identifiers & Profiles.
3. In App Store Connect, use the existing iOS app record named `Zen — Coding Agents`, selecting the exact bundle ID `com.daoleno.zen.preview`. App Store Connect record naming is separate from the installed `CFBundleDisplayName`, which remains `Zen`. Apple requires the app record before the first upload. Account Holder, Admin, or App Manager can create it when setup is necessary.
4. Create or supply an Apple Distribution certificate (including its private key in a password-protected `.p12`) and create an App Store distribution provisioning profile for `com.daoleno.zen.preview`. Apple limits distribution-certificate creation to the Account Holder or Admin; another authorized team member may be given the resulting signing material through an approved secure channel.
5. For `destination=testflight`, create an Individual App Store Connect API key for a user on the same account who can upload this app. Store its key ID and the one-time `.p8` download; an Individual key has no issuer ID. For `artifact-only`, no API-key secrets are needed.
6. Create the protected GitHub Environment `app-store-connect-preview`, add the generic secrets from the table above using only friend-team values, and restrict deployment to trusted branches/reviewers as appropriate. Do not modify `app-store-connect`.
7. The normal first-party `Release next beta` workflow prepares and verifies the reviewed app version, Android `versionCode`, independent unused iOS build number, changelog index, and matching release notes, then atomically advances `main` and the annotated tag. Because its `GITHUB_TOKEN` tag push does not recursively trigger workflows, it explicitly dispatches both the existing public-artifact workflow and `iOS signed release` at that tag with the tracked build number, `app_identity=preview`, and `destination=testflight`. For manual testing or recovery, dispatch `iOS signed release` with the same exact annotated tag and tracked unused build number, or another reviewed unused positive build number when the tracked one cannot be reused. A maintainer-pushed annotated beta tag remains the fallback automatic Preview path. Production remains manual.

Apple's current references are [Generating tokens for API requests](https://developer.apple.com/documentation/appstoreconnectapi/generating-tokens-for-api-requests), [Build uploads](https://developer.apple.com/documentation/appstoreconnectapi/build-uploads), [Add a new app](https://developer.apple.com/help/app-store-connect/create-an-app-record/add-a-new-app/), [Upload builds](https://developer.apple.com/help/app-store-connect/manage-builds/upload-builds/), [Roles and access](https://developer.apple.com/help/account/access/roles), and [Certificates overview](https://developer.apple.com/help/account/certificates/certificates-overview).

An invitation alone is not sufficient for CI: the exact role and Certificates, Identifiers & Profiles access must permit the assigned work. If the invited operator lacks certificate/App ID/profile or app-management permissions, the friend's Account Holder or Admin must perform those Apple-side steps. The repository workflow intentionally does not create Apple resources or testers.

## Local verification boundary

Linux can validate configuration, YAML structure, JavaScript/TypeScript tests, shell syntax, native lock consistency, and Android/daemon compatibility. It cannot run Xcode, CocoaPods integration, `security`, `codesign`, archive export, or App Store Connect upload.

The uploader's offline unit tests generate a disposable P-256 key and use only mocked HTTP responses. They verify `sub=user`, absence of `iss`, a 64-byte raw ES256 `R || S` signature that OpenSSL accepts, Build Upload API request bodies, Apple-reserved upload headers, full byte-range coverage, and the MD5 file-checksum commit. The test makes `Path.read_bytes()` fail during the upload flow to enforce streamed checksum calculation. Tests never contact App Store Connect:

```bash
python3 -m unittest discover -s scripts/tests -p 'test_*.py'
```

Both identity resolutions can be checked on Linux without generating native projects:

```bash
cd app
bunx expo config --json | jq '{name, bundleIdentifier: .ios.bundleIdentifier, displayName: .ios.infoPlist.CFBundleDisplayName, marketingVersion: .ios.infoPlist.CFBundleShortVersionString}'
ZEN_IOS_APP_VARIANT=preview bunx expo config --json | jq '{name, bundleIdentifier: .ios.bundleIdentifier, displayName: .ios.infoPlist.CFBundleDisplayName, marketingVersion: .ios.infoPlist.CFBundleShortVersionString}'
```

On a Mac, reproduce the unsigned portion with:

```bash
bun install --frozen-lockfile
bun run native:contract
bun run native:build:ios
cd app
bunx expo prebuild --platform ios --clean --no-install
cd ios
pod install
xcodebuild \
  -workspace Zen.xcworkspace \
  -scheme Zen \
  -configuration Debug \
  -sdk iphonesimulator \
  -destination 'generic/platform=iOS Simulator' \
  CODE_SIGNING_ALLOWED=NO \
  ONLY_ACTIVE_ARCH=YES \
  build
```

Do not test release signing by copying production secrets into a pull-request workflow or a developer `.env.local` file.

## Remaining release gates

- A successful GitHub-hosted macOS run with the currently selected Xcode image.
- A signed archive/export using the real team certificate and profile.
- Installation and terminal I/O testing on at least one physical device.
- First App Store Connect upload, processing, export-compliance answers, privacy metadata, screenshots, TestFlight tester configuration, and any Beta App Review.
- For Preview specifically: friend-team invitation acceptance, Preview App ID/app record/profile/API key creation, protected environment population, and tester-group assignment after upload.
- Crash-symbol and native Ghostty attribution review in the exported app.
- Certificate, provisioning-profile, and API-key rotation practice before expiry.
