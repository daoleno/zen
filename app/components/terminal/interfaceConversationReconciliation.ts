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
  return {
    ...replacement,
    activity: replacement.activity,
    events: replacement.events.slice().sort(compareConversationEvents),
  };
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

/** Canonical deltas append or stable-upsert; only snapshots replace. */
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
