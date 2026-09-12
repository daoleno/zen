#!/usr/bin/env bash
# Prepare (never launch) the isolated end-to-end run for the paired Moonlight
# path. Verifies pinned artifacts and the private host boundary, then prints the
# exact argv/commands and the resource budget to submit for review.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MODULE="$ROOT/app/modules/zen-remote-desktop"
LOCK="$MODULE/native.lock.json"

fail() {
  echo "zen-desktop-e2e-prepare: $1" >&2
  exit 1
}

# 1. Pinned client sources (hash-verified by the fetch script).
[ -f "$LOCK" ] || fail "missing $LOCK"
"$ROOT/scripts/fetch-moonlight-common-c.sh" >/dev/null || fail "pinned client sources unavailable"
CLIENT_PIN=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['moonlight_common_c']['commit'])" "$LOCK")
APP_LAYER_PIN=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['application_layer']['commit'])" "$LOCK")
HOST_PIN=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['host']['commit'])" "$LOCK")
echo "client core pin:        $CLIENT_PIN"
echo "application layer pin:  $APP_LAYER_PIN"
echo "host (Sunshine) pin:    $HOST_PIN"

# 2. Pinned OpenSSL static prefixes required by the Android client.
for abi in arm64-v8a x86_64; do
  prefix="$MODULE/third_party/openssl/$abi"
  [ -f "$prefix/lib/libcrypto.a" ] || fail "missing OpenSSL prefix for $abi (run scripts/build-openssl-android.sh --abi $abi)"
  echo "openssl $abi libcrypto sha256: $(sha256sum "$prefix/lib/libcrypto.a" | cut -d' ' -f1)"
done

# 3. Reviewed Sunshine binary and its hash. The host is never started here.
SUNSHINE_BIN="${ZEN_SUNSHINE_BINARY:-}"
[ -n "$SUNSHINE_BIN" ] || fail "set ZEN_SUNSHINE_BINARY to the reviewed Sunshine executable"
[ -x "$SUNSHINE_BIN" ] || fail "ZEN_SUNSHINE_BINARY is not executable: $SUNSHINE_BIN"
echo "sunshine binary sha256: $(sha256sum "$SUNSHINE_BIN" | cut -d' ' -f1)"

# 4. Private state boundary. Existing dirs must be 0700 and owned by us.
STATE_DIR="${ZEN_SUNSHINE_STATE_DIR:-$HOME/.zen/desktop/sunshine}"
if [ -e "$STATE_DIR" ]; then
  mode=$(stat -c '%a' "$STATE_DIR")
  owner=$(stat -c '%u' "$STATE_DIR")
  [ "$mode" = "700" ] || fail "state dir $STATE_DIR mode is $mode (expected 700)"
  [ "$owner" = "$(id -u)" ] || fail "state dir $STATE_DIR not owned by the current user"
  echo "sunshine state dir:     $STATE_DIR (0700)"
else
  echo "sunshine state dir:     $STATE_DIR (absent; Zen creates it 0700 on first start)"
fi

# 5. Run plan. Nothing below is executed by this script.
cat <<EOF

=== RUN PLAN (execute only after Brain review) ===
Host ports (Sunshine base $((47989))):  TCP 47984 (HTTPS/pairing), TCP 47989 (HTTP), TCP 48010 (RTSP), UDP 47998-48010 (video/audio/control/input)

AVD leg (own x86_64, isolated network):
  1) cd app/android && ./gradlew :zen-remote-desktop:assembleDebug -PreactNativeArchitectures=x86_64
  2) emulator -avd <owned-avd> -no-snapshot -netdelay none -netspeed full
  3) adb install -r <same-signer debug apk>
  4) point the Zen remote-desktop client at the pinned Sunshine host; pair with the 4-digit PIN shown by the host
  5) evidence: logcat stages + connectionStarted/connectionStatusUpdate, one editor frame, one keyboard/pointer event, revoke observation
  6) cleanup: adb shell am force-stop <pkg>; emulator -avd <owned-avd> -no-window kill

VM leg (KDE KWin Wayland same-session check), only if host-side capture proof is required:
  1) create a NEW manifest (approved:false) reusing runner PSI/OOM/watchdog/1800s/ownership guards
  2) run the pinned Sunshine host inside the guest against the same session (SDDM -> same KDE, no new session)
  3) cleanup: stop Sunshine, shut down the guest, verify no stray processes

Combined budget proposal (coupled AVD+VM, needs explicit approval):
  host floor 8 GiB free; VM 4 vCPU / 6 GiB; AVD 2 vCPU / 2 GiB; peak ~10 GiB -> request >=12 GiB free
  sequential component runs are cheaper but do NOT substitute for one coupled E2E
EOF
