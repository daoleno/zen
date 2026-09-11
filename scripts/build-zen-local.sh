#!/usr/bin/env bash
# Build a local zen ELF. On Linux, link desktop native roles into that ELF when
# pkg-config finds GTK/GStreamer/X11. This does not extract a second helper,
# emit libzen-desktop.so, or compile C at end-user startup.
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
  TOKEN="$(python3 - "$ROOT/daemon/desktop/native" <<'PY'
import hashlib, pathlib, sys
root = pathlib.Path(sys.argv[1])
names = [
    "encoder.c", "encoder.h", "host-agent.c", "linux.c",
    "portal.c", "portal.h", "portal_capture.c", "portal_capture.h",
]
digest = hashlib.sha256()
for name in names:
    digest.update(name.encode() + b"\n")
    digest.update((root / name).read_bytes())
    digest.update(b"\n")
print(digest.hexdigest()[:16])
PY
)"
  export CGO_CFLAGS="${CGO_CFLAGS:+$CGO_CFLAGS }-DZEN_NATIVE_BUILD_INPUT=h${TOKEN}"
  echo "Building desktop-capable zen (CGO, zen_desktop, native input h${TOKEN}) → $OUT"
else
  echo "Building zen without desktop native → $OUT"
fi
go build "${ARGS[@]}" ./cmd/zen
chmod +x "$OUT"

if [[ -e "$(dirname "$OUT")/libzen-desktop.so" ]]; then
  echo "error: local recipe must not emit libzen-desktop.so" >&2
  exit 1
fi
