import { describe, expect, test } from "bun:test";
import { desktopPoint, desktopKey, desktopText, desktopTextEdits, beginDesktopPan, advanceDesktopPan } from "./remoteDesktopModel";

describe("Remote desktop transport and input", () => {
  test("cumulative and repeated keyboard values emit each character once", () => {
    let previous = "";
    const events = ["a", "ab", "ab", "abc"].flatMap((next) => {
      const result = desktopTextEdits(previous, next).flat();
      previous = next;
      return result;
    });
    expect(events).toEqual(desktopText("abc"));
  });
  test("keyboard deletion and replacement preserve ordered key pairs", () => {
    expect(desktopTextEdits("abc", "ab")).toEqual([desktopKey(0xff08)]);
    expect(desktopTextEdits("abc", "axc").flat()).toEqual([
      ...desktopKey(0xff08), ...desktopKey(0xff08), ...desktopText("xc"),
    ]);
    expect(desktopTextEdits("", "")).toEqual([]);
  });
  test("pasted text is not truncated and every batch respects the host bound", () => {
    const batches = desktopTextEdits("a".repeat(65), "A".repeat(65));
    expect(batches.every((events) => events.length <= 64)).toBe(true);
    expect(batches.flat().filter((event) => event.type === "key" && event.code === 65 && event.down)).toHaveLength(65);
    for (const events of batches) expect(events.at(-1)).toMatchObject({ type: "key", down: false });
  });
  test("maps the video rectangle and rejects letterboxing", () => {
    expect(desktopPoint(200, 400, 400, 800, 1280, 720)).toEqual({ x: 0.5, y: 0.5 });
    expect(desktopPoint(200, 0, 400, 800, 1280, 720)).toBeNull();
    expect(desktopPoint(NaN, 0, 400, 800, 1280, 720)).toBeNull();
    expect(desktopPoint(0, 0, 0, 0, 1280, 720)).toBeNull();
  });
  test("keeps key down and up together, including shifted text", () => {
    expect(desktopKey(97)).toEqual([{ type: "key", code: 97, down: true }, { type: "key", code: 97, down: false }]);
    const events = desktopText("A!");
    expect(events).toHaveLength(8);
    expect(events.at(-1)).toEqual({ type: "key", code: 0xffe1, down: false });
    expect(desktopText("a".repeat(100))).toHaveLength(32);
  });
});

describe("Remote desktop pan state across PanResponder recreation", () => {
  test("accumulates the full cumulative delta across incremental moves", () => {
    // Each move hands the returned state back, exactly as the screen does when
    // React re-renders and rebuilds the PanResponder between moves.
    let state = beginDesktopPan({ x: 0, y: 0 }, { x: 10, y: 20 });
    for (const point of [{ x: 35, y: 50 }, { x: 90, y: 120 }, { x: 140, y: 180 }]) {
      state = advanceDesktopPan(state, point);
    }
    expect(state.offset).toEqual({ x: 130, y: 160 });
    expect(state.point).toEqual({ x: 140, y: 180 });
  });

  test("a second gesture continues from the settled offset", () => {
    let state = beginDesktopPan({ x: 0, y: 0 }, { x: 0, y: 0 });
    state = advanceDesktopPan(state, { x: 50, y: 70 });
    expect(state.offset).toEqual({ x: 50, y: 70 });
    state = beginDesktopPan(state.offset, { x: 200, y: 200 });
    state = advanceDesktopPan(state, { x: 230, y: 240 });
    expect(state.offset).toEqual({ x: 80, y: 110 });
  });

  test("cancel discards in-flight movement and a new gesture re-anchors", () => {
    const settled = { x: 80, y: 110 };
    const inFlight = advanceDesktopPan(beginDesktopPan(settled, { x: 10, y: 10 }), { x: 900, y: 900 });
    expect(inFlight.offset).not.toEqual(settled);
    // Cancelling keeps the settled offset; the next gesture starts from it.
    const restarted = beginDesktopPan(settled, { x: 500, y: 500 });
    expect(restarted.offset).toEqual(settled);
    expect(advanceDesktopPan(restarted, { x: 505, y: 510 }).offset).toEqual({ x: 85, y: 120 });
  });

  test("drag and pinch mapping stay on the shared desktopPoint contract", () => {
    // The screen maps taps/drag through desktopPoint after applying offset and
    // zoom; panning and zooming must not change the mapping formula itself.
    const map = (x: number, y: number, offset: { x: number; y: number }, zoom: number) =>
      desktopPoint((x - 200 - offset.x) / zoom + 200, (y - 400 - offset.y) / zoom + 400, 400, 800, 400, 800);
    expect(map(240, 400, { x: 0, y: 0 }, 1)).toEqual({ x: 0.6, y: 0.5 });
    expect(map(240, 400, { x: 40, y: 0 }, 1)).toEqual({ x: 0.5, y: 0.5 });
    expect(map(300, 400, { x: 0, y: 0 }, 2)).toEqual({ x: 0.625, y: 0.5 });
    expect(desktopPoint(200, 0, 400, 800, 1280, 720)).toBeNull();
  });
});
