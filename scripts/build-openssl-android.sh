#!/usr/bin/env bash
# Build the pinned OpenSSL static prefix required by moonlight-common-c on Android.
#
# moonlight-common-c@62e06638 PlatformCrypto.c uses OpenSSL EVP; the Android NDK
# does not ship OpenSSL, so this script cross-builds the pinned release for the
# selected ABI(s) into app/modules/zen-remote-desktop/third_party/openssl/<abi>.
#
# Bounded by design: two parallel jobs, no source mutation, hash-verified input.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MODULE="$ROOT/app/modules/zen-remote-desktop"
CACHE="${ZEN_BUILD_TMPDIR:-${TMPDIR:-/tmp}}/zen-openssl-cache"
OPENSSL_VERSION="3.5.4"
OPENSSL_SHA256="967311f84955316969bdb1d8d4b983718ef42338639c621ec4c34fddef355e99"
OPENSSL_URL="https://www.openssl.org/source/openssl-${OPENSSL_VERSION}.tar.gz"

fail() {
  echo "build-openssl-android: $1" >&2
  exit 1
}

ABIS=()
ANDROID_API=24
while [ $# -gt 0 ]; do
  case "$1" in
    --abi) ABIS+=("$2"); shift 2 ;;
    --abis) IFS=',' read -r -a extra <<< "$2"; ABIS+=("${extra[@]}"); shift 2 ;;
    --api) ANDROID_API="$2"; shift 2 ;;
    *) fail "unknown argument: $1" ;;
  esac
done
[ ${#ABIS[@]} -gt 0 ] || ABIS=(arm64-v8a x86_64)

find_ndk() {
  if [ -n "${ANDROID_NDK_HOME:-}" ] && [ -d "$ANDROID_NDK_HOME" ]; then
    echo "$ANDROID_NDK_HOME"; return
  fi
  local sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Android/Sdk}}"
  [ -d "$sdk/ndk" ] || fail "NDK not found; set ANDROID_NDK_HOME"
  ls -1 "$sdk/ndk" | sort -V | tail -1 | sed "s#^#$sdk/ndk/#"
}

NDK="$(find_ndk)"
HOST_TAG="linux-x86_64"
[ "$(uname -s)" = "Darwin" ] && HOST_TAG="darwin-x86_64"
TOOLCHAIN="$NDK/toolchains/llvm/prebuilt/$HOST_TAG"
[ -x "$TOOLCHAIN/bin/clang" ] || fail "clang not found under $TOOLCHAIN"

mkdir -p "$CACHE"
ARCHIVE="$CACHE/openssl-${OPENSSL_VERSION}.tar.gz"
if [ ! -f "$ARCHIVE" ] || [ "$(sha256sum "$ARCHIVE" | cut -d' ' -f1)" != "$OPENSSL_SHA256" ]; then
  echo "fetching OpenSSL ${OPENSSL_VERSION}"
  curl -sSL --fail --max-time 180 -o "$ARCHIVE.part" "$OPENSSL_URL" || fail "download failed"
  echo "$OPENSSL_SHA256  $ARCHIVE.part" | sha256sum -c - >/dev/null || fail "sha256 mismatch"
  mv "$ARCHIVE.part" "$ARCHIVE"
fi

for abi in "${ABIS[@]}"; do
  case "$abi" in
    arm64-v8a) target="android-arm64" ;;
    x86_64) target="android-x86_64" ;;
    *) fail "unsupported abi: $abi (expected arm64-v8a or x86_64)" ;;
  esac
  prefix="$MODULE/third_party/openssl/$abi"
  work="$CACHE/build-$abi"
  echo "building OpenSSL ${OPENSSL_VERSION} for $abi -> $prefix"
  rm -rf "$work" "$prefix"
  mkdir -p "$work"
  tar -xzf "$ARCHIVE" -C "$work" --strip-components=1
  (
    cd "$work"
    export PATH="$TOOLCHAIN/bin:$PATH"
    export ANDROID_NDK_ROOT="$NDK"
    perl Configure "$target" -D__ANDROID_API__=$ANDROID_API \
      --prefix="$prefix" --openssldir="$prefix/ssl" \
      no-shared no-tests no-ui-console >/dev/null
    make -j2 build_libs >/dev/null
    make install_dev >/dev/null
  )
  [ -f "$prefix/lib/libcrypto.a" ] || fail "missing $prefix/lib/libcrypto.a"
  echo "$abi libcrypto.a sha256 $(sha256sum "$prefix/lib/libcrypto.a" | cut -d' ' -f1)"
done

echo "OpenSSL prefixes ready under $MODULE/third_party/openssl"
