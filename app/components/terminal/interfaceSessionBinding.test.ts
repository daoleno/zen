import { describe, expect, mock, test } from "bun:test";
import { makeSessionKey, parseSessionKey } from "../../services/sessionKeys";
import { interfaceChatSessionCacheKey } from "./interfaceChatSessionIdentity";
import type { CodexConversation } from "../../services/codexConversation";

mock.module("react-native", () => ({ Platform: { OS: "web" } }));
mock.module("../../services/auth", () => ({
  buildAuthorizationHeader: async () => "test-authorization",
}));
mock.module("../../services/connectionIssue", () => ({
  diagnoseConnectionIssue: async () => null,
}));

const { interfaceChatThreadReducer } = await import("./InterfaceChatSession");

type ThreadState = Parameters<typeof interfaceChatThreadReducer>[0];

const SERVER_ID = "server-1";
/** Existing OpenCode session the user was viewing before /new. */
const PREVIOUS_AGENT_ID = "zen-opencode-prev";
/** The freshly created OpenCode session Terminal correctly shows. */
const NEW_AGENT_ID = "zen-opencode-new";

function initialThreadState(cacheKey: string): ThreadState {
  return {
    cacheKey,
    conversation: null,
    loading: true,
    error: null,
    pendingUserMessages: [],
    turnFocusAnchorAliases: new Map(),
    streamCursor: { revision: 0 },
    awaitingSnapshot: false,
    resyncToken: 0,
  };
}

function userEvent(id: string, body: string, seq: number) {
  return {
    id,
    seq,
    timestamp: "2026-08-21T09:00:00.000Z",
    kind: "user_message" as const,
    role: "user" as const,
    body,
  };
}

function populatedState(agentId: string, sessionId: string): ThreadState {
  const cacheKey = interfaceChatSessionCacheKey(SERVER_ID, agentId);
  const conversation: CodexConversation = {
    available: true,
    source: "opencode_db",
    session_id: sessionId,
    events: [userEvent(`${sessionId}:m1`, `history of ${sessionId}`, 1)],
  };
  const withConversation = interfaceChatThreadReducer(
    initialThreadState(cacheKey),
    {
      type: "snapshot",
      generation: 1,
      payload: {
        request_id: "sub-1",
        conversation_id: sessionId,
        revision: 1,
        conversation,
      },
    },
  );
  return {
    ...withConversation,
    pendingUserMessages: [
      {
        id: "pending-1",
        body: "queued",
        sentText: "queued",
        attachments: [],
        createdAt: "2026-08-21T09:00:01.000Z",
        lifecycle: "pending",
        dispatchRequestId: "request-1",
        dispatchAttemptOrder: 1,
        createdAfterMaxSeq: 1,
        createdAfterEventIds: [`${sessionId}:m1`],
      },
    ],
  };
}

function snapshotFor(
  sessionId: string,
  revision: number,
  events: any[],
  generation = 1,
) {
  return {
    type: "snapshot" as const,
    generation,
    payload: {
      request_id: `sub-${sessionId}`,
      conversation_id: sessionId,
      revision,
      conversation: {
        available: true,
        source: "opencode_db",
        session_id: sessionId,
        events,
      },
    },
  };
}

describe("Interface/Terminal session binding contract", () => {
  test("a newly created session never reuses the previous session's identity namespace", () => {
    const previousKey = makeSessionKey(SERVER_ID, PREVIOUS_AGENT_ID);
    const newKey = makeSessionKey(SERVER_ID, NEW_AGENT_ID);
    expect(newKey).not.toBe(previousKey);
    expect(interfaceChatSessionCacheKey(SERVER_ID, NEW_AGENT_ID)).not.toBe(
      interfaceChatSessionCacheKey(SERVER_ID, PREVIOUS_AGENT_ID),
    );
    // Both surfaces consume the same canonical route pair.
    expect(parseSessionKey(newKey)).toEqual({
      serverId: SERVER_ID,
      agentId: NEW_AGENT_ID,
    });
    expect(parseSessionKey(previousKey)).toEqual({
      serverId: SERVER_ID,
      agentId: PREVIOUS_AGENT_ID,
    });
  });

  test("new OpenCode session first load starts empty instead of inheriting previous history", () => {
    const previous = populatedState(PREVIOUS_AGENT_ID, "ses-prev");
    expect(previous.conversation?.events.length).toBe(1);

    // Navigation to /terminal/<new-id> remounts the surface under the new
    // session key; the hook resets thread state through cache_key_changed.
    const newCacheKey = interfaceChatSessionCacheKey(SERVER_ID, NEW_AGENT_ID);
    const next = interfaceChatThreadReducer(previous, {
      type: "cache_key_changed",
      cacheKey: newCacheKey,
    });
    expect(next.cacheKey).toBe(newCacheKey);
    expect(next.conversation).toBeNull();
    expect(next.loading).toBe(true);
    expect(next.pendingUserMessages).toHaveLength(0);

    // The daemon reports the provider row lazily (session_not_found) until
    // OpenCode writes it; that must render as an empty new conversation.
    const unavailable = interfaceChatThreadReducer(next, {
      type: "snapshot",
      generation: 1,
      payload: {
        request_id: "sub-new",
        conversation_id: "",
        revision: 1,
        conversation: {
          available: false,
          reason: "session_not_found",
          events: [],
        },
      },
    });
    expect(unavailable.loading).toBe(false);
    expect(unavailable.conversation?.events).toHaveLength(0);
    expect(unavailable.error).toBeNull();
  });

  test("the new session's own history arrives under its own identity, never merged with the previous session", () => {
    const previous = populatedState(PREVIOUS_AGENT_ID, "ses-prev");
    const newCacheKey = interfaceChatSessionCacheKey(SERVER_ID, NEW_AGENT_ID);
    let state = interfaceChatThreadReducer(previous, {
      type: "cache_key_changed",
      cacheKey: newCacheKey,
    });

    state = interfaceChatThreadReducer(
      state,
      snapshotFor("ses-new", 1, [
        userEvent("ses-new:m1", "first message of the new session", 1),
      ]),
    );
    expect(state.conversation?.session_id).toBe("ses-new");
    const bodies = state.conversation?.events.map((event) => event.body) ?? [];
    expect(bodies).toContain("first message of the new session");
    expect(bodies).not.toContain("history of ses-prev");
  });

  test("switching between existing sessions keeps each timeline in its own namespace", () => {
    const keyA = interfaceChatSessionCacheKey(SERVER_ID, "agent-a");
    const keyB = interfaceChatSessionCacheKey(SERVER_ID, "agent-b");
    let stateA = interfaceChatThreadReducer(
      initialThreadState(keyA),
      snapshotFor("ses-a", 1, [userEvent("m-a1", "alpha one", 1)]),
    );
    let stateB = interfaceChatThreadReducer(
      initialThreadState(keyB),
      snapshotFor("ses-b", 1, [userEvent("m-b1", "bravo one", 1)]),
    );

    // A -> B: reset, then B's snapshot applies without A's rows.
    stateB = interfaceChatThreadReducer(stateB, {
      type: "cache_key_changed",
      cacheKey: keyB,
    });
    stateB = interfaceChatThreadReducer(
      stateB,
      snapshotFor("ses-b", 2, [
        userEvent("m-b1", "bravo one", 1),
        userEvent("m-b2", "bravo two", 2),
      ]),
    );
    const bodiesB = stateB.conversation?.events.map((event) => event.body);
    expect(bodiesB).toEqual(["bravo one", "bravo two"]);

    // B -> A back-navigation: same independence in the other direction.
    stateA = interfaceChatThreadReducer(stateA, {
      type: "cache_key_changed",
      cacheKey: keyA,
    });
    stateA = interfaceChatThreadReducer(
      stateA,
      snapshotFor("ses-a", 2, [
        userEvent("m-a1", "alpha one", 1),
        userEvent("m-a2", "alpha two", 2),
      ]),
    );
    const bodiesA = stateA.conversation?.events.map((event) => event.body);
    expect(bodiesA).toEqual(["alpha one", "alpha two"]);
    expect(stateA.conversation?.session_id).toBe("ses-a");
  });

  test("refresh/deep-link recovery rebuilds the timeline from a fresh subscription", () => {
    const cacheKey = interfaceChatSessionCacheKey(SERVER_ID, NEW_AGENT_ID);
    // App relaunch or deep link: parse the persisted/route key back to the
    // canonical pair and start from a cold-loading state.
    const recovered = parseSessionKey(makeSessionKey(SERVER_ID, NEW_AGENT_ID));
    expect(recovered).toEqual({ serverId: SERVER_ID, agentId: NEW_AGENT_ID });

    let state = initialThreadState(cacheKey);
    state = interfaceChatThreadReducer(state, {
      type: "stream_start",
      generation: 7,
    });
    expect(state.awaitingSnapshot).toBe(true);

    // A stale-generation snapshot from a previous app run must not apply.
    const stale = interfaceChatThreadReducer(state, {
      ...snapshotFor("ses-new", 9, [userEvent("stale", "stale row", 1)]),
      generation: 3,
    });
    expect(stale.conversation).toBeNull();

    state = interfaceChatThreadReducer(
      state,
      snapshotFor(
        "ses-new",
        4,
        [userEvent("ses-new:m1", "recovered after refresh", 1)],
        7,
      ),
    );
    expect(state.awaitingSnapshot).toBe(false);
    expect(state.conversation?.session_id).toBe("ses-new");
    expect(state.conversation?.events.map((event) => event.body)).toEqual([
      "recovered after refresh",
    ]);
  });
});
