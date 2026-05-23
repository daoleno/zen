import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type SetStateAction,
} from "react";
import type { Agent, ConnectionState } from "../../store/agents";
import type { CodexConversation } from "../../services/codexConversation";
import type { UploadedAttachment } from "../../services/uploads";
import { wsClient, type CodexSlashCommand } from "../../services/websocket";

const POLL_INTERVAL_MS = 900;
const PENDING_USER_MESSAGE_MAX_AGE_MS = 120_000;
const ATTACHMENT_TAG_RE = /<zen_attachments>\s*([\s\S]*?)\s*<\/zen_attachments>/i;

type RefreshInFlight = {
  baseKey: string;
  requestSeq: number;
};

type KeyedState<T> = {
  cacheKey: string;
  value: T;
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

export type ChatCommandEvent = {
  id: string;
  command: CodexSlashCommand;
  tone: "neutral" | "success" | "failed";
  title: string;
  detail?: string;
  body?: string;
  createdAt: string;
};

interface UseCodexChatSessionInput {
  serverId: string;
  agentId: string;
  agent?: Agent;
  connectionState: ConnectionState;
  screenFocused: boolean;
}

const conversationCache = new Map<string, CodexConversation>();
const draftCache = new Map<string, string>();
const attachmentCache = new Map<string, ComposerAttachment[]>();
const chatCommandEventCache = new Map<string, ChatCommandEvent[]>();

export function useCodexChatSession({
  serverId,
  agentId,
  agent,
  connectionState,
  screenFocused,
}: UseCodexChatSessionInput) {
  const sessionStartedAt = normalizeSessionTimestamp(agent?.started_at);
  const cacheKey = `${serverId}:${agentId}:${sessionStartedAt || ""}`;
  const composerCacheKey = `${serverId}:${agentId}`;
  const requestSeqRef = useRef(0);
  const refreshInFlightRef = useRef<RefreshInFlight | null>(null);
  const refreshQueuedRef = useRef(false);
  const [conversationState, setConversationState] = useState<
    KeyedState<CodexConversation | null>
  >(
    () => ({
      cacheKey,
      value: conversationCache.get(cacheKey) ?? null,
    }),
  );
  const [loading, setLoading] = useState(false);
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
  const [chatCommandEventsState, setChatCommandEventsState] = useState<
    KeyedState<ChatCommandEvent[]>
  >(
    () => ({
      cacheKey,
      value: chatCommandEventCache.get(cacheKey) ?? [],
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
  const conversation =
    conversationState.cacheKey === cacheKey
      ? conversationState.value
      : conversationCache.get(cacheKey) ?? null;
  const draft =
    draftState.cacheKey === composerCacheKey
      ? draftState.value
      : draftCache.get(composerCacheKey) ?? "";
  const attachments =
    attachmentsState.cacheKey === composerCacheKey
      ? attachmentsState.value
      : attachmentCache.get(composerCacheKey) ?? [];
  const chatCommandEvents =
    chatCommandEventsState.cacheKey === cacheKey
      ? chatCommandEventsState.value
      : chatCommandEventCache.get(cacheKey) ?? [];
  const pendingUserMessages =
    pendingUserMessagesState.cacheKey === composerCacheKey
      ? pendingUserMessagesState.value
      : [];

  const setConversation = useCallback(
    (nextConversation: CodexConversation | null) => {
      if (nextConversation) {
        conversationCache.set(cacheKey, nextConversation);
      } else {
        conversationCache.delete(cacheKey);
      }
      setConversationState({
        cacheKey,
        value: nextConversation,
      });
    },
    [cacheKey],
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

  const setChatCommandEvents = useCallback(
    (nextValue: SetStateAction<ChatCommandEvent[]>) => {
      setChatCommandEventsState((current) => {
        const currentEvents =
          current.cacheKey === cacheKey
            ? current.value
            : chatCommandEventCache.get(cacheKey) ?? [];
        const nextEvents =
          typeof nextValue === "function"
            ? nextValue(currentEvents)
            : nextValue;
        const bounded = nextEvents.slice(-12);
        if (bounded.length > 0) {
          chatCommandEventCache.set(cacheKey, bounded);
        } else {
          chatCommandEventCache.delete(cacheKey);
        }
        return {
          cacheKey,
          value: bounded,
        };
      });
    },
    [cacheKey],
  );

  const recordChatCommandEvent = useCallback(
    (event: Omit<ChatCommandEvent, "id" | "createdAt">) => {
      setChatCommandEvents((current) => [
        ...current,
        {
          ...event,
          id: `chat-command:${Date.now().toString(36)}:${Math.random().toString(36).slice(2, 8)}`,
          createdAt: new Date().toISOString(),
        },
      ]);
    },
    [setChatCommandEvents],
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
        refreshQueuedRef.current = true;
        if (showLoading) {
          setLoading(true);
        }
        return;
      }

      const requestSeq = requestSeqRef.current + 1;
      requestSeqRef.current = requestSeq;
      refreshInFlightRef.current = { baseKey: requestBaseKey, requestSeq };
      if (showLoading) {
        setLoading(true);
      }
      try {
        const nextConversation = await wsClient.getCodexConversation(serverId, {
          targetId: agentId,
          cwd: agent?.cwd,
          command: agent?.command,
          name: agent?.name,
          startedAt: agent?.started_at,
        });
        if (requestSeqRef.current !== requestSeq) {
          return;
        }
        setConversation(nextConversation);
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
        if (refreshQueuedRef.current && requestSeqRef.current === requestSeq) {
          refreshQueuedRef.current = false;
          setTimeout(() => {
            void refreshConversation(false);
          }, 0);
        }
      }
    },
    [
      agent?.command,
      agent?.cwd,
      agent?.name,
      agent?.started_at,
      agentId,
      cacheKey,
      connectionState,
      serverId,
      setConversation,
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
      refreshQueuedRef.current = false;
      clearInterval(interval);
    };
  }, [refreshConversation, screenFocused]);

  useEffect(() => {
    if (!screenFocused || !agent?.updated_at) {
      return;
    }
    void refreshConversation(false);
  }, [agent?.updated_at, refreshConversation, screenFocused]);

  useEffect(() => {
    setConversationState({
      cacheKey,
      value: conversationCache.get(cacheKey) ?? null,
    });
    setError(null);
    setChatCommandEventsState({
      cacheKey,
      value: chatCommandEventCache.get(cacheKey) ?? [],
    });
    refreshInFlightRef.current = null;
    refreshQueuedRef.current = false;
  }, [cacheKey]);

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

  return {
    cacheKey,
    conversation,
    loading,
    error,
    draft,
    setDraft,
    attachments,
    setAttachments,
    chatCommandEvents,
    pendingUserMessages,
    recordChatCommandEvent,
    addPendingUserMessage,
    removePendingUserMessage,
    refreshConversation,
  };
}

function comparableUserMessageText(value: string) {
  return value
    .replace(ATTACHMENT_TAG_RE, "")
    .replace(/\s+/g, " ")
    .trim();
}

function normalizeSessionTimestamp(value: unknown): string {
  return typeof value === "number" && Number.isFinite(value) && value > 0
    ? String(Math.round(value))
    : "";
}
