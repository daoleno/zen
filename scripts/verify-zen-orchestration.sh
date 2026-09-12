#!/usr/bin/env bash
set -euo pipefail
umask 077

usage() {
  cat <<'EOF'
Usage: scripts/verify-zen-orchestration.sh --state-dir PATH [--json] [--root PATH]
       [--zen-bin PATH] [--timeout-seconds 1-300]

Check the project feature map and bounded Zen control paths.
The command does not start Zen or invoke an AI provider.
The state directory must already exist and belong to the daemon being checked.
EOF
}

json_output=false
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
state_dir=""
zen_bin="${ZEN_BIN:-zen}"
timeout_seconds=30

while (($# > 0)); do
  case "$1" in
    --help|-h) usage; exit 0 ;;
    --json) json_output=true; shift ;;
    --root|--state-dir|--zen-bin|--timeout-seconds)
      (($# >= 2)) || { echo "$1 requires a value" >&2; exit 2; }
      case "$1" in
        --root) root="$2" ;;
        --state-dir) state_dir="$2" ;;
        --zen-bin) zen_bin="$2" ;;
        --timeout-seconds) timeout_seconds="$2" ;;
      esac
      shift 2
      ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
  esac
done

root="$(cd "$root" 2>/dev/null && pwd)" || {
  echo "root is not an existing directory: $root" >&2
  exit 2
}
if ! [[ "$timeout_seconds" =~ ^[0-9]+$ ]] || ((timeout_seconds < 1 || timeout_seconds > 300)); then
  echo "timeout-seconds must be an integer from 1 to 300" >&2
  exit 2
fi

failures=()
source_count=0
test_count=0
feature_count=0
feature_json='[]'
manifest_valid=false
source_check_pass=false
skill_source_pass=false
doctor_pass=false
brain_playbooks_pass=false
brain_context_pass=false
worker_list_pass=false
worker_count=0
source_revision="unknown"
source_dirty=false
daemon_id=""
runtime_addr=""
runtime_running=false

manifest="$root/.agents/skills/zen-verification/features/manifest.json"
if [[ -f "$manifest" ]] && jq -e '
  def text: type == "string" and length > 0;
  def safe_path: text and (startswith("/") | not) and ((test("(^|/)\\.\\.?(/|$)") | not));
  def path_array: type == "array" and length > 0 and all(.[]; safe_path);
  def runtime_id: ["brain_context", "worker_list"] | index(.) != null;
  def source_id: . == "skill_source";
  .schema_version == 1
  and (.features | type == "array" and length == 3)
  and ([.features[].id] | length == (unique | length))
  and ([.features[] | .runtime_kind] | sort) == ["runtime", "runtime", "source"]
  and ([.features[] | select(.runtime_kind == "runtime") | .runtime_check] | sort) == ["brain_context", "worker_list"]
  and ([.features[] | select(.runtime_kind == "source") | .runtime_check] | sort) == ["skill_source"]
  and all(.features[];
    type == "object"
    and (.id | text)
    and (.runtime_kind == "runtime" or .runtime_kind == "source")
    and ((.runtime_kind == "runtime" and (.runtime_check | runtime_id)) or (.runtime_kind == "source" and (.runtime_check | source_id)))
    and (.source_paths | path_array)
    and (.test_paths | path_array)
  )
' "$manifest" >/dev/null 2>&1; then
  manifest_valid=true
  feature_count="$(jq '.features | length' "$manifest")"
  feature_json="$(jq '[.features[] | {id, runtime_kind, runtime_check, source_paths:(.source_paths | length), test_paths:(.test_paths | length)}]' "$manifest")"
else
  failures+=("manifest: invalid schema or runtime mapping")
fi

if [[ "$manifest_valid" == true ]]; then
  source_check_pass=true
  source_paths="$(jq -r '.features[].source_paths[]' "$manifest")"
  while IFS= read -r path; do
    [[ -n "$path" ]] || continue
    ((source_count += 1))
    if [[ ! -f "$root/$path" ]]; then
      source_check_pass=false
      failures+=("source: missing $path")
    fi
  done <<< "$source_paths"
  test_paths="$(jq -r '.features[].test_paths[]' "$manifest")"
  while IFS= read -r path; do
    [[ -n "$path" ]] || continue
    ((test_count += 1))
    if [[ ! -f "$root/$path" ]]; then
      source_check_pass=false
      failures+=("test: missing $path")
    fi
  done <<< "$test_paths"
  if [[ -f "$root/.agents/skills/zen-verification/SKILL.md" ]] && grep -q '^name: zen-verification$' "$root/.agents/skills/zen-verification/SKILL.md"; then
    skill_source_pass=true
  else
    source_check_pass=false
    failures+=("skill_source: zen-verification frontmatter is missing")
  fi
fi

if git -C "$root" rev-parse --verify HEAD >/dev/null 2>&1; then
  source_revision="$(git -C "$root" rev-parse HEAD)"
  [[ -z "$(git -C "$root" status --porcelain --untracked-files=all 2>/dev/null)" ]] || source_dirty=true
fi

if [[ -z "$state_dir" ]]; then
  failures+=("state-dir: an explicit existing state directory is required before doctor")
elif [[ "$state_dir" != /* ]]; then
  failures+=("state-dir: path must be absolute")
elif [[ -L "$state_dir" || ! -d "$state_dir" ]]; then
  failures+=("state-dir: path must be an existing non-symlink directory")
fi
command -v jq >/dev/null 2>&1 || failures+=("tool: jq is required")
command -v timeout >/dev/null 2>&1 || failures+=("tool: timeout is required")
if [[ "$zen_bin" != */* ]]; then
  zen_bin="$(command -v "$zen_bin" 2>/dev/null || true)"
fi
[[ -n "$zen_bin" && -x "$zen_bin" ]] || failures+=("tool: zen executable is unavailable")

report_dir=""
private_files=()
declare -A output_files=()
active_probe_pid=0

stop_active_probe() {
  local pid="$active_probe_pid"
  [[ "$pid" -gt 0 ]] || return 0
  kill -TERM "$pid" 2>/dev/null || true
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
  done
  kill -KILL "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  active_probe_pid=0
}

cleanup() {
  local status=$?
  trap - EXIT INT TERM HUP
  stop_active_probe
  local file
  for file in "${private_files[@]}"; do rm -f -- "$file"; done
  [[ -z "$report_dir" ]] || rmdir -- "$report_dir" 2>/dev/null || true
  exit "$status"
}
trap cleanup EXIT
trap 'stop_active_probe; exit 130' INT HUP
trap 'stop_active_probe; exit 143' TERM

if [[ -d "${TMPDIR:-/tmp}" ]]; then
  report_dir="$(mktemp -d "${TMPDIR:-/tmp}/zen-verification.XXXXXX")"
  chmod 700 "$report_dir"
else
  failures+=("temp: TMPDIR is unavailable")
fi

run_json() {
  local name="$1" output error status=0
  shift
  [[ -n "$report_dir" ]] || { failures+=("$name: private output directory is unavailable"); return 1; }
  output="$(mktemp "$report_dir/$name.XXXXXX")"
  error="$(mktemp "$report_dir/$name.stderr.XXXXXX")"
  chmod 600 "$output" "$error"
  private_files+=("$output" "$error")
  output_files["$name"]="$output"
  timeout --kill-after=1s "${timeout_seconds}s" bash -c '
    child_pid=0
    forward_signal() {
      if ((child_pid > 0)); then
        kill -TERM "$child_pid" 2>/dev/null || true
        wait "$child_pid" 2>/dev/null || true
      fi
      exit 143
    }
    trap forward_signal INT TERM HUP
    "$@" &
    child_pid=$!
    wait "$child_pid"
  ' zen-probe "$@" >"$output" 2>"$error" &
  active_probe_pid=$!
  wait "$active_probe_pid" || status=$?
  active_probe_pid=0
  ((status == 0)) || { failures+=("$name: command failed with exit $status"); return 1; }
  jq -e 'type == "object" and (.ok == true or .ready == true)' "$output" >/dev/null 2>&1 || {
    failures+=("$name: command returned invalid or unsuccessful JSON")
    return 1
  }
}

if [[ "$manifest_valid" == true && "$source_check_pass" == true && "$state_dir" == /* && -d "$state_dir" && ! -L "$state_dir" && -x "$zen_bin" && -n "$report_dir" ]]; then
  run_json doctor "$zen_bin" doctor --json --state-dir "$state_dir" || true
  if [[ -s "${output_files[doctor]:-}" ]] && jq -e '.ready == true' "${output_files[doctor]}" >/dev/null 2>&1; then
    doctor_pass=true
    daemon_id="$(jq -r '.listen.daemon_id // ""' "${output_files[doctor]}")"
    runtime_addr="$(jq -r '.listen.addr // ""' "${output_files[doctor]}")"
    runtime_running="$(jq -r '.listen.zen_running == true' "${output_files[doctor]}")"
  else
    failures+=("doctor: readiness is false")
  fi
else
  failures+=("doctor: skipped because the explicit state/tool/temp prerequisite failed")
fi

if [[ "$doctor_pass" == true ]]; then
  run_json brain_playbooks "$zen_bin" brain playbooks --json --state-dir "$state_dir" || true
  run_json brain_context "$zen_bin" brain context --json --state-dir "$state_dir" || true
  run_json worker_list "$zen_bin" worker list --json --state-dir "$state_dir" || true
  if [[ -s "${output_files[brain_playbooks]:-}" ]] && jq -e '.playbooks.playbooks | map(.name) | index("brain-flows") != null' "${output_files[brain_playbooks]}" >/dev/null 2>&1; then brain_playbooks_pass=true; else failures+=("brain_playbooks: brain-flows is not available"); fi
  if [[ -s "${output_files[brain_context]:-}" ]] && jq -e '.context.host_executor.id != null and .context.delegated_executor.id != null' "${output_files[brain_context]}" >/dev/null 2>&1; then brain_context_pass=true; else failures+=("brain_context: executor contract is incomplete"); fi
  if [[ -s "${output_files[worker_list]:-}" ]] && jq -e '.workers | type == "array"' "${output_files[worker_list]}" >/dev/null 2>&1; then
    worker_list_pass=true
    worker_count="$(jq '.workers | length' "${output_files[worker_list]}")"
  else
    failures+=("worker_list: canonical Worker list is unavailable")
  fi
else
  failures+=("runtime: Brain and Worker checks were not run")
fi

status="pass"
if ((${#failures[@]} != 0)) || [[ "$source_check_pass" != true || "$skill_source_pass" != true ]]; then
  status="fail"
fi

if [[ "$json_output" == true ]]; then
  failure_json="$(printf '%s\n' "${failures[@]:-}" | jq -Rsc 'split("\n") | map(select(length > 0))')"
  jq -n     --arg status "$status"     --arg root "$root"     --arg source_revision "$source_revision"     --arg daemon_id "$daemon_id"     --arg runtime_addr "$runtime_addr"     --argjson source_dirty "$source_dirty"     --argjson runtime_running "$runtime_running"     --argjson feature_count "$feature_count"     --argjson feature_items "$feature_json"     --argjson source_count "$source_count"     --argjson test_count "$test_count"     --argjson source_check "$source_check_pass"     --argjson skill_source "$skill_source_pass"     --argjson doctor "$doctor_pass"     --argjson brain_playbooks "$brain_playbooks_pass"     --argjson brain_context "$brain_context_pass"     --argjson worker_list "$worker_list_pass"     --argjson worker_count "$worker_count"     --argjson failures "$failure_json"     '{status:$status,source_identity:{root:$root,revision:$source_revision,dirty:$source_dirty},runtime_identity:{daemon_id:$daemon_id,address:$runtime_addr,running:$runtime_running},features:{count:$feature_count,source_paths:$source_count,test_paths:$test_count,items:$feature_items},source_checks:{manifest_and_anchors:$source_check,skill_source:$skill_source},runtime_checks:{doctor:$doctor,brain_playbooks:$brain_playbooks,brain_context:$brain_context,worker_list:$worker_list,worker_count:$worker_count},failures:$failures}'
else
  printf 'Zen verification: %s\n' "$status"
  printf 'Source: revision=%s dirty=%s. Features=%s source_anchors=%s test_anchors=%s.\n' "$source_revision" "$source_dirty" "$feature_count" "$source_count" "$test_count"
  printf 'Runtime: daemon=%s running=%s doctor=%s brain_playbooks=%s brain_context=%s worker_list=%s workers=%s.\n' "$daemon_id" "$runtime_running" "$doctor_pass" "$brain_playbooks_pass" "$brain_context_pass" "$worker_list_pass" "$worker_count"
  if ((${#failures[@]} > 0)); then
    printf 'Failures:\n'
    printf '  %s\n' "${failures[@]}"
  fi
fi

[[ "$status" == pass ]]
