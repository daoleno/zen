import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { GitDiffStream, diffStreamRow, restoreDiffStream, diffBookmark } from "./gitDiffStream";
import type { GitDiffPage, GitDiffPageRequest } from "../../services/gitDiff";

function page(request: GitDiffPageRequest, total = 517): GitDiffPage {
  const matches = request.query ? [17, 318, 499] : [];
  return {
    path: request.path, scope: request.scope, start: request.row, version: "v1", stale: false, total,
    rows: Array.from({ length: Math.min(120, total - request.row) }, (_, i) => ({ kind: "add", text: `line ${request.row + i}${matches.includes(request.row + i) ? " needle" : ""}`, new: request.row + i })),
    matches: matches.length, previous_match: matches.filter(n => n < request.row).at(-1) ?? -1, next_match: matches.find(n => n > request.row) ?? -1,
    hunks: 1, previous_hunk: -1, next_hunk: -1,
  };
}
const values = (stream: GitDiffStream) => {
  const s = stream.getSnapshot();
  return Array.from({ length: s.end - s.start }, (_, i) => diffStreamRow(s, i).index);
};

describe("continuous Git diff", () => {
  test("bookmarks actual mounted geometry rather than an estimated visible index", () => {
    const frames = new Map([[420, { offset: 0, length: 30 }], [424, { offset: 150, length: 100 }], [425, { offset: 250, length: 300 }]]);
    expect(diffBookmark(frames, 280, 420)).toEqual({ row: 425, offset: 30 });
    expect(diffBookmark(new Map(), 280, 425)).toEqual({ row: 425, offset: 0 });
  });
  test("initial closed search cannot cancel the first content request", async () => {
    let resolve!: (value: GitDiffPage) => void;
    const stream = new GitDiffStream(() => new Promise(done => { resolve = done; }), "file", "all");
    const pending = stream.loadNext(); await stream.search("");
    resolve(page({ path: "file", scope: "all", row: 0 })); await pending;
    expect(values(stream)).toHaveLength(120);
  });
  test("loads all rows once, preserving object identity and a stable list anchor", async () => {
    const requests: GitDiffPageRequest[] = [];
    const stream = new GitDiffStream(async r => { requests.push(r); return page(r); }, "file", "all");
    await stream.loadNext();
    const first = diffStreamRow(stream.getSnapshot(), 0);
    while (stream.getSnapshot().end < 517) await stream.loadNext();
    expect(values(stream)).toEqual(Array.from({ length: 517 }, (_, i) => i));
    expect(diffStreamRow(stream.getSnapshot(), 0)).toBe(first);
    expect(stream.getSnapshot().anchor).toBe(0);
    expect(requests.map(r => r.row)).toEqual([0, 120, 240, 360, 480]);
    expect(requests.slice(1).every(r => r.version === "v1")).toBe(true);
    await stream.loadNext(); expect(requests).toHaveLength(5);
  });
  test("coalesces near-end calls and retries without discarding loaded rows", async () => {
    let fail = true, calls = 0;
    const stream = new GitDiffStream(async r => { calls++; if (r.row === 120 && fail) throw Error("offline"); return page(r); }, "file", "working");
    await Promise.all([stream.loadNext(), stream.loadNext(), stream.loadNext()]);
    expect(calls).toBe(1);
    await stream.loadNext(); expect(stream.getSnapshot().error).toBe("offline");
    expect(values(stream)).toHaveLength(120);
    await stream.loadNext(); expect(calls).toBe(2);
    fail = false; await stream.retry(); expect(values(stream)).toHaveLength(240);
    expect(stream.getSnapshot().error).toBeNull();
  });
  test("version changes freeze old content instead of mixing it", async () => {
    const stream = new GitDiffStream(async r => ({ ...page(r), version: r.row ? "v2" : "v1" }), "file", "all");
    await stream.loadNext(); await stream.loadNext();
    expect(stream.getSnapshot().stale).toBe(true);
    expect(values(stream)).toHaveLength(120);
    await stream.loadNext(); expect(values(stream)).toHaveLength(120);
    await stream.refresh(); expect(stream.getSnapshot().stale).toBe(false);
    expect(stream.getSnapshot().anchor).toBe(1);
  });
  test("search anchors allow continuous prepend/append without overlap or gaps", async () => {
    const stream = new GitDiffStream(async r => page(r), "file", "staged");
    await stream.loadNext(); await stream.search("needle");
    expect(stream.getSnapshot().targetRow).toBe(17);
    await stream.loadPrevious(); expect(values(stream)).toEqual(Array.from({ length: 137 }, (_, i) => i));
    await stream.seek("next", 17);
    expect(stream.getSnapshot().targetRow).toBe(318);
    while (stream.getSnapshot().start > 0) await stream.loadPrevious();
    while (stream.getSnapshot().end < 517) await stream.loadNext();
    expect(values(stream)).toEqual(Array.from({ length: 517 }, (_, i) => i));
    expect(stream.getSnapshot().query).toBe("needle");
  });
  test("a last-line search can expand backwards from an underfilled viewport", async () => {
    const stream = new GitDiffStream(async r => page(r, 500), "file", "all");
    await stream.search("needle"); await stream.seek("next", 318);
    expect(stream.getSnapshot().targetRow).toBe(499);
    expect(values(stream)).toEqual(Array.from({ length: 121 }, (_, i) => 379 + i));
    await stream.loadPrevious();
    expect(values(stream)).toEqual(Array.from({ length: 241 }, (_, i) => 259 + i));
  });
  test("stale responses cannot enter another file, scope or search", async () => {
    let resolve!: (value: GitDiffPage) => void;
    const stream = new GitDiffStream(r => new Promise(done => { resolve = done; }), "file", "all");
    const pending = stream.loadNext(); stream.dispose();
    resolve(page({ path: "file", scope: "all", row: 0 })); await pending;
    expect(values(stream)).toEqual([]);
    const wrong = new GitDiffStream(async r => ({ ...page(r), scope: "staged" }), "file", "all");
    await wrong.loadNext(); expect(wrong.getSnapshot().error).toBe("Unexpected diff response");
  });
  test("saved chunks survive return and a pending request does not survive unmount", async () => {
    const stream = new GitDiffStream(async r => page(r), "file", "all");
    await stream.loadNext(); await stream.loadNext();
    const saved = stream.getSnapshot(); stream.dispose();
    const restored = new GitDiffStream(async r => page(r), "file", "all", saved);
    await restored.loadNext(); expect(values(restored)).toHaveLength(360);
    expect(diffStreamRow(restored.getSnapshot(), 0)).toBe(diffStreamRow(saved, 0));
  });
  test("restores a source anchor and can continuously read earlier rows again", async () => {
    const stream = new GitDiffStream(async r => page(r), "file", "all");
    await stream.loadNext(); await stream.loadNext();
    const restored = new GitDiffStream(async r => page(r), "file", "all", restoreDiffStream(stream.getSnapshot(), 174));
    expect(restored.getSnapshot().targetRow).toBe(174);
    while (restored.getSnapshot().start) await restored.loadPrevious();
    expect(values(restored)).toEqual(Array.from({ length: 240 }, (_, i) => i));
    restored.reanchor(174);
    expect(restored.getSnapshot().targetRow).toBe(174);
    expect(restored.getSnapshot().version).toBe("v1");
  });
  test("empty or nonprogressing pages cannot silently omit content", async () => {
    const bad = new GitDiffStream(async r => ({ ...page(r), rows: [] }), "file", "all");
    await bad.loadNext(); expect(bad.getSnapshot().error).toBe("Incomplete diff response");
    const empty = new GitDiffStream(async r => page(r, 0), "file", "all");
    await empty.loadNext(); expect(empty.getSnapshot().total).toBe(0); expect(empty.getSnapshot().error).toBeNull();
  });
  test("native chrome exposes no manual paging or permanent desktop toolbar", () => {
    const reader = readFileSync(new URL("./GitDiffReader.tsx", import.meta.url), "utf8");
    const chrome = readFileSync(new URL("./GitDiffSheetTopChrome.tsx", import.meta.url), "utf8");
    for (const copy of ["Previous page", "Next page", "styles.footer", "data={page.rows}"]) expect(reader).not.toContain(copy);
    expect(reader).toContain("onEndReached"); expect(reader).toContain("onStartReached");
    expect(reader).toContain("maintainVisibleContentPosition");
    for (const copy of ["Previous file", "Next file", "Staged + working tree", "HEAD → index"]) expect(chrome).not.toContain(copy);
    expect(chrome).toContain('"Changed files"'); expect(chrome).toContain('label="Diff options"');
    expect(chrome).not.toContain("modeBar");
    expect(chrome).not.toContain("compact");
  });
  test("overview list never controls contentOffset and resets imperatively", () => {
    const content = readFileSync(new URL("./GitDiffSheetDiffContent.tsx", import.meta.url), "utf8");
    expect(content).not.toContain("contentOffset={");
    expect(content).toContain("scrollToOffset");
    expect(content).toContain("ref={listRef}");
    expect(content).toContain("removeClippedSubviews={false}");
    expect(content).not.toContain("BottomSheetFrame");
    const reader = readFileSync(new URL("./GitDiffReader.tsx", import.meta.url), "utf8");
    expect(reader).not.toContain("contentOffset={{ x: horizontalOffset.current");
    expect(reader).toContain("initialHorizontalOffset");
  });
  test("large and split long-line streams retain every byte without flattening chunks", async () => {
    const text = "long-line-".repeat(50);
    const stream = new GitDiffStream(async r => ({ ...page(r, 50000), rows: page(r, 50000).rows.map((row, i) => ({ ...row, text, continuation: i > 0 })) }), "large", "all");
    while (stream.getSnapshot().total === null || stream.getSnapshot().end < 50000) await stream.loadNext();
    expect(stream.getSnapshot().chunks).toHaveLength(417);
    expect(diffStreamRow(stream.getSnapshot(), 49999).row.text).toBe(text);
    expect(values(stream)).toEqual(Array.from({ length: 50000 }, (_, i) => i));
  });
});
