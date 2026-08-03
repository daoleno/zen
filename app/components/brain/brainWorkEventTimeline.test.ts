import { describe, expect, test } from "bun:test";
import type { BrainWorkResultEvent } from "../../store/brain";
import { mergeSupplementaryTimelineItems } from "../terminal/InterfaceTimelineModel";
import type { ZenTimelineItem } from "../terminal/InterfaceTimelineItemView";
import { projectCanonicalBrainWorkResultEvents } from "./brainWorkEventTimeline";

const resultEvent: BrainWorkResultEvent = {
  event_id: "event-session-done",
  kind: "session.done",
  work_id: "work-cards",
  work_title: "Ship Brain cards",
  summary: "The delegated implementation completed.",
  session_id: "brain-agent-cards:@1",
  session_name: "Brain cards",
  occurred_at: "2026-08-04T02:00:00Z",
  unread: true,
};

describe("Brain Work event timeline projection", () => {
  test("uses event_id identity and activates the owning result", () => {
    const activated: string[] = [];
    const items = projectCanonicalBrainWorkResultEvents({
      events: [resultEvent],
      displayedThreadId: "thread-current",
      currentThreadId: "thread-current",
      readOnly: false,
      openSessionIds: new Set(["brain-agent-cards:@1"]),
      onActivate: (event, canOpenSession) =>
        activated.push(`${event.event_id}:${canOpenSession}`),
    });

    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({
      type: "brain-work-event",
      id: "event-session-done",
      timestamp: "2026-08-04T02:00:00Z",
    });
    items[0]?.onPress?.();
    expect(activated).toEqual(["event-session-done:true"]);
  });

  test("does not project into ordinary or targeted historical timelines", () => {
    const input = {
      events: [resultEvent],
      currentThreadId: "thread-current",
      readOnly: false,
      openSessionIds: new Set<string>(),
      onActivate: () => {},
    };
    expect(
      projectCanonicalBrainWorkResultEvents({
        ...input,
        displayedThreadId: "thread-history",
      }),
    ).toEqual([]);
    expect(
      projectCanonicalBrainWorkResultEvents({
        ...input,
        displayedThreadId: "thread-current",
        readOnly: true,
      }),
    ).toEqual([]);
    expect(
      projectCanonicalBrainWorkResultEvents({
        ...input,
        displayedThreadId: undefined,
      }),
    ).toEqual([]);
  });

  test("closed Sessions remain read-only actions only while unread", () => {
    const activated: boolean[] = [];
    const unread = projectCanonicalBrainWorkResultEvents({
      events: [resultEvent],
      displayedThreadId: "thread-current",
      currentThreadId: "thread-current",
      readOnly: false,
      openSessionIds: new Set(),
      onActivate: (_event, canOpenSession) =>
        activated.push(canOpenSession),
    });
    unread[0]?.onPress?.();
    expect(activated).toEqual([false]);

    const read = projectCanonicalBrainWorkResultEvents({
      events: [{ ...resultEvent, unread: false }],
      displayedThreadId: "thread-current",
      currentThreadId: "thread-current",
      readOnly: false,
      openSessionIds: new Set(),
      onActivate: () => {},
    });
    expect(read[0]?.onPress).toBeUndefined();
  });

  test("merges chronologically and deduplicates stable identities", () => {
    const providerItems: ZenTimelineItem[] = [
      {
        type: "message",
        role: "assistant",
        id: "provider-earlier",
        timestamp: "2026-08-04T01:00:00Z",
        body: "Earlier",
        attachments: [],
      },
      {
        type: "message",
        role: "assistant",
        id: "provider-later",
        timestamp: "2026-08-04T03:00:00Z",
        body: "Later",
        attachments: [],
      },
    ];
    const projected = projectCanonicalBrainWorkResultEvents({
      events: [resultEvent, { ...resultEvent }],
      displayedThreadId: "thread-current",
      currentThreadId: "thread-current",
      readOnly: false,
      openSessionIds: new Set(["brain-agent-cards:@1"]),
      onActivate: () => {},
    });

    expect(
      mergeSupplementaryTimelineItems(providerItems, projected).map(
        (item) => item.id,
      ),
    ).toEqual([
      "provider-earlier",
      "event-session-done",
      "provider-later",
    ]);
  });
});
