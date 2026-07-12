#!/usr/bin/env bash
# Verify canonical beta identity sources and optional stage layout.
#
# Usage:
#   ./scripts/verify-release-identity.sh
#   ./scripts/verify-release-identity.sh --stage dist-download/v0.1.0-beta.1

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

STAGE=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --stage) STAGE="${2:?}"; shift 2 ;;
    -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
    *) echo "error: unknown arg $1" >&2; exit 2 ;;
  esac
done

EXPECTED_VERSION="0.1.0-beta.1"
EXPECTED_PACKAGE="com.daoleno.zen"
EXPECTED_VERSION_CODE="1"
EXPECTED_CERT_FP="C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD"

python3 - "$ROOT" "$EXPECTED_VERSION" "$EXPECTED_PACKAGE" "$EXPECTED_VERSION_CODE" "$EXPECTED_CERT_FP" "$STAGE" <<'PY'
import hashlib
import json
import re
import sys
from pathlib import Path

root = Path(sys.argv[1])
exp_version = sys.argv[2]
exp_package = sys.argv[3]
exp_vc = int(sys.argv[4])
exp_cert = sys.argv[5]
stage = sys.argv[6] if len(sys.argv) > 6 else ""

errors = []

def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()

# --- tracked identity ---
base = json.loads((root / "app/app.base.json").read_text(encoding="utf-8"))
expo = base["expo"]
android = expo["android"]
if expo.get("version") != exp_version:
    errors.append(f"app.base.json version: got {expo.get('version')!r} want {exp_version!r}")
if android.get("package") != exp_package:
    errors.append(f"app.base.json package: got {android.get('package')!r} want {exp_package!r}")
if int(android.get("versionCode", -1)) != exp_vc:
    errors.append(f"app.base.json versionCode: got {android.get('versionCode')!r} want {exp_vc!r}")

version_go = (root / "daemon/cmd/zen/version.go").read_text(encoding="utf-8")
m = re.search(r'var Version = "([^"]+)"', version_go)
if not m:
    errors.append("daemon/cmd/zen/version.go: Version default not found")
elif m.group(1) != exp_version:
    errors.append(f"version.go default: got {m.group(1)!r} want {exp_version!r}")

crash_line = ""
for line in (root / "scripts/capture-android-crash.sh").read_text(encoding="utf-8").splitlines():
    if line.startswith("PACKAGE_NAME="):
        crash_line = line
        break
if exp_package not in crash_line:
    errors.append("capture-android-crash.sh default package does not match")

cfg = (root / "app/app.config.js").read_text(encoding="utf-8")
if "app.base.json" not in cfg:
    errors.append("app.config.js must load app.base.json as identity source")

apk_script = (root / "scripts/android-release-apk.sh").read_text(encoding="utf-8")
if "expo prebuild --clean" not in apk_script and "prebuild --clean" not in apk_script:
    errors.append("android-release-apk.sh must use expo prebuild --clean for package identity")
if "versionName" not in apk_script or "versionCode" not in apk_script:
    errors.append("android-release-apk.sh must assert generated versionName/versionCode identity")

for rel in (
    "scripts/stage-release.sh",
    "scripts/build-daemon-linux.sh",
    "scripts/verify-release-identity.sh",
    ".github/workflows/release-artifacts.yml",
):
    if not (root / rel).is_file():
        errors.append(f"missing required release file: {rel}")

notes_path = root / f"docs/releases/v{exp_version}.md"
if not notes_path.is_file():
    errors.append(f"missing tracked release notes: {notes_path.relative_to(root)}")
else:
    notes = notes_path.read_text(encoding="utf-8")
    if exp_version not in notes:
        errors.append("release notes missing version string")
    if exp_package not in notes:
        errors.append("release notes missing android package")
    if exp_cert not in notes:
        errors.append("release notes missing official certificate SHA-256 fingerprint")
    for needle in (
        "unknown sources",
        "Play Protect",
        "Obtainium",
        "iOS",
        "Play Store",
        "zen-linux-amd64",
        "zen-linux-arm64",
        "versionCode",
    ):
        if needle not in notes:
            errors.append(f"release notes missing required topic: {needle!r}")

for rel in ("LICENSE", "NOTICE", "TRADEMARKS.md", "app/assets/notices/GHOSTTY-MIT.txt"):
    if not (root / rel).is_file():
        errors.append(f"missing repo legal/notice file: {rel}")

# --- optional stage ---
if stage:
    stage_p = Path(stage)
    if ".." in Path(stage).parts:
        errors.append(f"stage path must not contain ..: {stage}")
    if not stage_p.is_dir():
        errors.append(f"stage dir missing: {stage}")
    else:
        required = [
            "zen-linux-amd64",
            "zen-linux-arm64",
            "LICENSE",
            "NOTICE",
            "TRADEMARKS.md",
            "GHOSTTY-MIT.txt",
            "RELEASE_NOTES.md",
            "SHA256SUMS",
            "identity.json",
        ]
        for rel in required:
            if not (stage_p / rel).is_file():
                errors.append(f"stage missing {rel}")

        # Nested bin/ layout is not the release contract.
        if (stage_p / "bin" / "zen-linux-amd64").is_file() and not (stage_p / "zen-linux-amd64").is_file():
            errors.append("stage has nested bin/ binaries but missing top-level zen-linux-amd64")

        sums_path = stage_p / "SHA256SUMS"
        if sums_path.is_file():
            sums_text = sums_path.read_text(encoding="utf-8")
            for name in (
                "zen-linux-amd64",
                "zen-linux-arm64",
                "LICENSE",
                "NOTICE",
                "TRADEMARKS.md",
                "GHOSTTY-MIT.txt",
                "RELEASE_NOTES.md",
            ):
                if name not in sums_text:
                    errors.append(f"SHA256SUMS missing {name}")
            # Verify checksums match files
            for line in sums_text.splitlines():
                line = line.strip()
                if not line or line.startswith("#"):
                    continue
                parts = line.split()
                if len(parts) < 2:
                    errors.append(f"bad SHA256SUMS line: {line!r}")
                    continue
                digest, rel = parts[0], parts[-1]
                p = stage_p / rel
                if not p.is_file():
                    errors.append(f"SHA256SUMS entry missing file: {rel}")
                    continue
                got = sha256_file(p)
                if got != digest:
                    errors.append(f"SHA256 mismatch for {rel}: sums={digest[:12]}… file={got[:12]}…")

        ident_path = stage_p / "identity.json"
        if ident_path.is_file():
            ident = json.loads(ident_path.read_text(encoding="utf-8"))
            if ident.get("version") != exp_version:
                errors.append(f"stage identity version: got {ident.get('version')!r}")
            and_id = ident.get("android") or {}
            if and_id.get("package") != exp_package:
                errors.append(f"stage identity package: got {and_id.get('package')!r}")
            if int(and_id.get("version_code", -1)) != exp_vc:
                errors.append("stage identity version_code mismatch")
            if and_id.get("certificate_sha256_fingerprint") != exp_cert:
                errors.append("stage identity missing/wrong certificate fingerprint")
            roles = {a.get("path"): a.get("role") for a in ident.get("artifacts") or []}
            expected_roles = {
                "zen-linux-amd64": "daemon",
                "zen-linux-arm64": "daemon",
                "LICENSE": "license",
                "NOTICE": "notice",
                "TRADEMARKS.md": "trademarks",
                "GHOSTTY-MIT.txt": "third_party_notice",
                "RELEASE_NOTES.md": "release_notes",
            }
            for path, role in expected_roles.items():
                if roles.get(path) != role:
                    errors.append(f"identity.json role for {path}: got {roles.get(path)!r} want {role!r}")

        # RELEASE_NOTES must come from tracked template (fingerprint present).
        rn = stage_p / "RELEASE_NOTES.md"
        if rn.is_file() and exp_cert not in rn.read_text(encoding="utf-8"):
            errors.append("staged RELEASE_NOTES.md missing certificate fingerprint")

        # Legal files must match repo sources byte-for-byte.
        pairs = [
            ("LICENSE", root / "LICENSE"),
            ("NOTICE", root / "NOTICE"),
            ("TRADEMARKS.md", root / "TRADEMARKS.md"),
            ("GHOSTTY-MIT.txt", root / "app/assets/notices/GHOSTTY-MIT.txt"),
            ("RELEASE_NOTES.md", notes_path if notes_path.is_file() else None),
        ]
        for rel, src in pairs:
            dst = stage_p / rel
            if src is not None and dst.is_file() and src.is_file():
                if dst.read_bytes() != src.read_bytes():
                    errors.append(f"stage {rel} differs from tracked source {src.relative_to(root)}")

if errors:
    print("FAIL: release identity checks", file=sys.stderr)
    for e in errors:
        print(f"  - {e}", file=sys.stderr)
    raise SystemExit(1)

print(f"ok: identity version={exp_version} package={exp_package} versionCode={exp_vc}")
print(f"ok: certificate fingerprint present in tracked notes")
if stage:
    print(f"ok: stage {stage}")
PY
