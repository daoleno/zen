#!/usr/bin/env bash
# Fetch and verify the pinned moonlight-common-c sources (core + enet + nanors).
#
# Sources land inside app/modules/zen-remote-desktop/third_party so the module's
# CMake subdirectory layout matches upstream's submodule paths. Every archive is
# hash-pinned in native.lock.json; no opaque prebuilt binary is downloaded.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MODULE="$ROOT/app/modules/zen-remote-desktop"
LOCK="$MODULE/native.lock.json"
THIRD_PARTY="$MODULE/third_party"
CACHE="${ZEN_BUILD_TMPDIR:-${TMPDIR:-/tmp}}/zen-moonlight-cache"

fail() {
  echo "fetch-moonlight-common-c: $1" >&2
  exit 1
}

[ -f "$LOCK" ] || fail "missing $LOCK"

field() {
  python3 -c "import json,sys; print(json.load(open(sys.argv[1]))$2)" "$LOCK"
}

fetch_one() {
  # $1 component key, $2 archive url, $3 sha256, $4 destination
  local key="$1" url="$2" digest="$3" dest="$4"
  local archive="$CACHE/$(basename "$url")"
  mkdir -p "$CACHE"
  if [ ! -f "$archive" ] || [ "$(sha256sum "$archive" | cut -d' ' -f1)" != "$digest" ]; then
    echo "fetching $key -> $archive"
    curl -sSL --fail --max-time 180 -o "$archive.part" "$url" || fail "download failed: $url"
    echo "$digest  $archive.part" | sha256sum -c - >/dev/null || fail "sha256 mismatch for $url"
    mv "$archive.part" "$archive"
  fi
  rm -rf "$dest"
  mkdir -p "$dest"
  tar -xzf "$archive" -C "$dest" --strip-components=1
}

MC_URL="$(field '' "['moonlight_common_c']['archive']")"
MC_SHA="$(field '' "['moonlight_common_c']['archive_sha256']")"
ENET_URL="$(field '' "['enet']['archive']")"
ENET_SHA="$(field '' "['enet']['archive_sha256']")"
NANORS_URL="$(field '' "['nanors']['archive']")"
NANORS_SHA="$(field '' "['nanors']['archive_sha256']")"

mkdir -p "$THIRD_PARTY"

# moonlight-common-c first; enet and nanors are extracted inside it so upstream's
# add_subdirectory(enet) and nanors/rs.c source list resolve unchanged.
fetch_one moonlight-common-c "$MC_URL" "$MC_SHA" "$THIRD_PARTY/moonlight-common-c"
fetch_one enet "$ENET_URL" "$ENET_SHA" "$THIRD_PARTY/moonlight-common-c/enet"
fetch_one nanors "$NANORS_URL" "$NANORS_SHA" "$THIRD_PARTY/moonlight-common-c/nanors"

echo "pinned sources ready under $THIRD_PARTY"
echo "licenses: moonlight-common-c GPL-3.0, enet MIT, nanors MIT"
echo "corresponding source and reproducible build notes: app/modules/zen-remote-desktop/THIRD_PARTY.md"
