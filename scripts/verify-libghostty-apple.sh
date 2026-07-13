#!/usr/bin/env bash
# Validate the Apple Ghostty VT artifact shape without linking it into Zen.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/app/modules/zen-terminal-vt/native.lock.json"
DEFAULT_PATH="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["apple"]["output"])' "$LOCK")"
XCFRAMEWORK="${1:-$ROOT/$DEFAULT_PATH}"
APPLE_DIR="$(dirname "$XCFRAMEWORK")"

[[ -d "$XCFRAMEWORK" ]] || { echo "error: missing XCFramework: $XCFRAMEWORK" >&2; exit 1; }
[[ -f "$XCFRAMEWORK/Info.plist" ]] || { echo "error: missing Info.plist" >&2; exit 1; }
command -v plutil >/dev/null 2>&1 || { echo "error: plutil is required" >&2; exit 1; }

python3 - "$XCFRAMEWORK/Info.plist" "$XCFRAMEWORK" <<'PY'
import plistlib, pathlib, sys
with open(sys.argv[1], "rb") as fh:
    plist = plistlib.load(fh)
root = pathlib.Path(sys.argv[2])
libs = plist.get("AvailableLibraries") or []
device = [x for x in libs if x.get("SupportedPlatform") == "ios" and not x.get("SupportedPlatformVariant")]
sim = [x for x in libs if x.get("SupportedPlatform") == "ios" and x.get("SupportedPlatformVariant") == "simulator"]
assert device, "missing iOS device slice"
assert sim, "missing iOS simulator slice"
assert any("arm64" in x.get("SupportedArchitectures", []) for x in device), "device arm64 missing"
assert any("arm64" in x.get("SupportedArchitectures", []) for x in sim), "simulator arm64 missing"
for entry in device + sim:
    assert entry.get("HeadersPath"), f"headers missing from {entry}"
    slice_root = root / entry["LibraryIdentifier"]
    assert (slice_root / entry["LibraryPath"]).is_file(), f"library file missing from {entry}"
    assert (slice_root / entry["HeadersPath"] / "ghostty" / "vt.h").is_file(), f"vt.h missing from {entry}"
print("ok: iOS arm64 device and simulator slices with headers")
PY

[[ -f "$APPLE_DIR/build-manifest.json" ]] || { echo "error: missing Apple build manifest" >&2; exit 1; }
[[ -f "$APPLE_DIR/SHA256SUMS" ]] || { echo "error: missing Apple SHA256SUMS" >&2; exit 1; }
(
  cd "$APPLE_DIR"
  shasum -a 256 -c SHA256SUMS >/dev/null
)
python3 - "$LOCK" "$APPLE_DIR/build-manifest.json" <<'PY'
import json, sys
lock = json.load(open(sys.argv[1]))
manifest = json.load(open(sys.argv[2]))
assert manifest["component"] == lock["ghostty"]["component"]
assert manifest["ghostty_commit_pinned"] == lock["ghostty"]["commit"]
assert manifest["ghostty_commit_resolved"] == lock["ghostty"]["commit"]
assert manifest["zig_version"].startswith(lock["zig"]["version"])
assert manifest["headers_sha256"] == lock["ghostty"]["headers_sha256"]
assert manifest["release_grade"] is True
print("ok: Apple provenance, pin, and checksums")
PY
