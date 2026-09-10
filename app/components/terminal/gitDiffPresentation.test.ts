import { describe, expect, test } from "bun:test";
import {
  describeGitDiffFile,
  splitGitDiffPath,
} from "./gitDiffPresentation";
import type { GitDiffFileInfo } from "../../services/gitDiff";

function file(overrides: Partial<GitDiffFileInfo>): GitDiffFileInfo {
  return {
    path: "src/app.ts",
    status: "modified",
    staged: false,
    unstaged: true,
    untracked: false,
    ...overrides,
  };
}

describe("Git diff file presentation", () => {
  test("splits top-level and nested paths into name and directory", () => {
    expect(splitGitDiffPath("README.md")).toEqual({
      directory: "",
      name: "README.md",
    });
    expect(splitGitDiffPath("src/terminal/app.ts")).toEqual({
      directory: "src/terminal/",
      name: "app.ts",
    });
  });
  test("status maps to a glyph tone and scope wording", () => {
    expect(describeGitDiffFile(file({ status: "added" }), "all")).toMatchObject({
      statusLabel: "Added",
      scopeLabel: "Unstaged",
      icon: "add-circle-outline",
      tone: "added",
    });
    expect(
      describeGitDiffFile(file({ status: "untracked", untracked: true }), "all"),
    ).toMatchObject({ scopeLabel: "Untracked", tone: "untracked" });
    expect(
      describeGitDiffFile(
        file({ status: "deleted", staged: true, unstaged: false }),
        "staged",
      ),
    ).toMatchObject({ scopeLabel: "Staged", tone: "deleted" });
  });
  test("rename keeps the destination name and the source", () => {
    const presentation = describeGitDiffFile(
      file({
        status: "renamed",
        old_path: "src/old-app.ts",
        path: "src/new/app.ts",
      }),
      "all",
    );
    expect(presentation.name).toBe("app.ts");
    expect(presentation.directory).toBe("src/new/");
    expect(presentation.oldName).toBe("old-app.ts");
    expect(presentation.oldDirectory).toBe("src/");
  });
  test("binary overrides code stats with a binary presentation", () => {
    const presentation = describeGitDiffFile(
      file({ binary: true, additions: 0, deletions: 0 }),
      "all",
    );
    expect(presentation.binary).toBe(true);
    expect(presentation.icon).toBe("cube-outline");
    expect(presentation.tone).toBe("binary");
  });
  test("scope counts use the selected comparison rather than a sum", () => {
    const both = file({
      staged: true,
      unstaged: true,
      additions: 5,
      deletions: 3,
      staged_additions: 2,
      staged_deletions: 1,
      working_additions: 3,
      working_deletions: 2,
    });
    expect(
      describeGitDiffFile(both, "staged"),
    ).toMatchObject({ additions: 2, deletions: 1 });
    expect(
      describeGitDiffFile(both, "working"),
    ).toMatchObject({ additions: 3, deletions: 2 });
    expect(describeGitDiffFile(both, "all")).toMatchObject({
      additions: 5,
      deletions: 3,
    });
  });
});
