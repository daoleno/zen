import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  createTimelineActivityExpansionState,
  reduceTimelineActivityExpansion,
  resolveTimelineActivityExpansion,
  type TimelineActivityExpansionState,
} from "./InterfaceTimelineActivityExpansionState";

function toggle(
  state: TimelineActivityExpansionState,
  eventId: string,
  defaultExpanded = false,
) {
  return reduceTimelineActivityExpansion(state, {
    eventId,
    defaultExpanded,
  });
}

describe("timeline activity expansion state", () => {
  test("rapid toggles preserve exact odd and even click parity", () => {
    let state = createTimelineActivityExpansionState("tool-a");

    for (let index = 0; index < 101; index += 1) {
      state = toggle(state, "tool-a");
    }
    expect(resolveTimelineActivityExpansion(state, "tool-a", false)).toBe(true);

    state = toggle(state, "tool-a");
    expect(resolveTimelineActivityExpansion(state, "tool-a", false)).toBe(
      false,
    );
  });

  test("five cards retain independent choices under different rapid parity", () => {
    const clickCounts = new Map([
      ["tool-a", 1],
      ["tool-b", 2],
      ["tool-c", 3],
      ["tool-d", 4],
      ["tool-e", 5],
    ]);
    const states = new Map(
      [...clickCounts.keys()].map((eventId) => [
        eventId,
        createTimelineActivityExpansionState(eventId),
      ]),
    );

    for (const [eventId, clickCount] of clickCounts) {
      for (let index = 0; index < clickCount; index += 1) {
        states.set(eventId, toggle(states.get(eventId)!, eventId));
      }
    }

    expect(
      [...states].map(([eventId, state]) =>
        resolveTimelineActivityExpansion(state, eventId, false),
      ),
    ).toEqual([true, false, true, false, true]);
  });

  test("same stable event ID retains user choice through streaming upserts", () => {
    const eventId = "tool-stream";
    let state = createTimelineActivityExpansionState(eventId);

    expect(resolveTimelineActivityExpansion(state, eventId, true)).toBe(true);
    state = toggle(state, eventId, true);

    // running -> done with a body upsert and a new default
    expect(resolveTimelineActivityExpansion(state, eventId, false)).toBe(false);
    // done -> failed changes the default again, but not the user's choice
    expect(resolveTimelineActivityExpansion(state, eventId, true)).toBe(false);
  });

  test("a new event ID ignores another card's choice and follows its own default", () => {
    let state = createTimelineActivityExpansionState("tool-a");
    state = toggle(state, "tool-a", false);

    expect(resolveTimelineActivityExpansion(state, "tool-b", false)).toBe(
      false,
    );
    expect(resolveTimelineActivityExpansion(state, "tool-b", true)).toBe(true);

    state = toggle(state, "tool-b", true);
    expect(resolveTimelineActivityExpansion(state, "tool-b", false)).toBe(
      false,
    );
  });

  test("expanded details follow local disclosure immediately without a deferred gate", () => {
    const eventId = "tool-rapid";
    let state = createTimelineActivityExpansionState(eventId);
    expect(resolveTimelineActivityExpansion(state, eventId, false)).toBe(false);

    state = toggle(state, eventId);
    expect(resolveTimelineActivityExpansion(state, eventId, false)).toBe(true);

    state = toggle(state, eventId);
    expect(resolveTimelineActivityExpansion(state, eventId, false)).toBe(false);

    state = toggle(state, eventId);
    expect(resolveTimelineActivityExpansion(state, eventId, false)).toBe(true);

    const hookSource = readFileSync(
      join(import.meta.dir, "InterfaceTimelineActivityExpansionState.ts"),
      "utf8",
    );
    expect(hookSource).not.toContain("useDeferredValue");
    expect(hookSource).toContain("detailsExpanded: expanded");
  });

  test("the card action uses React Native's touchable primitive", () => {
    const headerSource = readFileSync(
      join(import.meta.dir, "InterfaceTimelineActivityHeader.tsx"),
      "utf8",
    );

    expect(headerSource).not.toContain("react-native-gesture-handler");
  });
});
