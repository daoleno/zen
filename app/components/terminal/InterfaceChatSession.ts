import {
  useCallback,
  useEffect,
  useReducer,
  useRef,
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
  beginPendingUserMessageAttempt as beginPendingUserMessageAttemptState,
  rejectPendingUserMessage as rejectPendingUserMessageState,
  reconcilePendingUserMessagesAgainstEvents,
  type ProviderEventTurnFocusAlias,
  type PendingUserMessageLifecycle,
} from "./pendingUserMessageLifecycle";
import {
  EMPTY_CONVERSATION_STREAM_CURSOR,
  acceptConversationEnvelope,
  conversationIdentity,
  reconcileConversationDeltaEvents,
  reconcileConversationSnapshot,
  providerActivitiesEqual,
  type ConversationStreamCursor,
} from "./interfaceConversationReconciliation";
import { shouldDropStructuredChatEvent } from "./interfaceConversationVisibility";
import { interfaceChatSessionCacheKey } from "./interfaceChatSessionIdentity";

const INSTRUCTION_CONTEXT_ATTACHMENT_TAG_RE =
  /<zen_attachments>\s*([\s\S]*?)\s*<\/zen_attachments>/i;

type KeyedState<T> = {
  cacheKey: string;
  value: T;
};

export type ComposerAttachment = UploadedAttachment & {
  id: string;
};

export type { PendingUserMessageLifecycle };

export type PendingUserMessage = {
  id: string;
  body: string;
  sentText: string;
  attachments: Array<
    Pick<ComposerAttachment, "name" | "path" | "localUri" | "mimeType">
  >;
  createdAt: string;
  lifecycle: PendingUserMessageLifecycle;
  dispatchRequestId: string;
  dispatchAttemptOrder: number;
  failureCode?: string;
  failureMessage?: string;
  createdAfterMaxSeq?: number;
  createdAfterEventIds?: string[];
};

export type PendingUserMessageInput = Omit<
  PendingUserMessage,
  | "id"
  | "createdAt"
  | "dispatchAttemptOrder"
  | "createdAfterMaxSeq"
  | "createdAfterEventIds"
>;

export type PendingUserMessageAttempt = {
  requestId: string;
};

export type PendingUserMessageRejection = {
  requestId: string;
  code: string;
  message: string;
};

export type InterfaceChatAgentInfo = {
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

interface UseInterfaceChatSessionInput {
  serverId: string;
  agentId: string;
  conversationScopeKey?: string;
  agentInfo?: InterfaceChatAgentInfo;
  connectionState: ConnectionState;
  screenFocused: boolean;
}

const draftCache = new Map<string, string>();
const attachmentCache = new Map<string, ComposerAttachment[]>();

/** @internal Exported for behavior tests of the shared stream fallback. */
export function interfaceConversationStreamErrorMessage(error: Error) {
  return error.message || "Could not stream this conversation.";
}

type InterfaceChatThreadState = {
  cacheKey: string;
  conversation: CodexConversation | null;
  loading: boolean;
  error: string | null;
  pendingUserMessages: PendingUserMessage[];
  turnFocusAnchorAliases: ReadonlyMap<string, string>;
  streamCursor: ConversationStreamCursor;
  awaitingSnapshot: boolean;
  resyncToken: number;
};

type InterfaceChatThreadAction =
  | { type: "cache_key_changed"; cacheKey: string }
  | { type: "stream_start"; generation: number }
  | {
      type: "snapshot";
      payload: CodexConversationSnapshotPayload;
      generation: number;
    }
  | { type: "delta"; delta: CodexConversationDeltaPayload; generation: number }
  | {
      type: "sync_status";
      status: CodexConversationSyncStatusPayload;
      generation: number;
    }
  | { type: "stream_error"; error: string; generation: number }
  | {
      type: "add_pending_user_message";
      message: PendingUserMessageInput & { id: string; createdAt: string };
    }
  | {
      type: "begin_pending_user_message_attempt";
      id: string;
      requestId: string;
    }
  | {
      type: "reject_pending_user_message";
      id: string;
      requestId: string;
      code: string;
      message: string;
    };

function initialInterfaceChatThreadState(
  cacheKey: string,
): InterfaceChatThreadState {
  return {
    cacheKey,
    conversation: null,
    loading: true,
    error: null,
    pendingUserMessages: [],
    turnFocusAnchorAliases: new Map(),
    streamCursor: EMPTY_CONVERSATION_STREAM_CURSOR,
    awaitingSnapshot: false,
    resyncToken: 0,
  };
}

/** @internal Exported for behavior tests of the process-local Chat state. */
export function interfaceChatThreadReducer(
  state: InterfaceChatThreadState,
  action: InterfaceChatThreadAction,
): InterfaceChatThreadState {
  switch (action.type) {
    case "cache_key_changed":
      if (state.cacheKey === action.cacheKey) {
        return state;
      }
      return initialInterfaceChatThreadState(action.cacheKey);
    case "stream_start":
      return {
        ...state,
        loading: !state.conversation?.events.length,
        error: null,
        awaitingSnapshot: true,
        streamCursor: {
          conversationId: conversationIdentity(state.conversation),
          revision: 0,
          generation: action.generation,
        },
      };
    case "snapshot":
      return applyCodexConversationSnapshot(
        state,
        action.payload,
        action.generation,
      );
    case "delta":
      return applyCodexConversationDelta(
        state,
        action.delta,
        action.generation,
      );
    case "sync_status":
      return applyCodexConversationSyncStatus(
        state,
        action.status,
        action.generation,
      );
    case "stream_error":
      if (action.generation !== state.streamCursor.generation) {
        return state;
      }
      return {
        ...state,
        loading: false,
        error: action.error,
      };
    case "add_pending_user_message": {
      const message = {
        ...action.message,
        dispatchAttemptOrder: nextPendingDispatchAttemptOrder(
          state.pendingUserMessages,
        ),
        createdAfterMaxSeq: maxConversationEventSeq(state.conversation),
        createdAfterEventIds: Array.from(
          conversationEventIdSet(state.conversation),
        ),
      };
      return {
        ...state,
        pendingUserMessages: [...state.pendingUserMessages, message],
        turnFocusAnchorAliases:
          state.turnFocusAnchorAliases.size === 0
            ? state.turnFocusAnchorAliases
            : new Map(),
      };
    }
    case "begin_pending_user_message_attempt": {
      const createdAfterMaxSeq = maxConversationEventSeq(state.conversation);
      const createdAfterEventIds = Array.from(
        conversationEventIdSet(state.conversation),
      );
      const dispatchAttemptOrder = nextPendingDispatchAttemptOrder(
        state.pendingUserMessages,
      );
      let changed = false;
      const pendingUserMessages = state.pendingUserMessages.map((message) => {
        if (message.id !== action.id) {
          return message;
        }
        changed = true;
        return beginPendingUserMessageAttemptState(message, {
          requestId: action.requestId,
          dispatchAttemptOrder,
          createdAfterMaxSeq,
          createdAfterEventIds,
        });
      });
      return changed ? { ...state, pendingUserMessages } : state;
    }
    case "reject_pending_user_message": {
      let changed = false;
      const pendingUserMessages = state.pendingUserMessages.map((message) => {
        if (message.id !== action.id) {
          return message;
        }
        const next = rejectPendingUserMessageState(message, action);
        changed ||= next !== message;
        return next;
      });
      return !changed ? state : { ...state, pendingUserMessages };
    }
    default:
      return state;
  }
}

function nextPendingDispatchAttemptOrder(
  pendingUserMessages: PendingUserMessage[],
) {
  return (
    pendingUserMessages.reduce(
      (latest, message) =>
        Number.isFinite(message.dispatchAttemptOrder)
          ? Math.max(latest, message.dispatchAttemptOrder)
          : latest,
      0,
    ) + 1
  );
}

function applyCodexConversationSnapshot(
  state: InterfaceChatThreadState,
  payload: CodexConversationSnapshotPayload,
  generation: number,
): InterfaceChatThreadState {
  const accepted = acceptConversationEnvelope(
    state.streamCursor,
    {
      requestId: payload.request_id,
      conversationId: payload.conversation_id,
      revision: payload.revision,
      generation,
      kind: "snapshot",
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
  return applyIncomingConversation(
    {
      ...state,
      awaitingSnapshot: false,
      pendingUserMessages: accepted.sameConversation
        ? state.pendingUserMessages
        : [],
      turnFocusAnchorAliases: accepted.sameConversation
        ? state.turnFocusAnchorAliases
        : new Map(),
    },
    conversation,
    accepted.cursor,
  );
}

function applyIncomingConversation(
  state: InterfaceChatThreadState,
  conversation: CodexConversation,
  streamCursor: ConversationStreamCursor = state.streamCursor,
): InterfaceChatThreadState {
  const filteredConversation = filterCodexConversationForChat(conversation);
  const nextConversation = reuseStableConversationEvents(
    state.conversation,
    filteredConversation,
  );
  const pendingReconciliation = reconcilePendingUserMessagesAgainstEvents(
    state.pendingUserMessages,
    nextConversation.events,
  );
  const pendingUserMessages = pendingReconciliation.pendingUserMessages;
  const currentTurnFocusAnchorId = resolveCurrentTurnFocusAnchorId(
    state.turnFocusAnchorAliases,
    state.pendingUserMessages,
  );
  const turnFocusAnchorAliases = extendTurnFocusAnchorAliases(
    state.turnFocusAnchorAliases,
    pendingReconciliation.providerEventAliases,
    currentTurnFocusAnchorId,
  );
  if (
    state.conversation === nextConversation &&
    state.pendingUserMessages === pendingUserMessages &&
    state.turnFocusAnchorAliases === turnFocusAnchorAliases &&
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
    turnFocusAnchorAliases,
    loading: false,
    error: null,
    streamCursor,
  };
}

function applyCodexConversationDelta(
  state: InterfaceChatThreadState,
  delta: CodexConversationDeltaPayload,
  generation: number,
): InterfaceChatThreadState {
  if (state.awaitingSnapshot) {
    return state;
  }
  const accepted = acceptConversationEnvelope(
    state.streamCursor,
    {
      requestId: delta.request_id,
      conversationId: delta.conversation_id,
      revision: delta.revision,
      baseRevision: delta.base_revision,
      generation,
      kind: "delta",
    },
    conversationIdentity(state.conversation),
  );
  if (accepted.gap) {
    return {
      ...state,
      awaitingSnapshot: true,
      resyncToken: state.resyncToken + 1,
    };
  }
  if (!accepted.accepted || !accepted.sameConversation) {
    return state;
  }
  const baseConversation = state.conversation ?? {
    available: delta.available ?? false,
    reason: delta.reason,
    source: delta.source,
    path: delta.path,
    session_id: delta.session_id,
    cwd: delta.cwd,
    updated_at: delta.updated_at,
    activity: delta.activity ?? undefined,
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
  const nextConversation = {
    ...baseConversation,
    available: delta.available ?? baseConversation.available,
    reason: delta.reason ?? baseConversation.reason,
    source: delta.source ?? baseConversation.source,
    path: delta.path ?? baseConversation.path,
    session_id: delta.session_id ?? baseConversation.session_id,
    cwd: delta.cwd ?? baseConversation.cwd,
    updated_at: delta.updated_at ?? baseConversation.updated_at,
    activity:
      delta.activity !== undefined
        ? (delta.activity ?? undefined)
        : baseConversation.activity,
    events: nextEvents,
  };
  return applyIncomingConversation(state, nextConversation, accepted.cursor);
}

function codexDeltaMetadataChanged(
  baseConversation: CodexConversation,
  delta: CodexConversationDeltaPayload,
) {
  return (
    (delta.available !== undefined &&
      delta.available !== baseConversation.available) ||
    (delta.reason !== undefined && delta.reason !== baseConversation.reason) ||
    (delta.source !== undefined && delta.source !== baseConversation.source) ||
    (delta.path !== undefined && delta.path !== baseConversation.path) ||
    (delta.session_id !== undefined &&
      delta.session_id !== baseConversation.session_id) ||
    (delta.cwd !== undefined && delta.cwd !== baseConversation.cwd) ||
    (delta.updated_at !== undefined &&
      delta.updated_at !== baseConversation.updated_at) ||
    (delta.activity !== undefined &&
      !providerActivitiesEqual(
        baseConversation.activity,
        delta.activity ?? undefined,
      ))
  );
}

function applyCodexConversationSyncStatus(
  state: InterfaceChatThreadState,
  status: CodexConversationSyncStatusPayload,
  generation: number,
): InterfaceChatThreadState {
  const accepted = acceptConversationEnvelope(state.streamCursor, {
    requestId: status.request_id,
    conversationId: status.conversation_id,
    revision: status.revision,
    generation,
    kind: "sync",
  });
  if (!accepted.accepted) {
    return state;
  }
  // Sync is transport availability only. It cannot mutate Activity or visible
  // history; the next revisioned snapshot owns that replacement.
  return {
    ...state,
    loading: status.state === "syncing" && !state.conversation?.events.length,
    error: null,
    streamCursor: accepted.cursor,
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
  let changed =
    previousConversation.events.length !== nextConversation.events.length;
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
  if (
    !changed &&
    codexConversationMetadataEqual(previousConversation, nextConversation)
  ) {
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
    providerActivitiesEqual(left.activity, right.activity)
  );
}

function extendTurnFocusAnchorAliases(
  current: ReadonlyMap<string, string>,
  additions: ProviderEventTurnFocusAlias[],
  currentTurnFocusAnchorId: string | undefined,
) {
  if (!currentTurnFocusAnchorId) {
    return current.size === 0 ? current : new Map<string, string>();
  }
  let providerEventId: string | undefined;
  current.forEach((localPendingId, candidateProviderEventId) => {
    if (localPendingId === currentTurnFocusAnchorId) {
      providerEventId = candidateProviderEventId;
    }
  });
  additions.forEach((addition) => {
    if (addition.localPendingId === currentTurnFocusAnchorId) {
      providerEventId = addition.providerEventId;
    }
  });
  if (!providerEventId) {
    return current.size === 0 ? current : new Map<string, string>();
  }
  if (
    current.size === 1 &&
    current.get(providerEventId) === currentTurnFocusAnchorId
  ) {
    return current;
  }
  return new Map([[providerEventId, currentTurnFocusAnchorId]]);
}

function resolveCurrentTurnFocusAnchorId(
  currentAliases: ReadonlyMap<string, string>,
  pendingUserMessages: PendingUserMessage[],
) {
  let currentAlias: string | undefined;
  currentAliases.forEach((localPendingId) => {
    currentAlias = localPendingId;
  });
  return (
    currentAlias ?? pendingUserMessages[pendingUserMessages.length - 1]?.id
  );
}

function codexEventsEqual(
  left: CodexConversation["events"][number],
  right: CodexConversation["events"][number],
) {
  return (
    left === right ||
    (left.id === right.id &&
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
      fileChangesEqual(left.file_changes, right.file_changes) &&
      planStepsEqual(left.plan, right.plan))
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

function fileChangesEqual(
  left?: CodexConversation["events"][number]["file_changes"],
  right?: CodexConversation["events"][number]["file_changes"],
) {
  if (left === right) {
    return true;
  }
  if (!left || !right || left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    const leftChange = left[index];
    const rightChange = right[index];
    if (
      leftChange?.path !== rightChange?.path ||
      leftChange?.move_path !== rightChange?.move_path ||
      leftChange?.operation !== rightChange?.operation ||
      leftChange?.additions !== rightChange?.additions ||
      leftChange?.deletions !== rightChange?.deletions
    ) {
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

export function useInterfaceChatSession({
  serverId,
  agentId,
  conversationScopeKey,
  agentInfo,
  connectionState,
  screenFocused,
}: UseInterfaceChatSessionInput) {
  const composerCacheKey = interfaceChatSessionCacheKey(
    serverId,
    agentId,
    conversationScopeKey,
  );
  const cacheKey = composerCacheKey;
  const [threadState, dispatchThread] = useReducer(
    interfaceChatThreadReducer,
    cacheKey,
    initialInterfaceChatThreadState,
  );
  const streamGenerationRef = useRef(0);
  const [draftState, setDraftState] = useState<KeyedState<string>>(() => ({
    cacheKey: composerCacheKey,
    value: draftCache.get(composerCacheKey) ?? "",
  }));
  const [attachmentsState, setAttachmentsState] = useState<
    KeyedState<ComposerAttachment[]>
  >(() => ({
    cacheKey: composerCacheKey,
    value: attachmentCache.get(composerCacheKey) ?? [],
  }));
  const conversation =
    threadState.cacheKey === cacheKey ? threadState.conversation : null;
  const loading =
    threadState.cacheKey === cacheKey ? threadState.loading : true;
  const error = threadState.cacheKey === cacheKey ? threadState.error : null;
  const draft =
    draftState.cacheKey === composerCacheKey
      ? draftState.value
      : (draftCache.get(composerCacheKey) ?? "");
  const attachments =
    attachmentsState.cacheKey === composerCacheKey
      ? attachmentsState.value
      : (attachmentCache.get(composerCacheKey) ?? []);
  const pendingUserMessages =
    threadState.cacheKey === composerCacheKey
      ? threadState.pendingUserMessages
      : [];
  const resyncToken =
    threadState.cacheKey === cacheKey ? threadState.resyncToken : 0;
  const subscriptionGeneration =
    threadState.cacheKey === cacheKey
      ? (threadState.streamCursor.generation ?? 0)
      : 0;
  const visiblePendingUserMessages = pendingUserMessages;
  const presentationCacheKey = `${cacheKey}:${
    conversation?.session_id || conversation?.path || ""
  }`;

  const writeDraft = useCallback(
    (nextDraft: string) => {
      // The native input is cleared imperatively when a live send starts.
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
            : (attachmentCache.get(composerCacheKey) ?? []);
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

  const addPendingUserMessage = useCallback(
    (message: PendingUserMessageInput) => {
      const id = `pending:${Date.now().toString(36)}:${Math.random().toString(36).slice(2, 8)}`;
      dispatchThread({
        type: "add_pending_user_message",
        message: {
          ...message,
          id,
          createdAt: new Date().toISOString(),
        },
      });
      return id;
    },
    [],
  );

  const beginPendingUserMessageAttempt = useCallback(
    (id: string, attempt: PendingUserMessageAttempt) => {
      dispatchThread({
        type: "begin_pending_user_message_attempt",
        id,
        ...attempt,
      });
    },
    [],
  );

  const rejectPendingUserMessage = useCallback(
    (id: string, rejection: PendingUserMessageRejection) => {
      dispatchThread({
        type: "reject_pending_user_message",
        id,
        ...rejection,
      });
    },
    [],
  );

  useEffect(() => {
    dispatchThread({
      type: "cache_key_changed",
      cacheKey,
    });
  }, [cacheKey]);

  useEffect(() => {
    if (
      !screenFocused ||
      connectionState !== "connected" ||
      !serverId ||
      !agentId
    ) {
      return;
    }
    const generation = streamGenerationRef.current + 1;
    streamGenerationRef.current = generation;
    dispatchThread({ type: "stream_start", generation });
    let active = true;
    const unsubscribe = wsClient.subscribeCodexConversation(
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
          if (!active) return;
          dispatchThread({
            type: "snapshot",
            payload,
            generation,
          });
        },
        onDelta: (payload) => {
          if (!active) return;
          dispatchThread({
            type: "delta",
            delta: payload,
            generation,
          });
        },
        onSyncStatus: (payload) => {
          if (!active) return;
          dispatchThread({
            type: "sync_status",
            status: payload,
            generation,
          });
        },
        onError: (nextError) => {
          if (!active) return;
          dispatchThread({
            type: "stream_error",
            error: interfaceConversationStreamErrorMessage(nextError),
            generation,
          });
        },
      },
    );
    return () => {
      active = false;
      unsubscribe();
    };
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
    resyncToken,
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

  return {
    cacheKey: presentationCacheKey,
    conversation,
    loading,
    error,
    draft,
    setDraft,
    restoreDraft,
    attachments,
    setAttachments,
    pendingUserMessages: visiblePendingUserMessages,
    subscriptionGeneration,
    turnFocusAnchorAliases:
      threadState.cacheKey === cacheKey
        ? threadState.turnFocusAnchorAliases
        : undefined,
    addPendingUserMessage,
    beginPendingUserMessageAttempt,
    rejectPendingUserMessage,
  };
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
    Boolean(
      event.plan?.some((step, index) => step.step !== nextPlan?.[index]?.step),
    );

  if (
    !body &&
    !input &&
    !output &&
    !explanation &&
    !command &&
    !title &&
    !toolName &&
    !event.files?.length &&
    !event.file_changes?.length &&
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
  const text = normalizeInstructionContextText(value);
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

function normalizeInstructionContextText(value: string) {
  return value
    .replace(INSTRUCTION_CONTEXT_ATTACHMENT_TAG_RE, "")
    .replace(/\s+/g, " ")
    .trim();
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
      stripped =
        `${stripped.slice(0, best.start)}\n${stripped.slice(best.end)}`.trim();
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

function markedTextRange(
  openMarker: string,
  closeMarker: string,
  value: string,
) {
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
