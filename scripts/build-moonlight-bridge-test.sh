#!/usr/bin/env bash
# Build libzen_moonlight_test.so: the real Zen JNI bridge linked against the
# inert moonlight-common-c boundary, for host JVM lifecycle tests.
#
# No network, host or crypto dependency beyond the pinned Limelight.h header.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MODULE="$ROOT/app/modules/zen-remote-desktop"
OUT="${1:-$MODULE/android/build/moonlight-test}"

"$ROOT/scripts/fetch-moonlight-common-c.sh" >/dev/null

cmake -S "$MODULE/android" -B "$OUT" \
  -DZEN_HOST_BUILD=ON -DZEN_BRIDGE_TEST=ON -DCMAKE_BUILD_TYPE=Debug >/dev/null
cmake --build "$OUT" --target zen_moonlight_test -j2 >/dev/null

LIB="$OUT/libzen_moonlight_test.so"
[ -f "$LIB" ] || { echo "build-moonlight-bridge-test: missing $LIB" >&2; exit 1; }
echo "$LIB"
