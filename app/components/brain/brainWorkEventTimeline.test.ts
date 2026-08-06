import { describe, expect, mock, test } from "bun:test";
import type { BrainWorkResultEvent } from "../../store/brain";
import { interfaceChatSessionCacheKey } from "../terminal/interfaceChatSessionIdentity";
import {
  mergeSupplementaryTimelineItems,
  supplementaryTimelineItemsForConversation,
} from "../terminal/InterfaceTimelineModel";
import type { ZenTimelineItem } from "../terminal/InterfaceTimelineItemView";
import { projectCanonicalBrainWorkResultEvents } from "./brainWorkEventTimeline";

mock.module("react-native", () => ({ Platform: { OS: "web" } }));
mock.module("../../services/auth", () => ({
  buildAuthorizationHeader: async () => "test-authorization",
}));
mock.module("../../services/connectionIssue", () => ({
  diagnoseConnectionIssue: async () => null,
}));

const { interfaceChatThreadReducer } = await import("../terminal/InterfaceChatSession");

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

  test("waits for canonical history, then keeps cards merged after hydration", () => {
    const scopeKey = "brain-thread:thread-current";
    const projected = projectCanonicalBrainWorkResultEvents({
      events: [resultEvent],
      displayedThreadId: "thread-current",
      currentThreadId: "thread-current",
      readOnly: false,
      openSessionIds: new Set(),
      onActivate: () => {},
    });

    const provisional = supplementaryTimelineItemsForConversation({
      items: projected,
      conversationScopeKey: scopeKey,
      conversation: null,
      loading: true,
    });
    expect(provisional).toEqual([]);

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
    const hydrated = supplementaryTimelineItemsForConversation({
      items: projected,
      conversationScopeKey: scopeKey,
      conversation: {
        available: true,
        source: "brain_chat",
        session_id: scopeKey,
        events: [],
      },
      loading: false,
    });

    expect(
      mergeSupplementaryTimelineItems(providerItems, hydrated).map(
        (item) => item.id,
      ),
    ).toEqual([
      "provider-earlier",
      "event-session-done",
      "provider-later",
    ]);
  });

  test("keeps empty or reconnecting canonical timelines ready only for their exact scope", () => {
    const scopeKey = "brain-thread:thread-current";
    const projected = projectCanonicalBrainWorkResultEvents({
      events: [resultEvent],
      displayedThreadId: "thread-current",
      currentThreadId: "thread-current",
      readOnly: false,
      openSessionIds: new Set(),
      onActivate: () => {},
    });
    const canonicalEmptyConversation = {
      available: true,
      source: "brain_chat",
      session_id: scopeKey,
      events: [],
    };

    expect(
      supplementaryTimelineItemsForConversation({
        items: projected,
        conversationScopeKey: scopeKey,
        conversation: canonicalEmptyConversation,
        loading: false,
      }),
    ).toBe(projected);
    expect(
      supplementaryTimelineItemsForConversation({
        items: projected,
        conversationScopeKey: scopeKey,
        conversation: canonicalEmptyConversation,
        loading: true,
      }),
    ).toBe(projected);
    expect(
      supplementaryTimelineItemsForConversation({
        items: projected,
        conversationScopeKey: "brain-thread:thread-history",
        conversation: canonicalEmptyConversation,
        loading: false,
      }),
    ).toEqual([]);
    expect(
      supplementaryTimelineItemsForConversation({
        items: projected,
        conversationScopeKey: undefined,
        conversation: null,
        loading: true,
      }),
    ).toBe(projected);
  });

  test("InterfaceChatSession brain-thread scope retains messages across host rotation; empty host snapshot explains cards-only", () => {
    const scopeKey = "brain-thread:thread-current";
    const scopeCacheKey = interfaceChatSessionCacheKey(
      "server-1",
      "brain-agent-host-old:@1",
      scopeKey,
    );
    // Host rotation changes agentId but Brain uses the thread scope cache key.
    expect(
      interfaceChatSessionCacheKey("server-1", "brain-agent-host-new:@2", scopeKey),
    ).toBe(scopeCacheKey);

    let thread = interfaceChatThreadReducer(
      {
        cacheKey: scopeCacheKey,
        conversation: null,
        loading: true,
        error: null,
        pendingUserMessages: [],
        turnFocusAnchorAliases: new Map(),
        streamCursor: { revision: 0 },
        awaitingSnapshot: false,
        resyncToken: 0,
      },
      { type: "stream_start", generation: 1 },
    );
    thread = interfaceChatThreadReducer(thread, {
      type: "snapshot",
      generation: 1,
      payload: {
        request_id: "sub-old-host",
        conversation_id: scopeKey,
        revision: 1,
        conversation: {
          available: true,
          source: "brain_chat",
          session_id: scopeKey,
          events: [
            {
              id: "msg-user-1",
              seq: 1,
              kind: "user_message",
              role: "user",
              body: "Ship the Brain card correction.",
              timestamp: "2026-08-06T01:00:00Z",
            },
            {
              id: "msg-assistant-1",
              seq: 2,
              kind: "assistant_message",
              role: "assistant",
              body: "Working on the presentable card projection.",
              timestamp: "2026-08-06T01:01:00Z",
            },
          ],
        },
      },
    });
    expect(thread.conversation?.events.map((event) => event.id)).toEqual([
      "msg-user-1",
      "msg-assistant-1",
    ]);

    // Same scope cache key: host rotation resubscribes without clearing history.
    thread = interfaceChatThreadReducer(thread, {
      type: "stream_start",
      generation: 2,
    });
    expect(thread.conversation?.events.map((event) => event.id)).toEqual([
      "msg-user-1",
      "msg-assistant-1",
    ]);

    // Pagination / reconnect snapshot with the same brain-thread identity.
    thread = interfaceChatThreadReducer(thread, {
      type: "snapshot",
      generation: 2,
      payload: {
        request_id: "sub-reconnect",
        conversation_id: scopeKey,
        revision: 2,
        conversation: {
          available: true,
          source: "brain_chat",
          session_id: scopeKey,
          events: [
            {
              id: "msg-assistant-2",
              seq: 0,
              kind: "assistant_message",
              role: "assistant",
              body: "Paginated older turn restored.",
              timestamp: "2026-08-06T00:59:00Z",
            },
            {
              id: "msg-user-1",
              seq: 1,
              kind: "user_message",
              role: "user",
              body: "Ship the Brain card correction.",
              timestamp: "2026-08-06T01:00:00Z",
            },
            {
              id: "msg-assistant-1",
              seq: 2,
              kind: "assistant_message",
              role: "assistant",
              body: "Working on the presentable card projection.",
              timestamp: "2026-08-06T01:01:00Z",
            },
          ],
        },
      },
    });
    expect(thread.conversation?.events.map((event) => event.id)).toEqual([
      "msg-assistant-2",
      "msg-user-1",
      "msg-assistant-1",
    ]);

    const unreadCard: BrainWorkResultEvent = {
      ...resultEvent,
      event_id: "current-needs-input",
      kind: "session.needs_input",
      work_title: "zen-manual-input-and-brand-icons",
      summary: "go vet ./... ; echo VET_EXIT:$?",
      occurred_at: "2026-08-06T02:19:33Z",
      unread: true,
    };
    const cards = projectCanonicalBrainWorkResultEvents({
      events: [unreadCard],
      displayedThreadId: "thread-current",
      currentThreadId: "thread-current",
      readOnly: false,
      openSessionIds: new Set(),
      onActivate: () => {},
    });
    const providerItems: ZenTimelineItem[] = (thread.conversation?.events ?? []).map(
      (event) => ({
        type: "message",
        role: event.role === "user" ? "user" : "assistant",
        id: event.id,
        timestamp: event.timestamp ?? "2026-08-06T01:00:00Z",
        body: event.body ?? "",
        attachments: [],
      }),
    );
    const merged = mergeSupplementaryTimelineItems(
      providerItems,
      supplementaryTimelineItemsForConversation({
        items: cards,
        conversationScopeKey: scopeKey,
        conversation: thread.conversation,
        loading: false,
      }),
    );
    expect(merged.map((item) => item.id)).toEqual([
      "msg-assistant-2",
      "msg-user-1",
      "msg-assistant-1",
      "current-needs-input",
    ]);

    // A later host with an empty provider transcript publishes an exact empty
    // snapshot for the same brain-thread identity. Cards remain; messages are
    // gone because the snapshot replaced them — not because cards deleted them.
    thread = interfaceChatThreadReducer(thread, {
      type: "stream_start",
      generation: 3,
    });
    thread = interfaceChatThreadReducer(thread, {
      type: "snapshot",
      generation: 3,
      payload: {
        request_id: "sub-new-empty-host",
        conversation_id: scopeKey,
        revision: 3,
        conversation: {
          available: true,
          source: "brain_chat",
          session_id: scopeKey,
          events: [],
        },
      },
    });
    expect(thread.conversation?.events).toEqual([]);
    const cardsOnly = mergeSupplementaryTimelineItems(
      [],
      supplementaryTimelineItemsForConversation({
        items: cards,
        conversationScopeKey: scopeKey,
        conversation: thread.conversation,
        loading: false,
      }),
    );
    expect(cardsOnly.map((item) => item.id)).toEqual(["current-needs-input"]);
  });
});
