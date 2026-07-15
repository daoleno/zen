import {
  useCallback,
  useEffect,
  useMemo,
  useReducer,
  useState,
  type SetStateAction,
} from "react";
import type { ConnectionState } from "../../store/agents";
import type { CodexConversation } from "../../services/codexConversation";
import type { UploadedAttachment } from "../../services/uploads";
import {
  wsClient,
  type CodexConversationDeltaPayload,
  type CodexConversationSnapshotPayload,
  type CodexConversationSyncStatusPayload,
} from "../../services/websocket";
import type { AgentStatus } from "../../constants/tokens";
import {
  acknowledgePendingUserMessageWithStructuredTurns,
  canReconcilePendingAcknowledgementAgainstProjection,
  comparableUserMessageText,
  nextPendingUserMessagePruneAt,
  rejectPendingUserMessage as rejectPendingUserMessageState,
  redispatchPendingUserMessageInSubmissionOrder,
  reconcilePendingUserMessagesAgainstEvents,
  reconcilePendingUserMessagesWithStructuredTurns,
  retainPendingUserMessages,
  shouldPrunePendingUserMessageByLifecycle,
  type PendingUserMessageLifecycle,
} from "./pendingUserMessageLifecycle";
import {
  EMPTY_CONVERSATION_STREAM_CURSOR,
  acceptConversationEnvelope,
  conversationIdentity,
  reconcileConversationDeltaEvents,
  reconcileConversationSnapshot,
  reconcileConversationSyncLifecycle,
  reconcileStructuredLifecycleProjection,
  reconcileStructuredTurn,
  reconcileStructuredTurnQueue,
  structuredTurnQueuesEqual,
  structuredTurnsEqual,
  type ConversationStreamCursor,
} from "./codexConversationReconciliation";
import { shouldDropStructuredChatEvent } from "./codexConversationVisibility";
import { codexChatSessionCacheKey } from "./structuredTurnLifecycle";

const PENDING_SLASH_COMMAND_MAX_AGE_MS = 120_000;
const PENDING_SLASH_COMMAND_SETTLED_MAX_AGE_MS = 45_000;

type KeyedState<T> = {
  cacheKey: string;
  value: T;
};

export type CodexChatLocalState = "idle" | "starting-new-chat" | "new-chat-ready";

type NewChatBoundary = {
  previousEventIds: Set<string>;
  previousMaxSeq: number;
  startedAtMs: number;
};

export type ComposerAttachment = UploadedAttachment & {
  id: string;
};

export type { PendingUserMessageLifecycle };

export type PendingUserMessage = {
  id: string;
  turnId: string;
  turnStartedAt: string;
  body: string;
  sentText: string;
  attachments: Array<
    Pick<ComposerAttachment, "name" | "path" | "localUri" | "mimeType">
  >;
  createdAt: string;
  lifecycle: PendingUserMessageLifecycle;
  queuedHint?: boolean;
  acceptedAt?: string;
  dispatchRequestId?: string;
  lastAttemptAt?: string;
  failureCode?: string;
  failureMessage?: string;
  failedAt?: string;
  confirmedAt?: string;
  confirmedEventId?: string;
  authoritativeQueueObserved?: boolean;
  authoritativeActiveObserved?: boolean;
  authoritativeLifecycleEpoch?: string;
  authoritativeLifecycleRevision?: number;
  createdAfterMaxSeq?: number;
  createdAfterEventIds?: string[];
};

export type PendingUserMessageInput = Omit<PendingUserMessage, "id" | "createdAt">;

export type PendingUserMessageAcknowledgement = {
  requestId?: string;
  turnId: string;
  lifecycle: PendingUserMessageLifecycle;
  acceptedAt: string;
  turnEpoch?: string;
  turnRevision?: number;
};

export type PendingUserMessageDispatchAttempt = {
  requestId: string;
  attemptedAt: string;
  queuedHint?: boolean;
};

export type PendingUserMessageRejection = {
  requestId: string;
  code: string;
  message: string;
  failedAt: string;
};

export type PendingSlashCommand = {
  id: string;
  text: string;
  name: string;
  title?: string;
  description?: string;
  completedTitle?: string;
  createdAt: string;
  completedAt?: string;
  status: "running" | "done" | "failed";
};

export type PendingSlashCommandInput = Omit<
  PendingSlashCommand,
  "id" | "createdAt" | "completedAt" | "status"
>;

export type CodexChatAgentInfo = {
  status?: AgentStatus;
  summary?: string;
  phase?: string;
  attention?: string;
  taskClass?: string;
  eventKind?: string;
  detailsJson?: string;
  needsAttention?: boolean;
  lastOutputLines?: string[];
  cwd?: string;
  command?: string;
  name?: string;
  startedAt?: number;
  processId?: number;
};

interface UseCodexChatSessionInput {
  serverId: string;
  agentId: string;
  conversationScopeKey?: string;
  agentInfo?: CodexChatAgentInfo;
  connectionState: ConnectionState;
  screenFocused: boolean;
}

const conversationCache = new Map<string, CodexConversation>();
const draftCache = new Map<string, string>();
const attachmentCache = new Map<string, ComposerAttachment[]>();
const localChatStateCache = new Map<string, CodexChatLocalState>();
const newChatBoundaryCache = new Map<string, NewChatBoundary>();
const pendingUserMessageCache = new Map<string, PendingUserMessage[]>();

type CodexChatThreadState = {
  cacheKey: string;
  conversation: CodexConversation | null;
  localChatState: CodexChatLocalState;
  loading: boolean;
  error: string | null;
  pendingUserMessages: PendingUserMessage[];
  pendingSlashCommands: PendingSlashCommand[];
  streamCursor: ConversationStreamCursor;
};

type CodexChatThreadAction =
  | { type: "cache_key_changed"; cacheKey: string }
  | { type: "stream_start" }
  | { type: "snapshot"; payload: CodexConversationSnapshotPayload }
  | { type: "delta"; delta: CodexConversationDeltaPayload }
  | { type: "sync_status"; status: CodexConversationSyncStatusPayload }
  | { type: "stream_error"; error: string }
  | { type: "mark_new_chat_message_started" }
  | { type: "add_pending_user_message"; message: PendingUserMessage }
  | {
      type: "acknowledge_pending_user_message";
      id: string;
      requestId?: string;
      turnId: string;
      lifecycle: PendingUserMessageLifecycle;
      acceptedAt: string;
      turnEpoch?: string;
      turnRevision?: number;
    }
  | {
      type: "mark_pending_user_message_dispatched";
      id: string;
      requestId: string;
      attemptedAt: string;
      queuedHint?: boolean;
    }
  | {
      type: "reject_pending_user_message";
      id: string;
      requestId: string;
      code: string;
      message: string;
      failedAt: string;
    }
  | { type: "remove_pending_user_message"; id: string }
  | { type: "prune_pending_user_messages"; now: number }
  | { type: "add_pending_slash_command"; command: PendingSlashCommand }
  | { type: "settle_pending_slash_command"; id: string; status: PendingSlashCommand["status"]; completedAt: string }
  | { type: "remove_pending_slash_command"; id: string }
  | { type: "prune_pending_slash_commands"; now: number };

function initialCodexChatThreadState(cacheKey: string): CodexChatThreadState {
  const cachedConversation = conversationCache.get(cacheKey) ?? null;
  return {
    cacheKey,
    conversation: cachedConversation,
    localChatState: localChatStateCache.get(cacheKey) ?? "idle",
    loading: !conversationCache.has(cacheKey),
    error: null,
    pendingUserMessages: cachedPendingUserMessages(cacheKey),
    pendingSlashCommands: [],
    streamCursor: cachedConversation
      ? {
          ...EMPTY_CONVERSATION_STREAM_CURSOR,
          conversationId: conversationIdentity(cachedConversation),
        }
      : EMPTY_CONVERSATION_STREAM_CURSOR,
  };
}

function codexChatThreadReducer(
  state: CodexChatThreadState,
  action: CodexChatThreadAction,
): CodexChatThreadState {
  switch (action.type) {
    case "cache_key_changed":
      if (state.cacheKey === action.cacheKey) {
        return state;
      }
      return initialCodexChatThreadState(action.cacheKey);
    case "stream_start":
      if (state.error === null && state.loading === !state.conversation?.events.length) {
        return state;
      }
      return {
        ...state,
        loading: !state.conversation?.events.length,
        error: null,
      };
    case "snapshot":
      return applyCodexConversationSnapshot(state, action.payload);
    case "delta":
      return applyCodexConversationDelta(state, action.delta);
    case "sync_status":
      return applyCodexConversationSyncStatus(state, action.status);
    case "stream_error":
      return {
        ...state,
        loading: false,
        error: action.error,
      };
    case "mark_new_chat_message_started":
      if (state.localChatState === "idle") {
        return state;
      }
      localChatStateCache.delete(state.cacheKey);
      return {
        ...state,
        loading: false,
        localChatState: "idle",
      };
    case "add_pending_user_message": {
      const pendingUserMessages = cachePendingUserMessages(state.cacheKey, [
        ...state.pendingUserMessages,
        action.message,
      ]);
      return {
        ...state,
        pendingUserMessages,
      };
    }
    case "acknowledge_pending_user_message": {
      let acknowledged = false;
      const acknowledgedMessages = state.pendingUserMessages.map((message) => {
        if (message.id !== action.id) {
          return message;
        }
        acknowledged = true;
        return acknowledgePendingUserMessageWithStructuredTurns(
          message,
          action,
          state.conversation?.turn,
          state.conversation?.queued_turns,
        );
      });
      const pendingUserMessages = cachePendingUserMessages(
        state.cacheKey,
        canReconcilePendingAcknowledgementAgainstProjection(action, state.conversation)
          ? reconcilePendingUserMessagesWithStructuredTurns(
              acknowledgedMessages,
              state.conversation?.turn,
              state.conversation?.queued_turns,
              state.conversation?.turn_epoch,
              state.conversation?.turn_revision,
            )
          : acknowledgedMessages,
      );
      return acknowledged
        ? {
            ...state,
            pendingUserMessages,
          }
        : state;
    }
    case "mark_pending_user_message_dispatched": {
      const createdAfterMaxSeq = maxConversationEventSeq(state.conversation);
      const createdAfterEventIds = Array.from(
        conversationEventIdSet(state.conversation),
      );
      const pendingUserMessages = cachePendingUserMessages(
        state.cacheKey,
        redispatchPendingUserMessageInSubmissionOrder(
          state.pendingUserMessages,
          action.id,
          {
            requestId: action.requestId,
            attemptedAt: action.attemptedAt,
            queuedHint: action.queuedHint,
            createdAfterMaxSeq,
            createdAfterEventIds,
          },
        ),
      );
      return pendingUserMessages === state.pendingUserMessages
        ? state
        : { ...state, pendingUserMessages };
    }
    case "reject_pending_user_message": {
      const pendingUserMessages = cachePendingUserMessages(
        state.cacheKey,
        state.pendingUserMessages.map((message) =>
          message.id === action.id
            ? rejectPendingUserMessageState(message, action)
            : message,
        ),
      );
      return pendingUserMessages === state.pendingUserMessages
        ? state
        : { ...state, pendingUserMessages };
    }
    case "remove_pending_user_message": {
      const pendingUserMessages = cachePendingUserMessages(
        state.cacheKey,
        state.pendingUserMessages.filter((message) => message.id !== action.id),
      );
      return {
        ...state,
        pendingUserMessages,
      };
    }
    case "prune_pending_user_messages": {
      const pendingUserMessages = cachePendingUserMessages(
        state.cacheKey,
        state.pendingUserMessages,
        action.now,
      );
      if (pendingUserMessages === state.pendingUserMessages) {
        return state;
      }
      return {
        ...state,
        pendingUserMessages,
      };
    }
    case "add_pending_slash_command":
      return {
        ...state,
        pendingSlashCommands: [
          ...state.pendingSlashCommands,
          action.command,
        ].slice(-12),
      };
    case "settle_pending_slash_command":
      if (
        !state.pendingSlashCommands.some((command) =>
          command.id === action.id &&
          (command.status !== action.status || !command.completedAt)
        )
      ) {
        return state;
      }
      return {
        ...state,
        pendingSlashCommands: state.pendingSlashCommands.map((command) =>
          command.id === action.id
            ? {
                ...command,
                status: action.status,
                completedAt: command.completedAt ?? action.completedAt,
              }
            : command,
        ),
      };
    case "remove_pending_slash_command":
      return {
        ...state,
        pendingSlashCommands: state.pendingSlashCommands.filter(
          (command) => command.id !== action.id,
        ),
      };
    case "prune_pending_slash_commands":
      if (
        !state.pendingSlashCommands.some((command) =>
          shouldPrunePendingSlashCommand(command, action.now),
        )
      ) {
        return state;
      }
      return {
        ...state,
        pendingSlashCommands: state.pendingSlashCommands.filter(
          (command) => !shouldPrunePendingSlashCommand(command, action.now),
        ),
      };
    default:
      return state;
  }
}

function applyCodexConversationSnapshot(
  state: CodexChatThreadState,
  payload: CodexConversationSnapshotPayload,
): CodexChatThreadState {
  const accepted = acceptConversationEnvelope(
    state.streamCursor,
    {
      requestId: payload.request_id,
      conversationId: payload.conversation_id,
      revision: payload.revision,
    },
    conversationIdentity(payload.conversation),
  );
  if (!accepted.accepted) {
    return state;
  }
  const conversation = reconcileConversationSnapshot(
    state.conversation,
    payload.conversation,
    accepted.sameConversation,
  );
  return applyIncomingConversation(state, conversation, accepted.cursor);
}

function applyIncomingConversation(
  state: CodexChatThreadState,
  conversation: CodexConversation,
  streamCursor: ConversationStreamCursor = state.streamCursor,
): CodexChatThreadState {
  const boundary = newChatBoundaryCache.get(state.cacheKey);
  let filteredConversation = conversationForNewChatBoundary(
    conversation,
    boundary,
    state.localChatState === "starting-new-chat" ||
      state.localChatState === "new-chat-ready",
  );
  if (
    boundary &&
    state.localChatState === "idle" &&
    state.pendingUserMessages.length === 0 &&
    filteredConversation?.events.length === 0
  ) {
    const unboundedConversation = filterCodexConversationForChat(conversation);
    if (unboundedConversation.events.length > 0) {
      newChatBoundaryCache.delete(state.cacheKey);
      filteredConversation = unboundedConversation;
    }
  }
  if (!filteredConversation) {
    return {
      ...state,
      loading: false,
      error: null,
      streamCursor,
    };
  }
  if (
    state.conversation?.events.length &&
    filteredConversation.events.length === 0 &&
    isTransientEmptyConversation(filteredConversation.reason)
  ) {
    filteredConversation = {
      ...filteredConversation,
      events: state.conversation.events,
    };
  }
  const nextConversation = reuseStableConversationEvents(
    state.conversation,
    filteredConversation,
  );
  conversationCache.set(state.cacheKey, nextConversation);
  const localChatState =
    state.localChatState === "starting-new-chat" ||
    state.localChatState === "new-chat-ready"
      ? nextConversation.events.length === 0 ? "new-chat-ready" : "idle"
      : state.localChatState;
  if (localChatState === "idle") {
    localChatStateCache.delete(state.cacheKey);
  } else {
    localChatStateCache.set(state.cacheKey, localChatState);
  }
  const pendingUserMessages = cachePendingUserMessages(
    state.cacheKey,
    reconcilePendingUserMessages(
      state.pendingUserMessages,
      nextConversation,
    ),
  );
  if (
    state.conversation === nextConversation &&
    state.pendingUserMessages === pendingUserMessages &&
    state.localChatState === localChatState &&
    state.loading === false &&
    state.error === null &&
    state.streamCursor === streamCursor
  ) {
    return state;
  }
  return {
    ...state,
    conversation: nextConversation,
    pendingUserMessages,
    localChatState,
    loading: false,
    error: null,
    streamCursor,
  };
}

function applyCodexConversationDelta(
  state: CodexChatThreadState,
  delta: CodexConversationDeltaPayload,
): CodexChatThreadState {
  const accepted = acceptConversationEnvelope(
    state.streamCursor,
    {
      requestId: delta.request_id,
      conversationId: delta.conversation_id,
      revision: delta.revision,
    },
    conversationIdentity(state.conversation),
  );
  if (!accepted.accepted || !accepted.sameConversation) {
    return state;
  }
  const baseConversation = state.conversation ?? conversationCache.get(state.cacheKey) ?? {
    available: delta.available ?? false,
    reason: delta.reason,
    source: delta.source,
    path: delta.path,
    session_id: delta.session_id,
    cwd: delta.cwd,
    updated_at: delta.updated_at,
    active: delta.active,
    turn_epoch: delta.turn_epoch,
    turn_revision: delta.turn_revision,
    turn: delta.turn,
    queued_turns: delta.queued_turns,
    events: [],
  };
  if (
    delta.upserts.length === 0 &&
    delta.deletes.length === 0 &&
    !codexDeltaMetadataChanged(baseConversation, delta)
  ) {
    if (
      state.loading ||
      state.error !== null ||
      state.streamCursor !== accepted.cursor
    ) {
      return {
        ...state,
        loading: false,
        error: null,
        streamCursor: accepted.cursor,
      };
    }
    return state;
  }
  const nextEvents = reconcileConversationDeltaEvents(
    baseConversation.events,
    delta.upserts,
    delta.deletes,
  );
  const lifecycle = reconcileStructuredLifecycleProjection(
    baseConversation,
    delta,
  );
  const nextConversation = {
    ...baseConversation,
    available: delta.available ?? baseConversation.available,
    reason: delta.reason ?? baseConversation.reason,
    source: delta.source ?? baseConversation.source,
    path: delta.path ?? baseConversation.path,
    session_id: delta.session_id ?? baseConversation.session_id,
    cwd: delta.cwd ?? baseConversation.cwd,
    updated_at: delta.updated_at ?? baseConversation.updated_at,
    active: delta.active ?? baseConversation.active,
    ...lifecycle,
    events: nextEvents,
  };
  return applyIncomingConversation(state, nextConversation, accepted.cursor);
}

function codexDeltaMetadataChanged(
  baseConversation: CodexConversation,
  delta: CodexConversationDeltaPayload,
) {
  return (
    (delta.available !== undefined && delta.available !== baseConversation.available) ||
    (delta.reason !== undefined && delta.reason !== baseConversation.reason) ||
    (delta.source !== undefined && delta.source !== baseConversation.source) ||
    (delta.path !== undefined && delta.path !== baseConversation.path) ||
    (delta.session_id !== undefined && delta.session_id !== baseConversation.session_id) ||
    (delta.cwd !== undefined && delta.cwd !== baseConversation.cwd) ||
    (delta.updated_at !== undefined && delta.updated_at !== baseConversation.updated_at) ||
    (delta.active !== undefined && delta.active !== baseConversation.active) ||
    (delta.turn_epoch !== undefined &&
      delta.turn_epoch !== baseConversation.turn_epoch) ||
    (delta.turn_revision !== undefined &&
      delta.turn_revision !== baseConversation.turn_revision) ||
    (delta.turn !== undefined &&
      !structuredTurnsEqual(
        baseConversation.turn,
        reconcileStructuredTurn(baseConversation.turn, delta.turn),
      )) ||
    (delta.queued_turns !== undefined &&
      !structuredTurnQueuesEqual(
        baseConversation.queued_turns,
        reconcileStructuredTurnQueue(
          baseConversation.queued_turns,
          delta.queued_turns,
        ),
      ))
  );
}

function applyCodexConversationSyncStatus(
  state: CodexChatThreadState,
  status: CodexConversationSyncStatusPayload,
): CodexChatThreadState {
  const accepted = acceptConversationEnvelope(state.streamCursor, {
    requestId: status.request_id,
    conversationId: status.conversation_id,
    revision: status.revision,
  });
  if (!accepted.accepted) {
    return state;
  }
  const baseConversation = state.conversation ??
    conversationCache.get(state.cacheKey) ?? {
      available: false,
      reason: status.reason,
      events: [],
    };
  const conversation = reconcileConversationSyncLifecycle(
    baseConversation,
    status,
  );
  const applied = applyIncomingConversation(
    state,
    conversation,
    accepted.cursor,
  );
  const loading =
    status.state === "syncing" && applied.conversation?.events.length === 0;
  return applied.loading === loading
    ? applied
    : {
        ...applied,
        loading,
      };
}

function reuseStableConversationEvents(
  previousConversation: CodexConversation | null,
  nextConversation: CodexConversation,
): CodexConversation {
  if (!previousConversation?.events.length) {
    return nextConversation;
  }
  const previousById = new Map(
    previousConversation.events.map((event) => [event.id, event]),
  );
  let changed = previousConversation.events.length !== nextConversation.events.length;
  const events = nextConversation.events.map((event, index) => {
    const previousEvent = previousById.get(event.id);
    if (previousEvent && codexEventsEqual(previousEvent, event)) {
      if (previousConversation.events[index] !== previousEvent) {
        changed = true;
      }
      return previousEvent;
    }
    changed = true;
    return event;
  });
  if (!changed && codexConversationMetadataEqual(previousConversation, nextConversation)) {
    return previousConversation;
  }
  return {
    ...nextConversation,
    events: changed ? events : previousConversation.events,
  };
}

function codexConversationMetadataEqual(
  left: CodexConversation,
  right: CodexConversation,
) {
  return (
    left.available === right.available &&
    left.reason === right.reason &&
    left.source === right.source &&
    left.path === right.path &&
    left.session_id === right.session_id &&
    left.cwd === right.cwd &&
    left.updated_at === right.updated_at &&
    left.active === right.active &&
    left.turn_epoch === right.turn_epoch &&
    left.turn_revision === right.turn_revision &&
    structuredTurnsEqual(left.turn, right.turn) &&
    structuredTurnQueuesEqual(left.queued_turns, right.queued_turns)
  );
}

function reconcilePendingUserMessages(
  pendingUserMessages: PendingUserMessage[],
  conversation: CodexConversation,
): PendingUserMessage[] {
  return reconcilePendingUserMessagesWithStructuredTurns(
    reconcilePendingUserMessagesAgainstEvents(
      pendingUserMessages,
      conversation.events,
    ),
    conversation.turn,
    conversation.queued_turns,
    conversation.turn_epoch,
    conversation.turn_revision,
  );
}

function codexEventsEqual(
  left: CodexConversation["events"][number],
  right: CodexConversation["events"][number],
) {
  return (
    left === right ||
    (
      left.id === right.id &&
      left.seq === right.seq &&
      left.timestamp === right.timestamp &&
      left.kind === right.kind &&
      left.role === right.role &&
      left.title === right.title &&
      left.body === right.body &&
      left.command === right.command &&
      left.tool_name === right.tool_name &&
      left.input === right.input &&
      left.output === right.output &&
      left.call_id === right.call_id &&
      left.exit_code === right.exit_code &&
      left.status === right.status &&
      left.partial === right.partial &&
      left.transient === right.transient &&
      left.explanation === right.explanation &&
      left.source === right.source &&
      stringArraysEqual(left.files, right.files) &&
      planStepsEqual(left.plan, right.plan)
    )
  );
}

function stringArraysEqual(left?: string[], right?: string[]) {
  if (left === right) {
    return true;
  }
  if (!left || !right || left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) {
      return false;
    }
  }
  return true;
}

function planStepsEqual(
  left?: CodexConversation["events"][number]["plan"],
  right?: CodexConversation["events"][number]["plan"],
) {
  if (left === right) {
    return true;
  }
  if (!left || !right || left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    const leftStep = left[index];
    const rightStep = right[index];
    if (
      leftStep?.step !== rightStep?.step ||
      leftStep?.status !== rightStep?.status
    ) {
      return false;
    }
  }
  return true;
}

function cachedPendingUserMessages(cacheKey: string): PendingUserMessage[] {
  return cachePendingUserMessages(
    cacheKey,
    pendingUserMessageCache.get(cacheKey) ?? [],
    Date.now(),
  );
}

function cachePendingUserMessages(
  cacheKey: string,
  messages: PendingUserMessage[],
  now: number = Date.now(),
): PendingUserMessage[] {
  const conversation = conversationCache.get(cacheKey);
  let nextMessages = retainPendingUserMessages(
    messages,
    conversation?.turn,
    conversation?.queued_turns,
    now,
  );
  if (pendingUserMessagesShallowEqual(messages, nextMessages)) {
    nextMessages = messages;
  }
  if (nextMessages.length > 0) {
    pendingUserMessageCache.set(cacheKey, nextMessages);
  } else {
    pendingUserMessageCache.delete(cacheKey);
  }
  return nextMessages;
}

function pendingUserMessagesShallowEqual(
  left: PendingUserMessage[],
  right: PendingUserMessage[],
) {
  if (left === right) {
    return true;
  }
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) {
      return false;
    }
  }
  return true;
}

export function useCodexChatSession({
  serverId,
  agentId,
  conversationScopeKey,
  agentInfo,
  connectionState,
  screenFocused,
}: UseCodexChatSessionInput) {
  const composerCacheKey = codexChatSessionCacheKey(
    serverId,
    agentId,
    conversationScopeKey,
  );
  const cacheKey = composerCacheKey;
  const [threadState, dispatchThread] = useReducer(
    codexChatThreadReducer,
    cacheKey,
    initialCodexChatThreadState,
  );
  const [draftState, setDraftState] = useState<KeyedState<string>>(
    () => ({
      cacheKey: composerCacheKey,
      value: draftCache.get(composerCacheKey) ?? "",
    }),
  );
  const [attachmentsState, setAttachmentsState] = useState<
    KeyedState<ComposerAttachment[]>
  >(
    () => ({
      cacheKey: composerCacheKey,
      value: attachmentCache.get(composerCacheKey) ?? [],
    }),
  );
  const conversation = threadState.cacheKey === cacheKey
    ? threadState.conversation
    : conversationCache.get(cacheKey) ?? null;
  const localChatState = threadState.cacheKey === composerCacheKey
    ? threadState.localChatState
    : localChatStateCache.get(composerCacheKey) ?? "idle";
  const loading = threadState.cacheKey === cacheKey
    ? threadState.loading
    : !conversationCache.has(cacheKey);
  const error = threadState.cacheKey === cacheKey ? threadState.error : null;
  const draft =
    draftState.cacheKey === composerCacheKey
      ? draftState.value
      : draftCache.get(composerCacheKey) ?? "";
  const attachments =
    attachmentsState.cacheKey === composerCacheKey
      ? attachmentsState.value
      : attachmentCache.get(composerCacheKey) ?? [];
  const pendingUserMessages = threadState.cacheKey === composerCacheKey
    ? threadState.pendingUserMessages
    : cachedPendingUserMessages(composerCacheKey);
  const pendingSlashCommands = threadState.cacheKey === composerCacheKey
    ? threadState.pendingSlashCommands
    : [];
  const boundary = newChatBoundaryCache.get(composerCacheKey);
  const visiblePendingUserMessages = useMemo(
    () =>
      filterVisiblePendingUserMessages(
        pendingUserMessages,
        boundary,
      ),
    [boundary, pendingUserMessages],
  );
  const visiblePendingSlashCommands = useMemo(
    () =>
      filterVisiblePendingSlashCommands(
        pendingSlashCommands,
        boundary,
      ),
    [boundary, pendingSlashCommands],
  );
  const presentationCacheKey = `${cacheKey}:${
    conversation?.session_id || conversation?.path || ""
  }`;

  const writeDraft = useCallback(
    (nextDraft: string) => {
      // The native input is cleared imperatively when a submission starts.
      // Every subsequent onChangeText value is user input and must be kept
      // verbatim: suppressing a value equal to the submitted text corrupts
      // legitimate duplicate or pasted messages while a provider is Working.
      if (nextDraft) {
        draftCache.set(composerCacheKey, nextDraft);
      } else {
        draftCache.delete(composerCacheKey);
      }
      setDraftState({
        cacheKey: composerCacheKey,
        value: nextDraft,
      });
    },
    [composerCacheKey],
  );
  const setDraft = useCallback(
    (nextDraft: string) => writeDraft(nextDraft),
    [writeDraft],
  );
  const restoreDraft = useCallback(
    (nextDraft: string) => writeDraft(nextDraft),
    [writeDraft],
  );

  const setAttachments = useCallback(
    (nextValue: SetStateAction<ComposerAttachment[]>) => {
      setAttachmentsState((current) => {
        const currentAttachments =
          current.cacheKey === composerCacheKey
            ? current.value
            : attachmentCache.get(composerCacheKey) ?? [];
        const nextAttachments =
          typeof nextValue === "function"
            ? nextValue(currentAttachments)
            : nextValue;
        if (nextAttachments.length > 0) {
          attachmentCache.set(composerCacheKey, nextAttachments);
        } else {
          attachmentCache.delete(composerCacheKey);
        }
        return {
          cacheKey: composerCacheKey,
          value: nextAttachments,
        };
      });
    },
    [composerCacheKey],
  );

  const addPendingUserMessage = useCallback((message: PendingUserMessageInput) => {
    const id = `pending-user:${Date.now().toString(36)}:${Math.random().toString(36).slice(2, 8)}`;
    const baseConversation = conversation ?? conversationCache.get(cacheKey) ?? null;
    const pendingMessage = {
      ...message,
      id,
      createdAt: message.turnStartedAt,
      createdAfterMaxSeq: maxConversationEventSeq(baseConversation),
      createdAfterEventIds: Array.from(conversationEventIdSet(baseConversation)),
    };
    cachePendingUserMessages(composerCacheKey, [
      ...cachedPendingUserMessages(composerCacheKey),
      pendingMessage,
    ]);
    dispatchThread({
      type: "add_pending_user_message",
      message: pendingMessage,
    });
    return id;
  }, [cacheKey, composerCacheKey, conversation]);

  const acknowledgePendingUserMessage = useCallback((
    id: string,
    acknowledgement: PendingUserMessageAcknowledgement,
  ) => {
    const baseConversation = conversationCache.get(cacheKey) ?? conversation;
    const acknowledgedMessages = cachedPendingUserMessages(composerCacheKey).map(
      (message) =>
        message.id === id
          ? acknowledgePendingUserMessageWithStructuredTurns(
              message,
              acknowledgement,
              baseConversation?.turn,
              baseConversation?.queued_turns,
            )
          : message,
    );
    cachePendingUserMessages(
      composerCacheKey,
      canReconcilePendingAcknowledgementAgainstProjection(
        acknowledgement,
        baseConversation,
      )
        ? reconcilePendingUserMessagesWithStructuredTurns(
            acknowledgedMessages,
            baseConversation?.turn,
            baseConversation?.queued_turns,
            baseConversation?.turn_epoch,
            baseConversation?.turn_revision,
          )
        : acknowledgedMessages,
    );
    dispatchThread({
      type: "acknowledge_pending_user_message",
      id,
      ...acknowledgement,
    });
  }, [cacheKey, composerCacheKey, conversation]);

  const markPendingUserMessageDispatched = useCallback((
    id: string,
    attempt: PendingUserMessageDispatchAttempt,
  ) => {
    const baseConversation = conversationCache.get(cacheKey) ?? conversation;
    const boundary = {
      createdAfterMaxSeq: maxConversationEventSeq(baseConversation),
      createdAfterEventIds: Array.from(conversationEventIdSet(baseConversation)),
    };
    cachePendingUserMessages(
      composerCacheKey,
      redispatchPendingUserMessageInSubmissionOrder(
        cachedPendingUserMessages(composerCacheKey),
        id,
        {
          ...attempt,
          ...boundary,
        },
      ),
    );
    dispatchThread({
      type: "mark_pending_user_message_dispatched",
      id,
      ...attempt,
    });
  }, [cacheKey, composerCacheKey, conversation]);

  const rejectPendingUserMessage = useCallback((
    id: string,
    rejection: PendingUserMessageRejection,
  ) => {
    cachePendingUserMessages(
      composerCacheKey,
      cachedPendingUserMessages(composerCacheKey).map((message) =>
        message.id === id
          ? rejectPendingUserMessageState(message, rejection)
          : message,
      ),
    );
    dispatchThread({
      type: "reject_pending_user_message",
      id,
      ...rejection,
    });
  }, [composerCacheKey]);

  const removePendingUserMessage = useCallback((id: string) => {
    cachePendingUserMessages(
      composerCacheKey,
      cachedPendingUserMessages(composerCacheKey).filter(
        (message) => message.id !== id,
      ),
    );
    dispatchThread({
      type: "remove_pending_user_message",
      id,
    });
  }, [composerCacheKey]);

  const addPendingSlashCommand = useCallback((command: PendingSlashCommandInput) => {
    const id = `pending-slash:${Date.now().toString(36)}:${Math.random().toString(36).slice(2, 8)}`;
    dispatchThread({
      type: "add_pending_slash_command",
      command: {
        ...command,
        id,
        status: "running",
        createdAt: new Date().toISOString(),
      },
    });
    return id;
  }, []);

  const settlePendingSlashCommand = useCallback((
    id: string,
    status: PendingSlashCommand["status"] = "done",
  ) => {
    dispatchThread({
      type: "settle_pending_slash_command",
      id,
      status,
      completedAt: new Date().toISOString(),
    });
  }, []);

  const removePendingSlashCommand = useCallback((id: string) => {
    dispatchThread({
      type: "remove_pending_slash_command",
      id,
    });
  }, []);

  const markNewChatMessageStarted = useCallback(() => {
    dispatchThread({ type: "mark_new_chat_message_started" });
  }, []);

  useEffect(() => {
    dispatchThread({
      type: "cache_key_changed",
      cacheKey,
    });
  }, [cacheKey]);

  useEffect(() => {
    if (!screenFocused || connectionState !== "connected" || !serverId || !agentId) {
      return;
    }
    dispatchThread({ type: "stream_start" });
    return wsClient.subscribeCodexConversation(
      serverId,
      {
        targetId: agentId,
        cwd: agentInfo?.cwd,
        command: agentInfo?.command,
        name: agentInfo?.name,
        startedAt: agentInfo?.startedAt,
        processId: agentInfo?.processId,
        conversationScopeKey,
      },
      {
        onSnapshot: (payload) => {
          dispatchThread({
            type: "snapshot",
            payload,
          });
        },
        onDelta: (payload) => {
          dispatchThread({
            type: "delta",
            delta: payload,
          });
        },
        onSyncStatus: (payload) => {
          dispatchThread({
            type: "sync_status",
            status: payload,
          });
        },
        onError: (nextError) => {
          dispatchThread({
            type: "stream_error",
            error: nextError.message || "Could not stream Codex conversation.",
          });
        },
      },
    );
  }, [
    agentId,
    agentInfo?.command,
    agentInfo?.cwd,
    agentInfo?.name,
    agentInfo?.processId,
    agentInfo?.startedAt,
    conversationScopeKey,
    connectionState,
    screenFocused,
    serverId,
  ]);

  useEffect(() => {
    setDraftState({
      cacheKey: composerCacheKey,
      value: draftCache.get(composerCacheKey) ?? "",
    });
    setAttachmentsState({
      cacheKey: composerCacheKey,
      value: attachmentCache.get(composerCacheKey) ?? [],
    });
  }, [composerCacheKey]);

  useEffect(() => {
    if (pendingUserMessages.length === 0) {
      return;
    }
    const now = Date.now();
    const nextPruneAt = nextPendingUserMessagePruneAt(
      pendingUserMessages,
      now,
    );
    if (nextPruneAt === undefined) {
      return;
    }

    const prune = () => {
      dispatchThread({
        type: "prune_pending_user_messages",
        now: Date.now(),
      });
    };

    if (nextPruneAt <= now) {
      prune();
      return;
    }
    const timer = setTimeout(prune, nextPruneAt - now);
    return () => clearTimeout(timer);
  }, [pendingUserMessages]);

  useEffect(() => {
    if (pendingSlashCommands.length === 0) {
      return;
    }
    const now = Date.now();
    const nextPruneAt = pendingSlashCommands.reduce((soonest, command) => {
      const createdAt = new Date(command.createdAt).getTime();
      const completedAt = command.completedAt
        ? new Date(command.completedAt).getTime()
        : Number.NaN;
      const maxAgeAt = Number.isFinite(createdAt)
        ? createdAt + PENDING_SLASH_COMMAND_MAX_AGE_MS
        : now + PENDING_SLASH_COMMAND_MAX_AGE_MS;
      const settledAgeAt = Number.isFinite(completedAt)
        ? completedAt + PENDING_SLASH_COMMAND_SETTLED_MAX_AGE_MS
        : Number.POSITIVE_INFINITY;
      return Math.min(soonest, maxAgeAt, settledAgeAt);
    }, Number.POSITIVE_INFINITY);

    const prune = () => {
      dispatchThread({
        type: "prune_pending_slash_commands",
        now: Date.now(),
      });
    };

    if (nextPruneAt <= now) {
      prune();
      return;
    }
    const timer = setTimeout(prune, nextPruneAt - now);
    return () => clearTimeout(timer);
  }, [pendingSlashCommands]);

  return {
    cacheKey: presentationCacheKey,
    conversation,
    localChatState,
    loading,
    error,
    draft,
    setDraft,
    restoreDraft,
    attachments,
    setAttachments,
    pendingUserMessages: visiblePendingUserMessages,
    pendingSlashCommands: visiblePendingSlashCommands,
    addPendingUserMessage,
    acknowledgePendingUserMessage,
    markPendingUserMessageDispatched,
    rejectPendingUserMessage,
    removePendingUserMessage,
    addPendingSlashCommand,
    settlePendingSlashCommand,
    removePendingSlashCommand,
    markNewChatMessageStarted,
  };
}

function filterVisiblePendingUserMessages(
  pendingUserMessages: PendingUserMessage[],
  boundary?: NewChatBoundary,
) {
  if (pendingUserMessages.length === 0) {
    return pendingUserMessages;
  }
  return pendingUserMessages.filter((message) => {
    if (boundary && isPendingMessageBeforeNewChatBoundary(message.createdAt, boundary)) {
      return false;
    }
    return true;
  });
}

function filterVisiblePendingSlashCommands(
  pendingSlashCommands: PendingSlashCommand[],
  boundary?: NewChatBoundary,
) {
  if (pendingSlashCommands.length === 0) {
    return pendingSlashCommands;
  }
  return pendingSlashCommands.filter(
    (command) =>
      !isPendingMessageBeforeNewChatBoundary(command.createdAt, boundary),
  );
}

function shouldPrunePendingSlashCommand(
  command: PendingSlashCommand,
  now: number,
) {
  const createdAt = new Date(command.createdAt).getTime();
  if (
    Number.isFinite(createdAt) &&
    now - createdAt > PENDING_SLASH_COMMAND_MAX_AGE_MS
  ) {
    return true;
  }
  const completedAt = command.completedAt
    ? new Date(command.completedAt).getTime()
    : Number.NaN;
  return (
    Number.isFinite(completedAt) &&
    now - completedAt > PENDING_SLASH_COMMAND_SETTLED_MAX_AGE_MS
  );
}

function shouldPrunePendingUserMessage(
  message: PendingUserMessage,
  now: number,
) {
  return shouldPrunePendingUserMessageByLifecycle(message, now);
}

function isPendingMessageBeforeNewChatBoundary(
  createdAt: string,
  boundary?: NewChatBoundary,
) {
  if (!boundary) {
    return false;
  }
  const timestamp = new Date(createdAt).getTime();
  return Number.isFinite(timestamp) && timestamp < boundary.startedAtMs;
}

function filterCodexConversationForChat(
  conversation: CodexConversation,
): CodexConversation {
  let changed = false;
  const events: CodexConversation["events"] = [];
  for (const event of conversation.events) {
    if (shouldDropStructuredChatEvent(conversation.source, event)) {
      changed = true;
      continue;
    }
    const cleaned = cleanCodexConversationEventForChat(event);
    if (!cleaned) {
      changed = true;
      continue;
    }
    if (cleaned !== event) {
      changed = true;
    }
    events.push(cleaned);
  }
  return changed ? { ...conversation, events } : conversation;
}

function cleanCodexConversationEventForChat(
  event: CodexConversation["events"][number],
): CodexConversation["events"][number] | null {
  const body = cleanCodexVisibleText(event.body || "");
  const input = cleanCodexVisibleText(event.input || "");
  const output = cleanCodexVisibleText(event.output || "");
  const explanation = cleanCodexVisibleText(event.explanation || "");
  const command = cleanCodexVisibleText(event.command || "");
  const title = cleanCodexVisibleText(event.title || "");
  const toolName = cleanCodexVisibleText(event.tool_name || "");
  const nextPlan = event.plan
    ?.map((step) => ({
      ...step,
      step: cleanCodexVisibleText(step.step || ""),
    }))
    .filter((step) => step.step.trim().length > 0);
  const planChanged =
    (event.plan?.length ?? 0) !== (nextPlan?.length ?? 0) ||
    Boolean(event.plan?.some((step, index) => step.step !== nextPlan?.[index]?.step));

  if (
    !body &&
    !input &&
    !output &&
    !explanation &&
    !command &&
    !title &&
    !toolName &&
    !event.files?.length &&
    !nextPlan?.length
  ) {
    return null;
  }

  if (
    body === (event.body || "") &&
    input === (event.input || "") &&
    output === (event.output || "") &&
    explanation === (event.explanation || "") &&
    command === (event.command || "") &&
    title === (event.title || "") &&
    toolName === (event.tool_name || "") &&
    !planChanged
  ) {
    return event;
  }

  return {
    ...event,
    body,
    input,
    output,
    explanation,
    command,
    title,
    tool_name: toolName,
    plan: nextPlan,
  };
}

function looksLikeCodexInstructionContext(value: string) {
  const text = comparableUserMessageText(value);
  if (!text) {
    return false;
  }
  const lower = text.toLowerCase();
  if (
    lower.startsWith("# repository guidelines") ||
    lower.startsWith("repository guidelines ") ||
    lower.startsWith("## project structure & module organization") ||
    lower.includes("agents.md instructions for ")
  ) {
    return true;
  }
  const markerCount = [
    "repository guidelines",
    "project structure & module organization",
    "build, test, and development commands",
    "coding style & naming conventions",
    "testing guidelines",
    "commit & pull request guidelines",
    "security & configuration tips",
    "configuration & secrets",
    "agent & sandbox releases",
    "first-principles engineering",
  ].filter((marker) => lower.includes(marker)).length;
  const hasStrongMarker = [
    "agent & sandbox releases",
    "first-principles engineering",
    "exchange data & trading state",
    "refresh cadence is part of the product contract",
    "avoid compatibility barrels",
    "freeride-sandbox",
    "daytona",
  ].some((marker) => lower.includes(marker));
  if (markerCount >= 2 && hasStrongMarker) {
    return true;
  }
  return (
    lower.includes("agents.md instructions for ") ||
    lower.includes("<environment_context>") ||
    lower.includes("<skills_instructions>") ||
    lower.includes("<permissions instructions>")
  );
}

function cleanCodexVisibleText(value: string) {
  const stripped = stripCodexContextualFragments(value);
  if (looksLikeCodexInstructionContext(stripped)) {
    return "";
  }
  return stripped;
}

function stripCodexContextualFragments(value: string) {
  let stripped = value.trim();
  if (!stripped) {
    return "";
  }
  let changed = true;
  while (changed) {
    changed = false;
    let best: { start: number; end: number } | null = null;
    for (const [open, close] of CODEX_CONTEXTUAL_FRAGMENT_MARKERS) {
      const range = markedTextRange(open, close, stripped);
      if (!range) {
        continue;
      }
      if (!best || range.start < best.start) {
        best = range;
      }
    }
    if (best) {
      stripped = `${stripped.slice(0, best.start)}\n${stripped.slice(best.end)}`.trim();
      changed = true;
    }
  }
  return normalizeDisplayText(stripped);
}

function normalizeDisplayText(value: string) {
  return value
    .replace(/\r\n/g, "\n")
    .replace(/\r/g, "\n")
    .split("\n")
    .map((line) => line.trimEnd())
    .join("\n")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

const CODEX_CONTEXTUAL_FRAGMENT_MARKERS = [
  ["# AGENTS.md instructions for ", "</INSTRUCTIONS>"],
  ["<environment_context>", "</environment_context>"],
  ["<apps_instructions>", "</apps_instructions>"],
  ["<skills_instructions>", "</skills_instructions>"],
  ["<plugins_instructions>", "</plugins_instructions>"],
  ["<collaboration_mode>", "</collaboration_mode>"],
  ["<realtime_conversation>", "</realtime_conversation>"],
  ["<permissions instructions>", "</permissions instructions>"],
  ["<skill>", "</skill>"],
  ["<user_shell_command>", "</user_shell_command>"],
  ["<turn_aborted>", "</turn_aborted>"],
  ["<subagent_notification>", "</subagent_notification>"],
  ["<goal_context>", "</goal_context>"],
  ["<model_switch>", "</model_switch>"],
  ["<personality_spec>", "</personality_spec>"],
] as const;

function markedTextRange(openMarker: string, closeMarker: string, value: string) {
  let searchFrom = 0;
  while (searchFrom < value.length) {
    const start = value.indexOf(openMarker, searchFrom);
    if (start < 0) {
      return null;
    }
    if (!isLineStartMarker(value, start)) {
      searchFrom = start + openMarker.length;
      continue;
    }
    const closeSearchFrom = start + openMarker.length;
    const closeStart = value.indexOf(closeMarker, closeSearchFrom);
    if (closeStart < 0) {
      return null;
    }
    return {
      start,
      end: closeStart + closeMarker.length,
    };
  }
  return null;
}

function isLineStartMarker(value: string, index: number) {
  for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
    const char = value[cursor];
    if (char === " " || char === "\t" || char === "\r") {
      continue;
    }
    return char === "\n";
  }
  return true;
}

function conversationForNewChatBoundary(
  conversation: CodexConversation,
  boundary?: NewChatBoundary,
  hideEmptyBoundary: boolean = false,
): CodexConversation | null {
  const filteredConversation = filterCodexConversationForChat(conversation);
  if (!boundary) {
    return filteredConversation;
  }

  const candidateEvents = conversation.events.filter((event) =>
    isEventAfterNewChatBoundary(event, boundary),
  );
  const events = filterCodexConversationForChat({
    ...conversation,
    events: candidateEvents,
  }).events;
  if (hideEmptyBoundary && events.length === 0) {
    return null;
  }
  return {
    ...filteredConversation,
    events,
  };
}

function isTransientEmptyConversation(reason?: string) {
  return (
    reason === "session_not_ready" ||
    reason === "transcript_not_found" ||
    reason === "missing_cwd"
  );
}

function conversationEventIdSet(conversation: CodexConversation | null) {
  const ids = new Set<string>();
  conversation?.events.forEach((event) => {
    if (event.id) {
      ids.add(event.id);
    }
  });
  return ids;
}

function maxConversationEventSeq(conversation: CodexConversation | null) {
  let maxSeq = 0;
  conversation?.events.forEach((event) => {
    if (typeof event.seq === "number" && event.seq > maxSeq) {
      maxSeq = event.seq;
    }
  });
  return maxSeq;
}

function isEventAfterNewChatBoundary(
  event: CodexConversation["events"][number],
  boundary: NewChatBoundary,
) {
  if (event.id && boundary.previousEventIds.has(event.id)) {
    return false;
  }
  if (
    boundary.previousMaxSeq > 0 &&
    typeof event.seq === "number" &&
    event.seq > boundary.previousMaxSeq
  ) {
    return true;
  }
  if (!event.timestamp) {
    return false;
  }
  const timestamp = new Date(event.timestamp).getTime();
  return Number.isFinite(timestamp) && timestamp >= boundary.startedAtMs;
}
