#!/usr/bin/env bash
# Deterministic local staging for a beta release candidate.
#
# Stages under dist-download/v<version>/ (gitignored), always from a clean dir:
#   - Linux amd64/arm64 and macOS arm64 daemon archives
#   - SHA256SUMS, release-manifest.json, and its detached Ed25519 signature
#   - optional Android APK
#
# Does not read release keystores, commit, push, tag, or create a GitHub Release.
#
# Usage:
#   ./scripts/stage-release.sh
#   ./scripts/stage-release.sh --skip-build
#   ./scripts/stage-release.sh --with-apk
#   ./scripts/stage-release.sh --apk path/to.apk
#   ZEN_BUILD_TMPDIR=/durable/path ./scripts/stage-release.sh

set -euo pipefail
umask 077

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

SKIP_BUILD=0
WITH_APK=0
APK_PATH=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-build) SKIP_BUILD=1; shift ;;
    --with-apk) WITH_APK=1; shift ;;
    --apk)
      APK_PATH="${2:?}"
      shift 2
      ;;
    -h|--help)
      sed -n '2,20p' "$0"
      exit 0
      ;;
    *)
      echo "error: unknown arg $1" >&2
      exit 2
      ;;
  esac
done

if [[ -n "${ZEN_ANDROID_KEYSTORE:-}" && "$WITH_APK" -eq 0 && -z "$APK_PATH" ]]; then
  echo "note: ZEN_ANDROID_KEYSTORE is set but this run is not building an APK; env ignored."
fi

IDENTITY_JSON="$ROOT/app/app.base.json"
VERSION="$(python3 -c "import json;print(json.load(open('$IDENTITY_JSON'))['expo']['version'])")"
PACKAGE="$(python3 -c "import json;print(json.load(open('$IDENTITY_JSON'))['expo']['android']['package'])")"
VERSION_CODE="$(python3 -c "import json;print(json.load(open('$IDENTITY_JSON'))['expo']['android']['versionCode'])")"

# Validate version before any path join or rm (reject traversal / separators).
if ! python3 -c "
import re, sys
v = sys.argv[1]
if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9.]+)?', v):
    sys.exit('invalid version format: ' + v)
if any(x in v for x in ('/', '\\\\', '..', '\x00')):
    sys.exit('version must not contain path elements: ' + v)
" "$VERSION"; then
  echo "error: refusing unsafe or invalid version from app.base.json: ${VERSION@Q}" >&2
  exit 1
fi

NOTES_SRC="$ROOT/docs/releases/v${VERSION}.md"
if [[ ! -f "$NOTES_SRC" ]]; then
  echo "error: tracked release notes missing: docs/releases/v${VERSION}.md" >&2
  exit 1
fi

STAGE_DIR="$ROOT/dist-download/v${VERSION}"
# Resolve and ensure stage is exactly under dist-download/v<version>
STAGE_PARENT="$(cd "$ROOT/dist-download" 2>/dev/null && pwd || true)"
mkdir -p "$ROOT/dist-download"
STAGE_PARENT="$(cd "$ROOT/dist-download" && pwd)"
EXPECTED_STAGE="$STAGE_PARENT/v${VERSION}"

# Pre-build into a durable cache so a cross-platform release build never
# consumes the host's global temporary filesystem. Delegated sessions inject
# ZEN_BUILD_TMPDIR as their private lifecycle-owned resource directory.
default_build_tmpdir() {
  if [[ -n "${XDG_CACHE_HOME:-}" ]]; then
    printf '%s\n' "$XDG_CACHE_HOME/zen/build-tmp"
  elif [[ "$(uname -s)" == "Darwin" ]]; then
    printf '%s\n' "$HOME/Library/Caches/zen/build-tmp"
  else
    printf '%s\n' "$HOME/.cache/zen/build-tmp"
  fi
}
BUILD_TMP_ROOT="${ZEN_BUILD_TMPDIR:-$(default_build_tmpdir)}"
if [[ "$BUILD_TMP_ROOT" != /* ]]; then
  echo "error: ZEN_BUILD_TMPDIR must be an absolute path: $BUILD_TMP_ROOT" >&2
  exit 1
fi
mkdir -p "$BUILD_TMP_ROOT"
chmod 700 "$BUILD_TMP_ROOT"
BUILD_TMP="$(mktemp -d "$BUILD_TMP_ROOT/zen-stage-build.XXXXXX")"
cleanup() { rm -rf "$BUILD_TMP"; }
trap cleanup EXIT

export ZEN_VERSION="$VERSION"

if [[ $SKIP_BUILD -eq 0 ]]; then
  "$ROOT/scripts/build-daemon-linux.sh" --out-dir "$BUILD_TMP"
else
  # Prefer existing stage binaries if present; else staging default from build script.
  for f in zen-linux-amd64 zen-linux-arm64 zen-darwin-arm64; do
    if [[ -f "$EXPECTED_STAGE/$f" ]]; then
      cp -f "$EXPECTED_STAGE/$f" "$BUILD_TMP/$f"
    elif [[ -f "$EXPECTED_STAGE/bin/$f" ]]; then
      cp -f "$EXPECTED_STAGE/bin/$f" "$BUILD_TMP/$f"
    elif [[ -f "$ROOT/dist-download/staging/bin/$f" ]]; then
      cp -f "$ROOT/dist-download/staging/bin/$f" "$BUILD_TMP/$f"
    else
      echo "error: --skip-build but missing $f (looked in stage and dist-download/staging/bin)" >&2
      exit 1
    fi
  done
fi

for f in zen-linux-amd64 zen-linux-arm64 zen-darwin-arm64; do
  if [[ ! -f "$BUILD_TMP/$f" ]]; then
    echo "error: build output missing $f" >&2
    exit 1
  fi
done

# Safe clean: only the exact validated stage directory under dist-download.
if [[ -e "$EXPECTED_STAGE" || -L "$EXPECTED_STAGE" ]]; then
  # Refuse if path is not exactly dist-download/v<version> after resolve.
  resolved="$(python3 -c "
import os, sys
path = sys.argv[1]
parent = sys.argv[2]
version = sys.argv[3]
real = os.path.realpath(path)
expect = os.path.realpath(os.path.join(parent, 'v' + version))
if real != expect:
    sys.exit(f'refuse rm: {real} != {expect}')
if not real.startswith(os.path.realpath(parent) + os.sep):
    sys.exit(f'refuse rm outside dist-download: {real}')
print(real)
" "$EXPECTED_STAGE" "$STAGE_PARENT" "$VERSION")"
  echo "Cleaning stage directory: $resolved"
  rm -rf --one-file-system -- "$resolved"
fi

mkdir -p "$EXPECTED_STAGE"
STAGE_DIR="$EXPECTED_STAGE"

# Package each daemon with the legal files that apply to the daemon distribution.
# gzip -n and normalized tar metadata keep archives stable across CI runs.
ARCHIVE_EPOCH="${SOURCE_DATE_EPOCH:-0}"
package_daemon() {
  local binary_name="$1"
  local archive_name="$2"
  local package_dir="$BUILD_TMP/package"
  rm -rf "$package_dir"
  mkdir -p "$package_dir"
  cp -f "$BUILD_TMP/$binary_name" "$package_dir/zen"
  chmod +x "$package_dir/zen"
  cp -f "$ROOT/LICENSE" "$package_dir/LICENSE"
  cp -f "$ROOT/NOTICE" "$package_dir/NOTICE"
  cp -f "$ROOT/TRADEMARKS.md" "$package_dir/TRADEMARKS.md"
  tar --sort=name --mtime="@${ARCHIVE_EPOCH}" --owner=0 --group=0 --numeric-owner \
    -C "$package_dir" -cf - LICENSE NOTICE TRADEMARKS.md zen | gzip -n > "$STAGE_DIR/$archive_name"
}

package_daemon zen-linux-amd64 zen-linux-amd64.tar.gz
package_daemon zen-linux-arm64 zen-linux-arm64.tar.gz
package_daemon zen-darwin-arm64 zen-darwin-arm64.tar.gz

STAGED_APK=""
if [[ $WITH_APK -eq 1 ]]; then
  "$ROOT/scripts/android-release-apk.sh"
fi

if [[ -n "$APK_PATH" ]]; then
  if [[ ! -f "$APK_PATH" ]]; then
    echo "error: --apk path not found" >&2
    exit 1
  fi
  STAGED_APK="$STAGE_DIR/zen-android-arm64-v${VERSION}.apk"
  cp -f "$APK_PATH" "$STAGED_APK"
elif [[ $WITH_APK -eq 1 ]]; then
  if compgen -G "$ROOT/dist-download/android-native/zen-android-arm64-*.apk" > /dev/null; then
    newest="$(ls -t "$ROOT"/dist-download/android-native/zen-android-arm64-*.apk | head -1)"
    STAGED_APK="$STAGE_DIR/zen-android-arm64-v${VERSION}.apk"
    cp -f "$newest" "$STAGED_APK"
  fi
fi

if [[ -n "$STAGED_APK" ]]; then
  "$ROOT/scripts/verify-apk-notice.sh" "$STAGED_APK"
fi

# Checksums for every payload file (not identity.json / SHA256SUMS itself).
SUMS="$STAGE_DIR/SHA256SUMS"
(
  cd "$STAGE_DIR"
  files=(
    zen-linux-amd64.tar.gz
    zen-linux-arm64.tar.gz
    zen-darwin-arm64.tar.gz
  )
  if [[ -n "$STAGED_APK" && -f "$(basename "$STAGED_APK")" ]]; then
    files+=("$(basename "$STAGED_APK")")
  fi
  for rel in "${files[@]}"; do
    sha256sum "$rel"
  done | LC_ALL=C sort -k2
) > "$SUMS"

python3 - "$STAGE_DIR" "$VERSION" "$PACKAGE" "$VERSION_CODE" "${STAGED_APK:-}" <<'PY'
import hashlib, json, sys
from pathlib import Path

stage, version, package, version_code, staged_apk = sys.argv[1:6]
stage_p = Path(stage)

def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()

def artifact(rel: str, role: str, **extra):
    p = stage_p / rel
    if not p.is_file():
        raise SystemExit(f"missing staged file: {rel}")
    entry = {
        "path": rel,
        "role": role,
        "sha256": sha256(p),
        "size": p.stat().st_size,
    }
    entry.update(extra)
    return entry

artifacts = [
    artifact("zen-linux-amd64.tar.gz", "daemon_archive", goos="linux", goarch="amd64"),
    artifact("zen-linux-arm64.tar.gz", "daemon_archive", goos="linux", goarch="arm64"),
    artifact("zen-darwin-arm64.tar.gz", "daemon_archive", goos="darwin", goarch="arm64"),
]

if staged_apk:
    apk_p = Path(staged_apk)
    if apk_p.is_file():
        artifacts.append(
            artifact(
                apk_p.name,
                "android_sideload_apk",
                abi="arm64-v8a",
                status="staged_not_published",
            )
        )

identity = {
    "schema_version": 2,
    "product": "zen",
    "version": version,
    "android": {
        "package": package,
        "version_name": version,
        "version_code": int(version_code),
        "canonical_config": "app/app.base.json",
        "certificate_sha256_fingerprint": (
            "C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:"
            "4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD"
        ),
    },
    "daemon": {
        "module": "github.com/daoleno/zen/daemon",
        "targets": ["linux/amd64", "linux/arm64", "darwin/arm64"],
        "cgo": False,
        "artifact_layout": "platform_archives",
    },
    "artifacts": artifacts,
    "notes": {
        "release_status": "staged",
        "github_release": "publication_is_separate",
        "release_notes_source": f"docs/releases/v{version}.md",
        "signed_apk": "requires_maintainer_keystore_via_env_not_in_repo",
    },
}
(stage_p / "release-manifest.json").write_text(json.dumps(identity, indent=2) + "\n", encoding="utf-8")
print(f"wrote {stage_p / 'release-manifest.json'}")
PY

"$ROOT/scripts/sign-release-manifest.sh" "$STAGE_DIR/release-manifest.json"

echo ""
echo "Staged:   $STAGE_DIR"
echo "Version:  $VERSION"
echo "Package:  $PACKAGE"
echo "Checksums:$SUMS"
ls -la "$STAGE_DIR"
echo "Done (no commit/push/tag/release)."
