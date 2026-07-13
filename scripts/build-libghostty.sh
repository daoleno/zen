#!/usr/bin/env bash
# Build libghostty-vt for the Android ABIs declared in native.lock.json.
#
# Usage:
#   ./scripts/build-libghostty.sh
#   GHOSTTY_SRC=/path/to/ghostty ./scripts/build-libghostty.sh
#   ./scripts/build-libghostty.sh --abis arm64-v8a
#   ALLOW_DIRTY_GHOSTTY=1 ./scripts/build-libghostty.sh   # developer only; non-release
#   ALLOW_UNPROVEN_GHOSTTY=1 ./scripts/build-libghostty.sh  # no .git; non-release
#
# Requires: zig (version from native.lock.json), git, python3
# Ghostty source: GHOSTTY_SRC, or a clone under ${ZEN_GHOSTTY_CACHE:-$HOME/.cache/zen/ghostty}

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/app/modules/zen-terminal-vt/native.lock.json"
NOTICE_SRC="$ROOT/app/assets/notices/GHOSTTY-MIT.txt"

if [[ ! -f "$LOCK" ]]; then
  echo "error: missing lockfile $LOCK" >&2
  exit 1
fi

if [[ ! -f "$NOTICE_SRC" ]]; then
  echo "error: missing Ghostty MIT notice $NOTICE_SRC" >&2
  exit 1
fi

LOCK_ENV="$(mktemp "${TMPDIR:-/tmp}/zen-libghostty-lock.XXXXXX")"
cleanup() { rm -f "$LOCK_ENV"; }
trap cleanup EXIT

python3 - "$LOCK" >"$LOCK_ENV" <<'PY'
import json, sys
lock = json.load(open(sys.argv[1]))
print(f"LOCK_GHOSTTY_REPO={lock['ghostty']['repository']}")
print(f"LOCK_GHOSTTY_COMMIT={lock['ghostty']['commit']}")
print(f"LOCK_HEADERS_SHA256={lock['ghostty']['headers_sha256']}")
print(f"LOCK_ZIG_VERSION={lock['zig']['version']}")
print(f"LOCK_LIB_DIR={lock['outputs']['lib_dir']}")
print(f"LOCK_LIB_NAME={lock['outputs']['library_filename']}")
print(f"LOCK_HEADERS_DIR={lock['outputs']['headers_dir']}")
print(f"LOCK_CHECKSUMS={lock['outputs']['checksums_filename']}")
print(f"LOCK_MANIFEST={lock['outputs']['manifest_filename']}")
print(f"LOCK_MIN_API={lock['android']['min_api']}")
abis = lock["abis"]
print("LOCK_ABI_COUNT=%d" % len(abis))
allowed = []
for i, a in enumerate(abis):
    print(f"LOCK_ABI_{i}_ANDROID={a['android_abi']}")
    print(f"LOCK_ABI_{i}_ZIG={a['zig_target']}")
    print(f"LOCK_ABI_{i}_ELF={a['elf_machine_code']}")
    allowed.append(a["android_abi"])
print("LOCK_ALLOWED_ABIS=" + ",".join(allowed))
PY
# shellcheck disable=SC1090
source "$LOCK_ENV"

ONLY_ABIS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --abis)
      shift
      IFS=',' read -r -a ONLY_ABIS <<<"${1:-}"
      shift || true
      ;;
    -h|--help)
      sed -n '2,14p' "$0"
      exit 0
      ;;
    *)
      echo "error: unknown arg: $1" >&2
      exit 2
      ;;
  esac
done

IFS=',' read -r -a ALLOWED_ABIS <<<"$LOCK_ALLOWED_ABIS"
if [[ ${#ONLY_ABIS[@]} -gt 0 ]]; then
  for abi in "${ONLY_ABIS[@]}"; do
    ok=0
    for a in "${ALLOWED_ABIS[@]}"; do
      [[ "$a" == "$abi" ]] && ok=1 && break
    done
    if [[ $ok -ne 1 ]]; then
      echo "error: ABI '$abi' not in lock contract (${LOCK_ALLOWED_ABIS})" >&2
      exit 2
    fi
  done
fi

need_abi() {
  local abi="$1"
  if [[ ${#ONLY_ABIS[@]} -eq 0 ]]; then
    return 0
  fi
  local x
  for x in "${ONLY_ABIS[@]}"; do
    [[ "$x" == "$abi" ]] && return 0
  done
  return 1
}

ZIG_BIN="${ZIG_BIN:-zig}"
if ! command -v "$ZIG_BIN" >/dev/null 2>&1; then
  echo "error: zig not found on PATH (need ${LOCK_ZIG_VERSION})" >&2
  exit 1
fi
ZIG_VER="$("$ZIG_BIN" version)"
case "$ZIG_VER" in
  "${LOCK_ZIG_VERSION}"|"${LOCK_ZIG_VERSION}".*)
    ;;
  *)
    echo "error: zig version mismatch: have $ZIG_VER, lock wants ${LOCK_ZIG_VERSION}" >&2
    exit 1
    ;;
esac

PROVENANCE="proven"
RELEASE_GRADE=1
PROVENANCE_NOTES=()

resolve_ghostty_src() {
  if [[ -n "${GHOSTTY_SRC:-}" ]]; then
    echo "$GHOSTTY_SRC"
    return
  fi
  local cache="${ZEN_GHOSTTY_CACHE:-$HOME/.cache/zen/ghostty}"
  if [[ -f "$cache/build.zig" ]]; then
    echo "$cache"
    return
  fi
  echo "Cloning Ghostty into $cache (pinned ${LOCK_GHOSTTY_COMMIT})..." >&2
  mkdir -p "$(dirname "$cache")"
  git clone --filter=blob:none "$LOCK_GHOSTTY_REPO" "$cache" >&2
  echo "$cache"
}

GHOSTTY_SRC="$(resolve_ghostty_src)"
if [[ ! -f "$GHOSTTY_SRC/build.zig" ]]; then
  echo "error: Ghostty source not found at $GHOSTTY_SRC (set GHOSTTY_SRC)" >&2
  exit 1
fi

if [[ ! -d "$GHOSTTY_SRC/.git" ]]; then
  if [[ "${ALLOW_UNPROVEN_GHOSTTY:-0}" != "1" ]]; then
    echo "error: Ghostty source has no .git; cannot prove commit ${LOCK_GHOSTTY_COMMIT}" >&2
    echo "       use a git checkout or set ALLOW_UNPROVEN_GHOSTTY=1 (developer only; non-release)" >&2
    exit 1
  fi
  PROVENANCE="unproven_no_git"
  RELEASE_GRADE=0
  PROVENANCE_NOTES+=("no_git")
  RESOLVED_COMMIT="unknown-no-git"
  echo "warning: ALLOW_UNPROVEN_GHOSTTY=1 — non-release build" >&2
else
  CURRENT="$(git -C "$GHOSTTY_SRC" rev-parse HEAD)"
  if [[ "$CURRENT" != "$LOCK_GHOSTTY_COMMIT" ]]; then
    echo "Checking out pinned Ghostty commit ${LOCK_GHOSTTY_COMMIT}..."
    git -C "$GHOSTTY_SRC" fetch --depth=1 origin "$LOCK_GHOSTTY_COMMIT" 2>/dev/null \
      || git -C "$GHOSTTY_SRC" fetch origin "$LOCK_GHOSTTY_COMMIT"
    git -C "$GHOSTTY_SRC" checkout --detach "$LOCK_GHOSTTY_COMMIT"
  fi
  if [[ -n "$(git -C "$GHOSTTY_SRC" status --porcelain)" ]]; then
    if [[ "${ALLOW_DIRTY_GHOSTTY:-0}" != "1" ]]; then
      echo "error: Ghostty tree is dirty at $GHOSTTY_SRC" >&2
      echo "       clean it or set ALLOW_DIRTY_GHOSTTY=1 (developer only; non-release)" >&2
      git -C "$GHOSTTY_SRC" status -sb >&2
      exit 1
    fi
    PROVENANCE="dirty"
    RELEASE_GRADE=0
    PROVENANCE_NOTES+=("dirty_tree")
    echo "warning: ALLOW_DIRTY_GHOSTTY=1 — non-release build" >&2
  fi
  RESOLVED_COMMIT="$(git -C "$GHOSTTY_SRC" rev-parse HEAD)"
  if [[ "$RESOLVED_COMMIT" != "$LOCK_GHOSTTY_COMMIT" ]]; then
    echo "error: resolved commit $RESOLVED_COMMIT != pin ${LOCK_GHOSTTY_COMMIT}" >&2
    exit 1
  fi
fi

OUT_DIR="$ROOT/$LOCK_LIB_DIR"
HEADER_DST="$ROOT/$LOCK_HEADERS_DIR"
mkdir -p "$OUT_DIR"

echo "Building libghostty-vt"
echo "  ghostty: $GHOSTTY_SRC @ ${RESOLVED_COMMIT}"
echo "  zig:     $ZIG_VER"
echo "  out:     $OUT_DIR"
echo "  release_grade: $RELEASE_GRADE ($PROVENANCE)"

SUMS_FILE="$OUT_DIR/$LOCK_CHECKSUMS"
MANIFEST="$OUT_DIR/$LOCK_MANIFEST"

# Merge checksums for subset builds: keep other ABI lines; replace built ABIs.
declare -A EXISTING_SUMS=()
if [[ -f "$SUMS_FILE" ]]; then
  while read -r digest rel; do
    [[ -z "${digest:-}" || "$digest" == \#* ]] && continue
    rel="${rel#\*}"
    EXISTING_SUMS["$rel"]="$digest"
  done <"$SUMS_FILE"
fi

BUILT_LIBS_JSON="[]"
built_any=0

for ((i = 0; i < LOCK_ABI_COUNT; i++)); do
  eval "android_abi=\$LOCK_ABI_${i}_ANDROID"
  eval "zig_target=\$LOCK_ABI_${i}_ZIG"
  eval "elf_code=\$LOCK_ABI_${i}_ELF"

  if ! need_abi "$android_abi"; then
    continue
  fi
  built_any=1

  echo ""
  echo "=== $android_abi ($zig_target) ==="
  (
    cd "$GHOSTTY_SRC"
    "$ZIG_BIN" build -Demit-lib-vt=true -Doptimize=ReleaseFast "-Dtarget=$zig_target"
  )

  mkdir -p "$OUT_DIR/$android_abi"
  src_so=""
  if compgen -G "$GHOSTTY_SRC/zig-out/lib/libghostty-vt.so*" >/dev/null; then
    # Prefer a real file over a symlink.
    for candidate in "$GHOSTTY_SRC"/zig-out/lib/libghostty-vt.so*; do
      if [[ -f "$candidate" && ! -L "$candidate" ]]; then
        src_so="$candidate"
        break
      fi
    done
    if [[ -z "$src_so" && -e "$GHOSTTY_SRC/zig-out/lib/libghostty-vt.so" ]]; then
      src_so="$(readlink -f "$GHOSTTY_SRC/zig-out/lib/libghostty-vt.so")"
    fi
  fi
  if [[ -z "${src_so:-}" || ! -f "$src_so" ]]; then
    echo "error: built library not found under $GHOSTTY_SRC/zig-out/lib" >&2
    ls -la "$GHOSTTY_SRC/zig-out/lib" >&2 || true
    exit 1
  fi

  dest="$OUT_DIR/$android_abi/$LOCK_LIB_NAME"
  cp -f "$src_so" "$dest"
  chmod 755 "$dest"

  machine_dec="$(python3 - "$dest" <<'PY'
import struct, sys
path = sys.argv[1]
with open(path, "rb") as f:
    data = f.read(64)
if data[:4] != b"\x7fELF":
    raise SystemExit("not ELF")
print(struct.unpack_from("<H", data, 18)[0])
PY
)"
  if [[ "$machine_dec" != "$elf_code" ]]; then
    echo "error: ELF machine $machine_dec != expected $elf_code for $android_abi" >&2
    exit 1
  fi

  # Android API level from .note.android.ident when present
  api_level="$(python3 - "$dest" <<'PY'
import struct, sys
data = open(sys.argv[1], "rb").read()
# Scan for "Android" note name
needle = b"Android\x00"
idx = data.find(needle)
if idx < 0:
    print("")
    raise SystemExit(0)
# description typically follows 64-bit aligned name; API is first u32 of desc
# Back up to note header is fragile; parse via known layout after name:
# After "Android\0" pad to 4, then desc: api u32, ndk name...
pad = (4 - ((idx + len(needle)) % 4)) % 4
desc_off = idx + len(needle) + pad
if desc_off + 4 <= len(data):
    print(struct.unpack_from("<I", data, desc_off)[0])
else:
    print("")
PY
)"
  if [[ -n "$api_level" && "$api_level" != "$LOCK_MIN_API" ]]; then
    echo "error: Android API $api_level != lock min_api $LOCK_MIN_API for $android_abi" >&2
    exit 1
  fi
  if [[ -n "$api_level" ]]; then
    echo "  android API note: $api_level"
  fi

  digest="$(sha256sum "$dest" | awk '{print $1}')"
  rel="${android_abi}/${LOCK_LIB_NAME}"
  EXISTING_SUMS["$rel"]="$digest"
  size="$(wc -c <"$dest" | tr -d ' ')"
  echo "  -> $dest ($size bytes, sha256=${digest:0:12}…)"

  BUILT_LIBS_JSON="$(python3 - "$BUILT_LIBS_JSON" "$android_abi" "$zig_target" "$elf_code" "$digest" "$size" "${api_level:-}" <<'PY'
import json, sys
libs = json.loads(sys.argv[1])
entry = {
    "android_abi": sys.argv[2],
    "zig_target": sys.argv[3],
    "elf_machine_code": int(sys.argv[4]),
    "sha256": sys.argv[5],
    "bytes": int(sys.argv[6]),
}
if sys.argv[7]:
    entry["android_api"] = int(sys.argv[7])
libs.append(entry)
print(json.dumps(libs))
PY
)"
done

if [[ $built_any -eq 0 ]]; then
  echo "error: no ABIs selected to build" >&2
  exit 1
fi

# Write merged checksums (stable sorted order)
: >"$SUMS_FILE"
while IFS= read -r rel; do
  echo "${EXISTING_SUMS[$rel]}  $rel" >>"$SUMS_FILE"
done < <(printf '%s\n' "${!EXISTING_SUMS[@]}" | LC_ALL=C sort)

# Headers for JNI bridge
HEADER_PARENT="$(dirname "$HEADER_DST")"
mkdir -p "$HEADER_PARENT"
rm -rf "$HEADER_DST"
cp -a "$GHOSTTY_SRC/include/ghostty" "$HEADER_DST"
ACTUAL_HEADERS_SHA256="$(sha256sum "$HEADER_DST/vt.h" | awk '{print $1}')"
if [[ "$ACTUAL_HEADERS_SHA256" != "$LOCK_HEADERS_SHA256" ]]; then
  echo "error: copied vt.h sha256 $ACTUAL_HEADERS_SHA256 != lock $LOCK_HEADERS_SHA256" >&2
  exit 1
fi

cp -f "$NOTICE_SRC" "$OUT_DIR/GHOSTTY-MIT.txt"

NOTES_JSON='[]'
if [[ ${#PROVENANCE_NOTES[@]} -gt 0 ]]; then
  NOTES_JSON="$(python3 -c 'import json,sys; print(json.dumps(sys.argv[1:]))' "${PROVENANCE_NOTES[@]}")"
fi

python3 - "$MANIFEST" "$RESOLVED_COMMIT" "$ZIG_VER" "$BUILT_LIBS_JSON" \
  "$LOCK_GHOSTTY_COMMIT" "$PROVENANCE" "$RELEASE_GRADE" "$NOTES_JSON" <<'PY'
import json, sys
path, resolved, zig, built_json, pinned, provenance, release_grade, notes_json = sys.argv[1:9]
# Merge libraries in manifest: replace ABIs we just built, keep others if present.
prev_libs = []
try:
    prev = json.load(open(path))
    prev_libs = prev.get("libraries") or []
except Exception:
    pass
built = json.loads(built_json)
by_abi = {e["android_abi"]: e for e in prev_libs}
for e in built:
    by_abi[e["android_abi"]] = e
libraries = [by_abi[k] for k in sorted(by_abi)]
manifest = {
    "schema_version": 1,
    "ghostty_commit_pinned": pinned,
    "ghostty_commit_resolved": resolved,
    "zig_version": zig,
    "provenance": provenance,
    "release_grade": bool(int(release_grade)),
    "provenance_notes": json.loads(notes_json),
    "libraries": libraries,
}
with open(path, "w", encoding="utf-8") as f:
    json.dump(manifest, f, indent=2, sort_keys=True)
    f.write("\n")
print(f"Wrote {path} (release_grade={manifest['release_grade']})")
PY

echo ""
echo "Checksums: $SUMS_FILE"
echo "Headers:   $HEADER_DST"
echo "Done. Run: ./scripts/verify-libghostty.sh"
