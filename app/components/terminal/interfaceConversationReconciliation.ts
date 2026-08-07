import {
  normalizeCodexConversation,
  type CodexConversation,
  type ProviderActivity,
} from "../../services/codexConversation";

export interface ConversationStreamCursor {
  requestId?: string;
  conversationId?: string;
  revision: number;
  generation?: number;
}

export interface ConversationEnvelope {
  requestId?: string;
  conversationId?: string;
  revision: number;
  baseRevision?: number;
  generation?: number;
  kind?: "snapshot" | "delta" | "sync";
}

export interface AcceptedConversationEnvelope {
  accepted: boolean;
  sameConversation: boolean;
  cursor: ConversationStreamCursor;
  gap?: boolean;
  obsolete?: boolean;
}

export const EMPTY_CONVERSATION_STREAM_CURSOR: ConversationStreamCursor = {
  revision: 0,
};

export function acceptConversationEnvelope(
  current: ConversationStreamCursor,
  envelope: ConversationEnvelope,
  fallbackConversationId?: string,
): AcceptedConversationEnvelope {
  const requestId = envelope.requestId || current.requestId;
  const conversationId =
    envelope.conversationId || fallbackConversationId || current.conversationId;
  const sameRequest =
    !envelope.requestId || envelope.requestId === current.requestId;

  const currentGeneration = current.generation;
  const envelopeGeneration = envelope.generation;
  if (
    typeof currentGeneration === "number" &&
    typeof envelopeGeneration === "number" &&
    envelopeGeneration < currentGeneration
  ) {
    return {
      accepted: false,
      obsolete: true,
      sameConversation: conversationIdsMatch(
        current.conversationId,
        conversationId,
      ),
      cursor: current,
    };
  }
  const newerGeneration =
    typeof envelopeGeneration === "number" &&
    (typeof currentGeneration !== "number" ||
      envelopeGeneration > currentGeneration);
  if (
    !newerGeneration &&
    typeof currentGeneration === "number" &&
    typeof envelopeGeneration === "number" &&
    envelopeGeneration === currentGeneration &&
    Boolean(current.requestId) &&
    Boolean(envelope.requestId) &&
    !sameRequest
  ) {
    return {
      accepted: false,
      obsolete: true,
      sameConversation: conversationIdsMatch(
        current.conversationId,
        conversationId,
      ),
      cursor: current,
    };
  }

  if (
    envelope.kind === "delta" &&
    !newerGeneration &&
    envelope.baseRevision !== current.revision
  ) {
    return {
      accepted: false,
      gap: true,
      sameConversation: conversationIdsMatch(
        current.conversationId,
        conversationId,
      ),
      cursor: current,
    };
  }

  if (
    !newerGeneration &&
    sameRequest &&
    envelope.revision > 0 &&
    envelope.revision <= current.revision
  ) {
    return {
      accepted: false,
      sameConversation: conversationIdsMatch(
        current.conversationId,
        conversationId,
      ),
      cursor: current,
    };
  }

  return {
    accepted: true,
    sameConversation: conversationIdsMatch(
      current.conversationId,
      conversationId,
    ),
    cursor: {
      requestId,
      conversationId,
      revision:
        envelope.revision > 0
          ? envelope.revision
          : sameRequest
            ? current.revision
            : 0,
      generation: envelopeGeneration ?? currentGeneration,
    },
  };
}

export function conversationIdentity(conversation: CodexConversation | null) {
  return conversation?.session_id || conversation?.path || conversation?.cwd;
}

/** A revisioned snapshot is an exact replacement, including explicit empty. */
export function reconcileConversationSnapshot(
  _previous: CodexConversation | null,
  incoming: CodexConversation | null,
  _sameConversation: boolean,
): CodexConversation {
  const replacement = normalizeCodexConversation(incoming);
  const events = replacement.events;
  return {
    ...replacement,
    activity: replacement.activity,
    events: eventsSorted(events) ? events : events.slice().sort(compareConversationEvents),
  };
}

/**
 * O(n) monotonic-order verification with memoized timestamp parses. The
 * daemon streams sorted events, so a full sort is replaced by one cheap scan.
 */
export function eventsSorted(events: CodexConversation["events"]) {
  for (let index = 1; index < events.length; index += 1) {
    if (compareConversationEvents(events[index - 1]!, events[index]!) > 0) {
      return false;
    }
  }
  return true;
}

export function providerActivitiesEqual(
  left?: ProviderActivity,
  right?: ProviderActivity,
) {
  return (
    left === right ||
    Boolean(
      left &&
      right &&
      left.id === right.id &&
      left.status === right.status &&
      left.started_at === right.started_at &&
      left.settled_at === right.settled_at,
    )
  );
}

/**
 * Canonical deltas append or stable-upsert; only snapshots replace.
 *
 * The base array is already sorted (reconciliation invariant). Streaming
 * upserts either replace an existing event in place (same order keys) or
 * append new events at the end (daemon time order). Both are verified with an
 * O(n) monotonic scan using memoized timestamp parses; only when verification
 * fails does the merge fall back to a full sort.
 */
export function reconcileConversationDeltaEvents(
  previous: CodexConversation["events"],
  upserts: CodexConversation["events"],
  deletes: string[] = [],
) {
  const byId = new Map(previous.map((event) => [event.id, event]));
  // Canonical deltas append or stable-upsert only. A replacement snapshot is
  // the sole operation allowed to clear visible history.
  void deletes;
  upserts.forEach((event) => byId.set(event.id, event));
  if (upserts.length === 0) {
    return previous;
  }
  if (byId.size === previous.length) {
    // Stable-upsert only: every upsert id already existed, so the merged
    // array keeps its previous length and order unless keys moved.
    if (eventsSorted(previous)) {
      const merged = previous.map((event) => byId.get(event.id) ?? event);
      return eventsSorted(merged) ? merged : merged.sort(compareConversationEvents);
    }
  }
  const merged = Array.from(byId.values());
  return eventsSorted(merged) ? merged : merged.sort(compareConversationEvents);
}

function conversationIdsMatch(left?: string, right?: string) {
  if (left && right) {
    return left === right;
  }
  return !left || !right;
}

/**
 * Bounded memoization of RFC3339 timestamp parses. Timestamps repeat heavily
 * across polls (only streaming events change), so re-parsing every event on
 * every sort/scan is wasted work. Exact Date.parse semantics: the cached
 * number is the very value Date.parse returned.
 */
const parsedTimestampCache = new Map<string, number>();
const PARSED_TIMESTAMP_CACHE_MAX = 20_000;

function parseEventTimestamp(value: string | undefined): number {
  if (!value) {
    return Number.NaN;
  }
  const cached = parsedTimestampCache.get(value);
  if (cached !== undefined) {
    return cached;
  }
  const parsed = Date.parse(value);
  if (parsedTimestampCache.size >= PARSED_TIMESTAMP_CACHE_MAX) {
    parsedTimestampCache.clear();
  }
  parsedTimestampCache.set(value, parsed);
  return parsed;
}

export function compareConversationEvents(
  left: CodexConversation["events"][number],
  right: CodexConversation["events"][number],
) {
  const leftTime = parseEventTimestamp(left.timestamp);
  const rightTime = parseEventTimestamp(right.timestamp);
  const leftHasTime = Number.isFinite(leftTime);
  const rightHasTime = Number.isFinite(rightTime);
  if (leftHasTime !== rightHasTime) {
    return leftHasTime ? 1 : -1;
  }
  if (leftHasTime && leftTime !== rightTime) {
    return leftTime - rightTime;
  }
  if (left.seq !== right.seq) {
    return left.seq - right.seq;
  }
  return left.id.localeCompare(right.id);
}
