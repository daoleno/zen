#!/usr/bin/env bash
# Build a local zen ELF. On Linux, include desktop native roles when pkg-config
# finds GTK/GStreamer/X11. This does not extract a second helper or compile C
# at end-user startup. Cross-release CGO_ENABLED=0 archives stay daemon-only.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:-$ROOT/bin/zen}"
if [[ "$OUT" != /* ]]; then
  OUT="$ROOT/$OUT"
fi
mkdir -p "$(dirname "$OUT")"

cd "$ROOT/daemon"
ARGS=(-o "$OUT")
if [[ "$(uname -s)" == Linux ]] && pkg-config --exists gtk+-3.0 gstreamer-app-1.0 gstreamer-video-1.0 x11 xtst gio-unix-2.0; then
  export CGO_ENABLED=1
  ARGS+=(-tags zen_desktop)
  echo "Building desktop-capable zen (CGO, zen_desktop) → $OUT"
else
  echo "Building zen without desktop native → $OUT"
fi
go build "${ARGS[@]}" ./cmd/zen
chmod +x "$OUT"
