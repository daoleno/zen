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

type RefreshInFlight = {
  baseKey: string;
  requestSeq: number;
};

export type ComposerAttachment = UploadedAttachment & {
  id: string;
};

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
    refreshInFlightRef.current = null;
  }, [cacheKey]);

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
    recordChatCommandEvent,
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
    nextConversation.reason === "agent_not_found"
  );
}
