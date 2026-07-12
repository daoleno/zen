#!/usr/bin/env bash
# Verify a signed release APK against tracked product identity.
#
# Checks:
#   - package / versionName / versionCode match app/app.base.json
#   - only arm64-v8a native libs (no armeabi-v7a / x86 / x86_64)
#   - Ghostty MIT notice embedded (verify-apk-notice.sh)
#   - signing cert SHA-256 matches official public fingerprint in release notes /
#     verify-release-identity expected constant
#
# Requires: python3, unzip; aapt or aapt2 preferred; apksigner or keytool for certs.
# Does not require or print signing secrets.
#
# Usage:
#   ./scripts/verify-apk-release.sh path/to/app.apk

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

APK="${1:-}"
if [[ -z "$APK" || ! -f "$APK" ]]; then
  echo "error: APK path required" >&2
  exit 2
fi
APK="$(cd "$(dirname "$APK")" && pwd)/$(basename "$APK")"

EXPECTED_CERT="C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD"

find_tool() {
  local name="$1"
  if command -v "$name" >/dev/null 2>&1; then
    command -v "$name"
    return 0
  fi
  local root="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
  if [[ -n "$root" && -d "$root" ]]; then
    local hit
    hit="$(find "$root/build-tools" -type f -name "$name" 2>/dev/null | sort -V | tail -1 || true)"
    if [[ -n "$hit" ]]; then
      echo "$hit"
      return 0
    fi
  fi
  return 1
}

AAPT=""
AAPT="$(find_tool aapt 2>/dev/null || true)"
if [[ -z "$AAPT" ]]; then
  AAPT="$(find_tool aapt2 2>/dev/null || true)"
fi
APKSIGNER="$(find_tool apksigner 2>/dev/null || true)"

"$ROOT/scripts/verify-apk-notice.sh" "$APK"

python3 - "$APK" "$ROOT/app/app.base.json" "$EXPECTED_CERT" "$AAPT" "$APKSIGNER" <<'PY'
import re
import subprocess
import sys
import zipfile
from pathlib import Path
import json

apk, base_json, exp_cert, aapt, apksigner = sys.argv[1:6]
base = json.loads(Path(base_json).read_text(encoding="utf-8"))
exp_pkg = base["expo"]["android"]["package"]
exp_ver = base["expo"]["version"]
exp_vc = int(base["expo"]["android"]["versionCode"])
errors = []

def normalize_fp(s: str) -> str:
    hexonly = re.sub(r"[^0-9A-Fa-f]", "", s).upper()
    if len(hexonly) != 64:
        return ""
    return ":".join(hexonly[i : i + 2] for i in range(0, 64, 2))

# --- ABI: only arm64-v8a under lib/ ---
with zipfile.ZipFile(apk) as z:
    abis = set()
    for name in z.namelist():
        # lib/<abi>/...
        m = re.match(r"lib/([^/]+)/", name)
        if m:
            abis.add(m.group(1))
    if not abis:
        # Some packaging may place jni under different paths; also scan lib/**
        for name in z.namelist():
            if "/lib/" in name or name.startswith("lib/"):
                parts = name.split("/")
                for i, p in enumerate(parts):
                    if p == "lib" and i + 1 < len(parts):
                        abis.add(parts[i + 1])
    allowed = {"arm64-v8a"}
    unexpected = abis - allowed
    if "arm64-v8a" not in abis:
        errors.append(f"missing arm64-v8a native libs (found abis={sorted(abis)})")
    if unexpected:
        errors.append(f"unexpected ABIs in APK (must be arm64-only): {sorted(unexpected)}")

# --- package / version via aapt dump badging ---
pkg = ver = None
vc = None
if aapt:
    try:
        out = subprocess.check_output([aapt, "dump", "badging", apk], text=True, stderr=subprocess.STDOUT)
    except Exception as e:
        # aapt2 uses different subcommands; try dump badging anyway
        try:
            out = subprocess.check_output([aapt, "dump", "badging", apk], text=True, stderr=subprocess.STDOUT)
        except Exception as e2:
            errors.append(f"aapt dump badging failed: {e2}")
            out = ""
    m = re.search(r"package: name='([^']+)' versionCode='(\d+)' versionName='([^']+)'", out)
    if m:
        pkg, vc, ver = m.group(1), int(m.group(2)), m.group(3)
    else:
        errors.append("could not parse package line from aapt dump badging")
else:
    errors.append("aapt/aapt2 not found; install Android build-tools to verify package identity")

if pkg is not None and pkg != exp_pkg:
    errors.append(f"package: got {pkg!r} want {exp_pkg!r}")
if ver is not None and ver != exp_ver:
    errors.append(f"versionName: got {ver!r} want {exp_ver!r}")
if vc is not None and vc != exp_vc:
    errors.append(f"versionCode: got {vc!r} want {exp_vc!r}")

# --- certificate SHA-256 ---
fps = []
if apksigner:
    try:
        out = subprocess.check_output(
            [apksigner, "verify", "--print-certs", apk],
            text=True,
            stderr=subprocess.STDOUT,
        )
        for line in out.splitlines():
            # Signer #1 certificate SHA-256 digest: aa:bb:...
            if "SHA-256" in line or "SHA256" in line:
                # take last token that looks like hex/colons
                cand = line.split("digest:")[-1].strip() if "digest:" in line.lower() else line
                # extract hex sequences
                for m in re.finditer(r"((?:[0-9A-Fa-f]{2}:){31}[0-9A-Fa-f]{2})|([0-9A-Fa-f]{64})", cand):
                    fp = normalize_fp(m.group(0))
                    if fp:
                        fps.append(fp)
    except Exception as e:
        errors.append(f"apksigner verify failed: {e}")
else:
    # keytool -printcert -jarfile
    try:
        out = subprocess.check_output(
            ["keytool", "-printcert", "-jarfile", apk],
            text=True,
            stderr=subprocess.STDOUT,
        )
        for m in re.finditer(r"SHA256:\s*([0-9A-Fa-f:]+)", out, re.I):
            fp = normalize_fp(m.group(1))
            if fp:
                fps.append(fp)
    except Exception as e:
        errors.append(f"keytool cert inspect failed: {e}")

exp_norm = normalize_fp(exp_cert)
if not fps:
    errors.append("could not extract signing certificate SHA-256")
elif exp_norm not in fps:
    errors.append(
        f"certificate SHA-256 mismatch: want {exp_cert} (got {fps[0] if fps else 'none'})"
    )

if errors:
    print("FAIL: APK release verification", file=sys.stderr)
    for e in errors:
        print(f"  - {e}", file=sys.stderr)
    raise SystemExit(1)

print(f"ok: APK package={exp_pkg} versionName={exp_ver} versionCode={exp_vc}")
print("ok: APK arm64-v8a only")
print("ok: APK Ghostty MIT notice")
print(f"ok: APK certificate SHA-256 matches public identity")
PY
