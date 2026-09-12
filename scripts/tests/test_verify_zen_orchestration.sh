#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$SCRIPT_DIR/verify-zen-orchestration.sh"
REPO="$(cd "$SCRIPT_DIR/.." && pwd)"
SANDBOX="$(mktemp -d "${TMPDIR:-/tmp}/zen-verification-test.XXXXXX")"
chmod 700 "$SANDBOX"
trap 'rm -rf "$SANDBOX"' EXIT
export TMPDIR="$SANDBOX/tmp"
mkdir -p "$TMPDIR" "$SANDBOX/state"
chmod 700 "$TMPDIR" "$SANDBOX/state"

FAKE_ZEN="$SANDBOX/zen"
OBSERVE="$SANDBOX/observe"
DOCTOR_MARKER="$SANDBOX/doctor-called"
export OBSERVE DOCTOR_MARKER
cat >"$FAKE_ZEN" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
mode="${FAKE_MODE:-normal}"
if [[ "$1 $2" == "doctor --json" ]]; then
  [[ "$mode" != "doctor-nonzero" ]] || exit 7
  [[ "$mode" != "doctor-hang" ]] || sleep 30
  [[ -z "${DOCTOR_MARKER:-}" ]] || printf 'called\n' >"$DOCTOR_MARKER"
  printf '%s\n' '{"ready":true,"listen":{"addr":"127.0.0.1:9876","daemon_id":"fixture-daemon","zen_running":true}}'
  exit 0
fi
if [[ "$1 $2 $3" == "brain playbooks --json" ]]; then
  [[ "$mode" != "bad-json" ]] || { printf '{\n'; exit 0; }
  [[ "$mode" != "command-nonzero" ]] || exit 9
  printf '%s\n' '{"ok":true,"playbooks":{"playbooks":[{"name":"brain-flows"}]}}'
  exit 0
fi
if [[ "$1 $2 $3" == "brain context --json" ]]; then
  [[ "$mode" != "bad-json" ]] || { printf '{\n'; exit 0; }
  printf '%s\n' '{"ok":true,"context":{"host_executor":{"id":"codex"},"delegated_executor":{"id":"pi"}}}'
  exit 0
fi
if [[ "$1 $2 $3" == "worker list --json" ]]; then
  [[ "$mode" != "worker-hang" ]] || sleep 30
  report_dir="$(find "$TMPDIR" -maxdepth 1 -type d -name 'zen-verification.*' -print -quit)"
  if [[ -n "$report_dir" ]]; then
    stat -c '%a' "$report_dir" >"$OBSERVE"
    find "$report_dir" -maxdepth 1 -type f -printf '%m\n' | sort -n >>"$OBSERVE"
  fi
  printf '%s\n' '{"ok":true,"workers":[]}'
  exit 0
fi
printf 'unexpected fake zen command\n' >&2
exit 11
EOF
chmod 700 "$FAKE_ZEN"

run() {
  local output status
  set +e
  output="$("$SCRIPT" --json --root "$REPO" --state-dir "$SANDBOX/state" --zen-bin "$FAKE_ZEN" "$@" 2>&1)"
  status=$?
  set -e
  LAST_OUTPUT="$output"
  LAST_STATUS="$status"
}

run_mode() {
  export FAKE_MODE="$1"
  shift
  run "$@"
  unset FAKE_MODE
}

assert_eq() {
  [[ "$1" == "$2" ]] || { printf 'assertion failed: %s != %s\n' "$1" "$2" >&2; exit 1; }
}
assert_contains() {
  [[ "$1" == *"$2"* ]] || { printf 'assertion failed: output lacks %s\n%s\n' "$2" "$1" >&2; exit 1; }
}
assert_not_contains() {
  [[ "$1" != *"$2"* ]] || { printf 'assertion failed: output contains %s\n%s\n' "$2" "$1" >&2; exit 1; }
}

run
assert_eq "$LAST_STATUS" 0
assert_eq "$(jq -r .status <<<"$LAST_OUTPUT")" pass
assert_eq "$(jq -r .runtime_checks.worker_count <<<"$LAST_OUTPUT")" 0
assert_eq "$(jq -r .runtime_identity.daemon_id <<<"$LAST_OUTPUT")" fixture-daemon
assert_eq "$(jq -r .source_identity.root <<<"$LAST_OUTPUT")" "$REPO"
assert_eq "$(head -1 "$OBSERVE")" 700
if tail -n +2 "$OBSERVE" | grep -v '^600$' >/dev/null; then
  echo "private output file was not mode 600" >&2
  exit 1
fi

sentinel="$TMPDIR/zen-verification-$$"
mkdir "$sentinel"
printf 'keep\n' >"$sentinel/sentinel"
run
assert_eq "$LAST_STATUS" 0
[[ -f "$sentinel/sentinel" ]] || { echo "precreated sentinel was removed" >&2; exit 1; }

BAD_ROOT="$SANDBOX/bad-root"
mkdir -p "$BAD_ROOT/.agents/skills/zen-verification/features"
cp "$REPO/.agents/skills/zen-verification/SKILL.md" "$BAD_ROOT/.agents/skills/zen-verification/SKILL.md"
cp "$REPO/.agents/skills/zen-verification/features/README.md" "$BAD_ROOT/.agents/skills/zen-verification/features/README.md"
printf '%s\n' '{"schema_version":1,"features":[{"id":"broken","runtime_check":"does-not-exist"}]}' >"$BAD_ROOT/.agents/skills/zen-verification/features/manifest.json"
run --root "$BAD_ROOT"
assert_eq "$LAST_STATUS" 1
assert_eq "$(jq -r .status <<<"$LAST_OUTPUT")" fail
assert_contains "$LAST_OUTPUT" "invalid schema or runtime mapping"
assert_not_contains "$LAST_OUTPUT" "Cannot iterate over null"

run_mode bad-json
assert_eq "$LAST_STATUS" 1
assert_contains "$LAST_OUTPUT" "invalid or unsuccessful JSON"

run_mode command-nonzero
assert_eq "$LAST_STATUS" 1
assert_contains "$LAST_OUTPUT" "command failed with exit 9"

run_mode worker-hang --timeout-seconds 1
assert_eq "$LAST_STATUS" 1
assert_contains "$LAST_OUTPUT" "worker_list: command failed with exit 124"

rm -f "$DOCTOR_MARKER"
set +e
NO_STATE_OUTPUT="$("$SCRIPT" --json --root "$REPO" --zen-bin "$FAKE_ZEN" 2>&1)"
NO_STATE_STATUS=$?
set -e
assert_eq "$NO_STATE_STATUS" 1
assert_contains "$NO_STATE_OUTPUT" "explicit existing state directory"
[[ ! -e "$DOCTOR_MARKER" ]] || { echo "doctor ran without explicit state" >&2; exit 1; }

export FAKE_MODE=doctor-hang
set +e
("$SCRIPT" --json --root "$REPO" --state-dir "$SANDBOX/state" --zen-bin "$FAKE_ZEN" --timeout-seconds 30 >"$SANDBOX/signal.json" 2>"$SANDBOX/signal.err") &
signal_pid=$!
set -e
for _ in $(seq 1 50); do
  find "$TMPDIR" -maxdepth 1 -type d -name 'zen-verification.*' -print -quit | grep -q . && break
  sleep 0.1
done
kill -TERM "$signal_pid"
set +e
wait "$signal_pid"
signal_status=$?
set -e
[[ "$signal_status" -ne 0 ]] || { echo "signal run unexpectedly passed" >&2; exit 1; }
[[ -z "$(find "$TMPDIR" -maxdepth 1 -type d -name 'zen-verification.*' -print -quit)" ]] || {
  echo "signal cleanup left a private report directory" >&2
  exit 1
}

echo "verify-zen-orchestration regressions: PASS"
