import type { CodexConversation } from "../../services/codexConversation";

export interface ConversationStreamCursor {
  requestId?: string;
  conversationId?: string;
  revision: number;
}

export interface ConversationEnvelope {
  requestId?: string;
  conversationId?: string;
  revision: number;
}

export interface AcceptedConversationEnvelope {
  accepted: boolean;
  sameConversation: boolean;
  cursor: ConversationStreamCursor;
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
  const sameRequest = !envelope.requestId || envelope.requestId === current.requestId;

  if (sameRequest && envelope.revision > 0 && envelope.revision <= current.revision) {
    return {
      accepted: false,
      sameConversation: conversationIdsMatch(current.conversationId, conversationId),
      cursor: current,
    };
  }

  return {
    accepted: true,
    sameConversation: conversationIdsMatch(current.conversationId, conversationId),
    cursor: {
      requestId,
      conversationId,
      revision: envelope.revision > 0 ? envelope.revision : sameRequest ? current.revision : 0,
    },
  };
}

export function conversationIdentity(conversation: CodexConversation | null) {
  return conversation?.session_id || conversation?.path || conversation?.cwd;
}

/**
 * A fresh snapshot for the same logical conversation is a baseline, not a
 * deletion list. Preserve events missing from a shorter/rebuilding transcript;
 * a changed conversation identity remains responsible for logical replacement.
 */
export function reconcileConversationSnapshot(
  previous: CodexConversation | null,
  incoming: CodexConversation,
  sameConversation: boolean,
): CodexConversation {
  if (!previous || !sameConversation || previous.events.length === 0) {
    return incoming;
  }

  const incomingIds = new Set(incoming.events.map((event) => event.id));
  const byId = new Map(
    previous.events
      .filter((event) =>
        (event.partial !== true && event.transient !== true) || incomingIds.has(event.id)
      )
      .map((event) => [event.id, event]),
  );
  incoming.events.forEach((event) => byId.set(event.id, event));
  return {
    ...incoming,
    events: Array.from(byId.values()).sort(compareConversationEvents),
  };
}

/**
 * Canonical same-conversation history stays monotonic. Explicit deletes may
 * only clear provider projections that declared themselves partial/transient.
 */
export function reconcileConversationDeltaEvents(
  previous: CodexConversation["events"],
  upserts: CodexConversation["events"],
  deletes: string[] = [],
) {
  const byId = new Map(previous.map((event) => [event.id, event]));
  deletes.forEach((id) => {
    const event = byId.get(id);
    if (event?.partial === true || event?.transient === true) {
      byId.delete(id);
    }
  });
  upserts.forEach((event) => byId.set(event.id, event));
  return Array.from(byId.values()).sort(compareConversationEvents);
}

function conversationIdsMatch(left?: string, right?: string) {
  if (left && right) {
    return left === right;
  }
  return !left || !right;
}

export function compareConversationEvents(
  left: CodexConversation["events"][number],
  right: CodexConversation["events"][number],
) {
  const leftTime = Date.parse(left.timestamp || "");
  const rightTime = Date.parse(right.timestamp || "");
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
