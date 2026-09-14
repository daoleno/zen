#!/usr/bin/env bash
set -euo pipefail

# Owned, headless desktop fixture for Zen's virtual-x11 backend. It never
# touches the caller's Wayland/X11 session: all state and processes are scoped
# to one private directory and one explicit X display.
STATE_DIR="${ZEN_VIRTUAL_X11_STATE_DIR:-${XDG_RUNTIME_DIR:-${TMPDIR:-/tmp}}/zen-virtual-x11}"
DISPLAY_NUM="${ZEN_VIRTUAL_X11_DISPLAY:-92}"
DISPLAY_ADDR="$DISPLAY_NUM"
if [[ "$DISPLAY_NUM" != :* ]]; then DISPLAY_ADDR=":$DISPLAY_NUM"; fi
PID_FILE="$STATE_DIR/pids"
ENV_FILE="$STATE_DIR/environment"
LOG_FILE="$STATE_DIR/xvnc.log"
mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR"

find_cmd() {
  local name
  for name in "$@"; do
    if command -v "$name" >/dev/null 2>&1; then command -v "$name"; return 0; fi
  done
  return 1
}

running_pid() {
  local name="$1" pid
  [[ -s "$PID_FILE.$name" ]] || return 1
  pid="$(cat "$PID_FILE.$name")"
  kill -0 "$pid" 2>/dev/null || return 1
  echo "$pid"
}

stop_one() {
  local name="$1" pid
  if pid="$(running_pid "$name")"; then
    kill "$pid" 2>/dev/null || true
    for _ in {1..20}; do kill -0 "$pid" 2>/dev/null || break; sleep 0.05; done
    kill -9 "$pid" 2>/dev/null || true
  fi
  rm -f "$PID_FILE.$name"
}

stop_fixture() {
  stop_one app
  stop_one wm
  stop_one dbus
  stop_one xvnc
  rm -f "$ENV_FILE"
  echo "virtual-x11 stopped (${DISPLAY_ADDR})"
}

case "${1:-start}" in
stop)
  stop_fixture
  exit 0
  ;;
status)
  if running_pid xvnc >/dev/null && running_pid wm >/dev/null; then
    echo "virtual-x11 running display=${DISPLAY_ADDR} state=${STATE_DIR}"
    exit 0
  fi
  echo "virtual-x11 stopped display=${DISPLAY_ADDR} state=${STATE_DIR}"
  exit 1
  ;;
start)
  ;;
*)
  echo "usage: $0 {start|stop|status}" >&2
  exit 2
  ;;
esac

if running_pid xvnc >/dev/null && running_pid wm >/dev/null; then
  cat "$ENV_FILE"
  exit 0
fi
stop_fixture

xvnc="$(find_cmd Xvnc vncserver || true)"
if [[ -z "$xvnc" ]]; then echo "virtual-x11 requires Xvnc (or vncserver)" >&2; exit 1; fi
wm="$(find_cmd openbox fluxbox xfwm4 twm || true)"
if [[ -z "$wm" ]]; then echo "virtual-x11 requires a lightweight window manager (openbox, fluxbox, xfwm4, or twm)" >&2; exit 1; fi

if [[ "$(basename "$xvnc")" == "vncserver" ]]; then
  "$xvnc" "$DISPLAY_ADDR" -geometry 1280x720 -depth 24 -localhost yes >"$LOG_FILE" 2>&1 &
else
  "$xvnc" "$DISPLAY_ADDR" -geometry 1280x720 -depth 24 -SecurityTypes None -localhost=0 >"$LOG_FILE" 2>&1 &
fi
echo $! > "$PID_FILE.xvnc"
for _ in {1..80}; do [[ -S "/tmp/.X11-unix/X${DISPLAY_ADDR#:}" ]] && break; sleep 0.05; done
if ! [[ -S "/tmp/.X11-unix/X${DISPLAY_ADDR#:}" ]]; then echo "Xvnc did not create ${DISPLAY_ADDR}" >&2; stop_fixture; exit 1; fi

export DISPLAY="$DISPLAY_ADDR"
export ZEN_DESKTOP_BACKEND=virtual-x11
export ZEN_DESKTOP_DISPLAY="$DISPLAY_ADDR"
export XDG_CURRENT_DESKTOP=ZenVirtual
export XDG_SESSION_DESKTOP=ZenVirtual
export DBUS_SESSION_BUS_ADDRESS=""
# dbus-run-session gives the fixture its own bus/session environment. If it is
# available, use it for the WM and app; otherwise run the WM directly.
if dbus_run="$(find_cmd dbus-run-session || true)"; then
  "$dbus_run" -- "$wm" --replace >"$STATE_DIR/wm.log" 2>&1 &
  echo $! > "$PID_FILE.wm"
else
  "$wm" --replace >"$STATE_DIR/wm.log" 2>&1 &
  echo $! > "$PID_FILE.wm"
fi
# xmessage is present on minimal X installs and gives capture/input a visible
# owned target. xterm is preferred because typed text visibly changes it.
app="$(find_cmd xterm xmessage || true)"
if [[ -z "$app" ]]; then echo "virtual-x11 requires xterm or xmessage" >&2; stop_fixture; exit 1; fi
if [[ "$(basename "$app")" == "xterm" ]]; then
  "$app" -title ZenVirtualX11 -geometry 100x30+30+30 -e sh -c 'printf "Zen virtual-x11 fixture\\nType from the paired phone.\\n\\n"; exec sh' >"$STATE_DIR/app.log" 2>&1 &
else
  "$app" -title "Zen virtual-x11 fixture: pointer and keyboard target" >"$STATE_DIR/app.log" 2>&1 &
fi
echo $! > "$PID_FILE.app"
cat > "$ENV_FILE" <<ENV
export DISPLAY=$DISPLAY_ADDR
export ZEN_DESKTOP_BACKEND=virtual-x11
export ZEN_DESKTOP_DISPLAY=$DISPLAY_ADDR
export XDG_CURRENT_DESKTOP=ZenVirtual
export XDG_SESSION_DESKTOP=ZenVirtual
export ZEN_VIRTUAL_X11_STATE_DIR=$STATE_DIR
ENV
chmod 600 "$ENV_FILE"
echo "virtual-x11 running display=$DISPLAY_ADDR state=$STATE_DIR"
echo "source $ENV_FILE before starting Zen; then use Remote Desktop -> Connect"
