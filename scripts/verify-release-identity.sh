#!/usr/bin/env bash
# Verify canonical beta identity sources and optional stage layout.
#
# Usage:
#   ./scripts/verify-release-identity.sh
#   ./scripts/verify-release-identity.sh --tag v0.1.0-beta.8
#   ./scripts/verify-release-identity.sh --stage dist-download/v0.1.0-beta.8

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

STAGE=""
RELEASE_TAG=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --stage) STAGE="${2:?}"; shift 2 ;;
    --tag) RELEASE_TAG="${2:?}"; shift 2 ;;
    -h|--help) sed -n '2,10p' "$0"; exit 0 ;;
    *) echo "error: unknown arg $1" >&2; exit 2 ;;
  esac
done

EXPECTED_VERSION="0.1.0-beta.8"
EXPECTED_PACKAGE="com.daoleno.zen"
EXPECTED_VERSION_CODE="8"
EXPECTED_IOS_BUILD_NUMBER="9"
EXPECTED_CERT_FP="C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD"

if [[ -n "$RELEASE_TAG" ]]; then
  [[ "$RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-beta\.[0-9]+$ ]] || {
    echo "error: release tag must exactly match vX.Y.Z-beta.N" >&2
    exit 1
  }
  [[ "$RELEASE_TAG" == "v${EXPECTED_VERSION}" ]] || {
    echo "error: release tag $RELEASE_TAG does not match tracked version v${EXPECTED_VERSION}" >&2
    exit 1
  }
  [[ "$(git cat-file -t "refs/tags/$RELEASE_TAG" 2>/dev/null || true)" == "tag" ]] || {
    echo "error: $RELEASE_TAG must be an annotated tag" >&2
    exit 1
  }
  TAG_COMMIT="$(git rev-parse "refs/tags/${RELEASE_TAG}^{commit}")"
  HEAD_COMMIT="$(git rev-parse HEAD)"
  [[ "$TAG_COMMIT" == "$HEAD_COMMIT" ]] || {
    echo "error: checked-out release tag does not resolve to HEAD" >&2
    exit 1
  }
  git show-ref --verify --quiet refs/remotes/origin/main || {
    echo "error: origin/main must be fetched before release tag validation" >&2
    exit 1
  }
  git merge-base --is-ancestor "$HEAD_COMMIT" refs/remotes/origin/main || {
    echo "error: release tag commit is not on origin/main" >&2
    exit 1
  }
  echo "ok: annotated release tag $RELEASE_TAG resolves to HEAD on origin/main"
fi

verify_ios_identity() {
  local variant="$1"
  local expected_name="$2"
  local expected_bundle="$3"
  ZEN_IOS_APP_VARIANT="$variant" \
    node - "$expected_name" "$expected_bundle" "$EXPECTED_PACKAGE" "$EXPECTED_IOS_BUILD_NUMBER" <<'JS'
const createConfig = require('./app/app.config.js');
const expectedDisplayName = process.argv[2];
const expectedBundle = process.argv[3];
const expectedAndroidPackage = process.argv[4];
const expectedIOSBuildNumber = process.argv[5];
const config = createConfig();

if (config.name !== 'Zen') {
  throw new Error(`top-level Expo name must remain Zen; got ${config.name}`);
}
if (config.version !== '0.1.0-beta.8') {
  throw new Error(`general/Android version must remain 0.1.0-beta.8; got ${config.version}`);
}
if (config.ios.bundleIdentifier !== expectedBundle) {
  throw new Error(`iOS bundle identifier is ${config.ios.bundleIdentifier}; expected ${expectedBundle}`);
}
if (config.ios.infoPlist.CFBundleDisplayName !== expectedDisplayName) {
  throw new Error(
    `iOS display name is ${config.ios.infoPlist.CFBundleDisplayName}; expected ${expectedDisplayName}`,
  );
}
if (config.ios.infoPlist.CFBundleShortVersionString !== '0.1.0') {
  throw new Error(
    `iOS marketing version must resolve to 0.1.0; got ${config.ios.infoPlist.CFBundleShortVersionString}`,
  );
}
if (config.ios.infoPlist.CFBundleVersion !== expectedIOSBuildNumber) {
  throw new Error(
    `iOS build number must resolve to ${expectedIOSBuildNumber}; got ${config.ios.infoPlist.CFBundleVersion}`,
  );
}
if (config.android.package !== expectedAndroidPackage) {
  throw new Error(
    `Android package changed under iOS variant: ${config.android.package}; expected ${expectedAndroidPackage}`,
  );
}
JS
}

# Validate both closed iOS branches regardless of the caller's active variant.
# This keeps the canonical release verifier meaningful inside Preview CI.
verify_ios_identity production "Zen" "com.daoleno.zen"
verify_ios_identity preview "Zen" "com.daoleno.zen.preview"

python3 - "$ROOT" "$EXPECTED_VERSION" "$EXPECTED_PACKAGE" "$EXPECTED_VERSION_CODE" "$EXPECTED_IOS_BUILD_NUMBER" "$EXPECTED_CERT_FP" "$STAGE" <<'PY'
import hashlib
import json
import os
import re
import sys
from pathlib import Path

root = Path(sys.argv[1])
exp_version = sys.argv[2]
exp_package = sys.argv[3]
exp_vc = int(sys.argv[4])
exp_ios_build = int(sys.argv[5])
exp_cert = sys.argv[6]
stage = sys.argv[7] if len(sys.argv) > 7 else ""

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
if "buildNumber" in (expo.get("ios") or {}):
    errors.append("app.base.json must not duplicate the reviewed iOS build-number source")
ios_build_path = root / "app/ios-build.json"
if not ios_build_path.is_file():
    errors.append("missing reviewed iOS build-number source: app/ios-build.json")
else:
    ios_build = json.loads(ios_build_path.read_text(encoding="utf-8")).get("buildNumber")
    if not isinstance(ios_build, int) or isinstance(ios_build, bool) or ios_build != exp_ios_build:
        errors.append(f"ios-build.json buildNumber: got {ios_build!r} want {exp_ios_build}")

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
# Local default only; must not force a host path that overrides CI Temurin JAVA_HOME.
if 'JAVA_HOME="${JAVA_HOME:-' not in apk_script and "JAVA_HOME=\"${JAVA_HOME:-" not in apk_script:
    errors.append("android-release-apk.sh must default JAVA_HOME only when unset")
if "verify-android-native-symbols.py" not in apk_script:
    errors.append("android-release-apk.sh must verify packaged Android native symbols")

apk_verifier = (root / "scripts/verify-apk-release.sh").read_text(encoding="utf-8")
if "verify-android-native-symbols.py" not in apk_verifier:
    errors.append("verify-apk-release.sh must verify packaged Android native symbols")

app_pkg = json.loads((root / "app/package.json").read_text(encoding="utf-8"))
build_apk = (app_pkg.get("scripts") or {}).get("build:apk") or ""
if not build_apk:
    errors.append("app/package.json missing scripts.build:apk")
else:
    if "/usr/lib/jvm/" in build_apk or "JAVA_HOME=" in build_apk:
        errors.append(
            "app/package.json build:apk must inherit caller JAVA_HOME "
            "(no hardcoded JAVA_HOME or /usr/lib/jvm path)"
        )
    if "NODE_ENV=production" not in build_apk:
        errors.append("app/package.json build:apk must set NODE_ENV=production")
    if "-PreactNativeArchitectures=arm64-v8a" not in build_apk:
        errors.append("app/package.json build:apk must pin arm64-v8a architectures")
    if "app-release-arm64.apk" not in build_apk:
        errors.append("app/package.json build:apk must produce app-release-arm64.apk")
    for sibling in ("build:apk:universal", "build:apk:debug"):
        s = (app_pkg.get("scripts") or {}).get(sibling) or ""
        if s and ("/usr/lib/jvm/" in s or re.search(r"JAVA_HOME=", s)):
            errors.append(f"app/package.json {sibling} must not hardcode JAVA_HOME")

vt_gradle = root / "app/modules/zen-terminal-vt/android/build.gradle"
if not vt_gradle.is_file():
    errors.append("missing zen-terminal-vt android/build.gradle")
else:
    gtxt = vt_gradle.read_text(encoding="utf-8")
    # Hardcoded dual abiFilters override -PreactNativeArchitectures and break arm64-only release.
    if re.search(r"abiFilters\s+'arm64-v8a'\s*,\s*'x86_64'", gtxt) or re.search(
        r'abiFilters\s+"arm64-v8a"\s*,\s*"x86_64"', gtxt
    ):
        errors.append(
            "zen-terminal-vt build.gradle must not hardcode abiFilters arm64-v8a,x86_64; "
            "derive ABIs from reactNativeArchitectures"
        )
    if "reactNativeArchitectures" not in gtxt:
        errors.append(
            "zen-terminal-vt build.gradle must honor project property reactNativeArchitectures"
        )
    if "zenTerminalAbis" not in gtxt:
        errors.append("zen-terminal-vt build.gradle must define zenTerminalAbis()")
    if "libghostty_vt.so" not in gtxt:
        errors.append(
            "zen-terminal-vt build.gradle should fail configuration when libghostty_vt.so is missing for a selected ABI"
        )
    # Script-level `def ZEN_TERMINAL_*` is invisible inside methods (Gradle Script capture).
    if re.search(r"(?m)^def\s+ZEN_TERMINAL_SUPPORTED_ABIS\b", gtxt):
        errors.append(
            "zen-terminal-vt build.gradle must not use script-level def ZEN_TERMINAL_SUPPORTED_ABIS "
            "(methods cannot capture it; keep supported ABIs local to zenTerminalAbis())"
        )

abi_gradle_check = root / "scripts/verify-zen-terminal-abi-gradle.sh"
if not abi_gradle_check.is_file():
    errors.append("missing scripts/verify-zen-terminal-abi-gradle.sh")
elif not os.access(abi_gradle_check, os.X_OK):
    errors.append("scripts/verify-zen-terminal-abi-gradle.sh must be executable")

for rel in (
    "scripts/stage-release.sh",
    "scripts/build-daemon-linux.sh",
    "scripts/verify-release-identity.sh",
    "scripts/verify-apk-release.sh",
    "scripts/materialize-android-keystore.sh",
    "scripts/sign-release-manifest.sh",
    "release/zen-update-public-key.pem",
    "docs/ci-release.md",
    ".github/workflows/release-artifacts.yml",
):
    if not (root / rel).is_file():
        errors.append(f"missing required release file: {rel}")

wf = (root / ".github/workflows/release-artifacts.yml").read_text(encoding="utf-8")
if "ZEN_ANDROID_KEYSTORE_BASE64" not in wf:
    errors.append("release-artifacts.yml must reference ZEN_ANDROID_KEYSTORE_BASE64")
if "workflow_dispatch" not in wf:
    errors.append("release-artifacts.yml must support workflow_dispatch")
if 'tags:' not in wf or '"v*.*.*-beta.*"' not in wf:
    errors.append("release-artifacts.yml must trigger from beta tag pushes")
if "types: [published]" in wf or "github.event.release" in wf:
    errors.append("release-artifacts.yml must not depend on a release-published event")
if "type: boolean" not in wf or "needs.validate.outputs.publish == 'true'" not in wf:
    errors.append("manual release recovery must require the reviewed publish boolean")
if "--tag" not in wf or "git fetch --no-tags origin main" not in wf:
    errors.append("release-artifacts.yml must validate tag identity and origin/main ancestry")
if "gh release upload" not in wf:
    errors.append("release-artifacts.yml must upload assets via gh release upload")
if "materialize-android-keystore" not in wf:
    errors.append("release-artifacts.yml must materialize keystore via helper script")
if "-Dorg.gradle.jvmargs=-Xmx6g" not in wf:
    errors.append("release-artifacts.yml must provide enough Gradle heap for release dex merging")
for required in (
    "zen-android-ghostty-output-v2-arm64-",
    "app/modules/zen-terminal-vt/android/src/main/cpp/ghostty",
):
    if required not in wf:
        errors.append(f"release-artifacts.yml missing complete native cache contract: {required}")
if "ZEN_UPDATE_SIGNING_KEY_BASE64" not in wf:
    errors.append("release-artifacts.yml must use the updater manifest signing secret")
for asset in (
    "zen-linux-amd64.tar.gz",
    "zen-linux-arm64.tar.gz",
    "zen-darwin-arm64.tar.gz",
    "release-manifest.json",
    "release-manifest.json.sig",
):
    if asset not in wf:
        errors.append(f"release-artifacts.yml missing release asset: {asset}")
if "GH_REPO" not in wf or "github.repository" not in wf:
    errors.append("release-artifacts.yml publish path must set GH_REPO from github.repository")
for required in (
    'needs: [validate, daemon, android]',
    'gh release create "$TAG" --verify-tag --draft --prerelease',
    'gh release edit "$TAG" --draft=false --prerelease',
):
    if required not in wf:
        errors.append(f"release-artifacts.yml missing gated release contract: {required}")

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

public_key_pem = root / "release/zen-update-public-key.pem"
updater_go = root / "daemon/selfupdate/selfupdate.go"
if public_key_pem.is_file() and updater_go.is_file():
    public_der_b64 = "".join(
        line.strip()
        for line in public_key_pem.read_text(encoding="utf-8").splitlines()
        if not line.startswith("-----")
    )
    if public_der_b64 not in updater_go.read_text(encoding="utf-8"):
        errors.append("daemon embedded update key does not match release public key")

# --- optional stage ---
if stage:
    stage_p = Path(stage)
    if ".." in Path(stage).parts:
        errors.append(f"stage path must not contain ..: {stage}")
    if not stage_p.is_dir():
        errors.append(f"stage dir missing: {stage}")
    else:
        required = [
            "zen-linux-amd64.tar.gz",
            "zen-linux-arm64.tar.gz",
            "zen-darwin-arm64.tar.gz",
            "SHA256SUMS",
            "release-manifest.json",
            "release-manifest.json.sig",
        ]
        for rel in required:
            if not (stage_p / rel).is_file():
                errors.append(f"stage missing {rel}")

        sums_path = stage_p / "SHA256SUMS"
        if sums_path.is_file():
            sums_text = sums_path.read_text(encoding="utf-8")
            for name in (
                "zen-linux-amd64.tar.gz",
                "zen-linux-arm64.tar.gz",
                "zen-darwin-arm64.tar.gz",
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

        ident_path = stage_p / "release-manifest.json"
        if ident_path.is_file():
            ident = json.loads(ident_path.read_text(encoding="utf-8"))
            if ident.get("schema_version") != 2:
                errors.append(f"stage identity schema_version: got {ident.get('schema_version')!r}")
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
                "zen-linux-amd64.tar.gz": "daemon_archive",
                "zen-linux-arm64.tar.gz": "daemon_archive",
                "zen-darwin-arm64.tar.gz": "daemon_archive",
            }
            for path, role in expected_roles.items():
                if roles.get(path) != role:
                    errors.append(f"release-manifest.json role for {path}: got {roles.get(path)!r} want {role!r}")
            daemon = ident.get("daemon") or {}
            if daemon.get("targets") != ["linux/amd64", "linux/arm64", "darwin/arm64"]:
                errors.append(f"release manifest daemon targets: got {daemon.get('targets')!r}")

            signature_path = stage_p / "release-manifest.json.sig"
            public_key = root / "release/zen-update-public-key.pem"
            if signature_path.is_file() and public_key.is_file():
                import subprocess
                verified = subprocess.run(
                    [
                        "openssl", "pkeyutl", "-verify", "-rawin", "-pubin",
                        "-inkey", str(public_key), "-in", str(ident_path),
                        "-sigfile", str(signature_path),
                    ],
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                )
                if verified.returncode != 0:
                    errors.append("release manifest Ed25519 signature verification failed")

        import tarfile
        for archive in (
            "zen-linux-amd64.tar.gz",
            "zen-linux-arm64.tar.gz",
            "zen-darwin-arm64.tar.gz",
        ):
            archive_path = stage_p / archive
            if not archive_path.is_file():
                continue
            with tarfile.open(archive_path, "r:gz") as tf:
                names = sorted(member.name.lstrip("./") for member in tf.getmembers() if member.isfile())
                expected = ["LICENSE", "NOTICE", "TRADEMARKS.md", "zen"]
                if names != expected:
                    errors.append(f"{archive} contents: got {names!r} want {expected!r}")

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
