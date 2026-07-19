#!/usr/bin/env bash
# Cross-compile the zen daemon for Linux amd64/arm64 and macOS arm64.
#
# Reads product version from app/app.base.json (expo.version) unless ZEN_VERSION is set.
# Does not publish, tag, sign Android APKs, or read release keystores.
#
# Usage:
#   ./scripts/build-daemon-linux.sh
#   ./scripts/build-daemon-linux.sh --out-dir dist-download/staging/bin
#   ZEN_VERSION=0.1.0-beta.1 ./scripts/build-daemon-linux.sh

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
      sed -n '2,14p' "$0"
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

# Deterministic-ish Go builds for release staging (no VCS stamp, trim paths).
export CGO_ENABLED=0
export GOFLAGS="${GOFLAGS:-} -trimpath"
# Allow callers to pin timestamps for more reproducible archives.
if [[ -n "${SOURCE_DATE_EPOCH:-}" ]]; then
  export GOSUMDB="${GOSUMDB:-sum.golang.org}"
fi

LDFLAGS="-s -w -buildid= -X main.Version=${VERSION}"

build_one() {
  local goos="$1"
  local goarch="$2"
  local out_name="$3"
  local out_path="$OUT_DIR/$out_name"
  echo "Building ${goos}/${goarch} → ${out_path} (version=${VERSION})"
  (
    cd "$ROOT/daemon"
    GOOS="$goos" GOARCH="$goarch" go build -buildvcs=false -ldflags="$LDFLAGS" -o "$out_path" ./cmd/zen/
  )
  chmod +x "$out_path"
}

build_one linux amd64 "zen-linux-amd64"
build_one linux arm64 "zen-linux-arm64"
build_one darwin arm64 "zen-darwin-arm64"

echo ""
echo "VERSION=$VERSION"
echo "OUT_DIR=$OUT_DIR"
ls -la "$OUT_DIR"/zen-linux-amd64 "$OUT_DIR"/zen-linux-arm64 "$OUT_DIR"/zen-darwin-arm64
echo "Done (no publish)."
