#!/usr/bin/env bash
# Sign the exact release manifest bytes with the offline Zen updater key.
#
# The private key comes from either ZEN_UPDATE_SIGNING_KEY (a PEM path) or
# ZEN_UPDATE_SIGNING_KEY_BASE64 (a base64-encoded PEM, intended for CI).

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MANIFEST="${1:-}"
if [[ -z "$MANIFEST" || ! -f "$MANIFEST" ]]; then
  echo "usage: $0 path/to/release-manifest.json" >&2
  exit 2
fi

PUBLIC_KEY="$ROOT/release/zen-update-public-key.pem"
test -f "$PUBLIC_KEY"

TEMP_KEY=""
cleanup() {
  if [[ -n "$TEMP_KEY" && -f "$TEMP_KEY" ]]; then
    shred -u "$TEMP_KEY" 2>/dev/null || rm -f "$TEMP_KEY"
  fi
}
trap cleanup EXIT

KEY="${ZEN_UPDATE_SIGNING_KEY:-}"
if [[ -z "$KEY" ]]; then
  if [[ -z "${ZEN_UPDATE_SIGNING_KEY_BASE64:-}" ]]; then
    echo "error: set ZEN_UPDATE_SIGNING_KEY or ZEN_UPDATE_SIGNING_KEY_BASE64" >&2
    exit 1
  fi
  TEMP_KEY="$(mktemp "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/zen-update-key.XXXXXX")"
  chmod 600 "$TEMP_KEY"
  printf '%s' "$ZEN_UPDATE_SIGNING_KEY_BASE64" | base64 --decode > "$TEMP_KEY"
  KEY="$TEMP_KEY"
fi
if [[ ! -f "$KEY" ]]; then
  echo "error: update signing key path does not exist" >&2
  exit 1
fi

expected="$(openssl pkey -pubin -in "$PUBLIC_KEY" -outform DER | base64 | tr -d '\n')"
actual="$(openssl pkey -in "$KEY" -pubout -outform DER 2>/dev/null | base64 | tr -d '\n')"
if [[ -z "$actual" || "$actual" != "$expected" ]]; then
  echo "error: update signing key does not match release/zen-update-public-key.pem" >&2
  exit 1
fi

SIGNATURE="${MANIFEST}.sig"
openssl pkeyutl -sign -rawin -inkey "$KEY" -in "$MANIFEST" -out "$SIGNATURE"
openssl pkeyutl -verify -rawin -pubin -inkey "$PUBLIC_KEY" \
  -in "$MANIFEST" -sigfile "$SIGNATURE" >/dev/null
test "$(wc -c < "$SIGNATURE")" -eq 64
echo "Signed updater manifest: $SIGNATURE"
