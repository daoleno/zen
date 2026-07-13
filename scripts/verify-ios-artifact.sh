#!/usr/bin/env bash
# Verify the shape and identity of an unsigned Simulator .app or signed IPA.

set -euo pipefail

MODE="${1:-}"
ARTIFACT="${2:-}"
EXPECTED_BUNDLE_ID="${ZEN_IOS_BUNDLE_ID:-com.daoleno.zen}"

usage() {
  echo "usage: $0 simulator path/to/Zen.app | ipa path/to/Zen.ipa" >&2
  exit 2
}

[[ "$MODE" == "simulator" || "$MODE" == "ipa" ]] || usage
[[ -n "$ARTIFACT" ]] || usage

verify_app() {
  local app="$1"
  local require_signature="$2"
  local plist="$app/Info.plist"
  [[ -d "$app" ]] || { echo "error: app bundle is missing: $app" >&2; exit 1; }
  [[ -f "$plist" ]] || { echo "error: Info.plist is missing: $plist" >&2; exit 1; }

  local bundle_id executable
  bundle_id="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$plist")"
  executable="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleExecutable' "$plist")"
  [[ "$bundle_id" == "$EXPECTED_BUNDLE_ID" ]] || {
    echo "error: bundle identifier is $bundle_id; expected $EXPECTED_BUNDLE_ID" >&2
    exit 1
  }
  [[ -x "$app/$executable" ]] || {
    echo "error: app executable is missing or not executable: $app/$executable" >&2
    exit 1
  }
  file "$app/$executable" | grep -q 'Mach-O' || {
    echo "error: app executable is not Mach-O" >&2
    exit 1
  }

  if [[ "$require_signature" == "yes" ]]; then
    [[ -f "$app/embedded.mobileprovision" ]] || {
      echo "error: signed IPA has no embedded provisioning profile" >&2
      exit 1
    }
    codesign --verify --deep --strict "$app"
  fi

  echo "ok: $bundle_id app bundle contains a Mach-O executable"
}

if [[ "$MODE" == "simulator" ]]; then
  verify_app "$ARTIFACT" no
  echo "iOS Simulator artifact verification passed"
  exit 0
fi

[[ -s "$ARTIFACT" ]] || { echo "error: IPA is missing or empty: $ARTIFACT" >&2; exit 1; }
unzip -tq "$ARTIFACT" >/dev/null
TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TEMP_DIR"' EXIT
unzip -q "$ARTIFACT" -d "$TEMP_DIR"
mapfile_supported=false
if builtin help mapfile >/dev/null 2>&1; then mapfile_supported=true; fi
if [[ "$mapfile_supported" == true ]]; then
  mapfile -t APPS < <(find "$TEMP_DIR/Payload" -mindepth 1 -maxdepth 1 -type d -name '*.app')
else
  APPS=()
  while IFS= read -r app; do APPS+=("$app"); done < <(find "$TEMP_DIR/Payload" -mindepth 1 -maxdepth 1 -type d -name '*.app')
fi
[[ ${#APPS[@]} -eq 1 ]] || {
  echo "error: IPA must contain exactly one Payload/*.app; found ${#APPS[@]}" >&2
  exit 1
}
verify_app "${APPS[0]}" yes
echo "signed iOS IPA verification passed"
