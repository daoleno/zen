import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { selectCurrentServerItems } from "./currentServerSelection";
import { initialWorkState, workReducer } from "../store/work";

test("current-server rows exclude old and unknown owners even with colliding IDs", () => {
  const rows = [
    { id: "same", serverId: "a" },
    { id: "same", serverId: "b" },
    { id: "unknown", serverId: "removed" },
  ];
  expect(selectCurrentServerItems(rows, "a")).toEqual([rows[0]]);
  expect(selectCurrentServerItems(rows, "b")).toEqual([rows[1]]);
  expect(selectCurrentServerItems(rows, null)).toEqual([]);
  expect(selectCurrentServerItems(rows, "missing")).toEqual([]);
});

test("switching server clears remote Work projection without dropping an unsaved draft", () => {
  let state = workReducer(initialWorkState, { type: "WORK_ITEM_CHANGED", serverId: "a", serverName: "A", serverUrl: "https://a.test", workItem: { id: "note", body: "remote" } });
  state = workReducer(state, { type: "WORK_DRAFT_CHANGED", serverId: "a", id: "note", body: "unsaved", baseMtime: "v1" });
  state = workReducer(state, { type: "REMOVE_SERVER", serverId: "a" });
  expect(state.byKey["a:note"]).toBeUndefined();
  expect(state.draftsByKey["a:note"]).toEqual({ body: "unsaved", baseMtime: "v1" });
  expect(state.draftsByKey["b:note"]).toBeUndefined();
});

test("save acknowledgement preserves newer typing and rejects a stale base revision", () => {
  let state = workReducer(initialWorkState, { type: "WORK_DRAFT_CHANGED", serverId: "a", id: "note", body: "newer typing", baseMtime: "v1" });
  state = workReducer(state, { type: "WORK_DRAFT_SAVED", serverId: "a", id: "note", body: "submitted", baseMtime: "v1", mtime: "v2" });
  expect(state.draftsByKey["a:note"]).toEqual({ body: "newer typing", baseMtime: "v2" });
  const newer = state;
  state = workReducer(state, { type: "WORK_DRAFT_SAVED", serverId: "a", id: "note", body: "newer typing", baseMtime: "v1", mtime: "stale" });
  expect(state).toBe(newer);
  state = workReducer(state, { type: "WORK_DRAFT_SAVED", serverId: "a", id: "note", body: "newer typing", baseMtime: "v2", mtime: "v3" });
  expect(state.draftsByKey["a:note"]).toBeUndefined();
});

test("Sessions, services and Stats have no feature-local server enumeration or preference fallback", () => {
  const list = readFileSync(new URL("../app/(primary)/list.tsx", import.meta.url), "utf8");
  const stats = readFileSync(new URL("../app/stats.tsx", import.meta.url), "utf8");
  expect(list).toContain("selectCurrentServerItems(state.workers, currentServerId)");
  expect(list).toContain("isCurrentServer(server.id)");
  expect(list).toContain("isWorkerSessionListFreshForConnection(state, serverId)");
  for (const source of [list, stats]) {
    expect(source).not.toContain("getServers(");
    expect(source).not.toContain("connectedServers");
    expect(source).not.toContain("resolvePreferredServer");
  }
  expect(stats).toContain("statsSnapshot?.serverId === currentServerId");
  expect(stats).not.toContain("mergeStatsPayloads");
});

test("stale routed pages are hidden and only focused pages redirect", () => {
  for (const path of ["../app/terminal/[id].tsx", "../app/work/[id].tsx"]) {
    const source = readFileSync(new URL(path, import.meta.url), "utf8");
    expect(source).toContain("focused && hydrated && !isCurrentServer");
    expect(source).toContain("if (!hydrated || !isCurrentServer");
    expect(source).not.toContain("<Redirect");
  }
  const sheet = readFileSync(new URL("../components/terminal/NewTerminalSheet.tsx", import.meta.url), "utf8");
  expect(sheet).toContain("if (!serverId || !isCurrentServer(serverId)) return");
  expect(sheet).not.toContain("serverOptions");
  expect(sheet).not.toContain("onSelectServer");
});
