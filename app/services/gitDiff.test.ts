import { describe, expect, test } from "bun:test";
import {
  filterGitDiffFiles,
  gitDiffCounts,
  type GitDiffFileInfo,
} from "./gitDiff";

const files: GitDiffFileInfo[] = [
  {
    path: " both [x]\t.txt ",
    old_path: "old -> name",
    status: "renamed",
    staged: true,
    unstaged: true,
    untracked: false,
    additions: 5,
    deletions: 3,
    staged_additions: 2,
    staged_deletions: 1,
    working_additions: 3,
    working_deletions: 2,
  },
  {
    path: "new.txt",
    status: "untracked",
    staged: false,
    unstaged: false,
    untracked: true,
    additions: 1,
    working_additions: 1,
  },
  {
    path: "deleted.txt",
    status: "deleted",
    staged: true,
    unstaged: false,
    untracked: false,
    deletions: 4,
    staged_deletions: 4,
  },
];

describe("Git diff review", () => {
  test("working includes untracked but not staged-only; staged excludes untracked", () => {
    expect(filterGitDiffFiles(files, "working", "")).toEqual(files.slice(0, 2));
    expect(filterGitDiffFiles(files, "staged", "")).toEqual([
      files[0],
      files[2],
    ]);
  });
  test("path search includes rename source and preserves meaningful spaces", () => {
    expect(filterGitDiffFiles(files, "all", "OLD ->")).toEqual([files[0]]);
    expect(filterGitDiffFiles(files, "all", " [x]\t")[0].path).toBe(
      " both [x]\t.txt ",
    );
    expect(filterGitDiffFiles(files, "all", "missing")).toEqual([]);
    expect(filterGitDiffFiles([], "all", "")).toEqual([]);
  });
  test("comparison counts do not sum index and working changes in staged mode", () => {
    expect(gitDiffCounts(files[0], "all")).toEqual([5, 3]);
    expect(gitDiffCounts(files[0], "staged")).toEqual([2, 1]);
    expect(gitDiffCounts(files[0], "working")).toEqual([3, 2]);
    expect(gitDiffCounts(files[1], "working")).toEqual([1, 0]);
  });
});
