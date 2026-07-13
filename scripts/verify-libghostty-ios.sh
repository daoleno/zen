#!/usr/bin/env bash
# Verify that the pinned iOS libghostty-vt XCFramework contains both the
# arm64 device and Apple Silicon Simulator slices required by Zen.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/app/modules/zen-terminal-vt/native.lock.json"
OUT_DIR="$ROOT/app/modules/zen-terminal-vt/libs/ios"

eval "$(python3 - "$LOCK" <<'PY'
import json, shlex, sys
lock = json.load(open(sys.argv[1]))
for key, value in {
    "GHOSTTY_COMMIT": lock["ghostty"]["commit"],
    "XCFRAMEWORK_NAME": lock["ios"]["xcframework_name"],
}.items():
    print(f"{key}={shlex.quote(str(value))}")
PY
)"

FRAMEWORK="$OUT_DIR/$XCFRAMEWORK_NAME"
PLIST="$FRAMEWORK/Info.plist"
SUMS="$OUT_DIR/SHA256SUMS"
MANIFEST="$OUT_DIR/build-manifest.json"

[[ -f "$PLIST" ]] || { echo "FAIL: missing $PLIST" >&2; exit 1; }
[[ -f "$SUMS" ]] || { echo "FAIL: missing $SUMS" >&2; exit 1; }
[[ -f "$MANIFEST" ]] || { echo "FAIL: missing $MANIFEST" >&2; exit 1; }
[[ -f "$OUT_DIR/GHOSTTY-MIT.txt" ]] || { echo "FAIL: missing Ghostty notice" >&2; exit 1; }

python3 - "$PLIST" <<'PY'
import plistlib, sys
with open(sys.argv[1], "rb") as stream:
    plist = plistlib.load(stream)
slices = plist.get("AvailableLibraries", [])
device = any(
    item.get("SupportedPlatform") == "ios"
    and not item.get("SupportedPlatformVariant")
    and "arm64" in item.get("SupportedArchitectures", [])
    for item in slices
)
simulator = any(
    item.get("SupportedPlatform") == "ios"
    and item.get("SupportedPlatformVariant") == "simulator"
    and "arm64" in item.get("SupportedArchitectures", [])
    for item in slices
)
assert device, "missing arm64 iOS device slice"
assert simulator, "missing arm64 iOS Simulator slice"
print("ok: XCFramework contains arm64 iOS device and Simulator slices")
PY

(
  cd "$OUT_DIR"
  shasum -a 256 -c SHA256SUMS
)

python3 - "$MANIFEST" "$GHOSTTY_COMMIT" <<'PY'
import json, sys
manifest = json.load(open(sys.argv[1]))
assert manifest["ghostty_commit"] == sys.argv[2]
assert manifest["release_grade"] is True
print("ok: manifest matches pinned Ghostty commit")
PY

find "$FRAMEWORK" -path '*/Headers/ghostty/vt.h' -print -quit | grep -q . \
  || { echo "FAIL: XCFramework is missing ghostty/vt.h" >&2; exit 1; }
find "$FRAMEWORK" -path '*/Headers/module.modulemap' -print -quit | grep -q . \
  || { echo "FAIL: XCFramework is missing module.modulemap" >&2; exit 1; }

echo "iOS libghostty-vt verification passed"
