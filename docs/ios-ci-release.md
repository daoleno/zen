# iOS CI and release automation

Zen deliberately uses two different Apple artifacts because CI validation and end-user distribution have different signing and trust requirements.

| Path | Trigger | Signing | Artifact | Intended use |
| --- | --- | --- | --- | --- |
| iOS native CI | push to `main` or pull request | none | `Zen.app` for Apple Silicon iOS Simulator | Compile/link validation on macOS; not installable on an iPhone |
| iOS signed production | manual `workflow_dispatch` with `app_identity=production` (default) | `app-store-connect` environment | `Zen` / `com.daoleno.zen` App Store Connect IPA | Canonical formal release path |
| iOS signed Preview | manual `workflow_dispatch` with `app_identity=preview` | `app-store-connect-preview` environment | `Zen` / `com.daoleno.zen.preview` App Store Connect IPA | Temporary friend-account TestFlight path without changing production identity |
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

The [`.github/workflows/ios-release.yml`](../.github/workflows/ios-release.yml) workflow is manual-only. It has `contents: read`, creates no tag or release, and selects either the existing `app-store-connect` production GitHub Environment or the isolated `app-store-connect-preview` Environment so repository maintainers can add required reviewers or deployment restrictions.

Required dispatch inputs:

| Input | Meaning |
| --- | --- |
| `ref` | Explicit branch, tag, or full commit SHA to check out |
| `build_number` | Positive `CFBundleVersion`, greater than the builds already uploaded for the selected app identity and marketing version; exposed to Expo as `ZEN_IOS_BUILD_NUMBER` without editing tracked version files |
| `app_identity` | Closed choice: `production` (default) or `preview`; it atomically selects the iOS identity contract, including the bundle ID, artifact name, manifest identity, and protected GitHub Environment |
| `destination` | `artifact-only` (default) or `testflight` |

The identity mapping is defined in `app/iosIdentity.js`; the workflow does not accept a free-form display name or bundle ID. Unset local configuration and the workflow default both resolve to canonical production. Production and Preview both install with `CFBundleDisplayName` set to `Zen`, while Preview keeps the distinct bundle ID `com.daoleno.zen.preview`. Expo's top-level name deliberately remains `Zen`, so Android's normal name/package and the generated `Zen.xcworkspace`/`Zen` scheme cannot change when selecting the iOS Preview identity.

The tracked general/Android prerelease remains `0.1.0-beta.2`. iOS derives the App Store-valid numeric marketing version `0.1.0` from that value and writes it to both `CFBundleShortVersionString` and every generated Xcode `MARKETING_VERSION`. The required positive dispatch `build_number` is written to both `CFBundleVersion` and every generated `CURRENT_PROJECT_VERSION`. The workflow does not expose a separate free-form marketing-version input; changing the tracked numeric core is a reviewed source change. IPA verification and the release manifest use the resolved packaged iOS value, never the general prerelease string.

The workflow fails before building if an input or required signing secret is absent. It also cross-checks that the resolved Expo name, display name, and bundle ID are from the same identity. After building the XCFramework and generated project, it imports signing material into a temporary keychain, validates the provisioning profile's team, selected bundle identifier, expiration, distribution entitlements, and TestFlight eligibility, archives the app, exports exactly one IPA, verifies its display name, bundle ID, marketing version, build number, and code signature, and uploads it with a SHA-256 file and source/build manifest as a 14-day workflow artifact.

When `destination=testflight`, it also validates and uploads that same IPA with Apple's `altool` using an App Store Connect API key. Uploading a build does not select tester groups, complete export-compliance questions, submit Beta App Review, or submit the app for App Review; those remain App Store Connect operations.

## GitHub Environments and secrets

Production keeps the existing protected `app-store-connect` Environment. Create a separate protected `app-store-connect-preview` Environment for the friend account. Configure the same generic secret names in each environment; GitHub exposes only the environment selected by `app_identity`. This keeps workflow inputs minimal and prevents Preview values from overwriting or being combined with canonical signing values. Values must be base64-encoded without being committed or printed.

| Secret | Required for artifact-only | Required for TestFlight | Purpose |
| --- | --- | --- | --- |
| `ZEN_APPLE_CERTIFICATE_BASE64` | yes | yes | PKCS#12 containing the Apple Distribution certificate and private key |
| `ZEN_APPLE_CERTIFICATE_PASSWORD` | yes | yes | PKCS#12 password |
| `ZEN_APPLE_PROVISIONING_PROFILE_BASE64` | yes | yes | App Store distribution profile for the environment's exact bundle ID (`com.daoleno.zen` for production; `com.daoleno.zen.preview` for Preview) |
| `ZEN_APPLE_TEAM_ID` | yes | yes | Apple Developer Team identifier for that environment, cross-checked against the profile |
| `ZEN_ASC_KEY_ID` | no | yes | App Store Connect API key ID |
| `ZEN_ASC_ISSUER_ID` | no | yes | App Store Connect API issuer ID |
| `ZEN_ASC_API_KEY_BASE64` | no | yes | Base64 form of the API `.p8` private key |

The workflow deletes its temporary keychain, certificate, provisioning profile, decoded API key, and installed profile in an `always()` cleanup step. GitHub still controls runner disposal and workflow-artifact retention.

## Canonical production Apple prerequisites

Before the first run, a maintainer must complete these Apple-side operations:

1. Enroll the owning team in the Apple Developer Program and accept current agreements.
2. Register the explicit App ID `com.daoleno.zen`.
3. Create the matching app record in App Store Connect.
4. Create an Apple Distribution certificate and an App Store provisioning profile for that App ID.
5. Create an App Store Connect API key with permission to upload builds; keep the issuer, key ID, and `.p8` private key together when configuring the environment.
6. Configure the `app-store-connect` GitHub Environment, preferably with required reviewers.
7. Choose a build number greater than every build already uploaded for the same marketing version.

Certificate/profile rotation is a manual prerequisite. Expired or mismatched material fails with a named validation error before archive/export.

## Temporary friend-account Preview setup

The Preview App ID and app record belong to the friend's Apple team; they do not rename, transfer, or replace the canonical `Zen` record. After the team invitation is accepted, an authorized person on that team must:

1. Confirm the Apple Developer Program membership and current agreements are active. Apple requires the latest agreement before an app record can be created.
2. Register the explicit App ID `com.daoleno.zen.preview` in Certificates, Identifiers & Profiles.
3. In App Store Connect, create a separate iOS app record named `Zen Preview`, selecting that exact bundle ID and a team-unique SKU. Keep this recommendation even though the installed app is named `Zen`: App Store Connect record-name availability is separate from `CFBundleDisplayName`. Apple requires the app record before the first upload. Account Holder, Admin, or App Manager can create it.
4. Create or supply an Apple Distribution certificate (including its private key in a password-protected `.p12`) and create an App Store distribution provisioning profile for `com.daoleno.zen.preview`. Apple limits distribution-certificate creation to the Account Holder or Admin; another authorized team member may be given the resulting signing material through an approved secure channel.
5. For `destination=testflight`, create or supply an App Store Connect API key on the same App Store Connect account with permission to upload this app. Record its issuer ID and key ID when securely storing the one-time `.p8` download. For `artifact-only`, no API-key secrets are needed.
6. Create the protected GitHub Environment `app-store-connect-preview`, add the generic secrets from the table above using only friend-team values, and restrict deployment to trusted branches/reviewers as appropriate. Do not modify `app-store-connect`.
7. Dispatch `iOS signed release` with `app_identity=preview`, an explicit `ref`, an unused positive `build_number` for the Preview app/version, and initially `destination=artifact-only`. After archive verification succeeds, use a later unused build number with `destination=testflight` when upload is intended.

Apple's current references are [Add a new app](https://developer.apple.com/help/app-store-connect/create-an-app-record/add-a-new-app/), [Upload builds](https://developer.apple.com/help/app-store-connect/manage-builds/upload-builds/), [Roles and access](https://developer.apple.com/help/account/access/roles), and [Certificates overview](https://developer.apple.com/help/account/certificates/certificates-overview).

An invitation alone is not sufficient for CI: the exact role and Certificates, Identifiers & Profiles access must permit the assigned work. If the invited operator lacks certificate/App ID/profile or app-management permissions, the friend's Account Holder or Admin must perform those Apple-side steps. The repository workflow intentionally does not create Apple resources or testers.

## Local verification boundary

Linux can validate configuration, YAML structure, JavaScript/TypeScript tests, shell syntax, native lock consistency, and Android/daemon compatibility. It cannot run Xcode, CocoaPods integration, `security`, `codesign`, archive export, or App Store Connect upload.

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
