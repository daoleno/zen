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

  const byId = new Map(previous.events.map((event) => [event.id, event]));
  incoming.events.forEach((event) => byId.set(event.id, event));
  return {
    ...incoming,
    events: Array.from(byId.values()).sort(compareConversationEvents),
  };
}

/**
 * Same-conversation transcript refreshes are monotonic; transient deletes do
 * not remove history that may currently be the user's visual anchor.
 */
export function reconcileConversationDeltaEvents(
  previous: CodexConversation["events"],
  upserts: CodexConversation["events"],
) {
  const byId = new Map(previous.map((event) => [event.id, event]));
  upserts.forEach((event) => byId.set(event.id, event));
  return Array.from(byId.values()).sort(compareConversationEvents);
}

function conversationIdsMatch(left?: string, right?: string) {
  if (left && right) {
    return left === right;
  }
  return !left || !right;
}

function compareConversationEvents(
  left: CodexConversation["events"][number],
  right: CodexConversation["events"][number],
) {
  if (left.seq !== right.seq) {
    return left.seq - right.seq;
  }
  return left.id.localeCompare(right.id);
}
