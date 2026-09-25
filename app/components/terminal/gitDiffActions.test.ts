import { describe, expect, test } from "bun:test";
import type { GitDiffPage, GitDiffPageRequest, GitDiffRow } from "../../services/gitDiff";
import {
  buildGitDiffFileActions,
  collectGitDiffPatch,
  joinGitDiffRows,
} from "./gitDiffActions";

const PATCH: GitDiffRow[] = [
  { kind: "scope", text: "working" },
  { kind: "meta", text: "diff --git a/app.ts b/app.ts" },
  { kind: "meta", text: "--- a/app.ts" },
  { kind: "meta", text: "+++ b/app.ts" },
  { kind: "hunk", text: "@@ -1,2 +1,2 @@" },
  { kind: "context", text: " keep", old: 1, new: 1 },
  { kind: "delete", text: "-old", old: 2 },
  { kind: "add", text: "+new-", new: 2 },
  { kind: "add", text: "long", new: 2, continuation: true },
];

function pager(rows: GitDiffRow[], size = 3, overrides: Partial<GitDiffPage> = {}) {
  const requests: GitDiffPageRequest[] = [];
  const load = async (request: GitDiffPageRequest): Promise<GitDiffPage> => {
    requests.push(request);
    return {
      path: request.path, scope: request.scope, version: "v1", stale: false,
      start: request.row, total: rows.length,
      rows: rows.slice(request.row, request.row + size),
      hunks: 1, previous_hunk: -1, next_hunk: -1, matches: 0, previous_match: -1, next_match: -1,
      ...overrides,
    };
  };
  return { load, requests };
}

describe("Git diff copy", () => {
  test("rebuilds raw patch text: headers kept, section labels dropped, split lines rejoined", () => {
    expect(joinGitDiffRows(PATCH)).toBe(
      "diff --git a/app.ts b/app.ts\n--- a/app.ts\n+++ b/app.ts\n@@ -1,2 +1,2 @@\n keep\n-old\n+new-long\n",
    );
    expect(joinGitDiffRows([{ kind: "scope", text: "staged" }])).toBe("");
  });
  test("pages the complete comparison pinned to the first version", async () => {
    const { load, requests } = pager(PATCH);
    const result = await collectGitDiffPatch(load, "app.ts", "working");
    expect(result).toEqual({ ok: true, text: joinGitDiffRows(PATCH), lines: 7 });
    expect(requests.map((request) => request.row)).toEqual([0, 3, 6]);
    expect(requests[0].version).toBeUndefined();
    expect(requests.slice(1).every((request) => request.version === "v1")).toBe(true);
  });
  test("refuses oversized, changed and empty comparisons instead of copying part of them", async () => {
    expect(await collectGitDiffPatch(pager(PATCH).load, "app.ts", "all", 4)).toEqual({ ok: false, reason: "too-large", total: PATCH.length });
    expect(await collectGitDiffPatch(pager(PATCH, 3, { stale: true }).load, "app.ts", "all")).toEqual({ ok: false, reason: "changed" });
    expect(await collectGitDiffPatch(pager([]).load, "app.ts", "all")).toEqual({ ok: false, reason: "empty" });
  });
  test("multi-page walks report once so the UI can show progress", async () => {
    const totals: number[] = [];
    await collectGitDiffPatch(pager(PATCH).load, "app.ts", "all", undefined, (total) => totals.push(total));
    await collectGitDiffPatch(pager(PATCH, 50).load, "app.ts", "all", undefined, (total) => totals.push(total));
    expect(totals).toEqual([PATCH.length]);
  });
  test("a page that makes no progress ends the walk", async () => {
    const { load, requests } = pager(PATCH, 3, { rows: [] });
    expect(await collectGitDiffPatch(load, "app.ts", "all")).toEqual({ ok: false, reason: "empty" });
    expect(requests).toHaveLength(1);
  });
});

describe("Git diff file actions", () => {
  const noop = () => {};
  test("row menu offers view, open, copy path and copy diff; reader adds find and display", () => {
    const base = { path: "src/app.ts", deleted: false, onOpenFile: noop, onCopyPath: noop, onCopyPatch: noop, onViewDiff: noop, onToggleSearch: noop, onToggleOptions: noop };
    expect(buildGitDiffFileActions({ ...base, reading: false }).map((item) => item.key)).toEqual(["view", "open", "copy-path", "copy-diff"]);
    expect(buildGitDiffFileActions({ ...base, reading: true }).map((item) => item.key)).toEqual(["find", "display", "open", "copy-path", "copy-diff"]);
  });
  test("a deleted file cannot open a working copy", () => {
    const items = buildGitDiffFileActions({ path: "gone.ts", deleted: true, reading: false, onOpenFile: noop, onCopyPath: noop, onCopyPatch: noop });
    expect(items.find((item) => item.key === "open")?.disabled).toBe(true);
  });
  test("copy actions receive the file path", () => {
    const copied: string[] = [];
    const items = buildGitDiffFileActions({ path: "src/app.ts", deleted: false, reading: false, onOpenFile: noop, onCopyPath: (path) => copied.push(`path:${path}`), onCopyPatch: (path) => copied.push(`diff:${path}`) });
    for (const item of items) if (item.key.startsWith("copy")) item.onPress();
    expect(copied).toEqual(["path:src/app.ts", "diff:src/app.ts"]);
  });
});
