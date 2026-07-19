#!/usr/bin/env bash
# Assert expo-camera barcode companion pods were linked after `pod install`.
# Product intent is barcodeScannerEnabled:true in app/app.base.json.
#
# Usage:
#   ./scripts/assert-ios-camera-barcode-pod.sh [path/to/Podfile.lock]

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="${1:-$ROOT/app/ios/Podfile.lock}"
PROPS="$(dirname "$LOCK")/Podfile.properties.json"

if [[ ! -f "$LOCK" ]]; then
  echo "error: Podfile.lock missing at $LOCK (run expo prebuild + pod install first)" >&2
  exit 1
fi

if [[ -f "$PROPS" ]]; then
  if python3 - "$PROPS" <<'PY'
import json, sys
props = json.load(open(sys.argv[1], encoding="utf-8"))
value = props.get("expo.camera.barcode-scanner-enabled")
sys.exit(0 if str(value).lower() == "false" else 1)
PY
  then
    echo "error: expo.camera.barcode-scanner-enabled=false in $PROPS" >&2
    echo "expected barcode scanning enabled so ExpoCameraBarcodeScanning + ZXingObjC link" >&2
    exit 1
  fi
fi

if ! grep -q 'ExpoCameraBarcodeScanning' "$LOCK"; then
  echo "error: ExpoCameraBarcodeScanning was not linked in $LOCK" >&2
  echo "expected app/app.base.json expo-camera barcodeScannerEnabled:true to keep the companion pod" >&2
  exit 1
fi

if ! grep -q 'ZXingObjC' "$LOCK"; then
  echo "error: ZXingObjC was not linked in $LOCK" >&2
  echo "expected expo-camera barcode scanning to pull ZXingObjC with ExpoCameraBarcodeScanning" >&2
  exit 1
fi

echo "ok: ExpoCameraBarcodeScanning and ZXingObjC linked in $(basename "$LOCK")"
