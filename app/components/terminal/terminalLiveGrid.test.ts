import { describe, expect, test } from "bun:test";
import { TerminalLiveGridOwner } from "./terminalLiveGrid";

describe("Terminal phone grid ownership", () => {
  test("44x18 opens a 44x18 PTY and creates a 44x18 Ghostty model", () => {
    const ptyGrids: Array<[number, number]> = [];
    const ghosttyResets: Array<[string, number, number, number, number]> = [];
    const owner = new TerminalLiveGridOwner({
      requestPtyGrid(cols, rows) {
        ptyGrids.push([cols, rows]);
      },
      resetGhostty(sessionId, grid) {
        ghosttyResets.push([
          sessionId,
          grid.cols,
          grid.rows,
          grid.cellWidth,
          grid.cellHeight,
        ]);
        return true;
      },
      resizeGhostty() {
        throw new Error("initial attach must reset, not resize, Ghostty");
      },
    });

    expect(owner.update({ cols: 44, rows: 18, cellWidth: 8.5, cellHeight: 20 })).toBe(true);
    expect(owner.attach("session-a")).toBe(true);

    expect(ptyGrids).toEqual([[44, 18]]);
    expect(ghosttyResets).toEqual([["session-a", 44, 18, 8.5, 20]]);
  });

  test("keyboard and orientation changes resize both owners to the same phone grid", () => {
    const calls: string[] = [];
    const owner = new TerminalLiveGridOwner({
      requestPtyGrid(cols, rows) {
        calls.push(`pty:${cols}x${rows}`);
      },
      resetGhostty(sessionId, grid) {
        calls.push(`ghostty-reset:${sessionId}:${grid.cols}x${grid.rows}`);
        return true;
      },
      resizeGhostty(sessionId, grid) {
        calls.push(`ghostty-resize:${sessionId}:${grid.cols}x${grid.rows}`);
        return true;
      },
    });

    owner.update({ cols: 44, rows: 18, cellWidth: 8, cellHeight: 20 });
    owner.attach("session-a");
    owner.update({ cols: 44, rows: 12, cellWidth: 8, cellHeight: 20 });
    owner.update({ cols: 72, rows: 20, cellWidth: 8, cellHeight: 20 });

    expect(calls).toEqual([
      "pty:44x18",
      "ghostty-reset:session-a:44x18",
      "ghostty-resize:session-a:44x12",
      "pty:44x12",
      "ghostty-resize:session-a:72x20",
      "pty:72x20",
    ]);
  });

  test("invalid or duplicate grids cannot split Ghostty and PTY geometry", () => {
    const calls: string[] = [];
    const owner = new TerminalLiveGridOwner({
      requestPtyGrid(cols, rows) {
        calls.push(`pty:${cols}x${rows}`);
      },
      resetGhostty() {
        calls.push("ghostty-reset");
        return true;
      },
      resizeGhostty() {
        calls.push("ghostty-resize");
        return true;
      },
    });

    expect(owner.update({ cols: 0, rows: 18, cellWidth: 8, cellHeight: 20 })).toBe(false);
    expect(owner.update({ cols: 44.9, rows: 18.7, cellWidth: 8, cellHeight: 20 })).toBe(true);
    expect(owner.attach("session-a")).toBe(true);
    expect(owner.update({ cols: 44, rows: 18, cellWidth: 8, cellHeight: 20 })).toBe(false);

    expect(calls).toEqual(["pty:44x18", "ghostty-reset"]);
  });

  test("a rejected Ghostty resize cannot advance the PTY or suppress retry", () => {
    const calls: string[] = [];
    let acceptResize = false;
    const owner = new TerminalLiveGridOwner({
      requestPtyGrid(cols, rows) {
        calls.push(`pty:${cols}x${rows}`);
      },
      resetGhostty() {
        return true;
      },
      resizeGhostty(_sessionId, grid) {
        calls.push(`ghostty:${grid.cols}x${grid.rows}`);
        return acceptResize;
      },
    });

    owner.update({ cols: 44, rows: 18, cellWidth: 8, cellHeight: 20 });
    owner.attach("session-a");
    calls.length = 0;

    const nextGrid = { cols: 72, rows: 20, cellWidth: 8, cellHeight: 20 };
    expect(owner.update(nextGrid)).toBe(false);
    expect(calls).toEqual(["ghostty:72x20"]);

    acceptResize = true;
    expect(owner.update(nextGrid)).toBe(true);
    expect(calls).toEqual([
      "ghostty:72x20",
      "ghostty:72x20",
      "pty:72x20",
    ]);
  });
});
