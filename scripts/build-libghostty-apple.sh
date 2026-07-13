#!/usr/bin/env bash
# Build the pinned Ghostty VT XCFramework for iOS device + simulator.
# This prepares an input artifact; it does not enable Zen Terminal on iOS.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/app/modules/zen-terminal-vt/native.lock.json"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "error: Apple libghostty builds require macOS with Xcode" >&2
  exit 1
fi

for tool in git python3 xcodebuild xcrun; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "error: required tool not found: $tool" >&2
    exit 1
  }
done

eval "$(python3 - "$LOCK" <<'PY'
import json, shlex, sys
lock = json.load(open(sys.argv[1]))
values = {
    "GHOSTTY_REPO": lock["ghostty"]["repository"],
    "GHOSTTY_COMMIT": lock["ghostty"]["commit"],
    "ZIG_VERSION": lock["zig"]["version"],
    "APPLE_OUTPUT": lock["apple"]["output"],
    "HEADERS_SHA256": lock["ghostty"]["headers_sha256"],
}
for key, value in values.items():
    print(f"{key}={shlex.quote(str(value))}")
PY
)"

ZIG_BIN="${ZIG_BIN:-zig}"
command -v "$ZIG_BIN" >/dev/null 2>&1 || {
  echo "error: zig not found (need $ZIG_VERSION)" >&2
  exit 1
}
case "$("$ZIG_BIN" version)" in
  "$ZIG_VERSION"|"$ZIG_VERSION".*) ;;
  *) echo "error: zig version mismatch; need $ZIG_VERSION" >&2; exit 1 ;;
esac

for sdk in iphoneos iphonesimulator; do
  xcrun --sdk "$sdk" --show-sdk-path >/dev/null || {
    echo "error: Xcode SDK unavailable: $sdk" >&2
    exit 1
  }
done

GHOSTTY_SRC="${GHOSTTY_SRC:-${ZEN_GHOSTTY_CACHE:-$HOME/Library/Caches/zen/ghostty}}"
if [[ ! -d "$GHOSTTY_SRC/.git" ]]; then
  mkdir -p "$(dirname "$GHOSTTY_SRC")"
  git clone --filter=blob:none "$GHOSTTY_REPO" "$GHOSTTY_SRC"
fi
git -C "$GHOSTTY_SRC" fetch --depth=1 origin "$GHOSTTY_COMMIT"
git -C "$GHOSTTY_SRC" checkout --detach "$GHOSTTY_COMMIT"
if [[ -n "$(git -C "$GHOSTTY_SRC" status --porcelain)" ]]; then
  echo "error: Ghostty source is dirty: $GHOSTTY_SRC" >&2
  exit 1
fi
ACTUAL_HEADERS_SHA256="$(shasum -a 256 "$GHOSTTY_SRC/include/ghostty/vt.h" | awk '{print $1}')"
if [[ "$ACTUAL_HEADERS_SHA256" != "$HEADERS_SHA256" ]]; then
  echo "error: upstream vt.h sha256 $ACTUAL_HEADERS_SHA256 != lock $HEADERS_SHA256" >&2
  exit 1
fi

(
  cd "$GHOSTTY_SRC"
  "$ZIG_BIN" build \
    -Demit-lib-vt=true \
    -Demit-xcframework=true \
    -Doptimize=ReleaseFast
)

SOURCE="$GHOSTTY_SRC/zig-out/lib/ghostty-vt.xcframework"
DEST="$ROOT/$APPLE_OUTPUT"
if [[ ! -d "$SOURCE" ]]; then
  echo "error: expected XCFramework was not produced: $SOURCE" >&2
  exit 1
fi
rm -rf "$DEST"
mkdir -p "$(dirname "$DEST")"
cp -R "$SOURCE" "$DEST"
cp -f "$ROOT/app/assets/notices/GHOSTTY-MIT.txt" "$(dirname "$DEST")/GHOSTTY-MIT.txt"

APPLE_DIR="$(dirname "$DEST")"
python3 - "$APPLE_DIR/build-manifest.json" "$GHOSTTY_COMMIT" "$ZIG_VERSION" "$HEADERS_SHA256" "$(xcodebuild -version | tr '\n' ' ')" <<'PY'
import json, sys
path, commit, zig, headers_sha, xcode = sys.argv[1:]
with open(path, "w", encoding="utf-8") as fh:
    json.dump({
        "schema_version": 1,
        "component": "libghostty-vt",
        "ghostty_commit_pinned": commit,
        "ghostty_commit_resolved": commit,
        "zig_version": zig,
        "xcode_version": xcode.strip(),
        "headers_sha256": headers_sha,
        "release_grade": True,
        "slices": ["ios-arm64", "ios-arm64-simulator"],
    }, fh, indent=2, sort_keys=True)
    fh.write("\n")
PY
(
  cd "$APPLE_DIR"
  find ghostty-vt.xcframework -type f -print | LC_ALL=C sort | xargs shasum -a 256 > SHA256SUMS
  shasum -a 256 GHOSTTY-MIT.txt build-manifest.json >> SHA256SUMS
)

"$ROOT/scripts/verify-libghostty-apple.sh" "$DEST"
