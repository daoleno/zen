#!/usr/bin/env python3
"""Reject forbidden undefined imports in Android libghostty_vt artifacts."""

from __future__ import annotations

import argparse
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import zipfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_LOCK = ROOT / "app/modules/zen-terminal-vt/native.lock.json"
APK_LIBRARY_RE = re.compile(r"^lib/([^/]+)/libghostty_vt\.so$")


def load_forbidden_symbols(lock_path: Path) -> tuple[str, ...]:
    lock = json.loads(lock_path.read_text(encoding="utf-8"))
    symbols = lock.get("android", {}).get("forbidden_undefined_symbols")
    if not isinstance(symbols, list) or not symbols:
        raise ValueError(
            f"{lock_path}: android.forbidden_undefined_symbols must be a non-empty list"
        )

    result: list[str] = []
    for symbol in symbols:
        if not isinstance(symbol, str) or not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", symbol):
            raise ValueError(f"{lock_path}: invalid forbidden symbol {symbol!r}")
        if symbol in result:
            raise ValueError(f"{lock_path}: duplicate forbidden symbol {symbol!r}")
        result.append(symbol)
    return tuple(result)


def find_readelf() -> str:
    configured = os.environ.get("READELF")
    if configured:
        path = shutil.which(configured) if os.sep not in configured else configured
        if path and Path(path).is_file():
            return str(path)
        raise FileNotFoundError(f"READELF does not exist: {configured}")

    for name in ("llvm-readelf", "readelf"):
        path = shutil.which(name)
        if path:
            return path

    ndk_roots: list[Path] = []
    for key in ("ANDROID_NDK_HOME", "ANDROID_NDK_ROOT"):
        value = os.environ.get(key)
        if value:
            ndk_roots.append(Path(value))
    for key in ("ANDROID_HOME", "ANDROID_SDK_ROOT"):
        value = os.environ.get(key)
        if value:
            ndk_roots.extend(sorted((Path(value) / "ndk").glob("*"), reverse=True))

    for ndk_root in ndk_roots:
        for candidate in sorted(
            ndk_root.glob("toolchains/llvm/prebuilt/*/bin/llvm-readelf"), reverse=True
        ):
            if candidate.is_file():
                return str(candidate)

    raise FileNotFoundError(
        "readelf not found; install binutils or set READELF to an NDK llvm-readelf"
    )


def parse_undefined_symbols(output: str) -> set[str]:
    symbols: set[str] = set()
    for line in output.splitlines():
        fields = line.split()
        if len(fields) < 8 or not fields[0].endswith(":") or fields[6] != "UND":
            continue
        symbols.add(fields[7].split("@", 1)[0])
    return symbols


def undefined_symbols(readelf: str, artifact: Path) -> set[str]:
    result = subprocess.run(
        [readelf, "--wide", "--dyn-syms", str(artifact)],
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip() or "unknown readelf error"
        raise RuntimeError(f"readelf failed for {artifact}: {detail}")
    return parse_undefined_symbols(result.stdout)


def inspect_artifact(
    readelf: str,
    artifact: Path,
    label: str,
    forbidden: tuple[str, ...],
) -> list[str]:
    imports = undefined_symbols(readelf, artifact)
    found = sorted(set(forbidden).intersection(imports))
    if found:
        return [f"{label}: forbidden undefined Android imports: {', '.join(found)}"]
    print(f"ok: {label} has no forbidden undefined Android imports")
    return []


def inspect_apk(
    readelf: str,
    apk: Path,
    forbidden: tuple[str, ...],
) -> list[str]:
    errors: list[str] = []
    with zipfile.ZipFile(apk) as archive, tempfile.TemporaryDirectory(
        prefix="zen-apk-native-symbols."
    ) as temp:
        entries = [name for name in archive.namelist() if APK_LIBRARY_RE.fullmatch(name)]
        if not entries:
            return [f"{apk}: APK contains no lib/<abi>/libghostty_vt.so entries"]

        temp_root = Path(temp)
        for entry in sorted(entries):
            abi = APK_LIBRARY_RE.fullmatch(entry).group(1)  # type: ignore[union-attr]
            extracted = temp_root / abi / "libghostty_vt.so"
            extracted.parent.mkdir(parents=True, exist_ok=True)
            extracted.write_bytes(archive.read(entry))
            errors.extend(
                inspect_artifact(
                    readelf,
                    extracted,
                    f"{apk}!{entry}",
                    forbidden,
                )
            )
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "artifacts",
        metavar="LIBGHOSTTY_SO",
        nargs="*",
        type=Path,
        help="raw libghostty_vt.so artifact to inspect",
    )
    parser.add_argument("--apk", action="append", default=[], type=Path, help="APK to inspect")
    parser.add_argument("--lock", default=DEFAULT_LOCK, type=Path, help="native lockfile")
    args = parser.parse_args()

    if not args.artifacts and not args.apk:
        parser.error("provide at least one LIBGHOSTTY_SO or --apk")

    try:
        forbidden = load_forbidden_symbols(args.lock.resolve())
        readelf = find_readelf()
        errors: list[str] = []
        for artifact in args.artifacts:
            if not artifact.is_file():
                errors.append(f"{artifact}: native library does not exist")
                continue
            errors.extend(
                inspect_artifact(readelf, artifact, str(artifact), forbidden)
            )
        for apk in args.apk:
            if not apk.is_file():
                errors.append(f"{apk}: APK does not exist")
                continue
            errors.extend(inspect_apk(readelf, apk, forbidden))
    except (FileNotFoundError, OSError, RuntimeError, ValueError, zipfile.BadZipFile) as exc:
        print(f"FAIL: {exc}", file=sys.stderr)
        return 1

    if errors:
        print("FAIL: Android native symbol verification", file=sys.stderr)
        for error in errors:
            print(f"  - {error}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
