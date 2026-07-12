#!/usr/bin/env bash
# Real Gradle configuration check for zen-terminal-vt ABI resolution.
#
# Evaluates the actual module build.gradle with:
#   -PreactNativeArchitectures=arm64-v8a
# Catches script-local Groovy capture bugs (unknown property) and ensures
# abiFilters resolve to arm64-only for release architecture pins.
#
# Requires: app/android prebuild (gradlew), arm64 libghostty_vt.so present.
# Usage:
#   ./scripts/verify-zen-terminal-abi-gradle.sh

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

GRADLEW="$ROOT/app/android/gradlew"
if [[ ! -x "$GRADLEW" ]]; then
  echo "error: app/android/gradlew missing; run: cd app && bunx expo prebuild --platform android --no-install" >&2
  exit 1
fi

SO_ARM="$ROOT/app/modules/zen-terminal-vt/libs/android/arm64-v8a/libghostty_vt.so"
if [[ ! -f "$SO_ARM" ]]; then
  echo "error: missing $SO_ARM (needed for arm64-only configuration check)" >&2
  echo "  build with: ./scripts/build-libghostty.sh --abis arm64-v8a" >&2
  exit 1
fi

PROJECT=":zen-terminal-vt"
echo "ok: using Gradle project ${PROJECT}"
echo "ok: configuring with -PreactNativeArchitectures=arm64-v8a"

LOG="$(mktemp "${TMPDIR:-/tmp}/zen-abi-gradle.XXXXXX.log")"
cleanup() { rm -f "$LOG"; }
trap cleanup EXIT

set +e
(
  cd "$ROOT/app/android"
  # help forces full project evaluation (including zen-terminal-vt build.gradle)
  # without a full native link of the app.
  ./gradlew "${PROJECT}:help" \
    -PreactNativeArchitectures=arm64-v8a \
    --console=plain \
    2>&1
) | tee "$LOG"
status=${PIPESTATUS[0]}
set -e

if [[ $status -ne 0 ]]; then
  echo "FAIL: Gradle configuration failed for ${PROJECT} with arm64-v8a" >&2
  if grep -Eqi "unknown property|Cannot get property|No such property|ZEN_TERMINAL_SUPPORTED_ABIS" "$LOG"; then
    echo "FAIL: Groovy scope/capture regression (script-local def not visible to method)" >&2
  fi
  exit 1
fi

if ! grep -E "zen-terminal-vt: ndk abiFilters=\[arm64-v8a\]" "$LOG" >/dev/null; then
  echo "FAIL: expected lifecycle log showing abiFilters=[arm64-v8a] only" >&2
  grep -E "zen-terminal-vt: ndk abiFilters=" "$LOG" || true
  exit 1
fi

if grep -E "zen-terminal-vt: ndk abiFilters=.*x86_64" "$LOG" >/dev/null; then
  echo "FAIL: x86_64 still present in abiFilters under arm64-only pin" >&2
  exit 1
fi

echo "ok: Gradle configured zen-terminal-vt with arm64-v8a only (no Groovy capture error)"
