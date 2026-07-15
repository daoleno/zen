import {
  isStructuredTurnTerminal,
  type CodexConversation,
  type StructuredTurn,
} from "../../services/codexConversation";

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
  if (!previous || !sameConversation) {
    return incoming;
  }

  const lifecycle = reconcileStructuredLifecycleProjection(previous, incoming);
  const incomingIds = new Set(incoming.events.map((event) => event.id));
  const byId = previous.events.length === 0
    ? new Map<string, CodexConversation["events"][number]>()
    : new Map(
        previous.events
          .filter((event) =>
            (event.partial !== true && event.transient !== true) ||
            incomingIds.has(event.id)
          )
          .map((event) => [event.id, event]),
      );
  incoming.events.forEach((event) => byId.set(event.id, event));
  return {
    ...incoming,
    ...lifecycle,
    events: Array.from(byId.values()).sort(compareConversationEvents),
  };
}

/**
 * Same-turn lifecycle is monotonic. In particular, reconnect snapshots that
 * momentarily omit lifecycle metadata cannot hide Working, and a stale running
 * envelope cannot reopen a turn after an authoritative terminal state.
 */
export function reconcileStructuredTurn(
  previous?: StructuredTurn,
  incoming?: StructuredTurn,
): StructuredTurn | undefined {
  if (!incoming) {
    return previous;
  }
  if (!previous) {
    return incoming;
  }
  if (previous.id !== incoming.id) {
    return incoming;
  }

  const startedAt = previous.started_at || incoming.started_at;
  const settledAt = previous.settled_at || incoming.settled_at;
  const previousPrecedence = structuredTurnStatusPrecedence(previous);
  const incomingPrecedence = structuredTurnStatusPrecedence(incoming);
  const winner = previousPrecedence >= incomingPrecedence ? previous : incoming;
  const reconciled: StructuredTurn = {
    ...winner,
    started_at: startedAt,
    settled_at: settledAt,
  };
  return structuredTurnsEqual(previous, reconciled) ? previous : reconciled;
}

/** An omitted queue field means "no queue update"; an explicit [] clears it. */
export function reconcileStructuredTurnQueue(
  previous?: StructuredTurn[],
  incoming?: StructuredTurn[],
): StructuredTurn[] | undefined {
  if (incoming === undefined) {
    return previous;
  }
  if (incoming.length === 0) {
    return incoming;
  }
  const previousById = new Map(
    (previous ?? []).map((turn) => [turn.id, turn]),
  );
  const reconciled = incoming.map((turn) =>
    reconcileStructuredTurn(previousById.get(turn.id), turn) ?? turn,
  );
  if (
    previous &&
    previous.length === reconciled.length &&
    previous.every((turn, index) => turn === reconciled[index])
  ) {
    return previous;
  }
  return reconciled;
}

function structuredTurnStatusPrecedence(turn: StructuredTurn) {
  if (isStructuredTurnTerminal(turn)) {
    return 2;
  }
  return turn.status === "running" ? 1 : 0;
}

export function structuredTurnsEqual(
  left?: StructuredTurn,
  right?: StructuredTurn,
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

export function structuredTurnQueuesEqual(
  left?: StructuredTurn[],
  right?: StructuredTurn[],
) {
  if (left === right) {
    return true;
  }
  if (!left || !right || left.length !== right.length) {
    return false;
  }
  return left.every((turn, index) => structuredTurnsEqual(turn, right[index]));
}

export function reconcileConversationSyncLifecycle(
  previous: CodexConversation | null,
  status: {
    reason?: string;
    active?: boolean;
    turn_epoch?: string;
    turn_revision?: number;
    turn?: StructuredTurn;
    queued_turns?: StructuredTurn[];
  },
): CodexConversation {
  const base: CodexConversation = previous ?? {
    available: false,
    reason: status.reason,
    events: [],
  };
  const lifecycle = reconcileStructuredLifecycleProjection(base, status);
  return {
    ...base,
    reason: status.reason ?? base.reason,
    active: status.active ?? base.active,
    ...lifecycle,
  };
}

type StructuredLifecycleProjection = Pick<
  CodexConversation,
  "turn_epoch" | "turn_revision" | "turn" | "queued_turns"
>;

/**
 * A daemon epoch is causal ownership, not decoration. Once an incoming
 * envelope supplies a different (or first) epoch, its lifecycle replaces any
 * cached pre-restart record even when the envelope has no active turn.
 */
export function reconcileStructuredLifecycleProjection(
  previous: StructuredLifecycleProjection | null,
  incoming: StructuredLifecycleProjection,
): StructuredLifecycleProjection {
  const incomingEpoch = incoming.turn_epoch;
  const previousEpoch = previous?.turn_epoch;
  const epochChanged = Boolean(
    incomingEpoch && incomingEpoch !== previousEpoch,
  );
  const previousRevision = previous?.turn_revision;
  const incomingRevision = incoming.turn_revision;
  const sameVersionedEpoch = Boolean(
    incomingEpoch && incomingEpoch === previousEpoch,
  );

  if (
    sameVersionedEpoch &&
    typeof previousRevision === "number" &&
    (typeof incomingRevision !== "number" || incomingRevision <= previousRevision)
  ) {
    // Subscription-envelope revisions restart on reconnect. The daemon
    // lifecycle revision does not, so an older/equal projection cannot clear
    // or settle a newer cached turn even when it arrives on a fresh socket.
    return {
      turn_epoch: previousEpoch,
      turn_revision: previousRevision,
      turn: previous?.turn,
      queued_turns: previous?.queued_turns,
    };
  }

  const causallyNewer = Boolean(
    epochChanged ||
      (sameVersionedEpoch &&
        typeof incomingRevision === "number" &&
        (typeof previousRevision !== "number" ||
          incomingRevision > previousRevision)),
  );
  return {
    turn_epoch: incomingEpoch ?? previousEpoch,
    turn_revision: incomingRevision ?? previousRevision,
    turn: causallyNewer
      ? incoming.turn
      : reconcileStructuredTurn(previous?.turn, incoming.turn),
    queued_turns: causallyNewer
      ? incoming.queued_turns ?? []
      : reconcileStructuredTurnQueue(
          previous?.queued_turns,
          incoming.queued_turns,
        ),
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
