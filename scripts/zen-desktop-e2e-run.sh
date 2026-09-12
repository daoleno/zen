#!/usr/bin/env bash
# Guarded run procedure for the coupled Moonlight E2E.
#
# Fail-closed: it refuses to start anything unless the manifest is approved,
# every artifact hash matches bytes, the reviewed AVD/ADB/QEMU prerequisites
# exist, and the resource guards pass. While the manifest is approved=false it
# only reports the prerequisite state and exits.
#
# The guest provisioning/boot/test-driver steps are intentionally not wired in
# this revision: the script stops with `guest_procedure_unimplemented` instead of
# booting an unreviewed scenario or pretending to run the E2E.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MANIFEST="${ZEN_E2E_MANIFEST:-/home/daoleno/.zen/artifacts/2026-09-13-desktop-moonlight-e2e/manifest.json}"

die() {
  echo "zen-desktop-e2e-run: $1" >&2
  exit 1
}

json() {
  python3 -c "import json,sys; print(json.load(open(sys.argv[1]))$1)" "$MANIFEST"
}

[ -f "$MANIFEST" ] || die "missing manifest $MANIFEST"

APPROVED="$(json "['approved']")"
if [ "$APPROVED" != "True" ]; then
  echo "zen-desktop-e2e-run: manifest approved=false; no process started"
  echo "review prerequisites with: scripts/zen-desktop-e2e-validate.sh --launch-ready"
  exit 0
fi

# --- prerequisites (bytes, not manifest strings) ---------------------------
APK_PATH="$(json "['apk']['path']")"
APK_SHA="$(json "['apk']['sha256']")"
[ -n "$APK_SHA" ] || die "apk.sha256 missing; build and record after approval"
[ -f "$ROOT/$APK_PATH" ] || die "missing APK $APK_PATH"
[ "$(sha256sum "$ROOT/$APK_PATH" | cut -d' ' -f1)" = "$APK_SHA" ] || die "APK hash mismatch"

SUNSHINE_SHA="$(json "['vm']['sunshine']['binary_sha256']")"
[ -n "$SUNSHINE_SHA" ] || die "guest Sunshine hash missing; build closure not recorded"

ADB="$(json "['pins']['adb']['path']")"
EMULATOR="$(json "['pins']['emulator']['path']")"
QEMU="$(json "['pins']['qemu_binary']['path']")"
AVD="$(json "['pins']['avd']['name']")"
SERIAL="$(json "['pins']['avd']['serial']")"
ADB_PORT="$(json "['pins']['adb']['server_port']")"
[ -x "$ADB" ] && [ -f "$EMULATOR" ] && [ -f "$QEMU" ] || die "owned adb/emulator/qemu missing"
[ -f "$HOME/.android/avd/$AVD.ini" ] || die "owned AVD $AVD missing"

# --- resource guards (measured, before any launch) -------------------------
FLOOR_MIB="$(json "['guards']['memory_floor_mib']")"
PEAK_MIB="$(json "['budget']['combined_peak_upper_bound_mib']")"
AVAILABLE_MIB="$(awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo)"
[ "$AVAILABLE_MIB" -ge $((FLOOR_MIB + PEAK_MIB)) ] || die "MemAvailable ${AVAILABLE_MIB}MiB below floor+peak $((FLOOR_MIB + PEAK_MIB))MiB"

PSI="/sys/fs/cgroup/$(cat /proc/self/cgroup | sed -n 's/^0:://p')/memory.pressure"
if [ -f "$PSI" ]; then
  SOME="$(awk '/^some/ {gsub(/avg60=/,""); print int($2)}' "$PSI" | cut -d. -f1)"
  FULL="$(awk '/^full/ {gsub(/avg60=/,""); print int($2)}' "$PSI" | cut -d. -f1)"
  [ "${SOME:-0}" -le "$(json "['guards']['psi_some_avg60_max_percent']")" ] || die "PSI some avg60 ${SOME}% over guard"
  [ "${FULL:-0}" -le "$(json "['guards']['psi_full_avg60_max_percent']")" ] || die "PSI full avg60 ${FULL}% over guard"
fi

# No foreign emulator on the owned serial and the private ADB port is reachable.
if "$ADB" -P "$ADB_PORT" devices 2>/dev/null | grep -q "$SERIAL"; then
  die "$SERIAL already running; refusing to share an owned fixture"
fi

# --- exact launch argv from the manifest -----------------------------------
RUN_DIR="$(json "['evidence']['run_dir']")"
RUN_DIR="${RUN_DIR%<timestamp>}$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$RUN_DIR"
export ZEN_RUN_DIR="$RUN_DIR"
OVERLAY="$RUN_DIR/guest.qcow2"
BASE_IMAGE="$(json "['pins']['vm_base_image']['path']")"
"$(dirname "$QEMU")/qemu-img" create -f qcow2 -F qcow2 -b "$BASE_IMAGE" "$OVERLAY" 32G >/dev/null

mapfile -t VM_ARGV < <(python3 -c "
import json,sys
m=json.load(open(sys.argv[1]))
argv=[a.replace('\$ZEN_RUN_DIR', sys.argv[2]) for a in m['vm']['argv']]
argv[0]=sys.argv[3]
print('\n'.join(argv))
" "$MANIFEST" "$RUN_DIR" "$QEMU")
"${VM_ARGV[@]}" >"$RUN_DIR/qemu.log" 2>&1 &
ZEN_QEMU_PID=$!
echo "$ZEN_QEMU_PID" > "$RUN_DIR/qemu.pid"
export ZEN_QEMU_PID

# --- guest provisioning/test driver (reviewed, not yet implemented) --------
# The reviewed procedure must provide, in order: guest command access on the
# restricted network, pinned Sunshine build closure, SDDM -> SAME KDE session
# observation, the App test driver through the authenticated bootstrap, and
# the frame/input/revoke evidence collection.
kill "$ZEN_QEMU_PID" 2>/dev/null || true
rm -f "$OVERLAY"
die "guest_procedure_unimplemented: reviewed guest provisioning/test driver is not wired; no evidence produced"
