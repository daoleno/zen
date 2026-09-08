import { describe, expect, test } from "bun:test";
import { desktopURL, desktopPoint, desktopKey, desktopText } from "./remoteDesktopModel";

describe("Remote desktop transport and input", () => {
  test("requires TLS or the existing pinned Link loopback", () => {
    expect(desktopURL("wss://host.test/ws?old=value", false)).toBe("wss://host.test/desktop");
    expect(desktopURL("ws://127.0.0.1:1234/ws", true)).toBe("ws://127.0.0.1:1234/desktop");
    for (const url of ["ws://192.0.2.1/ws", "ws://127.0.0.1/ws", "wss://user:secret@host/ws"]) {
      expect(() => desktopURL(url, false)).toThrow();
    }
    expect(() => desktopURL("ws://remote.test/ws", true)).toThrow();
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
