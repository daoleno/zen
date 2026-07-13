# iOS CI and release automation

Zen deliberately uses two different Apple artifacts because CI validation and end-user distribution have different signing and trust requirements.

| Path | Trigger | Signing | Artifact | Intended use |
| --- | --- | --- | --- | --- |
| iOS native CI | push to `main` or pull request | none | `Zen.app` for Apple Silicon iOS Simulator | Compile/link validation on macOS; not installable on an iPhone |
| iOS signed release | manual `workflow_dispatch` only | Apple Distribution certificate plus App Store provisioning profile | App Store Connect IPA | Upload to TestFlight/App Store Connect; not a general-purpose sideload IPA |
| Android release | tag/dispatch build, with separate explicit GitHub publish gate | Android release keystore | arm64 signed APK | Direct sideload and optional attachment to an existing GitHub prerelease |

An unsigned Simulator `.app` is the correct CI product: it proves Expo prebuild, CocoaPods, Swift/Objective-C++, the Ghostty XCFramework, and the final app link without granting CI access to Apple signing credentials. It only runs in a compatible Simulator.

An iPhone build must be signed and provisioned. App Store distribution IPAs are delivered through App Store Connect and TestFlight; attaching one to a GitHub Release would suggest it can be installed like the Android APK, which is not true. The signed workflow stores the verified IPA briefly as a GitHub Actions workflow artifact for diagnostics, but it never attaches the IPA to a GitHub Release.

## Unsigned macOS CI

The `ios-native` job in [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) runs on the Apple Silicon `macos-15` image and:

1. installs the locked Bun workspace;
2. validates the shared Android/iOS native contract;
3. builds and verifies the pinned `libs/ios/GhosttyVt.xcframework` device and Simulator slices;
4. runs a clean Expo iOS prebuild and `pod install`;
5. runs `xcodebuild` for a generic iOS Simulator with `CODE_SIGNING_ALLOWED=NO`;
6. verifies the `.app` bundle identifier and Mach-O executable;
7. uploads the unsigned Simulator `.app` as a seven-day workflow artifact.

This job contains no Apple credentials. A JavaScript-only Expo export remains in the Linux app job, but it is not a substitute for this macOS compile/link job.

The optional [`native-libs.yml`](../.github/workflows/native-libs.yml) workflow uses the same canonical iOS build script and artifact layout for a manually requested or tag-triggered native-library build. It does not publish externally.

## Signed release and TestFlight

The [`.github/workflows/ios-release.yml`](../.github/workflows/ios-release.yml) workflow is manual-only. It has `contents: read`, creates no tag or release, and is attached to the `app-store-connect` GitHub Environment so repository maintainers can add required reviewers or deployment restrictions.

Required dispatch inputs:

| Input | Meaning |
| --- | --- |
| `ref` | Explicit branch, tag, or full commit SHA to check out |
| `build_number` | Positive and monotonically increasing `CFBundleVersion`; exposed to Expo as `ZEN_IOS_BUILD_NUMBER` without editing tracked version files |
| `destination` | `artifact-only` (default) or `testflight` |

The workflow fails before building if an input or required signing secret is absent. After building the XCFramework and generated project, it imports signing material into a temporary keychain, validates the provisioning profile's team, bundle identifier, expiration, distribution entitlements, and TestFlight eligibility, archives the app, exports exactly one IPA, verifies its bundle and code signature, and uploads it with a SHA-256 file and source/build manifest as a 14-day workflow artifact.

When `destination=testflight`, it also validates and uploads that same IPA with Apple's `altool` using an App Store Connect API key. Uploading a build does not select tester groups, complete export-compliance questions, submit Beta App Review, or submit the app for App Review; those remain App Store Connect operations.

## GitHub Environment secrets

Configure these only in the protected `app-store-connect` Environment. Values must be base64-encoded without being committed or printed.

| Secret | Required for artifact-only | Required for TestFlight | Purpose |
| --- | --- | --- | --- |
| `ZEN_APPLE_CERTIFICATE_BASE64` | yes | yes | PKCS#12 containing the Apple Distribution certificate and private key |
| `ZEN_APPLE_CERTIFICATE_PASSWORD` | yes | yes | PKCS#12 password |
| `ZEN_APPLE_PROVISIONING_PROFILE_BASE64` | yes | yes | App Store distribution profile for `com.daoleno.zen` |
| `ZEN_APPLE_TEAM_ID` | yes | yes | Apple Developer Team identifier used to cross-check the profile |
| `ZEN_ASC_KEY_ID` | no | yes | App Store Connect API key ID |
| `ZEN_ASC_ISSUER_ID` | no | yes | App Store Connect API issuer ID |
| `ZEN_ASC_API_KEY_BASE64` | no | yes | Base64 form of the API `.p8` private key |

The workflow deletes its temporary keychain, certificate, provisioning profile, decoded API key, and installed profile in an `always()` cleanup step. GitHub still controls runner disposal and workflow-artifact retention.

## Apple account prerequisites

Before the first run, a maintainer must complete these Apple-side operations:

1. Enroll the owning team in the Apple Developer Program and accept current agreements.
2. Register the explicit App ID `com.daoleno.zen`.
3. Create the matching app record in App Store Connect.
4. Create an Apple Distribution certificate and an App Store provisioning profile for that App ID.
5. Create an App Store Connect API key with permission to upload builds; keep the issuer, key ID, and `.p8` private key together when configuring the environment.
6. Configure the `app-store-connect` GitHub Environment, preferably with required reviewers.
7. Choose a build number greater than every build already uploaded for the same marketing version.

Certificate/profile rotation is a manual prerequisite. Expired or mismatched material fails with a named validation error before archive/export.

## Local verification boundary

Linux can validate configuration, YAML structure, JavaScript/TypeScript tests, shell syntax, native lock consistency, and Android/daemon compatibility. It cannot run Xcode, CocoaPods integration, `security`, `codesign`, archive export, or App Store Connect upload.

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
- Crash-symbol and native Ghostty attribution review in the exported app.
- Certificate, provisioning-profile, and API-key rotation practice before expiry.
