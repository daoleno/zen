#!/usr/bin/env bash
# Restore owned Xvfb/libXfont2/nettle from the verified 2026-09-08 archives.
# Extracts into an owned prefix. Does not install system packages.
set -euo pipefail

ARCHIVES="${ZEN_REMOTE_DESKTOP_ARCHIVES:-$HOME/.zen/artifacts/2026-09-08-remote-desktop-test-dependencies}"
PREFIX="${ZEN_REMOTE_DESKTOP_TEST_PREFIX:-$ARCHIVES/extracted}"
PACKAGES="$ARCHIVES/packages"
META="$ARCHIVES/dependencies.json"

if [[ ! -f "$META" || ! -d "$PACKAGES" ]]; then
  echo "error: verified archives not found at $ARCHIVES" >&2
  exit 1
fi

python3 - "$PACKAGES" "$META" <<'PY'
import hashlib, json, sys
from pathlib import Path
packages, meta = Path(sys.argv[1]), json.loads(Path(sys.argv[2]).read_text())
want = {
    "xorg-server-xvfb": "fec274b876d775b2a3a92aad0fd430b554b521ad0f72565c7ed6965e28c75c54",
    "libxfont2": "1e6e54047bb0c069671d20c6eb9582310d842e88ba8943833facc5861109640c",
    "nettle": "679138a8405ca383aba7836d54fdc282db9394b7dc23c097b8965f70119adf13",
}
for item in meta:
    name = item["name"]
    if name not in want:
        continue
    if item["sha256"] != want[name]:
        raise SystemExit(f"manifest hash drift for {name}")
    matches = list(packages.glob(name + "-*.pkg.tar.zst"))
    matches = [p for p in matches if not p.name.endswith(".sig")]
    if len(matches) != 1:
        raise SystemExit(f"archive count for {name}: {matches}")
    digest = hashlib.sha256(matches[0].read_bytes()).hexdigest()
    if digest != want[name]:
        raise SystemExit(f"archive hash mismatch for {name}: {digest}")
print("ok: archive hashes match retained dependencies.json")
PY

mkdir -p "$PREFIX"
chmod 700 "$PREFIX"
for pkg in "$PACKAGES"/xorg-server-xvfb-*.pkg.tar.zst "$PACKAGES"/libxfont2-*.pkg.tar.zst "$PACKAGES"/nettle-*.pkg.tar.zst; do
  [[ "$pkg" == *.sig ]] && continue
  tar -C "$PREFIX" --zstd -xf "$pkg"
done
test -x "$PREFIX/usr/bin/Xvfb"
echo "PREFIX=$PREFIX"
echo "XVFB=$PREFIX/usr/bin/Xvfb"
