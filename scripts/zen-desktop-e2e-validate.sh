#!/usr/bin/env bash
# Validate the isolated coupled Moonlight E2E manifest. This script never
# launches QEMU, the emulator, adb install or Sunshine; it only checks pinned
# artifacts and prints the exact argv for Brain review.
#
# Usage: scripts/zen-desktop-e2e-validate.sh [--launch-ready]
#   --launch-ready also requires the post-approval build hashes (APK and guest
#   Sunshine binary) to be recorded in the manifest.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MANIFEST="${ZEN_E2E_MANIFEST:-/home/daoleno/.zen/artifacts/2026-09-13-desktop-moonlight-e2e/manifest.json}"
MODE="${1:-}"

python3 - "$ROOT" "$MANIFEST" "$MODE" <<'PYEOF'
import hashlib
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
manifest_path = pathlib.Path(sys.argv[2])
mode = sys.argv[3]

failures = []
pending = []

def fail(message):
    failures.append(message)

def check(condition, message):
    if not condition:
        fail(message)

def sha256(path):
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()

manifest = json.loads(manifest_path.read_text())
check(manifest.get("schema") == "zen-desktop-moonlight-e2e/v1", "unexpected manifest schema")
if mode == "--launch-ready":
    check(manifest.get("approved") is True, "launch-ready requires approved=true")
else:
    check(manifest.get("approved") in (True, False), "approved must be a boolean")

lock = json.loads((root / "app/modules/zen-remote-desktop/native.lock.json").read_text())
pins = manifest["pins"]
check(pins["zen_client_core_commit"] == lock["moonlight_common_c"]["commit"], "client core pin mismatch")
check(pins["moonlight_app_layer_commit"] == lock["application_layer"]["commit"], "app layer pin mismatch")
check(pins["sunshine_commit"] == lock["host"]["commit"], "sunshine pin mismatch")

for abi, key in (("arm64-v8a", "arm64_libcrypto_sha256"), ("x86_64", "x86_64_libcrypto_sha256")):
    prefix = root / "app/modules/zen-remote-desktop/third_party/openssl" / abi / "lib/libcrypto.a"
    if not prefix.is_file():
        fail(f"missing OpenSSL prefix {prefix}")
    else:
        check(sha256(prefix) == pins["openssl"][key], f"OpenSSL {abi} libcrypto hash mismatch")

for name in ("vm_base_image", "qemu_binary"):
    entry = pins[name]
    path = pathlib.Path(entry["path"])
    if not path.is_file():
        fail(f"missing {name}: {path}")
    else:
        actual = sha256(path)
        check(actual == entry["sha256"], f"{name} hash mismatch: {actual}")

adb = pathlib.Path(pins["adb"]["path"])
emulator = pathlib.Path(pins["emulator"]["path"])
check(adb.is_file(), f"missing adb {adb}")
check(emulator.is_file(), f"missing emulator {emulator}")
avd_ini = pathlib.Path.home() / ".android/avd" / f"{pins['avd']['name']}.ini"
check(avd_ini.is_file(), f"missing owned AVD {avd_ini}")

for section in ("vm", "apk", "cleanup"):
    blob = json.dumps(manifest[section])
    for marker in ("<", ">", "TODO", "FILL", "PLACEHOLDER"):
        if marker in blob:
            # $ZEN_RUN_DIR is a real executor variable, not a placeholder.
            if marker in ("<", ">") and "$ZEN_RUN_DIR" in blob and "<" not in blob.replace("$ZEN_RUN_DIR", ""):
                continue
            fail(f"manifest {section} still contains placeholder marker {marker!r}")

if mode == "--launch-ready":
    if not manifest["apk"].get("sha256"):
        pending.append("apk.sha256 (build :app:assembleDebug after approval and record)")
    if not manifest["vm"]["sunshine"].get("binary_sha256"):
        pending.append("vm.sunshine.binary_sha256 (build pinned Sunshine in the guest and record)")
    for item in pending:
        fail(f"launch-ready: missing {item}")
else:
    if not manifest["apk"].get("sha256"):
        pending.append("apk.sha256")
    if not manifest["vm"]["sunshine"].get("binary_sha256"):
        pending.append("vm.sunshine.binary_sha256")

print("manifest:", manifest_path)
print("approved:", manifest["approved"])
print("client core:", pins["zen_client_core_commit"])
print("app layer:  ", pins["moonlight_app_layer_commit"])
print("sunshine:   ", pins["sunshine_commit"])
print("vm image:   ", pins["vm_base_image"]["sha256"])
print("qemu:       ", pins["qemu_binary"]["sha256"])
print("avd:        ", pins["avd"]["name"], pins["avd"]["serial"])
print("combined peak upper bound MiB:", manifest["budget"]["combined_peak_upper_bound_mib"])
print("remaining after peak MiB:     ", manifest["budget"]["remaining_after_peak_mib"])
if pending:
    print("pending after approval:", ", ".join(pending))
print("launch argv (display only; never executed by this validator):")
print("  ", " ".join(manifest["vm"]["argv"]))
print("  ", " ".join(manifest["apk"]["install_argv"]))
print("cleanup argv:")
for command in manifest["cleanup"]["exact"]:
    print("  ", command)
if failures:
    print("FAILED:")
    for message in failures:
        print("  -", message)
    sys.exit(1)
print("OK: preparation checks passed")
PYEOF
