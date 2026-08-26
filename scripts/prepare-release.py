#!/usr/bin/env python3
"""Prepare a Zen release as one deterministic tracked change."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Iterable


BETA_VERSION_RE = re.compile(
    r"^(?P<major>0|[1-9][0-9]*)\."
    r"(?P<minor>0|[1-9][0-9]*)\."
    r"(?P<patch>0|[1-9][0-9]*)-beta\."
    r"(?P<beta>[1-9][0-9]*)$"
)
RELEASE_VERSION_RE = re.compile(
    r"^(?P<major>0|[1-9][0-9]*)\."
    r"(?P<minor>0|[1-9][0-9]*)\."
    r"(?P<patch>0|[1-9][0-9]*)"
    r"(?:-beta\.(?P<beta>[1-9][0-9]*))?$"
)
CHANGELOG_ENTRY_RE = re.compile(
    r"^- \[(v[0-9]+\.[0-9]+\.[0-9]+(?:-beta\.[1-9][0-9]*)?)\]"
    r"\(docs/releases/\1\.md\)$",
    re.MULTILINE,
)
CHANGELOG_PREFIX = """# Changelog

Canonical release notes live under [`docs/releases/`](docs/releases/). This file is a reverse-chronological index and does not duplicate the full notes.

## Releases

"""


class PrepareError(RuntimeError):
    """A fail-closed preparation error."""


def next_beta_version(current: str) -> str:
    match = BETA_VERSION_RE.fullmatch(current)
    if match is None:
        raise PrepareError(
            f"current version must exactly match X.Y.Z-beta.N; got {current!r}"
        )
    return (
        f"{match.group('major')}.{match.group('minor')}.{match.group('patch')}"
        f"-beta.{int(match.group('beta')) + 1}"
    )


def release_precedence(version: str) -> tuple[int, int, int, int, int]:
    match = RELEASE_VERSION_RE.fullmatch(version)
    if match is None:
        raise PrepareError(
            f"version must exactly match X.Y.Z or X.Y.Z-beta.N; got {version!r}"
        )
    beta = match.group("beta")
    return (
        int(match.group("major")),
        int(match.group("minor")),
        int(match.group("patch")),
        1 if beta is None else 0,
        int(beta or 0),
    )


def validate_target_version(current: str, target: str) -> str:
    current_order = release_precedence(current)
    target_order = release_precedence(target)
    if target_order <= current_order:
        raise PrepareError(
            f"target version must be newer than current version {current}; got {target}"
        )
    return target


def run_git(root: Path, *args: str, check: bool = True) -> str:
    result = subprocess.run(
        ["git", *args],
        cwd=root,
        check=False,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    if check and result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip()
        raise PrepareError(f"git {' '.join(args)} failed: {detail}")
    return result.stdout.strip()


def read_text(root: Path, relative: str) -> str:
    path = root / relative
    if not path.is_file():
        raise PrepareError(f"missing required file: {relative}")
    return path.read_text(encoding="utf-8")


def read_json(root: Path, relative: str) -> dict:
    try:
        value = json.loads(read_text(root, relative))
    except (json.JSONDecodeError, UnicodeDecodeError) as exc:
        raise PrepareError(f"malformed JSON in {relative}: {exc}") from exc
    if not isinstance(value, dict):
        raise PrepareError(f"{relative} must contain a JSON object")
    return value


def replace_literal(
    text: str, old: str, new: str, *, relative: str, expected_count: int = 1
) -> str:
    count = text.count(old)
    if count != expected_count:
        raise PrepareError(
            f"{relative}: expected {expected_count} occurrence(s) of {old!r}; "
            f"found {count}"
        )
    return text.replace(old, new)


def require_positive_int(value: object, *, label: str) -> int:
    if not isinstance(value, int) or isinstance(value, bool) or value < 1:
        raise PrepareError(f"{label} must be a positive integer; got {value!r}")
    return value


def require_annotated_tag(root: Path, tag: str) -> str:
    tag_type = run_git(root, "cat-file", "-t", f"refs/tags/{tag}", check=False)
    if tag_type != "tag":
        raise PrepareError(f"current tag {tag} is missing or is not annotated")
    commit = run_git(root, "rev-parse", f"refs/tags/{tag}^{{commit}}")
    ancestor = subprocess.run(
        ["git", "merge-base", "--is-ancestor", commit, "HEAD"],
        cwd=root,
        check=False,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    if ancestor.returncode != 0:
        raise PrepareError(f"current tag {tag} is not an ancestor of HEAD")
    return commit


def commit_range(root: Path, current_tag: str) -> list[tuple[str, str]]:
    raw = run_git(root, "log", "--reverse", "--format=%H%x09%s", f"{current_tag}..HEAD")
    commits: list[tuple[str, str]] = []
    for line in raw.splitlines():
        sha, separator, subject = line.partition("\t")
        if (
            not separator
            or re.fullmatch(r"[0-9a-f]{40}", sha) is None
            or not subject.strip()
        ):
            raise PrepareError("git log returned a malformed commit record")
        commits.append((sha, subject.strip()))
    if not commits:
        raise PrepareError(f"no commits exist after {current_tag}")
    return commits


def validate_changelog(
    root: Path, content: str, current_version: str, next_tag: str
) -> None:
    if not content.startswith(CHANGELOG_PREFIX):
        raise PrepareError("CHANGELOG.md does not match the canonical index header")
    entries = CHANGELOG_ENTRY_RE.findall(content)
    current_tag = f"v{current_version}"
    if not entries or entries[0] != current_tag:
        raise PrepareError(
            f"CHANGELOG.md must start with the current release {current_tag}; "
            f"got {entries[:1]!r}"
        )
    if len(entries) != len(set(entries)):
        raise PrepareError("CHANGELOG.md contains duplicate release entries")
    if content.count("\n- [") != len(entries):
        raise PrepareError("CHANGELOG.md contains malformed release entries")

    order = [release_precedence(tag.removeprefix("v")) for tag in entries]
    if any(left <= right for left, right in zip(order, order[1:])):
        raise PrepareError(
            "CHANGELOG.md release entries are not strictly reverse chronological"
        )
    if next_tag in content:
        raise PrepareError(f"CHANGELOG.md already contains {next_tag}")
    for tag in entries:
        note = root / "docs" / "releases" / f"{tag}.md"
        if not note.is_file():
            raise PrepareError(f"CHANGELOG.md points to missing canonical notes: {note}")


def extract_note_facts(previous_notes: str, current_tag: str) -> dict[str, str]:
    def capture(pattern: str, label: str, flags: int = 0) -> str:
        matches = re.findall(pattern, previous_notes, flags)
        if len(matches) != 1:
            raise PrepareError(
                f"current release notes must contain exactly one {label}; "
                f"found {len(matches)}"
            )
        return matches[0].strip()

    source_tag_lines = re.findall(
        r"^- Source tag: `([^`]+)`$", previous_notes, re.MULTILINE
    )
    if source_tag_lines != [current_tag]:
        raise PrepareError(
            "current release notes must contain exactly one structured source-tag "
            f"line for {current_tag}; got {source_tag_lines!r}"
        )
    return {
        "install": capture(
            r"## Install\n\n(.+?)\n\n## iOS Preview identity",
            "Install section",
            re.DOTALL,
        ),
        "ios_bundle": capture(r"^- Bundle: `([^`]+)`$", "iOS bundle", re.MULTILINE),
        "android_abi": capture(r"^- ABI: `([^`]+)`$", "Android ABI", re.MULTILINE),
        "certificate": capture(
            r"^- Signing certificate SHA-256: `([^`]+)`$",
            "Android certificate fingerprint",
            re.MULTILINE,
        ),
    }


def extract_preview_bundle(ios_identity_source: str) -> str:
    preview_blocks = re.findall(
        r"\bpreview:\s*Object\.freeze\(\{(.+?)\}\),",
        ios_identity_source,
        re.DOTALL,
    )
    if len(preview_blocks) != 1:
        raise PrepareError(
            "app/iosIdentity.js must contain exactly one Preview identity"
        )
    bundles = re.findall(
        r'^\s*bundleIdentifier:\s*"([^"]+)",$',
        preview_blocks[0],
        re.MULTILINE,
    )
    if len(bundles) != 1 or not bundles[0]:
        raise PrepareError(
            "app/iosIdentity.js Preview identity must contain exactly one bundleIdentifier"
        )
    return bundles[0]


def extract_release_android_abi(native_lock: dict) -> str:
    try:
        release_abi = native_lock["release_apk"]["react_native_architectures"]
    except (KeyError, TypeError) as exc:
        raise PrepareError(
            "app/modules/zen-terminal-vt/native.lock.json is missing "
            f"release_apk.react_native_architectures: {exc}"
        ) from exc
    if not isinstance(release_abi, str) or not release_abi.strip():
        raise PrepareError(
            "app/modules/zen-terminal-vt/native.lock.json "
            "release_apk.react_native_architectures must be a non-empty string"
        )
    return release_abi


def markdown_subject(subject: str) -> str:
    return subject.replace("\\", "\\\\").replace("`", "\\`")


def build_release_notes(
    *,
    next_version: str,
    next_tag: str,
    current_tag: str,
    commits: Iterable[tuple[str, str]],
    install: str,
    ios_bundle: str,
    ios_build: int,
    android_package: str,
    android_version_code: int,
    android_abi: str,
    certificate: str,
) -> str:
    marketing_version = next_version.split("-beta.", 1)[0]
    release_description = (
        "This beta contains" if "-beta." in next_version else "This release contains"
    )
    changes = "\n".join(
        f"- {markdown_subject(subject)} (`{sha[:7]}`)" for sha, subject in commits
    )
    return f"""# Zen {next_tag}

{release_description} the reviewed changes on `main` since `{current_tag}`.

## What changed

{changes}

## Install

{install}

## iOS Preview identity

- Bundle: `{ios_bundle}`
- Marketing version: `{marketing_version}`
- TestFlight build: `{ios_build}`
- Source tag: `{next_tag}`

The iOS build number is tracked independently from Android because App Store Connect build history can be ahead.

## Android identity

- Package: `{android_package}`
- `versionCode`: `{android_version_code}`
- ABI: `{android_abi}`
- Signing certificate SHA-256: `{certificate}`

Android may require permission to install from unknown sources or display a Play Protect warning. Obtainium can follow this repository's GitHub Releases. Zen does not currently provide a Play Store package; iOS Preview distribution uses TestFlight.
"""


def atomic_write(path: Path, content: str) -> None:
    mode = path.stat().st_mode & 0o777 if path.exists() else 0o644
    path.parent.mkdir(parents=True, exist_ok=True)
    handle = tempfile.NamedTemporaryFile(
        mode="w",
        encoding="utf-8",
        newline="",
        dir=path.parent,
        prefix=f".{path.name}.",
        delete=False,
    )
    temp_path = Path(handle.name)
    try:
        with handle:
            handle.write(content)
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temp_path, mode)
        os.replace(temp_path, path)
    except BaseException:
        temp_path.unlink(missing_ok=True)
        raise


def prepare(root: Path, target_version: str | None = None) -> dict[str, object]:
    root = root.resolve()
    git_root = Path(run_git(root, "rev-parse", "--show-toplevel")).resolve()
    if git_root != root:
        raise PrepareError(f"--repo must be the repository root: {git_root}")
    dirty = run_git(root, "status", "--porcelain=v1", "--untracked-files=all")
    if dirty:
        raise PrepareError(f"repository has unexpected dirty state:\n{dirty}")

    base = read_json(root, "app/app.base.json")
    try:
        expo = base["expo"]
        android = expo["android"]
        current_version = expo["version"]
        android_package = android["package"]
        android_version_code = require_positive_int(
            android["versionCode"], label="app/app.base.json Android versionCode"
        )
    except (KeyError, TypeError) as exc:
        raise PrepareError(f"app/app.base.json is missing release identity: {exc}") from exc
    if not isinstance(current_version, str):
        raise PrepareError("app/app.base.json expo.version must be a string")
    if not isinstance(android_package, str) or not android_package.strip():
        raise PrepareError("app/app.base.json Android package must be a non-empty string")

    next_version = (
        next_beta_version(current_version)
        if target_version is None
        else validate_target_version(current_version, target_version)
    )
    current_tag = f"v{current_version}"
    next_tag = f"v{next_version}"
    require_annotated_tag(root, current_tag)
    if run_git(root, "show-ref", "--verify", f"refs/tags/{next_tag}", check=False):
        raise PrepareError(f"next tag already exists: {next_tag}")

    next_notes_relative = f"docs/releases/{next_tag}.md"
    if (root / next_notes_relative).exists():
        raise PrepareError(f"next release notes already exist: {next_notes_relative}")
    commits = commit_range(root, current_tag)

    ios_doc = read_json(root, "app/ios-build.json")
    ios_build = require_positive_int(
        ios_doc.get("buildNumber"), label="app/ios-build.json buildNumber"
    )
    next_version_code = android_version_code + 1
    next_ios_build = ios_build + 1
    native_lock = read_json(root, "app/modules/zen-terminal-vt/native.lock.json")

    sources = {
        relative: read_text(root, relative)
        for relative in (
            "app/app.base.json",
            "app/iosIdentity.js",
            "app/ios-build.json",
            "daemon/cmd/zen/version.go",
            "scripts/verify-release-identity.sh",
            "docs/install-daemon.md",
            "docs/ios-ci-release.md",
            "CHANGELOG.md",
            f"docs/releases/{current_tag}.md",
        )
    }
    validate_changelog(root, sources["CHANGELOG.md"], current_version, next_tag)
    note_facts = extract_note_facts(
        sources[f"docs/releases/{current_tag}.md"], current_tag
    )
    preview_bundle = extract_preview_bundle(sources["app/iosIdentity.js"])
    if note_facts["ios_bundle"] != preview_bundle:
        raise PrepareError(
            "current release notes iOS Preview bundle does not match "
            f"app/iosIdentity.js: {note_facts['ios_bundle']!r} != {preview_bundle!r}"
        )
    release_android_abi = extract_release_android_abi(native_lock)
    if note_facts["android_abi"] != release_android_abi:
        raise PrepareError(
            "current release notes Android ABI does not match "
            "app/modules/zen-terminal-vt/native.lock.json: "
            f"{note_facts['android_abi']!r} != {release_android_abi!r}"
        )

    updates: dict[str, str] = {}
    updates["app/app.base.json"] = replace_literal(
        sources["app/app.base.json"],
        f'"version": "{current_version}"',
        f'"version": "{next_version}"',
        relative="app/app.base.json",
    )
    updates["app/app.base.json"] = replace_literal(
        updates["app/app.base.json"],
        f'"versionCode": {android_version_code}',
        f'"versionCode": {next_version_code}',
        relative="app/app.base.json",
    )
    updates["app/ios-build.json"] = replace_literal(
        sources["app/ios-build.json"],
        f'"buildNumber": {ios_build}',
        f'"buildNumber": {next_ios_build}',
        relative="app/ios-build.json",
    )
    updates["daemon/cmd/zen/version.go"] = replace_literal(
        sources["daemon/cmd/zen/version.go"],
        f'var Version = "{current_version}"',
        f'var Version = "{next_version}"',
        relative="daemon/cmd/zen/version.go",
    )

    verifier = replace_literal(
        sources["scripts/verify-release-identity.sh"],
        current_version,
        next_version,
        relative="scripts/verify-release-identity.sh",
        expected_count=5,
    )
    verifier = replace_literal(
        verifier,
        f'EXPECTED_VERSION_CODE="{android_version_code}"',
        f'EXPECTED_VERSION_CODE="{next_version_code}"',
        relative="scripts/verify-release-identity.sh",
    )
    verifier = replace_literal(
        verifier,
        f'EXPECTED_IOS_BUILD_NUMBER="{ios_build}"',
        f'EXPECTED_IOS_BUILD_NUMBER="{next_ios_build}"',
        relative="scripts/verify-release-identity.sh",
    )
    updates["scripts/verify-release-identity.sh"] = verifier

    updates["docs/install-daemon.md"] = replace_literal(
        sources["docs/install-daemon.md"],
        current_tag,
        next_tag,
        relative="docs/install-daemon.md",
    )
    ios_docs = replace_literal(
        sources["docs/ios-ci-release.md"],
        f"`{current_version}`",
        f"`{next_version}`",
        relative="docs/ios-ci-release.md",
    )
    ios_docs = replace_literal(
        ios_docs,
        f"baseline is build `{ios_build}` in `app/ios-build.json`",
        f"baseline is build `{next_ios_build}` in `app/ios-build.json`",
        relative="docs/ios-ci-release.md",
    )
    updates["docs/ios-ci-release.md"] = ios_docs

    changelog_entry = f"- [{next_tag}](docs/releases/{next_tag}.md)\n"
    updates["CHANGELOG.md"] = replace_literal(
        sources["CHANGELOG.md"],
        CHANGELOG_PREFIX,
        CHANGELOG_PREFIX + changelog_entry,
        relative="CHANGELOG.md",
    )
    updates[next_notes_relative] = build_release_notes(
        next_version=next_version,
        next_tag=next_tag,
        current_tag=current_tag,
        commits=commits,
        install=note_facts["install"],
        ios_bundle=preview_bundle,
        ios_build=next_ios_build,
        android_package=android_package,
        android_version_code=next_version_code,
        android_abi=release_android_abi,
        certificate=note_facts["certificate"],
    )

    originals = {
        relative: (root / relative).read_bytes() if (root / relative).exists() else None
        for relative in updates
    }
    written: list[str] = []
    try:
        for relative, content in updates.items():
            atomic_write(root / relative, content)
            written.append(relative)
    except BaseException:
        for relative in reversed(written):
            original = originals[relative]
            path = root / relative
            if original is None:
                path.unlink(missing_ok=True)
            else:
                atomic_write(path, original.decode("utf-8"))
        raise

    return {
        "current_tag": current_tag,
        "current_version": current_version,
        "next_tag": next_tag,
        "next_version": next_version,
        "android_version_code": next_version_code,
        "ios_build_number": next_ios_build,
        "commit_count": len(commits),
        "changed_paths": sorted(updates),
    }


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Prepare a tracked Zen release"
    )
    parser.add_argument(
        "--repo",
        type=Path,
        default=Path(__file__).resolve().parent.parent,
        help="repository root (defaults to this script's repository)",
    )
    parser.add_argument(
        "--version",
        help="explicit target X.Y.Z or X.Y.Z-beta.N (default: increment beta ordinal)",
    )
    args = parser.parse_args(argv)
    try:
        result = prepare(args.repo, args.version)
    except PrepareError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    print(json.dumps(result, sort_keys=True, separators=(",", ":")))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
