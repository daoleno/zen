import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type SetStateAction,
} from "react";
import type { Agent, ConnectionState } from "../../store/agents";
import type { CodexConversation } from "../../services/codexConversation";
import type { UploadedAttachment } from "../../services/uploads";
import { wsClient } from "../../services/websocket";
import { cleanCodexTerminalOutputText } from "./codexTerminalOutputText";

const POLL_INTERVAL_MS = 1800;
const PENDING_USER_MESSAGE_MAX_AGE_MS = 120_000;
const PENDING_ASSISTANT_MESSAGE_MAX_AGE_MS = 180_000;
const PENDING_ASSISTANT_SETTLED_MAX_AGE_MS = 30_000;
const ATTACHMENT_TAG_RE = /<zen_attachments>\s*([\s\S]*?)\s*<\/zen_attachments>/i;

type RefreshInFlight = {
  baseKey: string;
  requestSeq: number;
};

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

export type PendingUserMessage = {
  id: string;
  body: string;
  sentText: string;
  attachments: Array<Pick<ComposerAttachment, "name" | "path">>;
  createdAt: string;
};

export type PendingUserMessageInput = Omit<PendingUserMessage, "id" | "createdAt">;

export type PendingAssistantMessage = {
  id: string;
  body: string;
  sentText: string;
  baselineLines: string[];
  createdAt: string;
  settledAt?: string;
};

interface UseCodexChatSessionInput {
  serverId: string;
  agentId: string;
  agent?: Agent;
  connectionState: ConnectionState;
  screenFocused: boolean;
}

const conversationCache = new Map<string, CodexConversation>();
const conversationFingerprintCache = new Map<string, string>();
const draftCache = new Map<string, string>();
const attachmentCache = new Map<string, ComposerAttachment[]>();
const localChatStateCache = new Map<string, CodexChatLocalState>();
const newChatBoundaryCache = new Map<string, NewChatBoundary>();
const pendingAssistantMessageCache = new Map<string, PendingAssistantMessage[]>();

export function useCodexChatSession({
  serverId,
  agentId,
  agent,
  connectionState,
  screenFocused,
}: UseCodexChatSessionInput) {
  const sessionStartedAt = normalizeSessionTimestamp(agent?.started_at);
  const agentProcessId = normalizeProcessID(agent?.process_id);
  const cacheKey = `${serverId}:${agentId}:${sessionStartedAt || ""}:${agentProcessId || ""}`;
  const composerCacheKey = `${serverId}:${agentId}`;
  const agentConversationCachePrefix = `${serverId}:${agentId}:`;
  const requestSeqRef = useRef(0);
  const refreshInFlightRef = useRef<RefreshInFlight | null>(null);
  const [conversationState, setConversationState] = useState<
    KeyedState<CodexConversation | null>
  >(
    () => ({
      cacheKey,
      value: conversationCache.get(cacheKey) ?? null,
    }),
  );
  const [loading, setLoading] = useState(false);
  const [localChatStateState, setLocalChatStateState] = useState<
    KeyedState<CodexChatLocalState>
  >(
    () => ({
      cacheKey: composerCacheKey,
      value: localChatStateCache.get(composerCacheKey) ?? "idle",
    }),
  );
  const [error, setError] = useState<string | null>(null);
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
  const [pendingUserMessagesState, setPendingUserMessages] = useState<
    KeyedState<PendingUserMessage[]>
  >(
    () => ({
      cacheKey: composerCacheKey,
      value: [],
    }),
  );
  const [pendingAssistantMessagesState, setPendingAssistantMessages] = useState<
    KeyedState<PendingAssistantMessage[]>
  >(
    () => ({
      cacheKey: composerCacheKey,
      value: pendingAssistantMessageCache.get(composerCacheKey) ?? [],
    }),
  );
  const conversation =
    conversationState.cacheKey === cacheKey
      ? conversationState.value
      : conversationCache.get(cacheKey) ?? null;
  const localChatState =
    localChatStateState.cacheKey === composerCacheKey
      ? localChatStateState.value
      : localChatStateCache.get(composerCacheKey) ?? "idle";
  const draft =
    draftState.cacheKey === composerCacheKey
      ? draftState.value
      : draftCache.get(composerCacheKey) ?? "";
  const attachments =
    attachmentsState.cacheKey === composerCacheKey
      ? attachmentsState.value
      : attachmentCache.get(composerCacheKey) ?? [];
  const pendingUserMessages =
    pendingUserMessagesState.cacheKey === composerCacheKey
      ? pendingUserMessagesState.value
      : [];
  const pendingAssistantMessages =
    pendingAssistantMessagesState.cacheKey === composerCacheKey
      ? pendingAssistantMessagesState.value
      : pendingAssistantMessageCache.get(composerCacheKey) ?? [];
  const boundary = newChatBoundaryCache.get(composerCacheKey);
  const visiblePendingUserMessages = useMemo(
    () => filterVisiblePendingUserMessages(
      pendingUserMessages,
      conversation,
      boundary,
    ),
    [boundary, conversation, pendingUserMessages],
  );
  const visiblePendingAssistantMessages = useMemo(
    () => filterVisiblePendingAssistantMessages(
      pendingAssistantMessages,
      conversation,
      boundary,
    ),
    [boundary, conversation, pendingAssistantMessages],
  );
  const presentationCacheKey = `${cacheKey}:${
    conversation?.session_id || conversation?.path || ""
  }`;

  const setConversation = useCallback(
    (nextConversation: CodexConversation | null) => {
      if (nextConversation) {
        conversationCache.set(cacheKey, nextConversation);
        conversationFingerprintCache.set(
          cacheKey,
          codexConversationFingerprint(nextConversation),
        );
      } else {
        conversationCache.delete(cacheKey);
        conversationFingerprintCache.delete(cacheKey);
      }
      setConversationState({
        cacheKey,
        value: nextConversation,
      });
    },
    [cacheKey],
  );

  const setLocalChatState = useCallback(
    (nextState: CodexChatLocalState) => {
      if (nextState === "idle") {
        localChatStateCache.delete(composerCacheKey);
      } else {
        localChatStateCache.set(composerCacheKey, nextState);
      }
      setLocalChatStateState({
        cacheKey: composerCacheKey,
        value: nextState,
      });
    },
    [composerCacheKey],
  );

  const setDraft = useCallback(
    (nextDraft: string) => {
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
    setPendingUserMessages((current) => {
      const currentMessages =
        current.cacheKey === composerCacheKey ? current.value : [];
      return {
        cacheKey: composerCacheKey,
        value: [
          ...currentMessages,
          {
            ...message,
            id,
            createdAt: new Date().toISOString(),
          },
        ].slice(-6),
      };
    });
    return id;
  }, [composerCacheKey]);

  const removePendingUserMessage = useCallback((id: string) => {
    setPendingUserMessages((current) => {
      if (current.cacheKey !== composerCacheKey) {
        return current;
      }
      return {
        cacheKey: composerCacheKey,
        value: current.value.filter((message) => message.id !== id),
      };
    });
  }, [composerCacheKey]);

  const setPendingAssistantMessageList = useCallback(
    (nextValue: SetStateAction<PendingAssistantMessage[]>) => {
      setPendingAssistantMessages((current) => {
        const currentMessages =
          current.cacheKey === composerCacheKey
            ? current.value
            : pendingAssistantMessageCache.get(composerCacheKey) ?? [];
        const nextMessages =
          typeof nextValue === "function"
            ? nextValue(currentMessages)
            : nextValue;
        const bounded = nextMessages.slice(-1);
        if (bounded.length > 0) {
          pendingAssistantMessageCache.set(composerCacheKey, bounded);
        } else {
          pendingAssistantMessageCache.delete(composerCacheKey);
        }
        return {
          cacheKey: composerCacheKey,
          value: bounded,
        };
      });
    },
    [composerCacheKey],
  );

  const startPendingAssistantMessage = useCallback(
    (sentText: string, baselineLines: string[]) => {
      const id = `pending-assistant:${Date.now().toString(36)}:${Math.random().toString(36).slice(2, 8)}`;
      setPendingAssistantMessageList(() => [
        {
          id,
          body: "",
          sentText,
          baselineLines: normalizeTerminalLines(baselineLines),
          createdAt: new Date().toISOString(),
        },
      ]);
      return id;
    },
    [setPendingAssistantMessageList],
  );

  const clearPendingAssistantMessages = useCallback(() => {
    setPendingAssistantMessageList([]);
  }, [setPendingAssistantMessageList]);

  const resetForNewChat = useCallback(() => {
    const baseConversation = conversation ?? conversationCache.get(cacheKey) ?? null;
    newChatBoundaryCache.set(composerCacheKey, {
      previousEventIds: conversationEventIdSet(baseConversation),
      previousMaxSeq: maxConversationEventSeq(baseConversation),
      startedAtMs: Date.now(),
    });
    requestSeqRef.current += 1;
    refreshInFlightRef.current = null;
    clearConversationCachesForAgent(agentConversationCachePrefix);
    setConversationState({
      cacheKey,
      value: null,
    });
    setPendingUserMessages({
      cacheKey: composerCacheKey,
      value: [],
    });
    clearPendingAssistantMessages();
    setError(null);
    setLoading(false);
    setLocalChatState("starting-new-chat");
  }, [
    agentConversationCachePrefix,
    cacheKey,
    composerCacheKey,
    conversation,
    clearPendingAssistantMessages,
    setLocalChatState,
  ]);

  const markNewChatReady = useCallback(() => {
    const currentState = localChatStateCache.get(composerCacheKey);
    if (currentState !== "starting-new-chat" && currentState !== "new-chat-ready") {
      return;
    }
    setLoading(false);
    setLocalChatState("new-chat-ready");
  }, [composerCacheKey, setLocalChatState]);

  const markNewChatMessageStarted = useCallback(() => {
    const currentState = localChatStateCache.get(composerCacheKey) ?? "idle";
    if (currentState === "idle") {
      return;
    }
    setLoading(false);
    setLocalChatState("idle");
  }, [composerCacheKey, setLocalChatState]);

  const refreshConversation = useCallback(
    async (showLoading: boolean = false) => {
      if (!serverId || !agentId || connectionState !== "connected") {
        requestSeqRef.current += 1;
        refreshInFlightRef.current = null;
        setLoading(false);
        return;
      }
      const requestBaseKey = cacheKey;
      if (refreshInFlightRef.current?.baseKey === requestBaseKey) {
        if (shouldShowRefreshLoading(showLoading, requestBaseKey, composerCacheKey)) {
          setLoading(true);
        }
        return;
      }

      const requestSeq = requestSeqRef.current + 1;
      requestSeqRef.current = requestSeq;
      refreshInFlightRef.current = { baseKey: requestBaseKey, requestSeq };
      if (shouldShowRefreshLoading(showLoading, requestBaseKey, composerCacheKey)) {
        setLoading(true);
      }
      try {
        const nextConversation = await wsClient.getCodexConversation(serverId, {
          targetId: agentId,
          cwd: agent?.cwd,
          command: agent?.command,
          name: agent?.name,
          startedAt: agent?.started_at,
          processId: agent?.process_id,
        });
        if (requestSeqRef.current !== requestSeq) {
          return;
        }
        const localState = localChatStateCache.get(composerCacheKey) ?? "idle";
        const waitingForNewChat =
          localState === "starting-new-chat" || localState === "new-chat-ready";
        const newChatBoundary = newChatBoundaryCache.get(composerCacheKey);
        const nextConversationForChat = conversationForNewChatBoundary(
          nextConversation,
          newChatBoundary,
          waitingForNewChat,
        );
        if (!nextConversationForChat) {
          setError(null);
          return;
        }
        const nextFingerprint = codexConversationFingerprint(nextConversationForChat);
        if (conversationFingerprintCache.get(requestBaseKey) === nextFingerprint) {
          setError(null);
          return;
        }
        setConversation(nextConversationForChat);
        if (
          localState === "starting-new-chat" ||
          localState === "new-chat-ready"
        ) {
          setLocalChatState(nextConversationForChat.events.length === 0 ? "new-chat-ready" : "idle");
        }
        setError(null);
      } catch (err: any) {
        if (requestSeqRef.current !== requestSeq) {
          return;
        }
        setError(err?.message || "Could not load Codex conversation.");
      } finally {
        if (refreshInFlightRef.current?.requestSeq === requestSeq) {
          refreshInFlightRef.current = null;
        }
        if (requestSeqRef.current === requestSeq) {
          setLoading(false);
        }
      }
    },
    [
      agent?.command,
      agent?.cwd,
      agent?.name,
      agent?.process_id,
      agent?.started_at,
      agentId,
      cacheKey,
      composerCacheKey,
      connectionState,
      serverId,
      setConversation,
      setLocalChatState,
    ],
  );

  useEffect(() => {
    if (!screenFocused) {
      return;
    }
    void refreshConversation(true);
    const interval = setInterval(() => {
      void refreshConversation(false);
    }, POLL_INTERVAL_MS);
    return () => {
      requestSeqRef.current += 1;
      refreshInFlightRef.current = null;
      clearInterval(interval);
    };
  }, [refreshConversation, screenFocused]);

  useEffect(() => {
    setConversationState({
      cacheKey,
      value: conversationCache.get(cacheKey) ?? null,
    });
    setLocalChatStateState({
      cacheKey: composerCacheKey,
      value: localChatStateCache.get(composerCacheKey) ?? "idle",
    });
    setError(null);
    refreshInFlightRef.current = null;
  }, [cacheKey, composerCacheKey]);

  useEffect(() => {
    setDraftState({
      cacheKey: composerCacheKey,
      value: draftCache.get(composerCacheKey) ?? "",
    });
    setAttachmentsState({
      cacheKey: composerCacheKey,
      value: attachmentCache.get(composerCacheKey) ?? [],
    });
    setPendingUserMessages({
      cacheKey: composerCacheKey,
      value: [],
    });
    setPendingAssistantMessages({
      cacheKey: composerCacheKey,
      value: pendingAssistantMessageCache.get(composerCacheKey) ?? [],
    });
  }, [composerCacheKey]);

  useEffect(() => {
    if (pendingUserMessages.length === 0 || !conversation?.events.length) {
      return;
    }

    const confirmedUserMessages = new Set(
      conversation.events
        .filter((event) => event.kind === "user_message")
        .map((event) => comparableUserMessageText(event.body || ""))
        .filter(Boolean),
    );
    const now = Date.now();
    setPendingUserMessages((current) => {
      if (current.cacheKey !== composerCacheKey) {
        return current;
      }
      return {
        cacheKey: composerCacheKey,
        value: current.value.filter((message) => {
          const createdAt = new Date(message.createdAt).getTime();
          if (
            Number.isFinite(createdAt) &&
            now - createdAt > PENDING_USER_MESSAGE_MAX_AGE_MS
          ) {
            return false;
          }
          const sentText = comparableUserMessageText(message.sentText);
          const body = comparableUserMessageText(message.body);
          return !(
            (sentText && confirmedUserMessages.has(sentText)) ||
            (body && confirmedUserMessages.has(body))
          );
        }),
      };
    });
  }, [composerCacheKey, conversation?.events, pendingUserMessages.length]);

  useEffect(() => {
    if (
      pendingAssistantMessages.length === 0 ||
      !agent?.last_output_lines?.length ||
      localChatState !== "idle"
    ) {
      return;
    }

    const currentLines = normalizeTerminalLines(agent.last_output_lines);
    setPendingAssistantMessageList((current) =>
      current.map((message) => {
        const body = extractPendingAssistantText(
          currentLines,
          message.baselineLines,
          message.sentText,
        );
        const settled = body.trim().length > 0 && terminalOutputHasReturnedToPrompt(
          currentLines,
          message.sentText,
        );
        if (!body && !settled) {
          return message;
        }
        const settledAt = settled
          ? message.settledAt ?? new Date().toISOString()
          : message.settledAt;
        return {
          ...message,
          body: body || message.body,
          settledAt,
        };
      }),
    );
  }, [
    agent?.last_output_lines,
    localChatState,
    pendingAssistantMessages.length,
    setPendingAssistantMessageList,
  ]);

  useEffect(() => {
    if (pendingAssistantMessages.length === 0) {
      return;
    }
    const now = Date.now();
    setPendingAssistantMessageList((current) =>
      current.filter((message) => {
        if (isPendingAssistantConfirmed(message, conversation)) {
          return false;
        }
        const createdAt = new Date(message.createdAt).getTime();
        const settledAt = message.settledAt
          ? new Date(message.settledAt).getTime()
          : Number.NaN;
        if (
          Number.isFinite(settledAt) &&
          now - settledAt > PENDING_ASSISTANT_SETTLED_MAX_AGE_MS
        ) {
          return false;
        }
        return !Number.isFinite(createdAt) ||
          now - createdAt <= PENDING_ASSISTANT_MESSAGE_MAX_AGE_MS;
      }),
    );
  }, [
    conversation,
    pendingAssistantMessages.length,
    setPendingAssistantMessageList,
  ]);

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
    pendingAssistantMessages: visiblePendingAssistantMessages,
    addPendingUserMessage,
    removePendingUserMessage,
    startPendingAssistantMessage,
    clearPendingAssistantMessages,
    resetForNewChat,
    markNewChatReady,
    markNewChatMessageStarted,
    refreshConversation,
  };
}

function comparableUserMessageText(value: string) {
  return value
    .replace(ATTACHMENT_TAG_RE, "")
    .replace(/\s+/g, " ")
    .trim();
}

function codexConversationFingerprint(conversation: CodexConversation) {
  return JSON.stringify({
    available: conversation.available,
    reason: conversation.reason,
    source: conversation.source,
    path: conversation.path,
    session_id: conversation.session_id,
    cwd: conversation.cwd,
    updated_at: conversation.updated_at,
    active: conversation.active,
    events: conversation.events.map((event) => ({
      id: event.id,
      seq: event.seq,
      timestamp: event.timestamp,
      kind: event.kind,
      role: event.role,
      title: event.title,
      body: event.body,
      command: event.command,
      tool_name: event.tool_name,
      input: event.input,
      output: event.output,
      call_id: event.call_id,
      exit_code: event.exit_code,
      status: event.status,
      files: event.files,
      explanation: event.explanation,
      plan: event.plan,
      source: event.source,
    })),
  });
}

function filterVisiblePendingUserMessages(
  pendingUserMessages: PendingUserMessage[],
  conversation: CodexConversation | null,
  boundary?: NewChatBoundary,
) {
  if (pendingUserMessages.length === 0 || !conversation?.events.length) {
    return pendingUserMessages;
  }
  const confirmedUserMessages = new Set(
    conversation.events
      .filter((event) => event.kind === "user_message")
      .map((event) => comparableUserMessageText(event.body || ""))
      .filter(Boolean),
  );
  if (confirmedUserMessages.size === 0) {
    return pendingUserMessages;
  }
  return pendingUserMessages.filter((message) => {
    if (boundary && isPendingMessageBeforeNewChatBoundary(message.createdAt, boundary)) {
      return false;
    }
    const sentText = comparableUserMessageText(message.sentText);
    const body = comparableUserMessageText(message.body);
    return !(
      (sentText && confirmedUserMessages.has(sentText)) ||
      (body && confirmedUserMessages.has(body))
    );
  });
}

function filterVisiblePendingAssistantMessages(
  pendingAssistantMessages: PendingAssistantMessage[],
  conversation: CodexConversation | null,
  boundary?: NewChatBoundary,
) {
  if (pendingAssistantMessages.length === 0) {
    return pendingAssistantMessages;
  }
  return pendingAssistantMessages.filter(
    (message) =>
      message.body.trim().length > 0 &&
      !isPendingMessageBeforeNewChatBoundary(message.createdAt, boundary) &&
      !isPendingAssistantConfirmed(message, conversation),
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

function isPendingAssistantConfirmed(
  message: PendingAssistantMessage,
  conversation: CodexConversation | null,
) {
  const body = comparableAssistantMessageText(message.body);
  if (!body || !conversation?.events.length) {
    return false;
  }
  return conversation.events.some((event) => {
    if (event.kind !== "assistant_message" && event.kind !== "status") {
      return false;
    }
    const eventBody = comparableAssistantMessageText(event.body || "");
    return eventBody.includes(body) || body.includes(eventBody);
  });
}

function comparableAssistantMessageText(value: string) {
  return value
    .replace(/\s+/g, " ")
    .trim();
}

function extractPendingAssistantText(
  currentLines: string[],
  baselineLines: string[],
  sentText: string,
) {
  const deltaLines = terminalLineDelta(baselineLines, currentLines);
  const lines = deltaLines.length > 0 ? deltaLines : linesAfterPrompt(currentLines, sentText);
  return cleanCodexTerminalOutputText(lines.join("\n"), sentText);
}

function terminalLineDelta(beforeLines: string[], afterLines: string[]) {
  if (afterLines.length === 0) {
    return [];
  }
  if (beforeLines.length === 0) {
    return afterLines;
  }
  for (
    let overlap = Math.min(beforeLines.length, afterLines.length);
    overlap > 0;
    overlap -= 1
  ) {
    if (linesEqual(beforeLines.slice(-overlap), afterLines.slice(0, overlap))) {
      return afterLines.slice(overlap);
    }
  }
  const beforeTail = beforeLines[beforeLines.length - 1];
  const tailIndex = beforeTail ? afterLines.lastIndexOf(beforeTail) : -1;
  return tailIndex >= 0 ? afterLines.slice(tailIndex + 1) : [];
}

function linesAfterPrompt(lines: string[], sentText: string) {
  const promptText = sentText.trim();
  if (!promptText) {
    return [];
  }
  for (let index = lines.length - 1; index >= 0; index -= 1) {
    const line = lines[index]?.trim() || "";
    if (
      line === promptText ||
      line === `› ${promptText}` ||
      line === `> ${promptText}` ||
      line.endsWith(promptText)
    ) {
      return lines.slice(index + 1);
    }
  }
  return [];
}

function terminalOutputHasReturnedToPrompt(lines: string[], sentText: string) {
  const promptText = sentText.trim();
  if (!promptText) {
    return false;
  }
  const commandIndex = lastPromptCommandLineIndex(lines, promptText);
  if (commandIndex < 0) {
    return false;
  }
  return lines.slice(commandIndex + 1).some((line) =>
    isCodexReadyPromptLine(line),
  );
}

function lastPromptCommandLineIndex(lines: string[], promptText: string) {
  for (let index = lines.length - 1; index >= 0; index -= 1) {
    const line = lines[index]?.trim() || "";
    if (
      line === promptText ||
      line === `› ${promptText}` ||
      line === `> ${promptText}` ||
      line.endsWith(promptText)
    ) {
      return index;
    }
  }
  return -1;
}

function isCodexReadyPromptLine(line: string) {
  const text = line.trim();
  return text === "›" || text.startsWith("› ") || text === ">" || text.startsWith("> ");
}

function normalizeTerminalLines(lines: string[]) {
  return lines.map((line) => line.replace(/\r/g, "").trimEnd());
}

function linesEqual(left: string[], right: string[]) {
  if (left.length !== right.length) {
    return false;
  }
  return left.every((line, index) => line === right[index]);
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

function shouldShowRefreshLoading(
  showLoading: boolean,
  cacheKey: string,
  localStateKey: string,
) {
  return (
    showLoading &&
    !conversationCache.has(cacheKey) &&
    (localChatStateCache.get(localStateKey) ?? "idle") === "idle"
  );
}

function clearConversationCachesForAgent(cacheKeyPrefix: string) {
  for (const key of Array.from(conversationCache.keys())) {
    if (key.startsWith(cacheKeyPrefix)) {
      conversationCache.delete(key);
    }
  }
  for (const key of Array.from(conversationFingerprintCache.keys())) {
    if (key.startsWith(cacheKeyPrefix)) {
      conversationFingerprintCache.delete(key);
    }
  }
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

function normalizeSessionTimestamp(value: unknown): string {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? String(Math.round(value))
    : "";
}

function normalizeProcessID(value: unknown): string {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? String(Math.round(value))
    : "";
}
