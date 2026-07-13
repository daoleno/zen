# iOS release readiness

Audit date: 2026-07-13. This document separates observed repository/upstream facts from work that still requires an Apple build host, credentials, or product validation.

## Ghostty baseline selection

Zen consumes the standalone `libghostty-vt` C ABI, not the full Ghostty application library or its macOS renderer. Upstream has not tagged `libghostty-vt` independently. At selection time the latest Ghostty application release tag was `v1.3.1` (`332b2aef…`), while official `main` identified itself as `1.3.2-dev`. Zen pins the then-current immutable `main` commit `55a3e33ab26a23d75b274b23c7f76d837db00578`, dated 2026-07-12 upstream, rather than following a moving branch.

This is the correct latest baseline because the exact commit builds `libghostty-vt` for both Zen Android ABIs with Zig 0.15.2, retains every C symbol used by Zen's JNI bridge, and contains upstream's iOS device/simulator XCFramework builder. The MIT license body and required Zig version are unchanged from the prior pin. The public VT headers did change substantially; the lock records the umbrella-header SHA-256 so source/artifact mismatches fail before packaging.

The Apple script passes both `-Demit-lib-vt=true` and the current upstream `-Demit-xcframework=true` gate explicitly, avoiding reliance on upstream's host-dependent default.

## Minimum honest iOS scope

The first iOS release may ship Zen's pairing, agent, work, Brain, and chat experiences while showing an explicit iOS-only Terminal unavailable state. It must not advertise interactive Terminal parity until the Ghostty VT core, Zen render-state bridge, WebView/input layer, and device lifecycle behavior all pass on-device testing.

The repository now contains an autolinkable Swift module plus an ObjC++ owner for the complete existing JavaScript handle API. The podspec mandatory-links the pinned XCFramework: pod installation must fail when that artifact is absent. The module still reports `vtCore: false` and `renderer: none` until the macOS compile/link lane and physical-device acceptance below succeed. This is a source gate, not a runtime file-existence fallback.

## Blocker matrix

| Area | Observed fact | Shipping implication | State / next evidence |
| --- | --- | --- | --- |
| Expo native module | `zen-terminal-vt` declared iOS before it had an iOS module or podspec. Expo requires a platform declaration, podspec, and Swift module class for Apple autolinking. | A generated iOS project could not link the declared local module coherently. | **First slice complete:** Swift capability module, podspec, and Apple autolinking registration exist. Verify with `expo-modules-autolinking resolve --platform apple`, then `pod install`/Xcode on macOS. |
| Ghostty Apple artifact | The pinned Ghostty commit builds `ghostty-vt.xcframework` on Darwin and includes arm64 iOS device and arm64 simulator slices when both SDKs are detected. Upstream's pinned minimum is iOS 17. | A real native input artifact is feasible, but cannot be produced or validated on Linux. | **Pipeline + mandatory linkage present, artifact absent locally:** the macOS workflow builds/verifies the artifact before CocoaPods/Xcode compilation. Retain provenance/checksums before release use. |
| C/Swift bridge | The ObjC++ bridge owns terminal/render/formatter/mouse state behind positive integer IDs, serializes every operation, removes ownership before idempotent destroy, frees all survivors on teardown, and rejects stale IDs elsewhere. Swift exposes all 14 methods in the Android/TypeScript surface. | Static source checks cannot prove Clang module import, Swift selector import, or final linkage. | **Compile blocker:** `verify-ios-terminal-bridge.py` checks the pinned ABI, explicit selectors, shapes, linkage, and ownership. The macOS workflow must pass Expo prebuild, `pod install`, and unsigned simulator `xcodebuild` before enabling `vtCore`. |
| Rendering | iOS returns the existing WebView snapshot fields and uses Ghostty's HTML formatter, dirty state, dimensions, and cursor data. Selection remains the established DOM/WebView selection contract. Android remains unchanged and retains its richer per-row HTML builder. | Source-level protocol parity exists, but formatter output, partial updates, selection, and WebView behavior are not device-proven. | **Device blocker:** test snapshots and selection on an arm64 iPhone before enabling the capability. A native Metal/CoreText renderer is deliberately out of scope. |
| Keyboard / IME | Current React Native input/accessory behavior has only been exercised against Android. Rich terminal protocols need key-down/modifier semantics that mobile text inputs may not expose completely. | Basic text may work while Ctrl/Alt, composition, dead keys, hardware keyboards, and Kitty keyboard mode fail. | **Device blocker:** define an iOS input event contract and test software IMEs, CJK composition, hardware keyboards, modifiers, paste, and secure-text assumptions. |
| Clipboard / selection | The renderer supplies visible text and the app has clipboard dependencies, but no iOS Terminal selection/paste test evidence exists. | Copy/paste can be incomplete or trigger iOS paste prompts unexpectedly. | **Device blocker:** test selection, paste permission UX, large/Unicode payloads, and background/foreground transitions. |
| Lifecycle / memory | Terminal handles are native allocations destroyed from a React hook. iOS scene/background, memory pressure, WebView process loss, and reconnect restoration are unverified. | Leaks, stale handles, blank rendering, or lost input are release risks. | **Device blocker:** instrument create/destroy balance and test repeated navigation, suspend/resume, reconnect, rotation policy, and memory warnings. |
| Architectures | Upstream pinned output is arm64 device + arm64 simulator; it does not claim x86_64 simulator in the Apple universal helper. | Apple Silicon simulator is covered; Intel simulator is not. | **Known scope:** document Apple Silicon CI/dev requirement or separately prove an x86_64 simulator build. Physical arm64 device remains mandatory. |
| App identity | The Expo config previously had Android identity only. | iOS archive/versioning lacked a canonical bundle ID/build number source. | **First slice complete:** `com.daoleno.zen` and build `2` are canonical config values. Because mandatory linkage makes Ghostty part of every iOS binary, a config plugin now raises generated Xcode targets to the pinned artifact's iOS 17 floor even while Terminal capability is gated. Confirm the identifier belongs to the intended Apple team before signing. |
| Signing / distribution | No Apple team, provisioning profile, distribution certificate, App Store Connect app, or EAS iOS credential evidence is stored in the repository (and secrets must not be committed). | No signed archive or TestFlight/App Store upload can be proven here. | **Credential blocker:** maintainer must choose/confirm Apple team and distribution path, then perform an archive/export on macOS or configured EAS. |
| Entitlements / permissions | Pairing uses Camera with an Expo permission string. No Terminal-specific entitlement is expected because the shell/PTY remains on the paired daemon. | Generated Info.plist and entitlements still require archive inspection; camera purpose text must be present. | **Archive blocker:** inspect the signed archive's Info.plist, entitlements, network policy, URL scheme, icons, and orientations. Do not add local process/JIT entitlements. |
| Privacy / review | Apple requires accurate privacy disclosures/purpose strings and may require privacy manifests/reasons for covered APIs and third-party SDKs. App Review also rejects misleading or incomplete functionality. | Disabled Terminal must be described honestly in iOS metadata; dependencies and required-reason APIs need archive-level review. | **Release blocker:** generate/archive, run Xcode privacy validation, complete App Privacy and support/privacy-policy URLs, and provide pairing/review access instructions. |
| CI / release | Existing workflows build/test Android and publish Android plus daemon artifacts. | iOS distribution remains unproven. | **Partial:** Linux CI exports both bundles and statically verifies the Apple bridge. The macOS native workflow builds/verifies the XCFramework, generates the Expo project, installs pods, and compiles/links an unsigned Apple Silicon simulator build. A credentialed archive lane remains future work. |

## Architecture boundary for the next slice

1. Generate and verify the pinned XCFramework on macOS; record the Ghostty commit, Zig/Xcode versions, slice architectures, minimum OS, headers, and SHA-256.
2. Run the static bridge contract and the macOS generated-project compile/link lane. Resolve any pinned-ABI or Swift-import mismatch without adding runtime fallback.
3. On an arm64 iPhone, validate create/write/resize/snapshot/cursor/scroll/mouse/theme/destroy, Unicode and escape-heavy output, repeated navigation, background/foreground, WebView loss, selection/copy/paste, IME, hardware keyboard, memory pressure, and reconnect restoration.
4. Compare representative iOS snapshots with Android, including ANSI/RGB colors, wide/combining characters, tmux status lines, cursor visibility, scrollback, and partial/full dirty transitions.
5. Only after those tests should `getCapabilities` switch to `vtCore: true`/`renderer: webview`, allowing `TerminalSurface` to select Ghostty on iOS.

## Primary sources

- [Ghostty repository and libghostty-vt status](https://github.com/ghostty-org/ghostty)
- [Pinned Ghostty Apple XCFramework implementation](https://github.com/ghostty-org/ghostty/blob/55a3e33ab26a23d75b274b23c7f76d837db00578/src/build/GhosttyLibVt.zig)
- [Pinned Ghostty target/minimum OS configuration](https://github.com/ghostty-org/ghostty/blob/55a3e33ab26a23d75b274b23c7f76d837db00578/src/build/Config.zig)
- [Latest application tag at selection: v1.3.1](https://github.com/ghostty-org/ghostty/releases/tag/v1.3.1)
- [Expo local native module conventions](https://docs.expo.dev/modules/get-started/)
- [Expo autolinking behavior](https://docs.expo.dev/modules/autolinking/)
- [Apple App Review readiness](https://developer.apple.com/app-store/review/)
- [Apple privacy manifest and SDK signature requirements](https://developer.apple.com/news/?id=pvszzano)

## Verification commands

```bash
cd app && bunx expo-modules-autolinking resolve --platform apple
cd app && bun test services/terminalCapabilities.test.ts
cd app && bunx tsc --noEmit
cd app && bunx expo export --platform ios
python3 scripts/verify-ios-terminal-bridge.py
./scripts/build-libghostty-apple.sh       # macOS + Xcode only
./scripts/verify-libghostty-apple.sh      # after the Apple artifact exists
cd app && bunx expo prebuild --platform ios --clean --no-install
cd app/ios && pod install
xcodebuild -workspace Zen.xcworkspace -scheme Zen -configuration Debug \
  -sdk iphonesimulator \
  -destination 'generic/platform=iOS Simulator' \
  CODE_SIGNING_ALLOWED=NO ONLY_ACTIVE_ARCH=YES build
xcodebuild -workspace Zen.xcworkspace -scheme Zen -configuration Release \
  -sdk iphoneos -destination 'generic/platform=iOS' \
  CODE_SIGNING_ALLOWED=NO ONLY_ACTIVE_ARCH=YES build
```

Generating `app/ios` is intentionally not part of this slice. Expo prebuild is expected to generate it when a macOS native build is performed; committing generated native churn should be a separate deliberate repository decision.
