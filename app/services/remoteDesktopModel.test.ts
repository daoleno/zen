import { describe, expect, test } from "bun:test";
import { desktopPoint, desktopKey, desktopText, desktopTextEdits } from "./remoteDesktopModel";

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
