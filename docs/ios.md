# iOS app

The iOS client is available as a source build. It has been validated on an Apple Silicon Mac with Xcode 26.6 and an iPhone 17 Pro / iOS 26.5 Simulator, including pairing, daemon reconnect, session discovery, tmux attachment, Ghostty rendering, and terminal input/output.

Zen does not currently claim a generally available App Store build. The repository includes macOS CI for an unsigned Simulator app and a manual, credential-gated signed IPA/TestFlight workflow. A physical-device source install requires an Apple development team and signing configuration.

## TestFlight Preview access

The [TestFlight Preview public invitation](https://testflight.apple.com/join/rTKCDzMt) is configured. The first external build is awaiting Apple Beta App Review, so the link does not yet guarantee that a new tester can install the Preview. Source builds remain available independently of TestFlight.

## Supported mobile interfaces

The agent interface is shared React Native code, not an Android-only feature. Codex, Claude Code, Cursor Agent, and Grok are classified as structured-chat agents by `supportsChatInterface` in `app/app/terminal/useTerminalRouteModel.ts`.

For all four agent kinds on both Android and iOS:

- a session defaults to the structured Chat interface when transcript events are available;
- the top-bar action can switch between Chat and Terminal;
- the selected mode is persisted per session;
- provider-specific composer labels, conversation timelines, tool calls, plans, attachments, Git diff, and action prompts use the shared UI;
- Terminal mode attaches to the same daemon/tmux session and uses the platform's Ghostty bridge.

Codex is currently the only kind that advertises Codex slash-command discovery in the composer. That is a provider capability distinction, not an iOS limitation. Empty or unavailable provider transcripts may also make Chat less informative; Terminal remains available as the authoritative live session view.

Platform-specific implementation is deliberately limited to native plumbing:

- Android loads `libghostty_vt.so` through JNI.
- iOS links `GhosttyVt.xcframework` through an Expo module/Objective-C++ bridge.
- Both mobile platforms use the shared enriched Markdown renderer; plain text is only the render/error fallback.
- Keyboard inset behavior differs by platform, while the accessory keys and terminal controller are shared.

## Prerequisites

- Apple Silicon Mac
- Full Xcode selected with `xcode-select`
- An installed iOS Simulator runtime, or a signed physical device target
- CocoaPods
- Bun and the workspace dependencies
- Network access for the first pinned Zig/Ghostty build

The native build script downloads the pinned Zig release when the active `zig` is not the version in `native.lock.json`. Do not substitute an incompatible newer Zig release.

## Build the native terminal

From the repository root:

```bash
bun install
bun run native:build:ios
bun run native:verify:ios
```

This creates the ignored native output under:

```text
app/modules/zen-terminal-vt/libs/ios/GhosttyVt.xcframework
```

The verifier checks the pinned Ghostty commit, checksums, headers/module map, an arm64 iOS-device slice, and an arm64 iOS-Simulator slice.

## Generate and run the Expo native project

From `app/`:

```bash
bunx expo prebuild --platform ios --clean
cd ios
pod install
cd ..
bun run ios
```

You can also open `app/ios/Zen.xcworkspace` in Xcode after `pod install`. The `app/ios/` directory is generated and gitignored; make durable native configuration changes through Expo config or the local Expo module instead of editing generated files.

The app uses bundle identifier `com.daoleno.zen`, targets iOS 16.4 or newer, and declares local-network access because Zen connects directly to a self-hosted daemon. Camera access is used for pairing QR codes; microphone access is not requested.

## Pair and validate

Start a reachable daemon, create a fresh pairing link, and import it in the iOS app:

```bash
zen --lan
# In another terminal, run the LAN or Tailscale pair command Zen prints.
```

For Simulator testing on the same Mac, a locally reachable address can be used. For a physical iPhone, use `zen --lan` with the Mac's printed LAN/Tailscale address on a trusted private network, or use an HTTPS origin that forwards every required route to bare `zen`. See [Connect and pair](connect-and-pair.md).

An end-to-end terminal check should cover:

1. Pair and reconnect without reusing an expired enrollment token.
2. Confirm Codex, Claude Code, Cursor Agent, or Grok sessions appear in Sessions.
3. Open a structured agent session and verify Chat is the default interface.
4. Toggle to Terminal and attach to its tmux session.
5. Confirm existing output, cursor, colors, and tmux status are rendered.
6. Enter a command and confirm shell output returns to the same terminal surface.
7. Background/foreground the app and confirm it resumes the connection and mounted surface.

## Native terminal / XCFramework contract

The machine-readable source of truth is:

[`app/modules/zen-terminal-vt/native.lock.json`](../app/modules/zen-terminal-vt/native.lock.json)

Important invariants:

1. Ghostty commit and Zig version are pinned; the build refuses a dirty Ghostty checkout.
2. `GhosttyVt.xcframework`, checksums, build manifest, and copied license notice are generated and gitignored.
3. The XCFramework must contain arm64 device and arm64 Simulator slices.
4. `ZenTerminalVt.podspec` is the CocoaPods/autolinking boundary and must keep the deployment target aligned with the lockfile.
5. Ghostty's MIT notice is packaged into redistributed iOS app bundles/IPAs by `withZenIOSBuild` at bundle-root `GHOSTTY-MIT.txt` (Xcode flattens ordinary resource files) and enforced by `scripts/verify-ios-artifact.sh`.
6. `withZenIOSBuild` disables precompiled Expo Swift modules so the app does not mix incompatible Expo binary interfaces.
7. App Transport Security is intentional: `NSAllowsLocalNetworking=true` for self-hosted LAN/tailnet daemons, and `NSAllowsArbitraryLoads=false` so cleartext internet remains denied.

## Verification

After dependency or native changes, run:

```bash
cd app
bunx tsc --noEmit
bun test
bunx expo-doctor
cd ..
bun run native:verify:ios
git diff --check
```

Then perform a real Xcode build-and-run. Static checks do not replace launching the app because Swift ABI, CocoaPods, signing, dyld, local-network permission, and native-module registration failures only appear in the Apple runtime path.

## Current distribution status

The source-build path is working, but a bare clone does not contain the ignored Ghostty XCFramework. CI rebuilds/checksums it and compiles an unsigned Simulator app. The protected Preview workflow has signed, exported, verified, and uploaded an IPA to App Store Connect; Apple Beta App Review remains the gate before public-link testers can install it. Zen does not claim a generally available App Store build. See [iOS CI and release automation](ios-ci-release.md).
