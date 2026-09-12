import { describe, expect, test } from "bun:test";
import { desktopPoint, desktopKey, desktopNamedKey, desktopText, desktopTextEdits, desktopCommittedText, beginDesktopPan, advanceDesktopPan, MAX_DESKTOP_BATCH_EVENTS } from "./remoteDesktopModel";

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
    expect(desktopTextEdits("abc", "ab")).toEqual([desktopNamedKey("Backspace")]);
    expect(desktopTextEdits("abc", "axc").flat()).toEqual([
      ...desktopNamedKey("Backspace"), ...desktopNamedKey("Backspace"), ...desktopText("xc"),
    ]);
    expect(desktopTextEdits("", "")).toEqual([]);
    expect(desktopTextEdits("ab", "ab")).toEqual([]);
  });
  test("pasted text is not truncated and every batch respects the host bound", () => {
    const batches = desktopTextEdits("a".repeat(65), "A".repeat(65));
    expect(batches.every((events) => events.length <= 64)).toBe(true);
    expect(batches.flat().filter((event) => event.type === "text" && event.code === 65)).toHaveLength(65);
  });
  test("maps the video rectangle and rejects letterboxing", () => {
    expect(desktopPoint(200, 400, 400, 800, 1280, 720)).toEqual({ x: 0.5, y: 0.5 });
    expect(desktopPoint(200, 0, 400, 800, 1280, 720)).toBeNull();
    expect(desktopPoint(NaN, 0, 400, 800, 1280, 720)).toBeNull();
    expect(desktopPoint(0, 0, 0, 0, 1280, 720)).toBeNull();
  });
  test("atomic text events carry Unicode and named keys stay keysyms", () => {
    expect(desktopKey(97)).toEqual([{ type: "key", code: 97, down: true }, { type: "key", code: 97, down: false }]);
    expect(desktopNamedKey("Backspace")).toEqual(desktopKey(0xff08));
    expect(desktopNamedKey("Enter")).toEqual(desktopKey(0xff0d));
    expect(desktopNamedKey("Delete")).toEqual(desktopKey(0xffff));
    // Press-then-release is the host's job for atomic characters; the phone
    // sends one event per scalar and lets the host map it to the keymap.
    expect(desktopText("Ab!")).toEqual([
      { type: "text", code: 0x41 }, { type: "text", code: 0x62 }, { type: "text", code: 0x21 },
    ]);
    expect(desktopText("你好😀")).toEqual([
      { type: "text", code: 0x4f60 }, { type: "text", code: 0x597d }, { type: "text", code: 0x1f600 },
    ]);
    expect(desktopText("a\nb\tc")).toEqual([{ type: "text", code: 0x61 }, { type: "text", code: 0x62 }, { type: "text", code: 0x63 }]);
    expect(desktopText("")).toEqual([]);
  });
  test("no batch exceeds the wire bound", () => {
    const batches = desktopTextEdits("", "x".repeat(100));
    expect(batches.every((events) => events.length <= 64)).toBe(true);
    expect(batches.flat().length).toBe(100);
  });
  test("committed text splits into bounded batches with newlines as Enter", () => {
    expect(desktopCommittedText("a\nb")).toEqual([
      [...desktopText("a"), ...desktopNamedKey("Enter"), ...desktopText("b")],
    ]);
    // One Enter pair per newline; CRLF counts once.
    expect(desktopCommittedText("\r\n").flat()).toEqual(desktopNamedKey("Enter"));
    expect(desktopCommittedText("\n").flat()).toEqual(desktopNamedKey("Enter"));
    expect(desktopCommittedText("a\r\nb")[0].map((event) => event.type)).toEqual(["text", "key", "key", "text"]);
    // Control characters that named keys own are dropped, not sent as text.
    expect(desktopCommittedText("a\u0000\u0007b").flat()).toEqual(desktopText("ab"));
    expect(desktopCommittedText("")).toEqual([]);
    const long = desktopCommittedText("x".repeat(200));
    expect(long.every((batch) => batch.length <= MAX_DESKTOP_BATCH_EVENTS)).toBe(true);
    expect(long.flat().filter((event) => event.type === "text")).toHaveLength(200);
    expect(long.flat().length).toBe(200);
    // A press/release pair is never split across a batch boundary.
    const pairAtBoundary = desktopCommittedText("x".repeat(63) + "\n");
    expect(pairAtBoundary[0]).toHaveLength(63);
    expect(pairAtBoundary[1]).toEqual(desktopNamedKey("Enter"));
    // Astral-plane scalars stay one event each and never split a surrogate.
    expect(desktopCommittedText("你好😀")[0]).toEqual(desktopText("你好😀"));
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
