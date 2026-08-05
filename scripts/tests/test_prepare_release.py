import importlib.util
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts" / "prepare-release.py"
SPEC = importlib.util.spec_from_file_location("prepare_release", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
prepare_release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(prepare_release)

ROOT_BASE = json.loads((ROOT / "app/app.base.json").read_text(encoding="utf-8"))
CURRENT_VERSION = ROOT_BASE["expo"]["version"]
CURRENT_TAG = f"v{CURRENT_VERSION}"
CURRENT_VERSION_CODE = ROOT_BASE["expo"]["android"]["versionCode"]
CURRENT_IOS_BUILD = json.loads(
    (ROOT / "app/ios-build.json").read_text(encoding="utf-8")
)["buildNumber"]
NEXT_VERSION = prepare_release.next_beta_version(CURRENT_VERSION)
NEXT_TAG = f"v{NEXT_VERSION}"
INDEXED_NOTE_PATHS = tuple(
    f"docs/releases/{tag}.md"
    for tag in prepare_release.CHANGELOG_ENTRY_RE.findall(
        (ROOT / "CHANGELOG.md").read_text(encoding="utf-8")
    )
)

FIXTURE_PATHS = (
    "CHANGELOG.md",
    "app/app.base.json",
    "app/iosIdentity.js",
    "app/ios-build.json",
    "app/modules/zen-terminal-vt/native.lock.json",
    "daemon/cmd/zen/version.go",
    "scripts/verify-release-identity.sh",
    "docs/install-daemon.md",
    "docs/ios-ci-release.md",
) + INDEXED_NOTE_PATHS


def git(root: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", *args],
        cwd=root,
        check=True,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return result.stdout.rstrip("\n")


class NextBetaVersionTests(unittest.TestCase):
    def test_increments_only_beta_ordinal(self):
        self.assertEqual(prepare_release.next_beta_version(CURRENT_VERSION), NEXT_VERSION)
        self.assertEqual(
            prepare_release.next_beta_version("12.34.56-beta.99"),
            "12.34.56-beta.100",
        )

    def test_rejects_noncanonical_versions(self):
        for value in (
            "v0.1.0-beta.8",
            "0.1.0",
            "0.1.0-beta.0",
            "0.1.0-beta.08",
            "01.1.0-beta.8",
        ):
            with self.subTest(value=value):
                with self.assertRaises(prepare_release.PrepareError):
                    prepare_release.next_beta_version(value)


class ChangelogValidationTests(unittest.TestCase):
    def test_accepts_older_semver_core_history(self):
        scratch = os.environ.get("ZEN_BUILD_TMPDIR") or os.environ.get("TMPDIR")
        with tempfile.TemporaryDirectory(
            prefix="zen-changelog-", dir=Path(scratch) if scratch else None
        ) as temporary:
            root = Path(temporary)
            notes = root / "docs/releases"
            notes.mkdir(parents=True)
            for tag in ("v0.1.1-beta.1", "v0.1.0-beta.8"):
                (notes / f"{tag}.md").write_text(
                    f"# Zen {tag}\n", encoding="utf-8"
                )
            changelog = (
                prepare_release.CHANGELOG_PREFIX
                + "- [v0.1.1-beta.1](docs/releases/v0.1.1-beta.1.md)\n"
                + "- [v0.1.0-beta.8](docs/releases/v0.1.0-beta.8.md)\n"
            )
            prepare_release.validate_changelog(
                root,
                changelog,
                "0.1.1-beta.1",
                "v0.1.1-beta.2",
            )


class PrepareReleaseIntegrationTests(unittest.TestCase):
    def setUp(self):
        scratch = os.environ.get("ZEN_BUILD_TMPDIR") or os.environ.get("TMPDIR")
        scratch_path = Path(scratch) if scratch else None
        if scratch_path is not None:
            scratch_path.mkdir(parents=True, exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(
            prefix="zen-prepare-release-", dir=scratch_path
        )
        self.addCleanup(self.temp.cleanup)
        self.fixture = Path(self.temp.name)

    def create_repo(
        self, *, with_commit: bool = True, root: Path | None = None
    ) -> Path:
        root = root or self.fixture
        for relative in FIXTURE_PATHS:
            source = ROOT / relative
            target = root / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, target)

        git(root, "init", "-b", "main")
        git(root, "config", "user.name", "Zen Test")
        git(root, "config", "user.email", "zen-test@example.invalid")
        git(root, "add", "--all")
        git(root, "commit", "-m", f"Release {CURRENT_TAG}")
        git(
            root,
            "tag",
            "-a",
            CURRENT_TAG,
            "-m",
            f"Zen {CURRENT_TAG}",
        )
        if with_commit:
            (root / "feature.txt").write_text(
                "reviewed release change\n", encoding="utf-8"
            )
            git(root, "add", "feature.txt")
            git(root, "commit", "-m", "Add reviewed release change")
        return root

    def run_script(self, root: Path) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [str(SCRIPT), "--repo", str(root)],
            cwd=ROOT,
            check=False,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )

    def test_updates_exact_identity_files_from_current_release_notes(self):
        root = self.create_repo()
        previous_notes = (root / f"docs/releases/{CURRENT_TAG}.md").read_text(
            encoding="utf-8"
        )
        self.assertEqual(previous_notes.count(CURRENT_TAG), 2)
        self.assertEqual(
            [
                line
                for line in previous_notes.splitlines()
                if line.startswith("- Source tag:")
            ],
            [f"- Source tag: `{CURRENT_TAG}`"],
        )

        result = self.run_script(root)
        self.assertEqual(result.returncode, 0, result.stderr)
        output = json.loads(result.stdout)
        self.assertEqual(output["current_version"], CURRENT_VERSION)
        self.assertEqual(output["next_version"], NEXT_VERSION)
        self.assertEqual(output["next_tag"], NEXT_TAG)
        self.assertEqual(output["android_version_code"], CURRENT_VERSION_CODE + 1)
        self.assertEqual(output["ios_build_number"], CURRENT_IOS_BUILD + 1)
        self.assertEqual(output["commit_count"], 1)

        next_notes_path = f"docs/releases/{NEXT_TAG}.md"
        expected_paths = sorted(
            (
                "CHANGELOG.md",
                "app/app.base.json",
                "app/ios-build.json",
                "daemon/cmd/zen/version.go",
                "docs/install-daemon.md",
                "docs/ios-ci-release.md",
                next_notes_path,
                "scripts/verify-release-identity.sh",
            )
        )
        self.assertEqual(output["changed_paths"], expected_paths)
        self.assertEqual(
            sorted(git(root, "status", "--short").splitlines()),
            sorted(
                [
                    " M CHANGELOG.md",
                    " M app/app.base.json",
                    " M app/ios-build.json",
                    " M daemon/cmd/zen/version.go",
                    " M docs/install-daemon.md",
                    " M docs/ios-ci-release.md",
                    " M scripts/verify-release-identity.sh",
                    f"?? {next_notes_path}",
                ]
            ),
        )

        base = json.loads((root / "app/app.base.json").read_text(encoding="utf-8"))
        self.assertEqual(base["expo"]["version"], NEXT_VERSION)
        self.assertEqual(
            base["expo"]["android"]["versionCode"], CURRENT_VERSION_CODE + 1
        )
        self.assertEqual(
            json.loads((root / "app/ios-build.json").read_text(encoding="utf-8"))[
                "buildNumber"
            ],
            CURRENT_IOS_BUILD + 1,
        )
        self.assertIn(
            f'var Version = "{NEXT_VERSION}"',
            (root / "daemon/cmd/zen/version.go").read_text(encoding="utf-8"),
        )

        verifier = (root / "scripts/verify-release-identity.sh").read_text(
            encoding="utf-8"
        )
        self.assertNotIn(CURRENT_VERSION, verifier)
        self.assertEqual(verifier.count(NEXT_VERSION), 5)
        self.assertIn(
            f'EXPECTED_VERSION_CODE="{CURRENT_VERSION_CODE + 1}"', verifier
        )
        self.assertIn(
            f'EXPECTED_IOS_BUILD_NUMBER="{CURRENT_IOS_BUILD + 1}"', verifier
        )
        self.assertIn(
            f"ZEN_VERSION={NEXT_TAG}",
            (root / "docs/install-daemon.md").read_text(encoding="utf-8"),
        )
        ios_docs = (root / "docs/ios-ci-release.md").read_text(encoding="utf-8")
        self.assertIn(f"`{NEXT_VERSION}`", ios_docs)
        self.assertIn(
            f"baseline is build `{CURRENT_IOS_BUILD + 1}` in `app/ios-build.json`",
            ios_docs,
        )

        changelog = (root / "CHANGELOG.md").read_text(encoding="utf-8")
        self.assertLess(changelog.index(NEXT_TAG), changelog.index(CURRENT_TAG))
        self.assertEqual(changelog.count(f"docs/releases/{NEXT_TAG}.md"), 1)
        self.assertEqual(changelog.count(f"docs/releases/{CURRENT_TAG}.md"), 1)

        notes = (root / next_notes_path).read_text(encoding="utf-8")
        self.assertEqual(
            notes.count("- Bundle: `com.daoleno.zen.preview`"),
            1,
        )
        self.assertEqual(notes.count("- ABI: `arm64-v8a`"), 1)
        for marker in (
            f"# Zen {NEXT_TAG}",
            "Add reviewed release change",
            f"- Source tag: `{NEXT_TAG}`",
            f"- TestFlight build: `{CURRENT_IOS_BUILD + 1}`",
            f"- `versionCode`: `{CURRENT_VERSION_CODE + 1}`",
            "com.daoleno.zen",
            "unknown sources",
            "Play Protect",
            "Obtainium",
            "Play Store",
        ):
            self.assertIn(marker, notes)

    def test_fails_closed_when_there_are_no_commits(self):
        result = self.run_script(self.create_repo(with_commit=False))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(f"no commits exist after {CURRENT_TAG}", result.stderr)

    def test_fails_closed_on_dirty_state_before_writing(self):
        root = self.create_repo()
        (root / "unexpected.txt").write_text("dirty\n", encoding="utf-8")
        result = self.run_script(root)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unexpected dirty state", result.stderr)
        self.assertFalse((root / f"docs/releases/{NEXT_TAG}.md").exists())

    def test_fails_closed_on_malformed_or_mismatched_identity(self):
        root = self.create_repo()
        version_go = root / "daemon/cmd/zen/version.go"
        version_go.write_text(
            version_go.read_text(encoding="utf-8").replace(
                CURRENT_VERSION, "9.9.9-beta.999"
            ),
            encoding="utf-8",
        )
        git(root, "add", "daemon/cmd/zen/version.go")
        git(root, "commit", "-m", "Introduce mismatched identity")
        result = self.run_script(root)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("daemon/cmd/zen/version.go", result.stderr)
        self.assertFalse((root / f"docs/releases/{NEXT_TAG}.md").exists())

    def test_fails_closed_on_missing_identity_source(self):
        root = self.create_repo()
        (root / "app/ios-build.json").unlink()
        git(root, "add", "app/ios-build.json")
        git(root, "commit", "-m", "Remove iOS identity source")
        result = self.run_script(root)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing required file: app/ios-build.json", result.stderr)
        self.assertFalse((root / f"docs/releases/{NEXT_TAG}.md").exists())

    def test_fails_closed_when_next_output_or_tag_exists(self):
        for existing in ("notes", "tag"):
            with self.subTest(existing=existing):
                root = self.create_repo(root=self.fixture / existing)
                if existing == "notes":
                    notes = root / f"docs/releases/{NEXT_TAG}.md"
                    notes.write_text("existing\n", encoding="utf-8")
                    git(root, "add", str(notes.relative_to(root)))
                    git(root, "commit", "-m", "Reserve beta 9 notes")
                else:
                    git(
                        root,
                        "tag",
                        "-a",
                        NEXT_TAG,
                        "-m",
                        f"Existing {NEXT_TAG}",
                    )
                result = self.run_script(root)
                self.assertNotEqual(result.returncode, 0)
            self.assertIn("already exist", result.stderr)

    def test_fails_closed_when_inherited_product_facts_mismatch_owners(self):
        mismatches = (
            (
                "ios-preview-bundle",
                "- Bundle: `com.daoleno.zen.preview`",
                "- Bundle: `com.example.wrong.preview`",
                "iOS Preview bundle does not match app/iosIdentity.js",
            ),
            (
                "android-release-abi",
                "- ABI: `arm64-v8a`",
                "- ABI: `x86_64`",
                "Android ABI does not match "
                "app/modules/zen-terminal-vt/native.lock.json",
            ),
        )
        for name, old, new, error in mismatches:
            with self.subTest(name=name):
                root = self.create_repo(root=self.fixture / name)
                notes_path = root / f"docs/releases/{CURRENT_TAG}.md"
                original_base = (root / "app/app.base.json").read_bytes()
                notes_path.write_text(
                    notes_path.read_text(encoding="utf-8").replace(old, new),
                    encoding="utf-8",
                )
                git(root, "add", str(notes_path.relative_to(root)))
                git(root, "commit", "-m", f"Mismatch {name}")

                result = self.run_script(root)

                self.assertNotEqual(result.returncode, 0)
                self.assertIn(error, result.stderr)
                self.assertEqual(
                    (root / "app/app.base.json").read_bytes(),
                    original_base,
                )
                self.assertFalse(
                    (root / f"docs/releases/{NEXT_TAG}.md").exists()
                )


class ReleaseWorkflowContractTests(unittest.TestCase):
    def test_next_beta_workflow_is_manual_atomic_and_build_gated(self):
        workflow = (ROOT / ".github/workflows/release-next-beta.yml").read_text(
            encoding="utf-8"
        )
        dispatch = workflow.split("workflow_dispatch:", 1)[1].split(
            "concurrency:", 1
        )[0]
        self.assertNotIn("inputs:", dispatch)
        self.assertLess(
            workflow.index("Test release preparation and workflow contracts"),
            workflow.index("Prepare deterministic release identity and notes"),
        )
        release_job_header = workflow.split("steps:", 1)[0]
        self.assertNotIn("runner.temp", release_job_header)
        self.assertEqual(
            workflow.count("ZEN_BUILD_TMPDIR: ${{ runner.temp }}/zen-build"),
            1,
        )
        test_step = workflow.split(
            "- name: Test release preparation and workflow contracts",
            1,
        )[1].split("- name: Prepare deterministic release identity and notes", 1)[0]
        self.assertLess(
            test_step.index('mkdir -p "$ZEN_BUILD_TMPDIR"'),
            test_step.index("python3 -m unittest discover"),
        )
        permissions = workflow.split("permissions:", 1)[1].split("jobs:", 1)[0]
        self.assertEqual(
            {
                line.strip()
                for line in permissions.splitlines()
                if line.strip()
            },
            {"actions: write", "contents: write"},
        )
        for marker in (
            "fetch-depth: 0",
            'PYTHONDONTWRITEBYTECODE: "1"',
            'START_SHA="$(git rev-parse refs/remotes/origin/main)"',
            "./scripts/prepare-release.py",
            "./scripts/verify-release-identity.sh",
            "go test ./cmd/zen",
            'git config user.name "github-actions[bot]"',
            'git add --pathspec-from-file="$RUNNER_TEMP/zen-release-paths"',
            'git commit -m "Prepare $NEXT_TAG"',
            'git tag -a "$NEXT_TAG" HEAD',
            'git rev-parse refs/remotes/origin/main)" == "$START_SHA"',
            "git push --atomic origin",
            '"HEAD:refs/heads/main"',
            '"refs/tags/$NEXT_TAG"',
            "gh workflow run release-artifacts.yml",
            '--ref "$NEXT_TAG"',
            '-f "ref=$NEXT_TAG"',
            '-f "publish=true"',
            "gh workflow run ios-release.yml",
            '-f "build_number=$IOS_BUILD_NUMBER"',
            '-f "app_identity=preview"',
            '-f "destination=testflight"',
            "steps.release.outputs.ios_build_number",
            "actions: write",
            "contents: write",
        ):
            self.assertIn(marker, workflow)
        self.assertNotIn("gh release", workflow)
        self.assertNotIn("gh pr", workflow)
        self.assertNotIn("pull-requests:", workflow)
        self.assertNotRegex(workflow, r"\n\s+push:")
        self.assertNotRegex(workflow, r"\n\s+pull_request:")
        self.assertNotIn("release-please", workflow)
        self.assertNotIn("PAT", workflow)
        self.assertNotIn("secrets.", workflow)


if __name__ == "__main__":
    unittest.main()
