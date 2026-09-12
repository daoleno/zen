#!/usr/bin/env bash
# Guarded run/preparation procedure for the coupled Moonlight E2E.
#
# Fail-closed: it checks every prerequisite before creating a run directory or
# starting any process, refuses while the manifest is unapproved, and refuses
# while the reviewed guest procedure is not implemented. Modes:
#   --prepare  guest build/dependency preparation stage (separate approval)
#   --run      the coupled test stage (approved=true and all hashes recorded)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MANIFEST="${ZEN_E2E_MANIFEST:-/home/daoleno/.zen/artifacts/2026-09-13-desktop-moonlight-e2e/manifest.json}"
MODE="${1:---run}"
case "$MODE" in --prepare|--run) ;; *) echo "usage: $0 [--prepare|--run]" >&2; exit 2 ;; esac

die() { echo "zen-desktop-e2e-run: $1" >&2; exit 1; }
json() { python3 -c "import json,sys; print(json.load(open(sys.argv[1]))$1)" "$MANIFEST"; }
require_hex() { python3 - "$1" "$2" <<'PY'
import re, sys
value, name = sys.argv[1], sys.argv[2]
if not re.fullmatch(r"[0-9a-f]{64}", value or ""):
    print(f"missing or invalid {name} hash: {value!r}", file=sys.stderr)
    raise SystemExit(1)
PY
}

[ -f "$MANIFEST" ] || die "missing manifest $MANIFEST"

# --- gates (no side effects before both pass) ------------------------------
if [ "$MODE" = "--run" ]; then
  [ "$(json "['approved']")" = "True" ] || die "manifest approved=false; no process started"
fi
if [ "$MODE" = "--prepare" ]; then
  [ "$(json "['preparation_approved']")" = "True" ] || die "preparation_approved=false; no process started"
  [ "$(json "['run_procedure']['guest_preparation_implemented']")" = "True" ] || die "guest_preparation_unimplemented: refusal before any side effect"
fi
if [ "$MODE" = "--run" ]; then
  [ "$(json "['run_procedure']['guest_procedure_implemented']")" = "True" ] || die "guest_procedure_unimplemented: refusal before any side effect"
fi

# --- prerequisites: bytes, not manifest strings ----------------------------
GATES_SHA="$(json "['run_procedure']['gates_sha256']")"
GATES="$(json "['run_procedure']['gates_path']")"
[ -f "$GATES" ] || die "missing gates script $GATES"
[ "$(sha256sum "$GATES" | cut -d' ' -f1)" = "$GATES_SHA" ] || die "gates script hash mismatch"

if [ "$MODE" = "--run" ]; then
  APK_PATH="$(json "['apk']['path']")"
  APK_SHA="$(json "['apk']['sha256']")"
  require_hex "$APK_SHA" apk.sha256
  [ -f "$ROOT/$APK_PATH" ] || die "missing APK $APK_PATH"
  [ "$(sha256sum "$ROOT/$APK_PATH" | cut -d' ' -f1)" = "$APK_SHA" ] || die "APK hash mismatch"
  require_hex "$(json "['vm']['sunshine']['binary_sha256']")" guest.sunshine
fi

ADB="$(json "['pins']['adb']['path']")"
EMULATOR="$(json "['pins']['emulator']['path']")"
QEMU="$(json "['pins']['qemu_binary']['path']")"
AVD="$(json "['pins']['avd']['name']")"
SERIAL="$(json "['pins']['avd']['serial']")"
ADB_PORT="$(json "['pins']['adb']['server_port']")"
[ -x "$ADB" ] && [ -f "$EMULATOR" ] && [ -f "$QEMU" ] || die "owned adb/emulator/qemu missing"
[ -f "$HOME/.android/avd/$AVD.ini" ] || die "owned AVD $AVD missing"
if "$ADB" -P "$ADB_PORT" devices 2>/dev/null | grep -q "$SERIAL"; then
  die "$SERIAL already running; refusing to share an owned fixture"
fi

FLOOR_MIB="$(json "['guards']['memory_floor_mib']")"
PEAK_MIB="$(json "['budget']['combined_peak_upper_bound_mib']")"
AVAILABLE_MIB="$(awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo)"
[ "$AVAILABLE_MIB" -ge $((FLOOR_MIB + PEAK_MIB)) ] || die "MemAvailable ${AVAILABLE_MIB}MiB below floor+peak $((FLOOR_MIB + PEAK_MIB))MiB"

psi_avg60() {
  # /proc/pressure/memory and cgroup v2 memory.pressure share the format
  # "some avg10=0.00 avg60=0.00 avg300=0.00 total=0"; read the avg60 field.
  awk -v line="$1" 'BEGIN { n=split(line,f," "); for (i=1;i<=n;i++) { if (f[i] ~ /^avg60=/) { sub(/avg60=/,"",f[i]); print int(f[i]+0); exit } } print 0 }'
}
read_psi() {
  local file line
  file="/proc/pressure/memory"
  [ -r "$file" ] || return 0
  line="$(grep -m1 '^some' "$file" || true)"
  [ -n "$line" ] || return 0
  local some full
  some="$(psi_avg60 "$line")"
  full="$(psi_avg60 "$(grep -m1 '^full' "$file" || true)")"
  [ "$some" -le "$(json "['guards']['psi_some_avg60_max_percent']")" ] || die "PSI some avg60 ${some}% over guard"
  [ "$full" -le "$(json "['guards']['psi_full_avg60_max_percent']")" ] || die "PSI full avg60 ${full}% over guard"
}
read_psi

if [ "$MODE" = "--prepare" ]; then
  echo "zen-desktop-e2e-run: preparation stage prerequisites verified; guest preparation not implemented yet"
  exit 0
fi

# --- launch (only reachable once the reviewed guest procedure exists) ------
RUN_DIR="$(json "['evidence']['run_dir']")"
RUN_DIR="${RUN_DIR%<timestamp>}$(date -u +%Y%m%dT%H%M%SZ)"
OVERLAY="$RUN_DIR/guest.qcow2"
QEMU_PID=""
cleanup() {
  local code=$?
  if [ -n "$QEMU_PID" ]; then
    kill "$QEMU_PID" 2>/dev/null || true
    wait "$QEMU_PID" 2>/dev/null || true
  fi
  [ -f "$OVERLAY" ] && rm -f "$OVERLAY"
  return $code
}
trap cleanup EXIT

mkdir -p "$RUN_DIR"
export ZEN_RUN_DIR="$RUN_DIR"
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
QEMU_PID=$!
echo "$QEMU_PID" > "$RUN_DIR/qemu.pid"

# Ongoing guards: memory floor, PSI and the deadline while the run is alive.
DEADLINE=$((SECONDS + $(json "['guards']['deadline_seconds']")))
SAMPLE="$(json "['guards']['sample_seconds']")"
while kill -0 "$QEMU_PID" 2>/dev/null; do
  [ "$SECONDS" -le "$DEADLINE" ] || die "deadline exceeded; cleanup by trap"
  AVAILABLE_MIB="$(awk '/MemAvailable/ {print int($2/1024)}' /proc/meminfo)"
  [ "$AVAILABLE_MIB" -ge "$FLOOR_MIB" ] || die "live MemAvailable ${AVAILABLE_MIB}MiB below floor"
  read_psi
  sleep "$SAMPLE"
done

die "guest_procedure_unimplemented: reviewed test driver is not wired; no evidence produced"
