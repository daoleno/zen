#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/verify-zen-orchestration.sh [--json] [--root PATH] [--state-dir PATH]

Check the project feature map and the live, read-only Zen control paths.
The command creates no resources and never invokes an AI provider.
EOF
}

json_output=false
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
state_dir=""

while (($# > 0)); do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    --json)
      json_output=true
      shift
      ;;
    --root)
      (($# >= 2)) || { echo "--root requires a path" >&2; exit 2; }
      root="$2"
      shift 2
      ;;
    --state-dir)
      (($# >= 2)) || { echo "--state-dir requires a path" >&2; exit 2; }
      state_dir="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

root="$(cd "$root" 2>/dev/null && pwd)" || {
  echo "root is not an existing directory: $root" >&2
  exit 2
}

manifest="$root/.agents/skills/zen-verification/features/manifest.json"
report_dir="${TMPDIR:-/tmp}/zen-verification-$$"
mkdir -p "$report_dir"
trap 'rm -rf "$report_dir"' EXIT

failures=()
source_count=0
test_count=0
feature_count=0
feature_json='[]'

if [[ ! -f "$manifest" ]]; then
  failures+=("project-skill-discovery: missing features/manifest.json")
else
  if ! jq -e '.schema_version == 1 and (.features | type == "array" and length > 0)' "$manifest" >/dev/null; then
    failures+=("project-skill-discovery: invalid manifest schema")
  else
    feature_count="$(jq '.features | length' "$manifest")"
    feature_json="$(jq '[.features[] | {id, runtime_check, source_paths:(.source_paths | length), test_paths:(.test_paths | length)}]' "$manifest")"
    while IFS= read -r path; do
      ((source_count += 1))
      [[ -f "$root/$path" ]] || failures+=("source: missing $path")
    done < <(jq -r '.features[].source_paths[]' "$manifest")
    while IFS= read -r path; do
      ((test_count += 1))
      [[ -f "$root/$path" ]] || failures+=("test: missing $path")
    done < <(jq -r '.features[].test_paths[]' "$manifest")
  fi
fi

run_json() {
  local name="$1"
  shift
  local output="$report_dir/$name.json"
  local error="$report_dir/$name.stderr"
  local status=0
  "$@" >"$output" 2>"$error" || status=$?
  if ((status != 0)); then
    failures+=("$name: command failed with exit $status")
    return 1
  fi
  if ! jq -e '.ok == true or .ready == true' "$output" >/dev/null 2>&1; then
    failures+=("$name: command returned an unsuccessful JSON response")
    return 1
  fi
  return 0
}

doctor_args=(doctor --json)
brain_args=(brain playbooks --json)
context_args=(brain context --json)
worker_args=(worker list --json)
if [[ -n "$state_dir" ]]; then
  doctor_args+=(--state-dir "$state_dir")
  brain_args+=(--state-dir "$state_dir")
  context_args+=(--state-dir "$state_dir")
  worker_args+=(--state-dir "$state_dir")
fi

run_json doctor zen "${doctor_args[@]}" || true
doctor_pass=false
if [[ -s "$report_dir/doctor.json" ]] && jq -e '.ready == true' "$report_dir/doctor.json" >/dev/null 2>&1; then
  doctor_pass=true
fi

brain_playbooks_pass=false
brain_context_pass=false
worker_list_pass=false
if [[ "$doctor_pass" == true ]]; then
  run_json brain_playbooks zen "${brain_args[@]}" || true
  run_json brain_context zen "${context_args[@]}" || true
  run_json worker_list zen "${worker_args[@]}" || true
else
  failures+=("runtime: doctor did not authorize Brain and Worker checks")
fi

if [[ -s "$report_dir/brain_playbooks.json" ]] && jq -e '.playbooks.playbooks | map(.name) | index("brain-flows") != null' "$report_dir/brain_playbooks.json" >/dev/null 2>&1; then
  brain_playbooks_pass=true
else
  failures+=("brain_playbooks: brain-flows is not discoverable")
fi
if [[ -s "$report_dir/brain_context.json" ]] && jq -e '.context.host_executor.id != null and .context.delegated_executor.id != null' "$report_dir/brain_context.json" >/dev/null 2>&1; then
  brain_context_pass=true
else
  failures+=("brain_context: executor contract is incomplete")
fi
if [[ -s "$report_dir/worker_list.json" ]] && jq -e '.workers | type == "array"' "$report_dir/worker_list.json" >/dev/null 2>&1; then
  worker_list_pass=true
else
  failures+=("worker_list: canonical Worker list is unavailable")
fi

status="pass"
if ((${#failures[@]} > 0)); then
  status="fail"
fi

worker_count=0
host_executor=""
delegated_executor=""
if [[ -s "$report_dir/worker_list.json" ]]; then
  worker_count="$(jq '.workers | length' "$report_dir/worker_list.json")"
fi
if [[ -s "$report_dir/brain_context.json" ]]; then
  host_executor="$(jq -r '.context.host_executor.id // ""' "$report_dir/brain_context.json")"
  delegated_executor="$(jq -r '.context.delegated_executor.id // ""' "$report_dir/brain_context.json")"
fi

if [[ "$json_output" == true ]]; then
  failure_json="$(printf '%s\n' "${failures[@]:-}" | jq -Rsc 'split("\n") | map(select(length > 0))')"
  jq -n \
    --arg status "$status" \
    --arg root "$root" \
    --arg host_executor "$host_executor" \
    --arg delegated_executor "$delegated_executor" \
    --argjson feature_count "$feature_count" \
    --argjson feature_items "$feature_json" \
    --argjson source_count "$source_count" \
    --argjson test_count "$test_count" \
    --argjson worker_count "$worker_count" \
    --argjson doctor "$doctor_pass" \
    --argjson brain_playbooks "$brain_playbooks_pass" \
    --argjson brain_context "$brain_context_pass" \
    --argjson worker_list "$worker_list_pass" \
    --argjson failures "$failure_json" \
    '{status:$status,root:$root,features:{count:$feature_count,source_paths:$source_count,test_paths:$test_count,items:$feature_items},runtime:{doctor:$doctor,brain_playbooks:$brain_playbooks,brain_context:$brain_context,worker_list:$worker_list,host_executor:$host_executor,delegated_executor:$delegated_executor,worker_count:$worker_count},failures:$failures}'
else
  printf 'Zen verification: %s\n' "$status"
  printf 'Features: %s. Source anchors: %s. Test anchors: %s.\n' "$feature_count" "$source_count" "$test_count"
  printf 'Runtime: doctor=%s brain_playbooks=%s brain_context=%s worker_list=%s workers=%s.\n' "$doctor_pass" "$brain_playbooks_pass" "$brain_context_pass" "$worker_list_pass" "$worker_count"
  if ((${#failures[@]} > 0)); then
    printf 'Failures:\n'
    printf '  %s\n' "${failures[@]}"
  fi
fi

[[ "$status" == pass ]]
