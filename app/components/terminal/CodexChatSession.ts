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
  const cacheKey = `${serverId}:${agentId}`;
  const requestSeqRef = useRef(0);
  const refreshInFlightRef = useRef<RefreshInFlight | null>(null);
  const refreshQueuedRef = useRef(false);
  const [conversation, setConversationState] = useState<CodexConversation | null>(
    () => conversationCache.get(cacheKey) ?? null,
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [draft, setDraftState] = useState(() => draftCache.get(cacheKey) ?? "");
  const [attachments, setAttachmentsState] = useState<ComposerAttachment[]>(
    () => attachmentCache.get(cacheKey) ?? [],
  );
  const [chatCommandEvents, setChatCommandEventsState] = useState<ChatCommandEvent[]>(
    () => chatCommandEventCache.get(cacheKey) ?? [],
  );
  const [pendingUserMessages, setPendingUserMessages] = useState<PendingUserMessage[]>([]);

  const setConversation = useCallback(
    (nextConversation: CodexConversation | null) => {
      if (nextConversation) {
        const cached = conversationCache.get(cacheKey);
        if (nextConversation.available || !cached?.available) {
          conversationCache.set(cacheKey, nextConversation);
        }
      }
      setConversationState(nextConversation);
    },
    [cacheKey],
  );

  const setDraft = useCallback(
    (nextDraft: string) => {
      if (nextDraft) {
        draftCache.set(cacheKey, nextDraft);
      } else {
        draftCache.delete(cacheKey);
      }
      setDraftState(nextDraft);
    },
    [cacheKey],
  );

  const setAttachments = useCallback(
    (nextValue: SetStateAction<ComposerAttachment[]>) => {
      setAttachmentsState((current) => {
        const nextAttachments =
          typeof nextValue === "function" ? nextValue(current) : nextValue;
        if (nextAttachments.length > 0) {
          attachmentCache.set(cacheKey, nextAttachments);
        } else {
          attachmentCache.delete(cacheKey);
        }
        return nextAttachments;
      });
    },
    [cacheKey],
  );

  const setChatCommandEvents = useCallback(
    (nextValue: SetStateAction<ChatCommandEvent[]>) => {
      setChatCommandEventsState((current) => {
        const nextEvents =
          typeof nextValue === "function" ? nextValue(current) : nextValue;
        const bounded = nextEvents.slice(-12);
        if (bounded.length > 0) {
          chatCommandEventCache.set(cacheKey, bounded);
        } else {
          chatCommandEventCache.delete(cacheKey);
        }
        return bounded;
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
    setPendingUserMessages((current) => [
      ...current,
      {
        ...message,
        id,
        createdAt: new Date().toISOString(),
      },
    ].slice(-6));
    return id;
  }, []);

  const removePendingUserMessage = useCallback((id: string) => {
    setPendingUserMessages((current) => current.filter((message) => message.id !== id));
  }, []);

  const refreshConversation = useCallback(
    async (showLoading: boolean = false) => {
      if (!serverId || !agentId || connectionState !== "connected") {
        requestSeqRef.current += 1;
        refreshInFlightRef.current = null;
        setLoading(false);
        return;
      }
      const requestBaseKey = `${serverId}:${agentId}`;
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
        const cachedConversation = conversationCache.get(requestBaseKey);
        if (
          cachedConversation &&
          shouldKeepCachedConversation(cachedConversation, nextConversation)
        ) {
          setConversation(cachedConversation);
        } else {
          setConversation(nextConversation);
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
    setConversationState(conversationCache.get(cacheKey) ?? null);
    setError(null);
    setDraftState(draftCache.get(cacheKey) ?? "");
    setAttachmentsState(attachmentCache.get(cacheKey) ?? []);
    setChatCommandEventsState(chatCommandEventCache.get(cacheKey) ?? []);
    setPendingUserMessages([]);
    refreshInFlightRef.current = null;
    refreshQueuedRef.current = false;
  }, [cacheKey]);

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
    setPendingUserMessages((current) =>
      current.filter((message) => {
        const createdAt = new Date(message.createdAt).getTime();
        if (Number.isFinite(createdAt) && now - createdAt > PENDING_USER_MESSAGE_MAX_AGE_MS) {
          return false;
        }
        const sentText = comparableUserMessageText(message.sentText);
        const body = comparableUserMessageText(message.body);
        return !(
          (sentText && confirmedUserMessages.has(sentText))
          || (body && confirmedUserMessages.has(body))
        );
      }),
    );
  }, [conversation?.events, pendingUserMessages.length]);

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

function shouldKeepCachedConversation(
  cached: CodexConversation | undefined,
  nextConversation: CodexConversation,
) {
  if (!cached?.available || nextConversation.available) {
    return false;
  }
  return (
    nextConversation.reason === "session_not_ready" ||
    nextConversation.reason === "transcript_not_found" ||
    nextConversation.reason === "missing_cwd" ||
    nextConversation.reason === "agent_not_found"
  );
}

function comparableUserMessageText(value: string) {
  return value
    .replace(ATTACHMENT_TAG_RE, "")
    .replace(/\s+/g, " ")
    .trim();
}
