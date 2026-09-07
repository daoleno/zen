import { expect, test } from "bun:test";
import {
  captureTimelineReadingAnchor,
  resolveTimelineReadingAnchor,
  timelineReadingOffset,
  rememberTimelineReadingPosition,
  recallTimelineReadingPosition,
  initialTimelineReadingWindow,
} from "./timelineReadingPosition";
import { readingFixtureMessage } from "./interfaceReadingFixtureData";
import { resolveInterfaceReadingIdentity } from "./interfaceChatSessionIdentity";

test("reading identity follows provider conversations, not turn/status or transient reconnect state", () => {
  const first = resolveInterfaceReadingIdentity("server-a:worker", false, "provider-thread-a");
  expect(resolveInterfaceReadingIdentity("server-a:worker", false, undefined, first).key).toBe(first.key);
  expect(resolveInterfaceReadingIdentity("server-a:worker", false, "provider-thread-b", first).key).not.toBe(first.key);
  expect(resolveInterfaceReadingIdentity("server-b:worker", false, undefined, first).providerId).toBeUndefined();
  expect(resolveInterfaceReadingIdentity("server-a:brain-thread", true, "host-a").key).toBe(resolveInterfaceReadingIdentity("server-a:brain-thread", true, "host-b").key);
});

test("older fixture pages support negative sequence positions without a render exception", () => {
  const page = Array.from({ length: 100 }, (_, i) =>
    readingFixtureMessage(i - 100),
  );
  expect(new Set(page.map((event) => event.id)).size).toBe(100);
  expect(page.every((event) => event.body?.includes("Reading anchor"))).toBe(
    true,
  );
});

test("inverted native geometry preserves a partial row with translated origin", () => {
  const frames = new Map([["row", { offset: 1000, length: 800 }]]);
  const anchor = captureTimelineReadingAnchor(
    frames,
    ["row"],
    1400,
    500,
    20,
    64,
  )!;
  expect(anchor.intraOffset).toBe(-16);
  expect(
    timelineReadingOffset(anchor, { offset: 1200, length: 800 }, 500, 20, 64),
  ).toBe(1600);
});

test("deleted anchors choose an existing neighbor, not the newest message", () => {
  const anchor = {
    id: "deleted",
    index: 10,
    intraOffset: 25,
    neighbors: ["nearby", "older"],
  };
  expect(
    resolveTimelineReadingAnchor(anchor, ["newest", "nearby", "older"]),
  ).toMatchObject({ id: "nearby", intraOffset: 0 });
  expect(resolveTimelineReadingAnchor(anchor, [])).toBeUndefined();
});

test("a deep saved anchor waits through empty loading and uses a bounded prefix when the snapshot arrives", () => {
  const anchor = {
    id: "row-9000",
    index: 9000,
    intraOffset: 40,
    neighbors: ["row-8999"],
  };
  expect(initialTimelineReadingWindow([], anchor)).toBeNull();
  const ids = Array.from({ length: 10000 }, (_, i) => `row-${i}`);
  expect(initialTimelineReadingWindow(ids, anchor)).toEqual({
    start: "row-8992",
    initialCount: 10,
  });
  expect(initialTimelineReadingWindow([], undefined)).toEqual({
    start: undefined,
    initialCount: 8,
  });
});

test("only bounded per-conversation position metadata is retained", () => {
  for (let i = 0; i < 40; i++)
    rememberTimelineReadingPosition(`bounded-${i}`, {
      mode: "detached",
      anchor: { id: `row-${i}`, index: i, intraOffset: 12, neighbors: [] },
    });
  expect(recallTimelineReadingPosition("bounded-0").mode).toBe("attached");
  expect(recallTimelineReadingPosition("bounded-39").anchor?.id).toBe("row-39");
});
