# Android app

## Honest scope

| Claim | Status |
| --- | --- |
| Pair, reconnect, session list, structured Chat | Intended beta surface |
| Native terminal (Ghostty VT) | Android only; requires built `libghostty_vt.so` |
| iOS | Not a supported distribution target in this beta |
| Expo Go | Can paste/scan pairing links; custom `zen://` deep links need a dev build/APK |
| Play Store | Not part of this release foundation |

## Prerequisites

- Bun (see root `packageManager`)
- JDK 17 for native Android builds (see root `app:android` script)
- Android SDK / device or emulator for `expo run:android`
- For native terminal: Zig + a Ghostty checkout to build libs (see below)

## Day-to-day JS workflow

From the monorepo root:

```bash
bun install
cd app
npx expo start
```

Typecheck and unit tests:

```bash
cd app
bun test
bunx tsc --noEmit
```

Bundle export check (no Play Store upload):

```bash
cd app
npx expo export --platform android
```

## Pairing on device

1. Run `zen pair https://your-origin` on the host.
2. Open Settings in the app.
3. Paste the `zen://...` link, scan the QR, or import a QR photo.

Remote Expo push is optional. To test it with your own EAS project, set `ZEN_EXPO_PROJECT_ID` (see `app/.env.example`). OSS builds work without push.

## Native terminal library

`app/modules/zen-terminal-vt/libs/android/*/libghostty_vt.so` is **gitignored**. Without those binaries, the terminal surface is unavailable even if Chat works.

Build locally:

```bash
GHOSTTY_SRC=/path/to/ghostty ./scripts/build-libghostty.sh
```

Ghostty is MIT-licensed; keep attribution when redistributing binaries. See [third-party-assets.md](third-party-assets.md).

## Package identity

Current Expo config still uses development-oriented identifiers (`com.anonymous.zen` in `app.base.json`). Treat sideload APKs as maintainer/beta artifacts until application id and signing are productized.

## Related docs

- [Connect and pair](connect-and-pair.md)
- [Troubleshooting](troubleshooting.md)
