#!/bin/bash
# Build libghostty-vt for Android targets.
# Requires: zig (0.15.x), ghostty source at $GHOSTTY_SRC
#
# Usage: GHOSTTY_SRC=/path/to/ghostty ./scripts/build-libghostty.sh

set -euo pipefail

GHOSTTY_SRC="${GHOSTTY_SRC:-$HOME/workspace/ghostty}"
OUT_DIR="$(cd "$(dirname "$0")/../app/modules/zen-terminal-vt/libs/android" && pwd)"

if [ ! -f "$GHOSTTY_SRC/build.zig" ]; then
    echo "Error: Ghostty source not found at $GHOSTTY_SRC"
    echo "Set GHOSTTY_SRC to your ghostty checkout"
    exit 1
fi

echo "Building libghostty-vt from: $GHOSTTY_SRC"
echo "Output directory: $OUT_DIR"

TARGETS=(
    "aarch64-linux-android:arm64-v8a"
    "x86_64-linux-android:x86_64"
)

for entry in "${TARGETS[@]}"; do
    IFS=":" read -r zig_target abi <<< "$entry"
    echo ""
    echo "=== Building for $abi ($zig_target) ==="

    cd "$GHOSTTY_SRC"
    zig build -Demit-lib-vt=true -Doptimize=ReleaseFast "-Dtarget=$zig_target"

    mkdir -p "$OUT_DIR/$abi"
    cp zig-out/lib/libghostty-vt.so.* "$OUT_DIR/$abi/libghostty_vt.so"

    # Fix: copy the actual file, not the symlink
    if [ -L "$OUT_DIR/$abi/libghostty_vt.so" ]; then
        real=$(readlink -f "$OUT_DIR/$abi/libghostty_vt.so")
        rm "$OUT_DIR/$abi/libghostty_vt.so"
        cp "$real" "$OUT_DIR/$abi/libghostty_vt.so"
    fi

    echo "  -> $OUT_DIR/$abi/libghostty_vt.so ($(du -h "$OUT_DIR/$abi/libghostty_vt.so" | cut -f1))"
done

# Also copy headers
HEADER_DST="$(cd "$(dirname "$0")/../app/modules/zen-terminal-vt/android/src/main/cpp" && pwd)"
rm -rf "$HEADER_DST/ghostty"
cp -r "$GHOSTTY_SRC/include/ghostty" "$HEADER_DST/"
echo ""
echo "Headers copied to: $HEADER_DST/ghostty/"
echo "Done!"
