#!/usr/bin/env bash
# Build an arm64-v8a sideload release APK with native terminal libs verified.
#
# Prerequisites:
#   - JDK 17, Android SDK (ANDROID_HOME), bun install
#   - release_grade libghostty arm64 (./scripts/build-libghostty.sh --abis arm64-v8a)
#   - Expo prebuild so withZenAndroidRelease has wired assets + signing
#
# Signing (secret-safe):
#   Default: debug keystore (local/dev sideload only).
#   Optional release keystore via environment only (never commit; never printed):
#     ZEN_ANDROID_KEYSTORE=/absolute/path/to/release.keystore
#     ZEN_ANDROID_KEYSTORE_PASSWORD=...
#     ZEN_ANDROID_KEY_ALIAS=...
#     ZEN_ANDROID_KEY_PASSWORD=...
#   Gradle reads these via System.getenv (withZenAndroidRelease plugin).
#   This script does not pass secrets as -P flags.
#
# Usage:
#   ./scripts/android-release-apk.sh
#   ./scripts/android-release-apk.sh --skip-prebuild

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SKIP_PREBUILD=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-prebuild) SKIP_PREBUILD=1; shift ;;
    -h|--help) sed -n '2,24p' "$0"; exit 0 ;;
    *) echo "error: unknown arg $1" >&2; exit 2 ;;
  esac
done

export JAVA_HOME="${JAVA_HOME:-/usr/lib/jvm/java-17-openjdk}"
export PATH="$JAVA_HOME/bin:$PATH"

if [[ ! -x "$JAVA_HOME/bin/java" ]]; then
  echo "error: JDK 17 not found at JAVA_HOME=$JAVA_HOME" >&2
  exit 1
fi

if [[ -z "${ANDROID_HOME:-}${ANDROID_SDK_ROOT:-}" ]]; then
  if [[ -d "$HOME/Android/Sdk" ]]; then
    export ANDROID_HOME="$HOME/Android/Sdk"
  else
    echo "error: set ANDROID_HOME" >&2
    exit 1
  fi
fi

"$ROOT/scripts/verify-libghostty.sh" --release

if [[ ! -f "$ROOT/app/assets/notices/GHOSTTY-MIT.txt" ]]; then
  echo "error: Ghostty MIT notice missing from app assets" >&2
  exit 1
fi

# Signing env completeness (do not print values)
if [[ -n "${ZEN_ANDROID_KEYSTORE:-}" ]]; then
  for v in ZEN_ANDROID_KEYSTORE_PASSWORD ZEN_ANDROID_KEY_ALIAS ZEN_ANDROID_KEY_PASSWORD; do
    if [[ -z "${!v:-}" ]]; then
      echo "error: $v is required when ZEN_ANDROID_KEYSTORE is set" >&2
      exit 1
    fi
  done
  if [[ ! -f "$ZEN_ANDROID_KEYSTORE" ]]; then
    echo "error: keystore file not found (path redacted)" >&2
    exit 1
  fi
  echo "note: ZEN_ANDROID_KEYSTORE is set; release signing will use env-based config if plugin wired."
else
  echo "note: no ZEN_ANDROID_KEYSTORE set — APK uses debug keystore (local/dev sideload only)."
fi

if [[ $SKIP_PREBUILD -eq 0 ]]; then
  echo "Running expo prebuild (android) to apply withZenAndroidRelease..."
  (cd "$ROOT/app" && bunx expo prebuild --platform android --no-install)
fi

if [[ ! -x "$ROOT/app/android/gradlew" ]]; then
  echo "error: app/android missing; run: cd app && bunx expo prebuild --platform android" >&2
  exit 1
fi

GRADLE_APP="$ROOT/app/android/app/build.gradle"
if ! grep -q 'System.getenv("ZEN_ANDROID_KEYSTORE")' "$GRADLE_APP"; then
  echo "error: generated app/build.gradle lacks ZEN_ANDROID_KEYSTORE wiring" >&2
  echo "       withZenAndroidRelease plugin did not apply; refuse false-confidence signing" >&2
  exit 1
fi
pass_notice_asset="$ROOT/app/android/app/src/main/assets/notices/GHOSTTY-MIT.txt"
if [[ ! -f "$pass_notice_asset" ]]; then
  echo "error: prebuild did not copy Ghostty MIT into android assets ($pass_notice_asset)" >&2
  exit 1
fi

(
  cd "$ROOT/app"
  bun run build:apk
)

APK="$ROOT/app/android/app/build/outputs/apk/release/app-release-arm64.apk"
if [[ ! -f "$APK" ]]; then
  APK="$ROOT/app/android/app/build/outputs/apk/release/app-release.apk"
fi
if [[ ! -f "$APK" ]]; then
  echo "error: APK not found after build" >&2
  exit 1
fi

"$ROOT/scripts/verify-apk-notice.sh" "$APK"

OUT_DIR="$ROOT/dist-download/android-native"
mkdir -p "$OUT_DIR"
# Stable-ish name from git describe when available; else pin short + apk sha prefix
PIN_SHORT="$(python3 -c "import json;print(json.load(open('app/modules/zen-terminal-vt/native.lock.json'))['ghostty']['commit'][:12])")"
APK_SHA="$(sha256sum "$APK" | awk '{print $1}')"
COPY="$OUT_DIR/zen-android-arm64-${PIN_SHORT}-${APK_SHA:0:12}.apk"
cp -f "$APK" "$COPY"
echo "$APK_SHA  $(basename "$COPY")" | tee "$COPY.sha256"
# Adjacent copy for humans who unpack the directory (APK itself already verified)
cp -f "$ROOT/app/assets/notices/GHOSTTY-MIT.txt" "$OUT_DIR/GHOSTTY-MIT.txt"
if [[ -f "$ROOT/app/modules/zen-terminal-vt/libs/android/SHA256SUMS" ]]; then
  cp -f "$ROOT/app/modules/zen-terminal-vt/libs/android/SHA256SUMS" "$OUT_DIR/libghostty-SHA256SUMS"
fi
if [[ -f "$ROOT/app/modules/zen-terminal-vt/libs/android/build-manifest.json" ]]; then
  cp -f "$ROOT/app/modules/zen-terminal-vt/libs/android/build-manifest.json" "$OUT_DIR/libghostty-build-manifest.json"
fi

echo ""
echo "APK:            $COPY"
echo "Checksum:       $COPY.sha256"
echo "Notice in APK:  assets/notices/GHOSTTY-MIT.txt (verified)"
echo "Notice adjacent:$OUT_DIR/GHOSTTY-MIT.txt"
echo "Done (no publish)."
