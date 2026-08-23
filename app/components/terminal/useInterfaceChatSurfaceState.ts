import React, {
  type ReactNode,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { AppState } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { useAppTheme } from "../../constants/tokens";
import {
  isAgentSessionListFreshForConnection,
  useAgents,
  type ConnectionState,
} from "../../store/agents";
import { agentKindFromCommand } from "../../services/chatComposerPresentation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import { wsClient } from "../../services/websocket";
import type { InterfaceChatBodyProps } from "./InterfaceChatBody";
import { useInterfaceChatController } from "./InterfaceChatController";
import {
  type InterfaceChatAgentInfo,
  type PendingUserMessage,
  useInterfaceChatSession,
} from "./InterfaceChatSession";
import { CodexStatusSheet } from "./CodexStatusSheet";
import { CodexSkillsSheet } from "./CodexSkillsSheet";
import { buildTerminalActionPrompt } from "./TerminalActionPromptModel";
import { liveActionPromptScopeKey } from "../../services/agentSessionListTransport";
import { useCodexSlashCommands } from "./CodexSlashCommands";
import { useInterfaceChatBodyProps } from "./useInterfaceChatBodyProps";
import { brainWorkEventsFromConversationEvents } from "../brain/brainWorkActivityModel";
import {
  useInterfaceComposerInput,
  useInterfaceComposerPresentation,
  usePinnedTimeline,
  useRelativeTimeLabel,
} from "./InterfaceChatSurfaceHooks";
import { resolveRunningProviderActivity } from "./providerActivity";
import { shouldClearTurnFocusForSurfaceLifecycle } from "./turnFocusState";
import {
  resolveInterfaceComposerInitialFocusEffect,
  type InterfaceComposerInitialFocusGrant,
} from "./interfaceComposerInitialFocus";
import { makeSessionKey } from "../../services/sessionKeys";

interface UseInterfaceChatSurfaceStateInput {
  visible: boolean;
  serverId: string;
  serverUrl: string;
  daemonId: string;
  agentId: string;
  conversationScopeKey?: string;
  agentInfo?: InterfaceChatAgentInfo;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  initialComposerFocusGrant?: InterfaceComposerInitialFocusGrant;
  placeholder?: string;
  keyboardVerticalOffset?: number;
  topChromeInset?: number;
  showUnavailableAction?: boolean;
  emptyTitle?: string;
  emptyBody?: string;
  onBrainWorkEventActivate?: (
    event: import("../brain/brainWorkEvent").BrainWorkResultEvent,
    canOpenSession: boolean,
  ) => void;
  onBrainWorkEventsChange?: (
    serverId: string,
    events: import("../brain/brainWorkEvent").BrainWorkResultEvent[],
  ) => void;
  openSessionIds?: ReadonlySet<string>;
  composerAccessory?: ReactNode;
  onDraftChange?: (value: string) => void;
  renderComposerAccessory?: (args: {
    draft: string;
    setDraft: (value: string) => void;
  }) => ReactNode;
  composerModelControl?: import("../../services/providers/sessionModelHelpers").ComposerModelControlPresentation | null;
  onComposerModelControlPress?: () => void;
  onSwitchToTerminal?: () => void;
  onConsumeInitialComposerFocus?: () => void;
}

interface InterfaceChatSurfaceState {
  bodyProps: InterfaceChatBodyProps;
}

type CodexStatusRequest = {
  baselineSeq: number;
  requestedAt: string;
};

const CODEX_STATUS_OUTPUT_TIMEOUT_MS = 9000;
const CODEX_STATUS_TERMINAL_POLL_INTERVAL_MS = 750;
const EMPTY_CONVERSATION_EVENTS: CodexConversationEvent[] = [];
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

export function useInterfaceChatSurfaceState({
  visible,
  serverId,
  serverUrl,
  daemonId,
  agentId,
  conversationScopeKey,
  agentInfo,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  screenFocused,
  initialComposerFocusGrant = null,
  placeholder,
  keyboardVerticalOffset,
  topChromeInset,
  showUnavailableAction,
  emptyTitle,
  emptyBody,
  onBrainWorkEventActivate,
  onBrainWorkEventsChange,
  openSessionIds,
  composerAccessory,
  onDraftChange,
  renderComposerAccessory,
  composerModelControl,
  onComposerModelControlPress,
  onSwitchToTerminal,
  onConsumeInitialComposerFocus,
}: UseInterfaceChatSurfaceStateInput): InterfaceChatSurfaceState {
  const insets = useSafeAreaInsets();
  const { theme: zenTheme } = useAppTheme();
  const { state: agentState } = useAgents();
  const composerLayout = zenTheme.chat.layout;
  const active = visible && screenFocused;
  const handledInitialComposerFocusGrantRef =
    useRef<InterfaceComposerInitialFocusGrant>(null);
  const chatAgentKind = agentKindFromCommand(agentInfo?.command);
  const connectionGeneration =
    agentState.connectionGenerationByServer[serverId] ?? 0;
  const agentSessionListFresh = isAgentSessionListFreshForConnection(
    agentState,
    serverId,
  );
  const slashCommands = useCodexSlashCommands({
    serverId,
    connectionState,
    screenFocused: active,
    agentKind: chatAgentKind,
  });
  const [actionMenuPinned, setActionMenuPinned] = useState(false);
  const [skillsSheetVisible, setSkillsSheetVisible] = useState(false);
  const [statusSheetVisible, setStatusSheetVisible] = useState(false);
  const [statusRequest, setStatusRequest] = useState<CodexStatusRequest | null>(
    null,
  );
  const [statusTerminalEvent, setStatusTerminalEvent] =
    useState<CodexConversationEvent | null>(null);
  const [statusTimedOut, setStatusTimedOut] = useState(false);
  const session = useInterfaceChatSession({
    serverId,
    agentId,
    conversationScopeKey,
    agentInfo,
    connectionState,
    screenFocused: active,
  });
  const {
    cacheKey: conversationCacheKey,
    conversation,
    loading,
    error,
    draft,
    setDraft,
    restoreDraft,
    attachments,
    setAttachments,
    pendingUserMessages,
    subscriptionGeneration,
    turnFocusAnchorAliases,
    addPendingUserMessage,
    beginPendingUserMessageAttempt,
    rejectPendingUserMessage,
  } = session;
  const setObservedDraft = useCallback(
    (value: string) => {
      setDraft(value);
      onDraftChange?.(value);
    },
    [onDraftChange, setDraft],
  );
  const restoreObservedDraft = useCallback(
    (value: string) => {
      restoreDraft(value);
      onDraftChange?.(value);
    },
    [onDraftChange, restoreDraft],
  );
  const renderedComposerAccessory = renderComposerAccessory
    ? renderComposerAccessory({ draft, setDraft: setObservedDraft })
    : composerAccessory;
  const composerInput = useInterfaceComposerInput({
    enabled: active && connectionState === "connected",
  });
  useEffect(() => {
    const routeSessionKey = makeSessionKey(serverId, agentId);
    const effect = resolveInterfaceComposerInitialFocusEffect({
      grant: initialComposerFocusGrant,
      handledGrant: handledInitialComposerFocusGrantRef.current,
      sessionKey: routeSessionKey,
      screenActive: active,
      appActive: AppState.currentState === "active",
      connected: connectionState === "connected",
    });
    if (effect === "ignore" || effect === "wait") {
      return;
    }
    handledInitialComposerFocusGrantRef.current = initialComposerFocusGrant;
    onConsumeInitialComposerFocus?.();
    if (effect === "deliver") {
      composerInput.focus();
    }
  }, [
    active,
    agentId,
    composerInput.focus,
    connectionState,
    initialComposerFocusGrant,
    onConsumeInitialComposerFocus,
    serverId,
  ]);
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
  }, [
    actionMenuPinned,
    composerInput.blur,
    composerInput.focus,
    connectionState,
  ]);

  const dismissActionMenu = useCallback(() => {
    setActionMenuPinned(false);
  }, []);
  const handleModelControlPress = useCallback(() => {
    composerInput.blur();
    onComposerModelControlPress?.();
  }, [composerInput.blur, onComposerModelControlPress]);
  const openSkillsSheet = useCallback(() => {
    setActionMenuPinned(false);
    setSkillsSheetVisible(true);
  }, []);
  const closeSkillsSheet = useCallback(() => {
    setSkillsSheetVisible(false);
  }, []);
  const events = conversation?.events ?? EMPTY_CONVERSATION_EVENTS;
  const brainWorkEvents = useMemo(
    () => brainWorkEventsFromConversationEvents(events),
    [events],
  );
  useEffect(() => {
    onBrainWorkEventsChange?.(serverId, brainWorkEvents);
  }, [brainWorkEvents, onBrainWorkEventsChange, serverId]);
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
    () => latestChatTimelineTimestamp(conversation, pendingUserMessages),
    [conversation, pendingUserMessages],
  );
  const jumpLabel = useRelativeTimeLabel(latestTimelineTimestamp);
  const runningActivity = useMemo(
    () => resolveRunningProviderActivity(conversation?.activity),
    [conversation?.activity],
  );
  const timeline = usePinnedTimeline(
    events.length +
      pendingUserMessages.length +
      (runningActivity ? 1 : 0),
    conversationCacheKey,
    topChromeInset,
  );
  const handleKeyboardLifecycleInvalidate = useCallback(
    (reason: "route" | "app") => {
      composerInput.blur();
      timeline.clearTurnFocusForLifecycle();
      if (
        reason === "app" &&
        initialComposerFocusGrant &&
        handledInitialComposerFocusGrantRef.current !==
          initialComposerFocusGrant
      ) {
        handledInitialComposerFocusGrantRef.current = initialComposerFocusGrant;
        onConsumeInitialComposerFocus?.();
      }
    },
    [
      composerInput.blur,
      initialComposerFocusGrant,
      onConsumeInitialComposerFocus,
      timeline.clearTurnFocusForLifecycle,
    ],
  );
  useEffect(() => {
    if (
      shouldClearTurnFocusForSurfaceLifecycle(
        active && connectionState === "connected",
        subscriptionGeneration,
      )
    ) {
      timeline.clearTurnFocusForLifecycle();
    }
  }, [
    active,
    connectionState,
    subscriptionGeneration,
    timeline.clearTurnFocusForLifecycle,
  ]);
  const controller = useInterfaceChatController({
    serverId,
    agentId,
    conversationScopeKey,
    connectionState,
    connectionIssue,
    conversation,
    runningActivity,
    draft,
    setDraft: setObservedDraft,
    restoreDraft: restoreObservedDraft,
    attachments,
    pendingUserMessages,
    setAttachments,
    slashCommands,
    addPendingUserMessage,
    beginPendingUserMessageAttempt,
    rejectPendingUserMessage,
    requestTurnFocus: timeline.requestTurnFocus,
    focusComposer: composerInput.focus,
    clearComposerNativeText: composerInput.clearNativeText,
    dismissActionMenu,
    openStatusSheet,
    openSkillsSheet,
    onSwitchToTerminal,
  });

  const composerPresentation = useInterfaceComposerPresentation({
    draft,
    slashCommands,
    agentCommand: agentInfo?.command,
    connectionState,
    runningActivity,
    attachmentCount: attachments.length,
    interrupting: controller.interrupting,
    canSend: controller.canSend,
    elapsedStartedAt: runningActivity?.started_at,
    actionMenuPinned,
    safeAreaBottom: insets.bottom,
    placeholder,
    keyboardVerticalOffset,
    composerLayout,
    modelControl: composerModelControl,
  });
  const terminalActionPrompt = useMemo(() => {
    // Live pane fact only: require a full agent_session_list for this WebSocket
    // connection generation so retained pre-disconnect snapshots cannot flash.
    if (!agentId || !agentSessionListFresh) {
      return null;
    }
    return buildTerminalActionPrompt({
      status: agentInfo?.status,
      summary: agentInfo?.summary,
      lastOutputLines: agentInfo?.lastOutputLines,
      command: agentInfo?.command,
      scopeKey: liveActionPromptScopeKey({
        agentId,
        processId: agentInfo?.processId,
        startedAt: agentInfo?.startedAt,
        connectionGeneration,
      }),
    });
  }, [
    agentId,
    agentInfo?.command,
    agentInfo?.lastOutputLines,
    agentInfo?.processId,
    agentInfo?.startedAt,
    agentInfo?.status,
    agentInfo?.summary,
    agentSessionListFresh,
    connectionGeneration,
  ]);
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
        loading: Boolean(
          statusRequest && !statusDisplayEvent && !statusTimedOut,
        ),
        timedOut: statusTimedOut,
        chrome,
        theme,
        onRetry: retryStatusCommand,
        onSwitchToTerminal: onSwitchToTerminal
          ? switchFromStatusToTerminal
          : undefined,
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
  const bodyProps = useInterfaceChatBodyProps({
    screenFocused,
    serverId,
    serverUrl,
    daemonId,
    agentId,
    agentProcessId: agentInfo?.processId,
    agentStartedAt: agentInfo?.startedAt,
    agentCwd: agentInfo?.cwd,
    connectionState,
    conversation,
    events,
    pendingUserMessages,
    turnFocusAnchorAliases,
    runningActivity,
    onBrainWorkEventActivate,
    openSessionIds,
    loading,
    error,
    draft,
    attachments,
    composerPresentation,
    topChromeInset,
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
    setDraft: setObservedDraft,
    onToggleActionMenu: toggleActionMenu,
    onDismissActionMenu: dismissActionMenu,
    onModelControlPress: handleModelControlPress,
    onTerminalActionKey: sendTerminalActionKey,
    onKeyboardLifecycleInvalidate: handleKeyboardLifecycleInvalidate,
    showUnavailableAction,
    composerAccessory: renderedComposerAccessory,
    skillsSheet: sheets,
  });
  return {
    bodyProps,
  };
}

function latestChatTimelineTimestamp(
  conversation: CodexConversation | null,
  pendingUserMessages: PendingUserMessage[],
) {
  let latest = 0;
  conversation?.events.forEach((event: CodexConversationEvent) => {
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
    if (
      !isCodexStatusOutputEvent(event) ||
      !isEventAfterStatusRequest(event, request)
    ) {
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
  return lines
    .slice(start, end + 1)
    .join("\n")
    .trim();
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
