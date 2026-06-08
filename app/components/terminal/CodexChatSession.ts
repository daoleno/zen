import {
  useCallback,
  useEffect,
  useMemo,
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
  type CodexConversationSyncStatusPayload,
} from "../../services/websocket";
import type { AgentStatus } from "../../constants/tokens";

const PENDING_SLASH_COMMAND_MAX_AGE_MS = 120_000;
const PENDING_SLASH_COMMAND_SETTLED_MAX_AGE_MS = 45_000;
const PENDING_USER_MESSAGE_MAX_AGE_MS = 45_000;
const DRAFT_REPLAY_SUPPRESSION_MS = 1_800;
const ATTACHMENT_TAG_RE = /<zen_attachments>\s*([\s\S]*?)\s*<\/zen_attachments>/i;

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

type RecentlyClearedDraft = {
  cacheKey: string;
  text: string;
  clearedAt: number;
};

export type ComposerAttachment = UploadedAttachment & {
  id: string;
};

export type PendingUserMessage = {
  id: string;
  body: string;
  sentText: string;
  attachments: Array<Pick<ComposerAttachment, "name" | "path">>;
  createdAt: string;
  confirmedAt?: string;
  confirmedEventId?: string;
  createdAfterMaxSeq?: number;
  createdAfterEventIds?: string[];
};

export type PendingUserMessageInput = Omit<PendingUserMessage, "id" | "createdAt">;

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

type CodexChatThreadState = {
  cacheKey: string;
  conversation: CodexConversation | null;
  localChatState: CodexChatLocalState;
  loading: boolean;
  error: string | null;
  pendingUserMessages: PendingUserMessage[];
  pendingSlashCommands: PendingSlashCommand[];
};

type CodexChatThreadAction =
  | { type: "cache_key_changed"; cacheKey: string }
  | { type: "stream_start" }
  | { type: "snapshot"; conversation: CodexConversation }
  | { type: "delta"; delta: CodexConversationDeltaPayload }
  | { type: "sync_status"; status: CodexConversationSyncStatusPayload }
  | { type: "stream_error"; error: string }
  | { type: "reset_for_new_chat"; boundary: NewChatBoundary }
  | { type: "mark_new_chat_ready" }
  | { type: "mark_new_chat_message_started" }
  | { type: "add_pending_user_message"; message: PendingUserMessage }
  | { type: "remove_pending_user_message"; id: string }
  | { type: "prune_pending_user_messages"; now: number }
  | { type: "add_pending_slash_command"; command: PendingSlashCommand }
  | { type: "settle_pending_slash_command"; id: string; status: PendingSlashCommand["status"]; completedAt: string }
  | { type: "remove_pending_slash_command"; id: string }
  | { type: "prune_pending_slash_commands"; now: number };

function initialCodexChatThreadState(cacheKey: string): CodexChatThreadState {
  return {
    cacheKey,
    conversation: conversationCache.get(cacheKey) ?? null,
    localChatState: localChatStateCache.get(cacheKey) ?? "idle",
    loading: !conversationCache.has(cacheKey),
    error: null,
    pendingUserMessages: [],
    pendingSlashCommands: [],
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
      return {
        ...state,
        loading: !state.conversation?.events.length,
        error: null,
      };
    case "snapshot":
      return applyIncomingConversation(state, action.conversation);
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
    case "reset_for_new_chat": {
      newChatBoundaryCache.set(state.cacheKey, action.boundary);
      conversationCache.delete(state.cacheKey);
      localChatStateCache.set(state.cacheKey, "starting-new-chat");
      return {
        ...state,
        conversation: null,
        loading: false,
        error: null,
        localChatState: "starting-new-chat",
        pendingUserMessages: [],
        pendingSlashCommands: [],
      };
    }
    case "mark_new_chat_ready":
      if (
        state.localChatState !== "starting-new-chat" &&
        state.localChatState !== "new-chat-ready"
      ) {
        return state;
      }
      localChatStateCache.set(state.cacheKey, "new-chat-ready");
      return {
        ...state,
        loading: false,
        localChatState: "new-chat-ready",
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
    case "add_pending_user_message":
      return {
        ...state,
        pendingUserMessages: [
          ...state.pendingUserMessages,
          action.message,
        ].slice(-12),
      };
    case "remove_pending_user_message":
      return {
        ...state,
        pendingUserMessages: state.pendingUserMessages.filter(
          (message) => message.id !== action.id,
        ),
      };
    case "prune_pending_user_messages":
      return {
        ...state,
        pendingUserMessages: state.pendingUserMessages.filter(
          (message) => !shouldPrunePendingUserMessage(message, action.now),
        ),
      };
    case "add_pending_slash_command":
      return {
        ...state,
        pendingSlashCommands: [
          ...state.pendingSlashCommands,
          action.command,
        ].slice(-12),
      };
    case "settle_pending_slash_command":
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

function applyIncomingConversation(
  state: CodexChatThreadState,
  conversation: CodexConversation,
): CodexChatThreadState {
  const boundary = newChatBoundaryCache.get(state.cacheKey);
  const filteredConversation = conversationForNewChatBoundary(
    conversation,
    boundary,
    state.localChatState === "starting-new-chat" ||
      state.localChatState === "new-chat-ready",
  );
  if (!filteredConversation) {
    return {
      ...state,
      loading: false,
      error: null,
    };
  }
  if (
    state.conversation?.events.length &&
    filteredConversation.events.length === 0 &&
    isTransientEmptyConversation(filteredConversation.reason)
  ) {
    return {
      ...state,
      loading: false,
      error: null,
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
  return {
    ...state,
    conversation: nextConversation,
    pendingUserMessages: reconcilePendingUserMessages(
      state.pendingUserMessages,
      nextConversation,
    ),
    localChatState,
    loading: false,
    error: null,
  };
}

function applyCodexConversationDelta(
  state: CodexChatThreadState,
  delta: CodexConversationDeltaPayload,
): CodexChatThreadState {
  const baseConversation = state.conversation ?? conversationCache.get(state.cacheKey) ?? {
    available: delta.available ?? false,
    reason: delta.reason,
    source: delta.source,
    path: delta.path,
    session_id: delta.session_id,
    cwd: delta.cwd,
    updated_at: delta.updated_at,
    active: delta.active,
    events: [],
  };
  const deleted = new Set(delta.deletes);
  const byId = new Map(baseConversation.events.map((event) => [event.id, event]));
  delta.upserts.forEach((event) => {
    byId.set(event.id, event);
    deleted.delete(event.id);
  });
  const nextEvents = baseConversation.events
    .filter((event) => !deleted.has(event.id))
    .map((event) => byId.get(event.id) ?? event);
  delta.upserts.forEach((event) => {
    if (!nextEvents.some((candidate) => candidate.id === event.id)) {
      nextEvents.push(event);
    }
  });
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
    events: nextEvents.sort((left, right) => left.seq - right.seq),
  };
  return applyIncomingConversation(state, nextConversation);
}

function applyCodexConversationSyncStatus(
  state: CodexChatThreadState,
  status: CodexConversationSyncStatusPayload,
): CodexChatThreadState {
  if (state.conversation?.events.length) {
    return {
      ...state,
      loading: false,
      error: null,
    };
  }
  const conversation: CodexConversation = {
    available: false,
    reason: status.reason,
    events: [],
  };
  return {
    ...state,
    conversation,
    loading: status.state === "syncing",
    error: null,
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
    if (previousEvent && codexEventFingerprint(previousEvent) === codexEventFingerprint(event)) {
      if (previousConversation.events[index] !== previousEvent) {
        changed = true;
      }
      return previousEvent;
    }
    changed = true;
    return event;
  });
  if (!changed) {
    return previousConversation;
  }
  return {
    ...nextConversation,
    events,
  };
}

function reconcilePendingUserMessages(
  pendingUserMessages: PendingUserMessage[],
  conversation: CodexConversation,
): PendingUserMessage[] {
  if (pendingUserMessages.length === 0 || conversation.events.length === 0) {
    return pendingUserMessages;
  }
  const userEvents = conversation.events.filter(
    (event) => event.kind === "user_message",
  );
  if (userEvents.length === 0) {
    return pendingUserMessages;
  }
  const usedEventIds = new Set(
    pendingUserMessages
      .map((message) => message.confirmedEventId)
      .filter((id): id is string => Boolean(id)),
  );
  const reconciled: PendingUserMessage[] = [];
  for (const message of pendingUserMessages) {
    if (message.confirmedEventId) {
      continue;
    }
    const previousEventIds = new Set(message.createdAfterEventIds ?? []);
    const sentText = comparableUserMessageText(message.sentText);
    const body = comparableUserMessageText(message.body);
    const confirmedEvent = userEvents.find((event) => {
      if (!event.id || usedEventIds.has(event.id) || previousEventIds.has(event.id)) {
        return false;
      }
      if (
        typeof message.createdAfterMaxSeq === "number" &&
        Number.isFinite(message.createdAfterMaxSeq) &&
        typeof event.seq === "number" &&
        event.seq <= message.createdAfterMaxSeq
      ) {
        return false;
      }
      const eventText = comparableUserMessageText(event.body || "");
      return Boolean(
        eventText &&
        ((sentText && eventText === sentText) ||
          (body && eventText === body)),
      );
    });
    if (!confirmedEvent) {
      reconciled.push(message);
      continue;
    }
    usedEventIds.add(confirmedEvent.id);
  }
  return reconciled;
}

function codexEventFingerprint(event: CodexConversation["events"][number]) {
  return JSON.stringify(event);
}

export function useCodexChatSession({
  serverId,
  agentId,
  conversationScopeKey,
  agentInfo,
  connectionState,
  screenFocused,
}: UseCodexChatSessionInput) {
  const scopedAgentId = conversationScopeKey
    ? `${agentId}:${conversationScopeKey}`
    : agentId;
  const composerCacheKey = `${serverId}:${scopedAgentId}`;
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
  const recentlyClearedDraftRef = useRef<RecentlyClearedDraft | null>(null);
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
    : [];
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

  const setDraft = useCallback(
    (nextDraft: string) => {
      const currentDraft = draftCache.get(composerCacheKey) ?? "";
      const normalizedDraft = normalizeDraftAfterRecentClear(
        nextDraft,
        composerCacheKey,
        recentlyClearedDraftRef.current,
      );
      if (!normalizedDraft && currentDraft) {
        recentlyClearedDraftRef.current = {
          cacheKey: composerCacheKey,
          text: currentDraft,
          clearedAt: Date.now(),
        };
      }
      if (normalizedDraft) {
        draftCache.set(composerCacheKey, normalizedDraft);
      } else {
        draftCache.delete(composerCacheKey);
      }
      setDraftState({
        cacheKey: composerCacheKey,
        value: normalizedDraft,
      });
    },
    [composerCacheKey],
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
    dispatchThread({
      type: "add_pending_user_message",
      message: {
        ...message,
        id,
        createdAt: new Date().toISOString(),
        createdAfterMaxSeq: maxConversationEventSeq(baseConversation),
        createdAfterEventIds: Array.from(conversationEventIdSet(baseConversation)),
      },
    });
    return id;
  }, [cacheKey, conversation]);

  const removePendingUserMessage = useCallback((id: string) => {
    dispatchThread({
      type: "remove_pending_user_message",
      id,
    });
  }, []);

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

  const resetForNewChat = useCallback(() => {
    const baseConversation = conversation ?? conversationCache.get(cacheKey) ?? null;
    dispatchThread({
      type: "reset_for_new_chat",
      boundary: {
        previousEventIds: conversationEventIdSet(baseConversation),
        previousMaxSeq: maxConversationEventSeq(baseConversation),
        startedAtMs: Date.now(),
      },
    });
  }, [
    cacheKey,
    conversation,
  ]);

  const markNewChatReady = useCallback(() => {
    dispatchThread({ type: "mark_new_chat_ready" });
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
      },
      {
        onSnapshot: (payload) => {
          dispatchThread({
            type: "snapshot",
            conversation: payload.conversation,
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
    const nextPruneAt = pendingUserMessages.reduce((soonest, message) => {
      const createdAt = new Date(message.createdAt).getTime();
      const maxAgeAt = Number.isFinite(createdAt)
        ? createdAt + PENDING_USER_MESSAGE_MAX_AGE_MS
        : now + PENDING_USER_MESSAGE_MAX_AGE_MS;
      return Math.min(soonest, maxAgeAt);
    }, Number.POSITIVE_INFINITY);

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
    attachments,
    setAttachments,
    pendingUserMessages: visiblePendingUserMessages,
    pendingSlashCommands: visiblePendingSlashCommands,
    addPendingUserMessage,
    removePendingUserMessage,
    addPendingSlashCommand,
    settlePendingSlashCommand,
    removePendingSlashCommand,
    resetForNewChat,
    markNewChatReady,
    markNewChatMessageStarted,
  };
}

function comparableUserMessageText(value: string) {
  return value
    .replace(ATTACHMENT_TAG_RE, "")
    .replace(/\s+/g, " ")
    .trim();
}

function normalizeDraftAfterRecentClear(
  nextDraft: string,
  cacheKey: string,
  recent: RecentlyClearedDraft | null,
) {
  if (!recent || recent.cacheKey !== cacheKey || !recent.text) {
    return nextDraft;
  }
  if (Date.now() - recent.clearedAt > DRAFT_REPLAY_SUPPRESSION_MS) {
    return nextDraft;
  }
  if (nextDraft === recent.text) {
    return "";
  }
  const replayIndex = nextDraft.indexOf(recent.text);
  if (replayIndex < 0) {
    return nextDraft;
  }
  return `${nextDraft.slice(0, replayIndex)}${nextDraft.slice(
    replayIndex + recent.text.length,
  )}`;
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
  const createdAt = new Date(message.createdAt).getTime();
  return (
    Number.isFinite(createdAt) &&
    now - createdAt > PENDING_USER_MESSAGE_MAX_AGE_MS
  );
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
  if (isSlashCommandInvocationEvent(event)) {
    return null;
  }

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

function isSlashCommandInvocationEvent(event: CodexConversation["events"][number]) {
  if (event.kind !== "user_message") {
    return false;
  }
  const text = comparableUserMessageText(event.body || "");
  return /^\/[a-z][a-z0-9-]*(?:\s|$)/.test(text);
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
