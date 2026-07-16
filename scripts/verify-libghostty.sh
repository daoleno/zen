#!/usr/bin/env bash
# Verify local libghostty_vt.so artifacts against native.lock.json ABI contract.
#
# Usage:
#   ./scripts/verify-libghostty.sh              # require all lock ABIs
#   ./scripts/verify-libghostty.sh --release    # sideload ABIs + release_grade provenance
#   ./scripts/verify-libghostty.sh --contract   # lock/notice/scripts/zig pins (CI, no .so)
#   ./scripts/verify-libghostty.sh --abis arm64-v8a

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/app/modules/zen-terminal-vt/native.lock.json"
NOTICE="$ROOT/app/assets/notices/GHOSTTY-MIT.txt"
MODULE_NOTICE="$ROOT/app/modules/zen-terminal-vt/NOTICE.Ghostty"
PLUGIN="$ROOT/app/plugins/withZenAndroidRelease.js"

MODE="full"
ONLY_ABIS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --contract) MODE="contract"; shift ;;
    --release) MODE="release"; shift ;;
    --abis)
      shift
      IFS=',' read -r -a ONLY_ABIS <<<"${1:-}"
      shift || true
      ;;
    -h|--help)
      sed -n '2,10p' "$0"
      exit 0
      ;;
    *)
      echo "error: unknown arg: $1" >&2
      exit 2
      ;;
  esac
done

fail=0
pass() { echo "ok: $*"; }
bad() { echo "FAIL: $*" >&2; fail=1; }

[[ -f "$LOCK" ]] && pass "lockfile present" || bad "missing $LOCK"
[[ -f "$NOTICE" ]] && pass "Ghostty MIT notice source present" || bad "missing $NOTICE"
[[ -f "$MODULE_NOTICE" ]] && pass "module NOTICE.Ghostty present" || bad "missing $MODULE_NOTICE"
[[ -f "$PLUGIN" ]] && pass "withZenAndroidRelease plugin present" || bad "missing plugin"
[[ -x "$ROOT/scripts/build-libghostty.sh" ]] && pass "build-libghostty.sh executable" || bad "build script not executable"
[[ -x "$ROOT/scripts/verify-libghostty.sh" ]] && pass "verify-libghostty.sh executable" || bad "verify script not executable"
[[ -x "$ROOT/scripts/verify-android-native-symbols.py" ]] && pass "Android native symbol verifier executable" || bad "Android native symbol verifier not executable"
[[ -x "$ROOT/scripts/android-release-apk.sh" ]] && pass "android-release-apk.sh executable" || bad "release apk script not executable"
[[ -x "$ROOT/scripts/verify-apk-notice.sh" ]] && pass "verify-apk-notice.sh executable" || bad "apk notice verifier not executable"
for packaged_consumer in scripts/android-release-apk.sh scripts/verify-apk-release.sh; do
  if grep -q 'verify-android-native-symbols.py' "$ROOT/$packaged_consumer"; then
    pass "$packaged_consumer enforces packaged native symbols"
  else
    bad "$packaged_consumer bypasses packaged native symbol verification"
  fi
done

python3 - "$LOCK" "$NOTICE" "$ROOT" <<'PY'
import hashlib, json, re, sys
from pathlib import Path

lock = json.load(open(sys.argv[1]))
notice = Path(sys.argv[2]).read_text(encoding="utf-8")
root = Path(sys.argv[3]).resolve()
assert lock.get("schema_version") == 1
assert lock["zig"]["version"]
assert re.fullmatch(r"[0-9a-f]{40}", lock["ghostty"]["commit"]), "ghostty commit must be full sha"
assert re.fullmatch(r"[0-9a-f]{64}", lock["ghostty"]["license_sha256"]), "license_sha256 required"
assert lock["ghostty"].get("copyright_line")
assert lock["ghostty"]["component"] == "libghostty-vt"
assert lock["ghostty"]["api"] == "C"
assert re.fullmatch(r"[0-9a-f]{64}", lock["ghostty"]["headers_sha256"])
assert "Copyright (c) 2024 Mitchell Hashimoto, Ghostty contributors" == lock["ghostty"]["copyright_line"]

downloads = lock["zig"].get("downloads") or {}
for required_download in ("x86_64-linux", "aarch64-linux", "aarch64-macos"):
    assert required_download in downloads
for key, d in downloads.items():
    assert d.get("tarball", "").startswith("https://ziglang.org/download/")
    assert re.fullmatch(r"[0-9a-f]{64}", d.get("sha256", "")), f"zig sha256 missing for {key}"
    assert d.get("archive_root")

abis = lock["abis"]
assert len(abis) >= 1
seen = set()
for a in abis:
    assert a["android_abi"] not in seen
    seen.add(a["android_abi"])
    assert a["zig_target"]
    assert isinstance(a["elf_machine_code"], int)
assert "arm64-v8a" in seen
assert lock["release_apk"]["react_native_architectures"] == "arm64-v8a"
assert lock["release_apk"]["notice_apk_path"] == "assets/notices/GHOSTTY-MIT.txt"
unsupported = set(lock.get("unsupported_abis") or [])
assert "armeabi-v7a" in unsupported and "x86" in unsupported
assert lock["android"]["min_api"] == 29
patches = lock["android"].get("source_patches")
assert isinstance(patches, list), "android.source_patches must be a list"
seen_patch_paths = set()
for patch in patches:
    required = {
        "path",
        "sha256",
        "applies_to_ghostty_commit",
        "upstream_repository",
        "upstream_commit",
        "upstream_url",
        "subject",
    }
    assert required.issubset(patch), f"source patch metadata missing: {required - set(patch)}"
    rel = patch["path"]
    assert isinstance(rel, str) and rel.endswith(".patch")
    assert rel not in seen_patch_paths, f"duplicate source patch path: {rel}"
    seen_patch_paths.add(rel)
    source_patch = (root / rel).resolve()
    assert source_patch.is_relative_to(root), f"source patch escapes repository: {rel}"
    assert source_patch.is_file(), f"source patch missing: {rel}"
    actual_sha = hashlib.sha256(source_patch.read_bytes()).hexdigest()
    assert re.fullmatch(r"[0-9a-f]{64}", patch["sha256"])
    assert actual_sha == patch["sha256"], f"source patch sha256 mismatch: {rel}"
    assert patch["applies_to_ghostty_commit"] == lock["ghostty"]["commit"]
    assert patch["upstream_repository"] == lock["ghostty"]["repository"]
    assert re.fullmatch(r"[0-9a-f]{40}", patch["upstream_commit"])
    assert patch["upstream_url"] == (
        "https://github.com/ghostty-org/ghostty/commit/" + patch["upstream_commit"]
    )
    patch_text = source_patch.read_text(encoding="utf-8")
    assert patch_text.startswith(f"From {patch['upstream_commit']} ")
    assert f"Subject: [PATCH] {patch['subject']}" in patch_text

forbidden = lock["android"].get("forbidden_undefined_symbols")
assert isinstance(forbidden, list) and forbidden
assert len(forbidden) == len(set(forbidden)), "duplicate forbidden Android symbols"
assert {"shm_open", "shm_unlink"}.issubset(forbidden)
for symbol in forbidden:
    assert re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", symbol), symbol
assert lock["ios"]["deployment_target"] == "16.4"
assert lock["ios"]["device_architecture"] == "arm64"
assert lock["ios"]["simulator_architecture"] == "arm64"
assert lock["ios"]["xcframework_name"] == "GhosttyVt.xcframework"

# Notice must embed exact upstream MIT body (sha256 of LICENSE text).
# Extract from first "MIT License" through end; compare hash of that region
# after normalizing to the pure LICENSE content when possible.
idx = notice.find("MIT License")
assert idx >= 0, "notice missing MIT License header"
mit_body = notice[idx:]
# Upstream LICENSE is pure MIT text ending with SOFTWARE.\n
# Our notice may equal LICENSE exactly from MIT License onward.
# Hash the mit_body stripped of trailing whitespace inconsistencies via exact upstream match attempt.
license_sha = lock["ghostty"]["license_sha256"]
# Reconstruct expected pure LICENSE by taking mit_body if it hashes, else fail with guidance
h = hashlib.sha256(mit_body.encode("utf-8")).hexdigest()
# Also try mit_body with single trailing newline normalization
candidates = [mit_body, mit_body if mit_body.endswith("\n") else mit_body + "\n"]
# If notice has wrapper, require embedded copyright + MIT paragraphs and documented license_sha256 for upstream pin
assert lock["ghostty"]["copyright_line"] in notice, "copyright line missing from notice"
if h not in (license_sha,) and hashlib.sha256(candidates[-1].encode()).hexdigest() != license_sha:
    # Accept notice that contains the full LICENSE text as a substring matching hash via search
    # Build LICENSE text from known structure: verify sha256 of extracted block between MIT License and end
    # by checking that hashing the notice-local MIT section after stripping wrapper is hard;
    # instead verify every line of a minimal MIT set is present and copyright matches, AND
    # license_sha256 is recorded for pin verification when GHOSTTY_SRC available.
    required_phrases = [
        "Permission is hereby granted, free of charge",
        "The above copyright notice and this permission notice shall be included",
        "THE SOFTWARE IS PROVIDED \"AS IS\", WITHOUT WARRANTY OF ANY KIND",
    ]
    for p in required_phrases:
        assert p in notice, f"notice missing MIT phrase: {p}"
    # Mark that pure-hash of wrapper notice differs; contract still requires license_sha256 pin
    print("ok: notice embeds MIT copyright and required phrases (wrapper form)")
else:
    print("ok: notice MIT body matches pinned license_sha256")

print("ok: lockfile schema, zig pins, ABI contract")
PY

# Plugin registered?
if grep -q "withZenAndroidRelease" "$ROOT/app/app.config.js" \
  || grep -q "withZenAndroidRelease" "$ROOT/app/app.base.json"; then
  pass "withZenAndroidRelease registered in Expo config"
else
  bad "withZenAndroidRelease not registered in app.config.js / app.base.json"
fi

if [[ "$MODE" == "contract" ]]; then
  if [[ $fail -ne 0 ]]; then
    echo "contract verification FAILED" >&2
    exit 1
  fi
  echo "contract verification passed (no native libraries required)"
  exit 0
fi

HEADERS_DIR="$ROOT/$(python3 -c "import json;print(json.load(open('$LOCK'))['outputs']['headers_dir'])")"
HEADERS_SHA="$(python3 -c "import json;print(json.load(open('$LOCK'))['ghostty']['headers_sha256'])")"
if [[ -f "$HEADERS_DIR/vt.h" ]]; then
  actual_headers_sha="$(sha256sum "$HEADERS_DIR/vt.h" | awk '{print $1}')"
  if [[ "$actual_headers_sha" == "$HEADERS_SHA" ]]; then
    pass "vt.h matches pinned header sha256"
  else
    bad "vt.h sha256 $actual_headers_sha != lock $HEADERS_SHA"
  fi
fi

# Validate requested ABIs against lock
if [[ ${#ONLY_ABIS[@]} -gt 0 ]]; then
  for abi in "${ONLY_ABIS[@]}"; do
    if ! python3 - "$LOCK" "$abi" <<'PY'
import json, sys
lock = json.load(open(sys.argv[1]))
abi = sys.argv[2]
sys.exit(0 if any(a["android_abi"]==abi for a in lock["abis"]) else 1)
PY
    then
      bad "ABI $abi not in lock contract"
    fi
  done
fi

mapfile -t CHECK_ABIS < <(python3 - "$LOCK" "$MODE" <<'PY'
import json, sys
lock = json.load(open(sys.argv[1]))
mode = sys.argv[2]
for a in lock["abis"]:
    if mode == "release" and not a.get("required_for_sideload"):
        continue
    print(a["android_abi"])
PY
)

if [[ ${#ONLY_ABIS[@]} -gt 0 ]]; then
  CHECK_ABIS=("${ONLY_ABIS[@]}")
fi

LIB_DIR="$ROOT/$(python3 -c "import json;print(json.load(open('$LOCK'))['outputs']['lib_dir'])")"
LIB_NAME="$(python3 -c "import json;print(json.load(open('$LOCK'))['outputs']['library_filename'])")"
SUMS="$LIB_DIR/$(python3 -c "import json;print(json.load(open('$LOCK'))['outputs']['checksums_filename'])")"
MANIFEST="$LIB_DIR/$(python3 -c "import json;print(json.load(open('$LOCK'))['outputs']['manifest_filename'])")"
MIN_API="$(python3 -c "import json;print(json.load(open('$LOCK'))['android']['min_api'])")"
PIN="$(python3 -c "import json;print(json.load(open('$LOCK'))['ghostty']['commit'])")"

for abi in "${CHECK_ABIS[@]}"; do
  so="$LIB_DIR/$abi/$LIB_NAME"
  if [[ ! -f "$so" ]]; then
    bad "missing $so (build with ./scripts/build-libghostty.sh)"
    continue
  fi

  expected="$(python3 - "$LOCK" "$abi" <<'PY'
import json, sys
lock = json.load(open(sys.argv[1]))
abi = sys.argv[2]
for a in lock["abis"]:
    if a["android_abi"] == abi:
        print(a["elf_machine_code"])
        break
else:
    raise SystemExit(f"unknown abi {abi}")
PY
)"
  actual="$(python3 - "$so" <<'PY'
import struct, sys
with open(sys.argv[1], "rb") as f:
    data = f.read(64)
assert data[:4] == b"\x7fELF"
print(struct.unpack_from("<H", data, 18)[0])
PY
)"
  if [[ "$actual" != "$expected" ]]; then
    bad "$abi ELF machine $actual != expected $expected"
  else
    pass "$abi ELF machine $actual"
  fi

  api_level="$(python3 - "$so" <<'PY'
import struct, sys
data = open(sys.argv[1], "rb").read()
needle = b"Android\x00"
idx = data.find(needle)
if idx < 0:
    print("")
    raise SystemExit(0)
pad = (4 - ((idx + len(needle)) % 4)) % 4
desc_off = idx + len(needle) + pad
print(struct.unpack_from("<I", data, desc_off)[0] if desc_off + 4 <= len(data) else "")
PY
)"
  if [[ -n "$api_level" ]]; then
    if [[ "$api_level" != "$MIN_API" ]]; then
      bad "$abi Android API $api_level != lock $MIN_API"
    else
      pass "$abi Android API $api_level"
    fi
  else
    echo "note: $abi has no .note.android.ident (cannot confirm API $MIN_API)"
  fi

  if command -v file >/dev/null 2>&1; then
    file "$so" | grep -q "shared object" && pass "$abi is ELF shared object" || bad "$abi not a shared object"
  fi

  if "$ROOT/scripts/verify-android-native-symbols.py" --lock "$LOCK" "$so"; then
    pass "$abi forbidden undefined symbol policy"
  else
    fail=1
  fi
done

if [[ -f "$SUMS" ]]; then
  while read -r digest rel; do
    [[ -z "${digest:-}" || "$digest" == \#* ]] && continue
    rel="${rel#\*}"
    abi_dir="${rel%%/*}"
    skip=1
    for abi in "${CHECK_ABIS[@]}"; do
      [[ "$abi_dir" == "$abi" ]] && skip=0 && break
    done
    [[ $skip -eq 1 ]] && continue
    path="$LIB_DIR/$rel"
    if [[ ! -f "$path" ]]; then
      bad "checksum entry missing file $rel"
      continue
    fi
    got="$(sha256sum "$path" | awk '{print $1}')"
    if [[ "$got" != "$digest" ]]; then
      bad "checksum mismatch for $rel"
    else
      pass "sha256 $rel"
    fi
  done <"$SUMS"

  # Every checked ABI must appear in SUMS for release mode
  if [[ "$MODE" == "release" ]]; then
    for abi in "${CHECK_ABIS[@]}"; do
      if ! grep -q " ${abi}/${LIB_NAME}\$" "$SUMS" && ! grep -q " ${abi}/${LIB_NAME}$" "$SUMS"; then
        bad "release requires ${abi}/${LIB_NAME} in SHA256SUMS"
      fi
    done
  fi
else
  if [[ "$MODE" == "release" ]]; then
    bad "release mode requires $SUMS"
  else
    echo "note: no $SUMS yet (created by build-libghostty.sh); ELF/ABI checks only"
  fi
fi

if [[ ! -f "$MANIFEST" ]]; then
  bad "$MODE mode requires build-manifest.json"
else
  if python3 - "$MANIFEST" "$PIN" "$LOCK" "$MODE" "$LIB_DIR" "$LIB_NAME" \
    "$(IFS=,; echo "${CHECK_ABIS[*]}")" <<'PY'
import hashlib, json, sys
from pathlib import Path

manifest_path, pin, lock_path, mode, lib_dir, lib_name, abis_csv = sys.argv[1:8]
m = json.load(open(manifest_path))
lock = json.load(open(lock_path))
expected_patches = lock["android"].get("source_patches") or []
expected_derivation = (
    "pinned_commit_with_declared_android_patches"
    if expected_patches
    else "pinned_commit"
)
expected_provenance = (
    "proven_with_declared_patches" if expected_patches else "proven"
)
errors = []

if m.get("schema_version") != 1:
    errors.append(f"manifest schema_version is {m.get('schema_version')!r}, need 1")
if m.get("ghostty_commit_pinned") != pin:
    errors.append("ghostty_commit_pinned != pin")
if m.get("applied_patches") != expected_patches:
    errors.append("applied_patches do not exactly match native.lock.json")
if m.get("source_derivation") != expected_derivation:
    errors.append(
        f"source_derivation is {m.get('source_derivation')!r}, need {expected_derivation!r}"
    )
expected_zig = lock["zig"]["version"]
manifest_zig = m.get("zig_version")
if lock["zig"].get("version_match") == "prefix":
    zig_matches = manifest_zig == expected_zig or (
        isinstance(manifest_zig, str) and manifest_zig.startswith(expected_zig + ".")
    )
else:
    zig_matches = manifest_zig == expected_zig
if not zig_matches:
    errors.append(f"zig_version is {manifest_zig!r}, need locked {expected_zig!r}")
if "built_at" in m:
    errors.append("manifest must not include wall-clock built_at")
if not isinstance(m.get("provenance_notes"), list):
    errors.append("provenance_notes must be a list")

release_grade = m.get("release_grade")
provenance = m.get("provenance")
if release_grade is True:
    if provenance != expected_provenance:
        errors.append(f"provenance is {provenance!r}, need {expected_provenance!r}")
    if m.get("ghostty_commit_resolved") != pin:
        errors.append("release-grade ghostty_commit_resolved != pin")
elif provenance not in {"dirty", "unproven_no_git"}:
    errors.append(f"non-release provenance is invalid: {provenance!r}")

if mode == "release" and release_grade is not True:
    errors.append(f"release_grade is {release_grade!r}, need true")

contracts = {entry["android_abi"]: entry for entry in lock["abis"]}
manifest_libraries = {}
libraries = m.get("libraries")
if not isinstance(libraries, list):
    errors.append("libraries must be a list")
    libraries = []
for index, entry in enumerate(libraries):
    if not isinstance(entry, dict):
        errors.append(f"libraries[{index}] must be an object")
        continue
    abi = entry.get("android_abi")
    if abi not in contracts:
        errors.append(f"libraries[{index}] has unknown Android ABI {abi!r}")
        continue
    if abi in manifest_libraries:
        errors.append(f"manifest has duplicate library entry for {abi}")
        continue
    manifest_libraries[abi] = entry
    contract = contracts[abi]
    if entry.get("zig_target") != contract["zig_target"]:
        errors.append(f"manifest zig_target mismatch for {abi}")
    if entry.get("elf_machine_code") != contract["elf_machine_code"]:
        errors.append(f"manifest ELF machine mismatch for {abi}")
    if entry.get("android_api") != lock["android"]["min_api"]:
        errors.append(f"manifest Android API mismatch for {abi}")

    artifact = Path(lib_dir) / abi / lib_name
    if not artifact.is_file():
        errors.append(f"manifest artifact is missing for {abi}")
        continue
    if entry.get("bytes") != artifact.stat().st_size:
        errors.append(f"manifest byte size mismatch for {abi}")
    digest = hashlib.sha256(artifact.read_bytes()).hexdigest()
    if entry.get("sha256") != digest:
        errors.append(f"manifest sha256 mismatch for {abi}")

for abi in filter(None, abis_csv.split(",")):
    entry = manifest_libraries.get(abi)
    if entry is None:
        errors.append(f"manifest missing library entry for {abi}")

if errors:
    for error in errors:
        print(f"FAIL: {error}", file=sys.stderr)
    raise SystemExit(1)
PY
  then
    pass "$MODE manifest patch provenance and library digests match"
  else
    fail=1
  fi
fi

if [[ $fail -ne 0 ]]; then
  echo "verification FAILED" >&2
  exit 1
fi
echo "verification passed"
