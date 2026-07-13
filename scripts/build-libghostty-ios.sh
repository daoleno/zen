#!/usr/bin/env bash
# Build the pinned libghostty-vt XCFramework for arm64 iOS devices and
# Apple Silicon iOS Simulator. The output is consumed by ZenTerminalVt.podspec.

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/app/modules/zen-terminal-vt/native.lock.json"
OUT_DIR="$ROOT/app/modules/zen-terminal-vt/libs/ios"
NOTICE_SRC="$ROOT/app/assets/notices/GHOSTTY-MIT.txt"
TOOL_CACHE="${ZEN_TOOL_CACHE:-$ROOT/.cache/zen-tools}"
GHOSTTY_CACHE="${ZEN_GHOSTTY_CACHE:-$ROOT/.cache/zen/ghostty}"

eval "$(python3 - "$LOCK" <<'PY'
import json, shlex, sys
lock = json.load(open(sys.argv[1]))
values = {
    "GHOSTTY_REPO": lock["ghostty"]["repository"],
    "GHOSTTY_COMMIT": lock["ghostty"]["commit"],
    "ZIG_VERSION": lock["zig"]["version"],
    "ZIG_URL": lock["zig"]["downloads"]["aarch64-macos"]["tarball"],
    "ZIG_SHA256": lock["zig"]["downloads"]["aarch64-macos"]["sha256"],
    "ZIG_ARCHIVE_ROOT": lock["zig"]["downloads"]["aarch64-macos"]["archive_root"],
    "XCFRAMEWORK_NAME": lock["ios"]["xcframework_name"],
    "UPSTREAM_XCFRAMEWORK_NAME": lock["ios"]["upstream_xcframework_name"],
}
for key, value in values.items():
    print(f"{key}={shlex.quote(str(value))}")
PY
)"

if ! xcodebuild -version >/dev/null 2>&1; then
  echo "error: full Xcode is required; install Xcode and select it with:" >&2
  echo "       sudo xcode-select --switch /Applications/Xcode.app/Contents/Developer" >&2
  exit 1
fi

resolve_zig() {
  local candidate="${ZIG_BIN:-zig}"
  if command -v "$candidate" >/dev/null 2>&1; then
    local version
    version="$("$candidate" version)"
    if [[ "$version" == "$ZIG_VERSION" || "$version" == "$ZIG_VERSION".* ]]; then
      command -v "$candidate"
      return
    fi
  fi

  local install_dir="$TOOL_CACHE/$ZIG_ARCHIVE_ROOT"
  local archive="$TOOL_CACHE/${ZIG_URL##*/}"
  mkdir -p "$TOOL_CACHE"
  if [[ ! -x "$install_dir/zig" ]]; then
    echo "Downloading pinned Zig $ZIG_VERSION..." >&2
    curl -fL "$ZIG_URL" -o "$archive"
    local actual
    actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
    if [[ "$actual" != "$ZIG_SHA256" ]]; then
      echo "error: Zig archive checksum mismatch" >&2
      exit 1
    fi
    tar -xf "$archive" -C "$TOOL_CACHE"
  fi
  echo "$install_dir/zig"
}

ZIG_BIN="$(resolve_zig)"

prepare_macos_sdk_for_zig() {
  local sdk
  sdk="$(realpath "$(xcrun --sdk macosx --show-sdk-path)")"
  local libsystem="$sdk/usr/lib/libSystem.B.tbd"

  if grep -m1 '^targets:' "$libsystem" | grep -q 'arm64-macos'; then
    echo "$sdk"
    return
  fi
  if ! grep -m1 '^targets:' "$libsystem" | grep -q 'arm64e-macos'; then
    echo "error: unsupported libSystem target list in $libsystem" >&2
    exit 1
  fi

  # Xcode 26.6's macOS 26.5 SDK advertises the top-level libSystem stub as
  # arm64e-only. Apple's linker accepts it for arm64 hosts, while Zig 0.15.2
  # intentionally requires an exact target match. Keep Xcode untouched and
  # give Zig a copy-on-write SDK clone with arm64 added to those target lists.
  local xcode_build
  xcode_build="$(xcodebuild -version | awk '/Build version/ {print $3}')"
  local compat_sdk="$TOOL_CACHE/macos-sdk-$xcode_build-arm64"
  local marker="$compat_sdk/.zen-zig-arm64-libsystem"
  if [[ ! -f "$marker" ]]; then
    echo "Preparing Zig-compatible macOS SDK clone for Xcode $xcode_build..." >&2
    rm -rf "$compat_sdk"
    mkdir -p "$(dirname "$compat_sdk")"
    cp -cR "$sdk" "$compat_sdk"
    python3 - "$compat_sdk/usr/lib/libSystem.B.tbd" <<'PY'
import pathlib, sys
path = pathlib.Path(sys.argv[1])
text = path.read_text()
text = text.replace("arm64e-macos", "arm64-macos, arm64e-macos")
text = text.replace("arm64e-maccatalyst", "arm64-maccatalyst, arm64e-maccatalyst")
path.write_text(text)
PY
    touch "$marker"
  fi
  echo "$compat_sdk"
}

MACOS_SDKROOT="$(prepare_macos_sdk_for_zig)"

GHOSTTY_SRC="${GHOSTTY_SRC:-$GHOSTTY_CACHE}"
if [[ ! -d "$GHOSTTY_SRC/.git" ]]; then
  mkdir -p "$(dirname "$GHOSTTY_SRC")"
  git clone --filter=blob:none "$GHOSTTY_REPO" "$GHOSTTY_SRC"
fi

CURRENT="$(git -C "$GHOSTTY_SRC" rev-parse HEAD)"
if [[ "$CURRENT" != "$GHOSTTY_COMMIT" ]]; then
  git -C "$GHOSTTY_SRC" fetch --depth=1 origin "$GHOSTTY_COMMIT" \
    || git -C "$GHOSTTY_SRC" fetch origin "$GHOSTTY_COMMIT"
  git -C "$GHOSTTY_SRC" checkout --detach "$GHOSTTY_COMMIT"
fi

if [[ -n "$(git -C "$GHOSTTY_SRC" status --porcelain)" ]]; then
  echo "error: Ghostty checkout is dirty: $GHOSTTY_SRC" >&2
  exit 1
fi

echo "Building libghostty-vt XCFramework"
echo "  ghostty: $GHOSTTY_SRC @ $GHOSTTY_COMMIT"
echo "  zig:     $($ZIG_BIN version)"

(
  cd "$GHOSTTY_SRC"
  PATH="$ROOT/scripts/zig-xcode-compat:$PATH" \
    ZEN_MACOS_SDKROOT="$MACOS_SDKROOT" \
    "$ZIG_BIN" build -Demit-lib-vt=true -Doptimize=ReleaseFast
)

SOURCE="$GHOSTTY_SRC/zig-out/lib/$UPSTREAM_XCFRAMEWORK_NAME"
DESTINATION="$OUT_DIR/$XCFRAMEWORK_NAME"
if [[ ! -d "$SOURCE" ]]; then
  echo "error: expected XCFramework was not generated: $SOURCE" >&2
  exit 1
fi

mkdir -p "$OUT_DIR"
rm -rf "$DESTINATION"
ditto "$SOURCE" "$DESTINATION"
cp "$NOTICE_SRC" "$OUT_DIR/GHOSTTY-MIT.txt"

(
  cd "$OUT_DIR"
  find "$XCFRAMEWORK_NAME" -type f -print0 \
    | sort -z \
    | xargs -0 shasum -a 256 > SHA256SUMS
)

python3 - "$LOCK" "$DESTINATION" "$OUT_DIR/build-manifest.json" <<'PY'
import json, pathlib, platform, subprocess, sys
lock = json.load(open(sys.argv[1]))
framework = pathlib.Path(sys.argv[2])
manifest = {
    "schema_version": 1,
    "ghostty_commit": lock["ghostty"]["commit"],
    "zig_version": lock["zig"]["version"],
    "host": platform.platform(),
    "xcframework": framework.name,
    "files": sum(1 for path in framework.rglob("*") if path.is_file()),
    "release_grade": True,
}
pathlib.Path(sys.argv[3]).write_text(json.dumps(manifest, indent=2) + "\n")
PY

"$ROOT/scripts/verify-libghostty-ios.sh"
echo "iOS libghostty-vt build complete: $DESTINATION"
