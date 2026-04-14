#!/usr/bin/env bash

set -euo pipefail

PACKAGE_NAME="${1:-com.anonymous.zen}"
OUT_ROOT="${2:-artifacts/android-crash}"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
OUT_DIR="${OUT_ROOT}/${TIMESTAMP}"

mkdir -p "${OUT_DIR}"

if ! command -v adb >/dev/null 2>&1; then
  echo "adb not found in PATH" >&2
  exit 1
fi

adb wait-for-device >/dev/null

{
  echo "timestamp=${TIMESTAMP}"
  echo "package=${PACKAGE_NAME}"
  echo "host_pwd=$(pwd)"
} > "${OUT_DIR}/meta.txt"

adb devices -l > "${OUT_DIR}/devices.txt"
adb shell getprop > "${OUT_DIR}/getprop.txt"
adb shell dumpsys package "${PACKAGE_NAME}" > "${OUT_DIR}/package.txt" 2>&1 || true

echo "Clearing logcat buffers..."
adb logcat -c

echo "Capturing live logcat to ${OUT_DIR}/logcat-live.txt"
adb logcat -b main -b system -b crash -v threadtime > "${OUT_DIR}/logcat-live.txt" 2>&1 &
LOGCAT_PID=$!

cleanup() {
  set +e
  if kill -0 "${LOGCAT_PID}" >/dev/null 2>&1; then
    kill "${LOGCAT_PID}" >/dev/null 2>&1 || true
    wait "${LOGCAT_PID}" >/dev/null 2>&1 || true
  fi

  adb logcat -d -b main -b system -b crash -v threadtime > "${OUT_DIR}/logcat-dump.txt" 2>&1 || true
  adb logcat -d -b crash -v threadtime > "${OUT_DIR}/logcat-crash.txt" 2>&1 || true
  adb shell pidof "${PACKAGE_NAME}" > "${OUT_DIR}/pid.txt" 2>&1 || true
  adb shell dumpsys activity top > "${OUT_DIR}/activity-top.txt" 2>&1 || true
  adb shell dumpsys input_method > "${OUT_DIR}/input-method.txt" 2>&1 || true
  adb shell dumpsys window windows > "${OUT_DIR}/windows.txt" 2>&1 || true
  adb shell dumpsys dropbox --print SYSTEM_TOMBSTONE > "${OUT_DIR}/dropbox-system-tombstone.txt" 2>&1 || true
  adb shell dumpsys dropbox --print system_app_crash > "${OUT_DIR}/dropbox-system-app-crash.txt" 2>&1 || true
  adb shell dumpsys dropbox --print data_app_native_crash > "${OUT_DIR}/dropbox-data-app-native-crash.txt" 2>&1 || true

  rg -n \
    'FATAL EXCEPTION|AndroidRuntime|Fatal signal|SIGSEGV|SIGABRT|Abort message|backtrace|tombstone|crash_dump|Process .* has died|Native crash|libghostty|ghostty_vt|zen_terminal_vt|com\.anonymous\.zen' \
    "${OUT_DIR}/logcat-live.txt" \
    "${OUT_DIR}/logcat-dump.txt" \
    "${OUT_DIR}/logcat-crash.txt" \
    "${OUT_DIR}/dropbox-system-tombstone.txt" \
    "${OUT_DIR}/dropbox-system-app-crash.txt" \
    "${OUT_DIR}/dropbox-data-app-native-crash.txt" \
    > "${OUT_DIR}/summary.txt" 2>/dev/null || true

  echo
  echo "Crash capture saved to ${OUT_DIR}"
  echo "Summary: ${OUT_DIR}/summary.txt"
}

trap cleanup EXIT

cat <<EOF
Crash capture is running.

1. Reproduce the crash on the connected Android device.
2. Come back here after the app flashes/crashes.
3. Press Enter once to stop capture and write the reports.

Package: ${PACKAGE_NAME}
Output:  ${OUT_DIR}
EOF

read -r _
