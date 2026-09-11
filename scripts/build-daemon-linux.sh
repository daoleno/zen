#!/usr/bin/env bash
# Build release-shaped zen binaries for Linux amd64/arm64 and macOS arm64.
#
# Linux amd64 on a Linux amd64 host: CGO_ENABLED=1 -tags zen_desktop. Native
# capture/agent C is linked into that one zen ELF (GTK/GStreamer/X11/XTest as
# DT_NEEDED system libraries). This is not a second helper ELF or a Zen sidecar
# .so, and it does not compile C at end-user startup.
#
# Linux arm64 and Darwin stay CGO_ENABLED=0 daemon-only (no aarch64/macOS GTK
# sysroot from this recipe). Cross-building linux/amd64 from another host is
# also daemon-only; do not advertise desktop capture for those archives.
#
# Reads product version from app/app.base.json (expo.version) unless ZEN_VERSION is set.
# Does not publish, tag, sign Android APKs, or read release keystores.
#
# Usage:
#   ./scripts/build-daemon-linux.sh
#   ./scripts/build-daemon-linux.sh --out-dir dist-download/staging/bin
#   ZEN_VERSION=0.1.2 ./scripts/build-daemon-linux.sh

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

OUT_DIR=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out-dir)
      OUT_DIR="${2:?}"
      shift 2
      ;;
    -h|--help)
      sed -n '2,22p' "$0"
      exit 0
      ;;
    *)
      echo "error: unknown arg $1" >&2
      exit 2
      ;;
  esac
done

VERSION="${ZEN_VERSION:-}"
if [[ -z "$VERSION" ]]; then
  VERSION="$(python3 -c "import json;print(json.load(open('app/app.base.json'))['expo']['version'])")"
fi

if [[ -z "$OUT_DIR" ]]; then
  OUT_DIR="$ROOT/dist-download/staging/bin"
elif [[ "$OUT_DIR" != /* ]]; then
  OUT_DIR="$ROOT/$OUT_DIR"
fi
mkdir -p "$OUT_DIR"

export GOFLAGS="${GOFLAGS:-} -trimpath"
if [[ -n "${SOURCE_DATE_EPOCH:-}" ]]; then
  export GOSUMDB="${GOSUMDB:-sum.golang.org}"
fi

LDFLAGS="-s -w -buildid= -X main.Version=${VERSION}"
HOST_OS="$(uname -s)"
HOST_ARCH="$(uname -m)"

build_one() {
  local goos="$1"
  local goarch="$2"
  local out_name="$3"
  local cgo="$4"
  shift 4
  local out_path="$OUT_DIR/$out_name"
  echo "Building ${goos}/${goarch} CGO=${cgo} $* → ${out_path} (version=${VERSION})"
  (
    cd "$ROOT/daemon"
    CGO_ENABLED="$cgo" GOOS="$goos" GOARCH="$goarch" go build -buildvcs=false -ldflags="$LDFLAGS" "$@" -o "$out_path" ./cmd/zen/
  )
  chmod +x "$out_path"
}

# macOS archive and linux/arm64 from this amd64 host have no matching GTK
# sysroot, so they are explicit daemon-only binaries.
build_one linux arm64 "zen-linux-arm64" 0
build_one darwin arm64 "zen-darwin-arm64" 0

LINUX_AMD64_DESKTOP=0
if [[ "$HOST_OS" == Linux && "$HOST_ARCH" == x86_64 ]]; then
  if ! pkg-config --exists gtk+-3.0 gstreamer-app-1.0 gstreamer-video-1.0 x11 xtst gio-unix-2.0; then
    echo "error: Linux amd64 production recipe requires GTK3, GStreamer app/video, GIO Unix, X11 and XTest pkg-config modules" >&2
    exit 1
  fi
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
  CGO_CFLAGS="${CGO_CFLAGS:+$CGO_CFLAGS }-DZEN_NATIVE_BUILD_INPUT=h${TOKEN}" \
    build_one linux amd64 "zen-linux-amd64" 1 -tags zen_desktop
  if ! readelf -d "$OUT_DIR/zen-linux-amd64" | grep -q 'NEEDED.*libgtk-3'; then
    echo "error: zen-linux-amd64 is not a desktop-capable ELF (missing DT_NEEDED libgtk-3)" >&2
    exit 1
  fi
  if [[ -e "$OUT_DIR/libzen-desktop.so" ]]; then
    echo "error: production recipe must not emit libzen-desktop.so" >&2
    exit 1
  fi
  LINUX_AMD64_DESKTOP=1
else
  echo "note: host is ${HOST_OS}/${HOST_ARCH}; linux/amd64 is daemon-only (no native GTK sysroot)"
  build_one linux amd64 "zen-linux-amd64" 0
fi

echo ""
echo "VERSION=$VERSION"
echo "OUT_DIR=$OUT_DIR"
echo "LINUX_AMD64_DESKTOP=$LINUX_AMD64_DESKTOP"
ls -la "$OUT_DIR"/zen-linux-amd64 "$OUT_DIR"/zen-linux-arm64 "$OUT_DIR"/zen-darwin-arm64
echo "Done (no publish)."
