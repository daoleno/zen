import React, {
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { ConnectionState } from "../../store/agents";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import { wsClient } from "../../services/websocket";
import type { CodexChatBodyProps } from "./CodexChatBody";
import { useCodexChatController } from "./CodexChatController";
import { isCodexRequestRunning } from "./CodexChatControllerModel";
import {
  type CodexChatAgentInfo,
  type PendingSlashCommand,
  type PendingUserMessage,
  useCodexChatSession,
} from "./CodexChatSession";
import { CodexStatusSheet } from "./CodexStatusSheet";
import { CodexSkillsSheet } from "./CodexSkillsSheet";
import { buildTerminalActionPrompt } from "./TerminalActionPromptModel";
import { useCodexSlashCommands } from "./CodexSlashCommands";
import { useCodexChatBodyProps } from "./useCodexChatBodyProps";
import {
  useCodexComposerInput,
  useCodexComposerPresentation,
  useElapsedDurationLabel,
  usePinnedTimeline,
  useRelativeTimeLabel,
} from "./CodexChatSurfaceHooks";

interface UseCodexChatSurfaceStateInput {
  visible: boolean;
  serverId: string;
  agentId: string;
  conversationScopeKey?: string;
  agentInfo?: CodexChatAgentInfo;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  placeholder?: string;
  minimalComposer?: boolean;
  showAttachmentControl?: boolean;
  keyboardVerticalOffset?: number;
  showUnavailableAction?: boolean;
  emptyTitle?: string;
  emptyBody?: string;
  onSwitchToTerminal?: () => void;
}

interface CodexChatSurfaceState {
  bodyProps: CodexChatBodyProps;
}

type CodexStatusRequest = {
  baselineSeq: number;
  requestedAt: string;
};

const CODEX_STATUS_OUTPUT_TIMEOUT_MS = 9000;
const CODEX_STATUS_TERMINAL_POLL_INTERVAL_MS = 750;
const EMPTY_CHAT_TERMINAL_FALLBACK_POLL_MS = 1800;
const TERMINAL_FALLBACK_MAX_LINES = 160;
const TERMINAL_FALLBACK_MAX_CHARS = 12000;
const CODEX_STATUS_KEYS = new Set([
  "account",
  "agentsmd",
  "approval",
  "codexversion",
  "configprofile",
  "context",
  "cwd",
  "model",
  "provider",
  "reasoningeffort",
  "sandbox",
  "session",
  "sessionid",
  "tokens",
  "workingdirectory",
]);

export function useCodexChatSurfaceState({
  visible,
  serverId,
  agentId,
  conversationScopeKey,
  agentInfo,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  screenFocused,
  placeholder,
  minimalComposer,
  showAttachmentControl,
  keyboardVerticalOffset,
  showUnavailableAction,
  emptyTitle,
  emptyBody,
  onSwitchToTerminal,
}: UseCodexChatSurfaceStateInput): CodexChatSurfaceState {
  const insets = useSafeAreaInsets();
  const slashCommands = useCodexSlashCommands({
    serverId,
    connectionState,
    screenFocused,
  });
  const [actionMenuPinned, setActionMenuPinned] = useState(false);
  const [skillsSheetVisible, setSkillsSheetVisible] = useState(false);
  const [statusSheetVisible, setStatusSheetVisible] = useState(false);
  const [statusRequest, setStatusRequest] = useState<CodexStatusRequest | null>(null);
  const [statusTerminalEvent, setStatusTerminalEvent] =
    useState<CodexConversationEvent | null>(null);
  const [terminalFallbackEvent, setTerminalFallbackEvent] =
    useState<CodexConversationEvent | null>(null);
  const [statusTimedOut, setStatusTimedOut] = useState(false);
  const session = useCodexChatSession({
    serverId,
    agentId,
    conversationScopeKey,
    agentInfo,
    connectionState,
    screenFocused,
  });
  const {
    cacheKey: conversationCacheKey,
    conversation,
    localChatState,
    loading,
    error,
    draft,
    setDraft,
    attachments,
    setAttachments,
    pendingUserMessages,
    pendingSlashCommands,
    addPendingUserMessage,
    removePendingUserMessage,
    addPendingSlashCommand,
    settlePendingSlashCommand,
    removePendingSlashCommand,
    resetForNewChat,
    markNewChatReady,
    markNewChatMessageStarted,
  } = session;
  const composerInput = useCodexComposerInput({
    enabled: visible && screenFocused && connectionState === "connected",
  });
  useEffect(() => {
    if ((!visible || connectionState !== "connected") && actionMenuPinned) {
      setActionMenuPinned(false);
    }
  }, [actionMenuPinned, connectionState, visible]);

  const toggleActionMenu = useCallback(() => {
    if (connectionState !== "connected") {
      composerInput.focus();
      return;
    }
    if (!actionMenuPinned) {
      composerInput.blur();
    }
    setActionMenuPinned(!actionMenuPinned);
  }, [actionMenuPinned, composerInput.blur, composerInput.focus, connectionState]);

  const dismissActionMenu = useCallback(() => {
    setActionMenuPinned(false);
  }, []);
  const openSkillsSheet = useCallback(() => {
    setActionMenuPinned(false);
    setSkillsSheetVisible(true);
  }, []);
  const closeSkillsSheet = useCallback(() => {
    setSkillsSheetVisible(false);
  }, []);
  const events = conversation?.events ?? [];
  const shouldUseTerminalFallback =
    visible &&
    connectionState === "connected" &&
    localChatState === "idle" &&
    events.length === 0 &&
    pendingUserMessages.length === 0 &&
    pendingSlashCommands.length === 0 &&
    Boolean(serverId && agentId);
  const timelineEvents =
    shouldUseTerminalFallback && terminalFallbackEvent
      ? [terminalFallbackEvent]
      : events;

  useEffect(() => {
    if (!shouldUseTerminalFallback) {
      setTerminalFallbackEvent(null);
      return;
    }

    let cancelled = false;
    let inFlight = false;

    const refreshTerminalFallback = () => {
      if (cancelled || inFlight) {
        return;
      }
      inFlight = true;
      void wsClient
        .getCodexTerminalSnapshot(serverId, agentId)
        .then((text) => {
          if (cancelled) {
            return;
          }
          const body = terminalFallbackBodyFromSnapshot(text);
          setTerminalFallbackEvent(
            body
              ? {
                  id: `terminal-fallback:${serverId}:${agentId}`,
                  seq: 0,
                  timestamp: new Date().toISOString(),
                  kind: "assistant_message",
                  role: "assistant",
                  body,
                  source: "terminal_snapshot",
                }
              : null,
          );
        })
        .catch(() => {
          if (!cancelled) {
            setTerminalFallbackEvent(null);
          }
        })
        .finally(() => {
          inFlight = false;
        });
    };

    refreshTerminalFallback();
    const interval = setInterval(
      refreshTerminalFallback,
      EMPTY_CHAT_TERMINAL_FALLBACK_POLL_MS,
    );

    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [
    agentId,
    connectionState,
    events.length,
    localChatState,
    pendingSlashCommands.length,
    pendingUserMessages.length,
    serverId,
    shouldUseTerminalFallback,
    visible,
  ]);

  const openStatusSheet = useCallback(() => {
    setActionMenuPinned(false);
    setStatusTimedOut(false);
    setStatusTerminalEvent(null);
    setStatusSheetVisible(true);
    setStatusRequest({
      baselineSeq: latestConversationEventSeq(events),
      requestedAt: new Date().toISOString(),
    });
  }, [events]);
  const closeStatusSheet = useCallback(() => {
    setStatusSheetVisible(false);
  }, []);
  const switchFromStatusToTerminal = useCallback(() => {
    if (!onSwitchToTerminal) {
      return;
    }
    setStatusSheetVisible(false);
    onSwitchToTerminal();
  }, [onSwitchToTerminal]);
  const statusOutputEvent = useMemo(
    () => latestCodexStatusOutputEvent(events, statusRequest),
    [events, statusRequest],
  );
  const statusDisplayEvent = statusOutputEvent ?? statusTerminalEvent;
  useEffect(() => {
    if (
      !statusSheetVisible ||
      !statusRequest ||
      statusOutputEvent ||
      statusTerminalEvent ||
      statusTimedOut ||
      connectionState !== "connected" ||
      !serverId ||
      !agentId
    ) {
      return;
    }
    let cancelled = false;
    let inFlight = false;
    const pollTerminalStatus = () => {
      if (cancelled || inFlight) {
        return;
      }
      inFlight = true;
      void wsClient
        .getCodexTerminalSnapshot(serverId, agentId)
        .then((text) => {
          if (cancelled) {
            return;
          }
          const body = codexStatusBodyFromTerminalSnapshot(text);
          if (!body) {
            return;
          }
          setStatusTerminalEvent({
            id: `terminal-status:${statusRequest.requestedAt}`,
            seq: statusRequest.baselineSeq + 1,
            timestamp: new Date().toISOString(),
            kind: "status",
            title: "Codex",
            body,
            source: "terminal_snapshot",
          });
        })
        .catch(() => {
          // The regular timeout state handles unavailable terminal snapshots.
        })
        .finally(() => {
          inFlight = false;
        });
    };
    const firstPoll = setTimeout(pollTerminalStatus, 350);
    const interval = setInterval(
      pollTerminalStatus,
      CODEX_STATUS_TERMINAL_POLL_INTERVAL_MS,
    );
    return () => {
      cancelled = true;
      clearTimeout(firstPoll);
      clearInterval(interval);
    };
  }, [
    agentId,
    connectionState,
    serverId,
    statusOutputEvent,
    statusRequest,
    statusSheetVisible,
    statusTerminalEvent,
    statusTimedOut,
  ]);
  useEffect(() => {
    if (!statusSheetVisible || !statusRequest || statusDisplayEvent) {
      return;
    }
    const timer = setTimeout(() => {
      setStatusTimedOut(true);
    }, CODEX_STATUS_OUTPUT_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [statusDisplayEvent, statusRequest, statusSheetVisible]);
  useEffect(() => {
    if (statusDisplayEvent && statusTimedOut) {
      setStatusTimedOut(false);
    }
  }, [statusDisplayEvent, statusTimedOut]);
  const latestTimelineTimestamp = useMemo(
    () =>
      latestChatTimelineTimestamp(
        timelineEvents,
        conversation,
        pendingUserMessages,
        pendingSlashCommands,
      ),
    [conversation, pendingSlashCommands, pendingUserMessages, timelineEvents],
  );
  const jumpLabel = useRelativeTimeLabel(latestTimelineTimestamp);
  const timeline = usePinnedTimeline(
    timelineEvents.length +
      pendingUserMessages.length +
      pendingSlashCommands.length,
    conversationCacheKey,
  );
  const controller = useCodexChatController({
    serverId,
    agentId,
    agentStatus: agentInfo?.status,
    connectionState,
    connectionIssue,
    conversation,
    events,
    draft,
    setDraft,
    attachments,
    setAttachments,
    slashCommands,
    addPendingUserMessage,
    removePendingUserMessage,
    addPendingSlashCommand,
    settlePendingSlashCommand,
    removePendingSlashCommand,
    resetForNewChat,
    markNewChatReady,
    markNewChatMessageStarted,
    scrollToLatest: timeline.scrollToLatest,
    focusComposer: composerInput.focus,
    clearComposerNativeText: composerInput.clearNativeText,
    dismissActionMenu,
    openStatusSheet,
    openSkillsSheet,
    onSwitchToTerminal,
  });

  const requestRunning =
    (isCodexRequestRunning({
      conversation,
      events,
      agentStatus: agentInfo?.status,
    }) &&
      localChatState === "idle");
  const turnStartedAt = useMemo(
    () => currentTurnStartedAt(conversation, events, pendingUserMessages),
    [conversation, events, pendingUserMessages],
  );
  const turnElapsedLabel = useElapsedDurationLabel(
    turnStartedAt,
    requestRunning && localChatState === "idle",
  );
  const composerPresentation = useCodexComposerPresentation({
    draft,
    slashCommands,
    connectionState,
    requestRunning,
    attachmentCount: attachments.length,
    sending: controller.sending,
    startingNewChat: controller.startingNewChat,
    interrupting: controller.interrupting,
    canSend: controller.canSend,
    elapsedLabel: turnElapsedLabel,
    actionMenuPinned,
    safeAreaTop: insets.top,
    safeAreaBottom: insets.bottom,
    placeholder,
    keyboardVerticalOffset,
    minimalComposer,
    showAttachmentControl,
  });
  const terminalActionPrompt = useMemo(
    () =>
      buildTerminalActionPrompt({
        status: agentInfo?.status,
        summary: agentInfo?.summary,
        lastOutputLines: agentInfo?.lastOutputLines,
      }),
    [agentInfo?.lastOutputLines, agentInfo?.status, agentInfo?.summary],
  );
  const sendTerminalActionKey = useCallback(
    (key: string) => {
      if (connectionState !== "connected" || !serverId || !agentId) {
        throw new Error("Daemon is not connected.");
      }
      return wsClient.sendKey(serverId, agentId, key);
    },
    [agentId, connectionState, serverId],
  );
  const skillsSheet = useMemo(
    () =>
      React.createElement(CodexSkillsSheet, {
        visible: skillsSheetVisible,
        serverId,
        cwd: agentInfo?.cwd,
        chrome,
        theme,
        onSelectSkill: (skill) => {
          closeSkillsSheet();
          controller.insertSkillMention(skill);
        },
        onClose: closeSkillsSheet,
      }),
    [
      agentInfo?.cwd,
      chrome,
      closeSkillsSheet,
      controller.insertSkillMention,
      serverId,
      skillsSheetVisible,
      theme,
    ],
  );
  const retryStatusCommand = useCallback(() => {
    controller.runStatusCommand(
      "/status",
      slashCommands.find((command) => command.name === "status"),
    );
  }, [controller.runStatusCommand, slashCommands]);
  const statusSheet = useMemo(
    () =>
      React.createElement(CodexStatusSheet, {
        visible: statusSheetVisible,
        event: statusDisplayEvent,
        loading: Boolean(statusRequest && !statusDisplayEvent && !statusTimedOut),
        timedOut: statusTimedOut,
        chrome,
        theme,
        onRetry: retryStatusCommand,
        onSwitchToTerminal: onSwitchToTerminal ? switchFromStatusToTerminal : undefined,
        onClose: closeStatusSheet,
      }),
    [
      chrome,
      closeStatusSheet,
      onSwitchToTerminal,
      retryStatusCommand,
      statusDisplayEvent,
      statusRequest,
      statusSheetVisible,
      statusTimedOut,
      switchFromStatusToTerminal,
      theme,
    ],
  );
  const sheets = useMemo(
    () => React.createElement(React.Fragment, null, skillsSheet, statusSheet),
    [skillsSheet, statusSheet],
  );
  const bodyProps = useCodexChatBodyProps({
    screenFocused,
    serverId,
    agentCwd: agentInfo?.cwd,
    connectionState,
    conversation,
    events: timelineEvents,
    pendingUserMessages,
    pendingSlashCommands,
    loading,
    localChatState,
    error,
    draft,
    attachments,
    composerPresentation,
    terminalActionPrompt,
    timeline,
    jumpLabel,
    emptyTitle,
    emptyBody,
    composerInput,
    controller,
    chrome,
    theme,
    onSwitchToTerminal,
    setDraft,
    onToggleActionMenu: toggleActionMenu,
    onDismissActionMenu: dismissActionMenu,
    onTerminalActionKey: sendTerminalActionKey,
    showUnavailableAction,
    skillsSheet: sheets,
  });
  return {
    bodyProps,
  };
}

function latestChatTimelineTimestamp(
  events: CodexConversationEvent[],
  conversation: CodexConversation | null,
  pendingUserMessages: PendingUserMessage[],
  pendingSlashCommands: PendingSlashCommand[],
) {
  let latest = 0;
  events.forEach((event: CodexConversationEvent) => {
    const timestamp = new Date(event.timestamp || "").getTime();
    if (Number.isFinite(timestamp) && timestamp > latest) {
      latest = timestamp;
    }
  });
  pendingUserMessages.forEach((message) => {
    const timestamp = new Date(message.createdAt).getTime();
    if (Number.isFinite(timestamp) && timestamp > latest) {
      latest = timestamp;
    }
  });
  pendingSlashCommands.forEach((command) => {
    const timestamp = new Date(command.createdAt).getTime();
    if (Number.isFinite(timestamp) && timestamp > latest) {
      latest = timestamp;
    }
  });
  return latest > 0 ? new Date(latest).toISOString() : undefined;
}

function latestConversationEventSeq(events: CodexConversationEvent[]) {
  return events.reduce(
    (latest, event) =>
      Number.isFinite(event.seq) && event.seq > latest ? event.seq : latest,
    0,
  );
}

function latestCodexStatusOutputEvent(
  events: CodexConversationEvent[],
  request: CodexStatusRequest | null,
) {
  if (!request) {
    return null;
  }
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (!isCodexStatusOutputEvent(event) || !isEventAfterStatusRequest(event, request)) {
      continue;
    }
    return event;
  }
  return null;
}

function isCodexStatusOutputEvent(event: CodexConversationEvent) {
  if (event.kind !== "status") {
    return false;
  }
  const body = event.body?.trim();
  if (!body) {
    return false;
  }
  if (event.title?.trim().toLowerCase() === "codex") {
    return true;
  }
  return looksLikeCodexStatusBody(body);
}

function looksLikeCodexStatusBody(body: string) {
  for (const line of body.split(/\r?\n/)) {
    if (codexStatusKeyFromLine(line)) {
      return true;
    }
  }
  return false;
}

function codexStatusBodyFromTerminalSnapshot(text: string) {
  const lines = text
    .replace(/\r/g, "")
    .split("\n")
    .map(cleanCodexStatusTerminalLine)
    .filter((line) => line.trim().length > 0);
  if (lines.length === 0) {
    return "";
  }
  const statusLineIndexes: number[] = [];
  for (let index = 0; index < lines.length; index += 1) {
    if (codexStatusKeyFromLine(lines[index])) {
      statusLineIndexes.push(index);
    }
  }
  if (statusLineIndexes.length < 2) {
    return "";
  }
  const start = statusLineIndexes[0];
  const end = statusLineIndexes[statusLineIndexes.length - 1];
  return lines.slice(start, end + 1).join("\n").trim();
}

function codexStatusKeyFromLine(line: string) {
  const cleaned = cleanCodexStatusTerminalLine(line);
  const colonMatch = /^([^:：]{1,48})[:：]\s*\S/.exec(cleaned);
  if (colonMatch && isCodexStatusKey(colonMatch[1])) {
    return true;
  }
  const spacedMatch = /^([A-Za-z][A-Za-z0-9 ._-]{0,40})\s{2,}\S/.exec(cleaned);
  return Boolean(spacedMatch && isCodexStatusKey(spacedMatch[1]));
}

function isCodexStatusKey(value: string) {
  return CODEX_STATUS_KEYS.has(value.toLowerCase().replace(/[\s._-]/g, ""));
}

function cleanCodexStatusTerminalLine(line: string) {
  return line
    .replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, "")
    .replace(/^[\s│┃|>]+/, "")
    .replace(/[\s│┃|]+$/, "")
    .trim();
}

function terminalFallbackBodyFromSnapshot(text: string) {
  const lines = text
    .replace(/\r/g, "")
    .split("\n")
    .map((line) => line.replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, "").trimEnd());
  const firstContentIndex = lines.findIndex((line) => line.trim().length > 0);
  if (firstContentIndex < 0) {
    return "";
  }
  const meaningfulLines = lines.slice(firstContentIndex).slice(-TERMINAL_FALLBACK_MAX_LINES);
  const body = meaningfulLines.join("\n").trim();
  if (body.length <= TERMINAL_FALLBACK_MAX_CHARS) {
    return body;
  }
  return body.slice(body.length - TERMINAL_FALLBACK_MAX_CHARS).trimStart();
}

function isEventAfterStatusRequest(
  event: CodexConversationEvent,
  request: CodexStatusRequest,
) {
  if (Number.isFinite(event.seq) && event.seq > request.baselineSeq) {
    return true;
  }
  const eventTimestamp = new Date(event.timestamp || "").getTime();
  const requestTimestamp = new Date(request.requestedAt).getTime();
  return (
    Number.isFinite(eventTimestamp) &&
    Number.isFinite(requestTimestamp) &&
    eventTimestamp >= requestTimestamp
  );
}

function currentTurnStartedAt(
  conversation: CodexConversation | null,
  events: CodexConversationEvent[],
  pendingUserMessages: PendingUserMessage[],
) {
  const activeUserTimestamp = latestUserMessageTimestamp(conversation, events);
  const pendingTimestamp = latestPendingUserMessageTimestamp(pendingUserMessages);

  if (!activeUserTimestamp) {
    return pendingTimestamp;
  }
  if (!pendingTimestamp) {
    return activeUserTimestamp;
  }

  const activeMs = new Date(activeUserTimestamp).getTime();
  const pendingMs = new Date(pendingTimestamp).getTime();
  if (!Number.isFinite(activeMs)) {
    return pendingTimestamp;
  }
  if (!Number.isFinite(pendingMs)) {
    return activeUserTimestamp;
  }
  return pendingMs > activeMs ? pendingTimestamp : activeUserTimestamp;
}

function latestUserMessageTimestamp(
  conversation: CodexConversation | null,
  events: CodexConversationEvent[],
) {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    const event = events[index];
    if (event.kind !== "user_message" || !event.timestamp) {
      continue;
    }
    const timestamp = new Date(event.timestamp).getTime();
    if (Number.isFinite(timestamp)) {
      return event.timestamp;
    }
  }
  return conversation?.updated_at;
}

function latestPendingUserMessageTimestamp(
  pendingUserMessages: PendingUserMessage[],
) {
  let latest = 0;
  pendingUserMessages.forEach((message) => {
    const timestamp = new Date(message.createdAt).getTime();
    if (Number.isFinite(timestamp) && timestamp > latest) {
      latest = timestamp;
    }
  });
  return latest > 0 ? new Date(latest).toISOString() : undefined;
}
