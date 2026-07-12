#!/usr/bin/env bash
# Verify a built APK (zip) contains the Ghostty MIT notice at the contract path.
#
# Usage:
#   ./scripts/verify-apk-notice.sh path/to/app.apk
#   ./scripts/verify-apk-notice.sh --expect-missing path/to/app.apk   # negative test

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/app/modules/zen-terminal-vt/native.lock.json"
NOTICE_SRC="$ROOT/app/assets/notices/GHOSTTY-MIT.txt"

EXPECT_MISSING=0
APK=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --expect-missing) EXPECT_MISSING=1; shift ;;
    -h|--help) sed -n '2,8p' "$0"; exit 0 ;;
    *) APK="$1"; shift ;;
  esac
done

if [[ -z "$APK" || ! -f "$APK" ]]; then
  echo "error: APK path required" >&2
  exit 2
fi

NOTICE_PATH="$(python3 -c "import json;print(json.load(open('$LOCK'))['release_apk']['notice_apk_path'])")"
COPYRIGHT="$(python3 -c "import json;print(json.load(open('$LOCK'))['ghostty']['copyright_line'])")"

python3 - "$APK" "$NOTICE_PATH" "$NOTICE_SRC" "$COPYRIGHT" "$EXPECT_MISSING" <<'PY'
import sys, zipfile
from pathlib import Path

apk, notice_path, notice_src, copyright, expect_missing = sys.argv[1:6]
expect_missing = expect_missing == "1"
src_text = Path(notice_src).read_text(encoding="utf-8")

with zipfile.ZipFile(apk) as z:
    names = set(z.namelist())
    # Android packages assets under assets/
    candidates = [notice_path]
    if not notice_path.startswith("assets/"):
        candidates.append("assets/" + notice_path)

    found = None
    for c in candidates:
        if c in names:
            found = c
            break
    # Also accept legacy META-INF placement if we ever use it
    if found is None:
        for n in names:
            if n.endswith("GHOSTTY-MIT.txt"):
                found = n
                break

    if expect_missing:
        if found is not None:
            print(f"FAIL: expected notice absent but found {found}", file=sys.stderr)
            raise SystemExit(1)
        print("ok: notice absent as expected")
        raise SystemExit(0)

    if found is None:
        print(f"FAIL: {notice_path} not in APK", file=sys.stderr)
        print("  (sample asset entries:)", file=sys.stderr)
        for n in sorted(names):
            if "notice" in n.lower() or n.startswith("assets/"):
                print(f"   {n}", file=sys.stderr)
        raise SystemExit(1)

    data = z.read(found).decode("utf-8")
    if copyright not in data:
        print(f"FAIL: copyright line missing inside {found}", file=sys.stderr)
        raise SystemExit(1)
    if "MIT License" not in data:
        print(f"FAIL: MIT License header missing inside {found}", file=sys.stderr)
        raise SystemExit(1)
    # Content should match the repo notice source
    if data.strip() != src_text.strip():
        print(f"FAIL: APK notice content differs from {notice_src}", file=sys.stderr)
        raise SystemExit(1)
    print(f"ok: APK contains {found} with matching Ghostty MIT notice")
PY
