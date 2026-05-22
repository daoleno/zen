import React, {
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  ActivityIndicator,
  Alert,
  Image,
  Keyboard,
  Linking,
  Platform,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Clipboard from "expo-clipboard";
import { KeyboardAvoidingView } from "react-native-keyboard-controller";
import {
  EnrichedMarkdownText,
  type LinkPressEvent,
  type MarkdownStyle,
} from "react-native-enriched-markdown";
import remend, { type RemendOptions } from "remend";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Typography, type AgentStatus, statusColor } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { Agent, ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
  CodexPlanStep,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import {
  uploadDocumentForServer,
  type UploadedAttachment,
} from "../../services/uploads";
import { wsClient, type CodexSlashCommand } from "../../services/websocket";
import {
  ChatHeaderIconButton,
  ComposerIconButton,
  ComposerSendButton,
} from "./CodexChatControls";

interface CodexChatSurfaceProps {
  serverId: string;
  agentId: string;
  agent?: Agent;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  gitDiff?: {
    label: string;
    tone: "clean" | "dirty" | "error" | "loading";
    onPress(): void;
  } | null;
  onSwitchToTerminal(): void;
  onOpenGitDiff?: () => void;
}

const POLL_INTERVAL_MS = 900;
const SCROLL_BOTTOM_THRESHOLD = 96;
const FALLBACK_SLASH_COMMANDS = [
  ["model", "choose what model and reasoning effort to use"],
  ["fast", "1.5x speed, increased usage"],
  ["ide", "include current selection, open files, and other context from your IDE"],
  ["permissions", "choose what Codex is allowed to do"],
  ["keymap", "remap TUI shortcuts"],
  ["setup-default-sandbox", "set up elevated agent sandbox"],
  ["sandbox-add-read-dir", "let sandbox read a directory: /sandbox-add-read-dir <absolute_path>"],
  ["vim", "toggle Vim mode for the composer"],
  ["experimental", "toggle experimental features"],
  ["approve", "approve one retry of a recent auto-review denial"],
  ["memories", "configure memory use and generation"],
  ["skills", "use skills to improve how Codex performs specific tasks"],
  ["hooks", "view and manage lifecycle hooks"],
  ["review", "review my current changes and find issues"],
  ["rename", "rename the current thread"],
  ["new", "start a new chat during a conversation"],
  ["resume", "resume a saved chat"],
  ["fork", "fork the current chat"],
  ["init", "create an AGENTS.md file with instructions for Codex"],
  ["compact", "summarize conversation to prevent hitting the context limit"],
  ["plan", "switch to Plan mode"],
  ["goal", "set or view the goal for a long-running task"],
  ["side", "start a side conversation in an ephemeral fork"],
  ["copy", "copy last response as markdown"],
  ["raw", "toggle raw scrollback mode for copy-friendly terminal selection"],
  ["diff", "show git diff (including untracked files)"],
  ["mention", "mention a file"],
  ["status", "show current session configuration and token usage"],
  ["debug-config", "show config layers and requirement sources for debugging"],
  ["title", "configure which items appear in the terminal title"],
  ["statusline", "configure which items appear in the status line"],
  ["theme", "choose a syntax highlighting theme"],
  ["pets", "choose or hide the terminal pet"],
  ["mcp", "list configured MCP tools; use /mcp verbose for details"],
  ["apps", "manage apps"],
  ["plugins", "browse plugins"],
  ["logout", "log out of Codex"],
  ["quit", "exit Codex"],
  ["exit", "exit Codex"],
  ["feedback", "send logs to maintainers"],
  ["rollout", "print the rollout file path"],
  ["ps", "list background terminals"],
  ["stop", "stop all background terminals"],
  ["clear", "clear the terminal and start a new chat"],
  ["personality", "choose a communication style for Codex"],
  ["realtime", "toggle realtime voice mode (experimental)"],
  ["settings", "configure realtime microphone/speaker"],
  ["test-approval", "test approval request"],
  ["agent", "switch the active agent thread"],
  ["subagents", "switch the active agent thread"],
  ["btw", "start a side conversation in an ephemeral fork"],
  ["debug-m-drop", "DO NOT USE"],
  ["debug-m-update", "DO NOT USE"],
].map(([name, description]) => ({
  value: `/${name}`,
  name,
  title: slashCommandTitle(name),
  description,
  source: "fallback",
  ...fallbackSlashCommandCapability(name),
})) satisfies CodexSlashCommand[];
const ATTACHMENT_TAG_RE = /<zen_attachments>\s*([\s\S]*?)\s*<\/zen_attachments>/i;
const COMMAND_OUTPUT_PREVIEW_LINES = 7;
const COMMAND_OUTPUT_PREVIEW_CHARS = 1200;
const TOOL_PAYLOAD_PREVIEW_LINES = 6;
const TOOL_PAYLOAD_PREVIEW_CHARS = 1000;
const COMMENTARY_PREVIEW_LINES = 3;
const COMMENTARY_PREVIEW_CHARS = 260;
const MAX_COMPOSER_ATTACHMENTS = 8;
const FULL_OUTPUT_HINT = "Open Terminal for full output.";
const USE_NATIVE_MARKDOWN_BODY = true;
const TERMINAL_ROUTE_BAR_HEIGHT = 38;
const SCROLL_TO_BOTTOM_LAYOUT_DELAY_MS = 30;
const COMPOSER_FOCUS_LOCK_MS = 1000;
const COMPOSER_REFOCUS_DELAYS_MS = [0, 60, 140, 280, 520, 820] as const;
const STREAMING_REMEND_OPTIONS: RemendOptions = {
  images: true,
  inlineKatex: false,
  linkMode: "text-only",
};

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

type ToolField = {
  label: string;
  value: string;
  mono?: boolean;
};

type ToolPresentation = {
  title: string;
  subtitle?: string;
  icon: IoniconName;
  fields: ToolField[];
  previewUri?: string;
  localImagePath?: string;
  rawInputLabel: string;
  rawOutputLabel: string;
};

type CommandKind =
  | "read"
  | "list"
  | "search"
  | "test"
  | "check"
  | "git"
  | "install"
  | "run";

type CommandPresentation = {
  kind: CommandKind;
  target?: string;
  query?: string;
  detail?: string;
  icon: IoniconName;
  runningTitle: string;
  doneTitle: string;
  failedTitle: string;
  groupable: boolean;
  explorationLabel?: string;
};

type OutputPreview = {
  text: string;
  truncated: boolean;
};

type OutputPreviewOptions = {
  maxLines: number;
  maxChars: number;
};

type ExplorationEntry = {
  event: CodexConversationEvent;
  presentation: CommandPresentation;
  running: boolean;
  failed: boolean;
  output: OutputPreview;
};

type PatchOperation = "add" | "delete" | "update";

type PatchFileSummary = {
  path: string;
  movePath?: string;
  operation: PatchOperation;
  added: number;
  removed: number;
};

type PatchSummary = {
  title: string;
  files: PatchFileSummary[];
  totalAdded: number;
  totalRemoved: number;
};

type RefreshInFlight = {
  baseKey: string;
  requestSeq: number;
};

type ComposerAttachment = UploadedAttachment & {
  id: string;
};

type ChatCommandEvent = {
  id: string;
  command: CodexSlashCommand;
  tone: "neutral" | "success" | "failed";
  title: string;
  detail?: string;
  body?: string;
  createdAt: string;
};

type SlashCommandRequest = {
  command: CodexSlashCommand;
  rawText: string;
  known: boolean;
};

const conversationCache = new Map<string, CodexConversation>();
const draftCache = new Map<string, string>();
const attachmentCache = new Map<string, ComposerAttachment[]>();
const slashCommandCache = new Map<string, CodexSlashCommand[]>();
const chatCommandEventCache = new Map<string, ChatCommandEvent[]>();
const TimelineTextSelectableContext = React.createContext(true);

type LocalSlashCommandCapability = Pick<
  CodexSlashCommand,
  | "category"
  | "execution"
  | "input"
  | "output"
  | "interactive"
  | "chat_supported"
  | "terminal_supported"
>;

type DisplayAttachment = {
  name: string;
  path: string;
};

type ZenTimelineItem =
  | {
      type: "message";
      id: string;
      role: "user";
      timestamp?: string;
      body: string;
      attachments: DisplayAttachment[];
    }
  | {
      type: "message";
      id: string;
      role: "assistant";
      timestamp?: string;
      body: string;
      attachments: DisplayAttachment[];
    }
  | {
      type: "activity";
      id: string;
      timestamp?: string;
      title: string;
      tone: "neutral" | "running" | "success" | "failed";
      icon: IoniconName;
      detail?: string;
      body?: string;
      files?: string[];
      fileSummaries?: PatchFileSummary[];
      previewPath?: string;
    }
  | {
      type: "plan";
      id: string;
      timestamp?: string;
      explanation?: string;
      steps: CodexPlanStep[];
    };

type MarkdownErrorBoundaryProps = {
  fallback: React.ReactNode;
  children: React.ReactNode;
  resetKey: string;
};

type MarkdownErrorBoundaryState = {
  failed: boolean;
};

class MarkdownErrorBoundary extends React.Component<
  MarkdownErrorBoundaryProps,
  MarkdownErrorBoundaryState
> {
  state: MarkdownErrorBoundaryState = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidUpdate(previousProps: MarkdownErrorBoundaryProps) {
    if (previousProps.resetKey !== this.props.resetKey && this.state.failed) {
      this.setState({ failed: false });
    }
  }

  componentDidCatch(error: unknown) {
    console.warn("[codex] native markdown renderer failed", error);
  }

  render() {
    if (this.state.failed) {
      return this.props.fallback;
    }
    return this.props.children;
  }
}

function usePinnedTimeline(itemCount: number) {
  const scrollRef = useRef<ScrollView>(null);
  const nearBottomRef = useRef(true);
  const contentReadyRef = useRef(false);
  const [showJumpToLatest, setShowJumpToLatest] = useState(false);

  const scrollToLatest = useCallback(
    (animated: boolean = true, delay: number = SCROLL_TO_BOTTOM_LAYOUT_DELAY_MS) => {
      nearBottomRef.current = true;
      setShowJumpToLatest(false);
      setTimeout(() => {
        scrollRef.current?.scrollToEnd({ animated });
      }, delay);
    },
    [],
  );

  const pinToBottomIfNeeded = useCallback(
    (animated: boolean = false, delay: number = 0) => {
      if (nearBottomRef.current) {
        scrollToLatest(animated, delay);
      }
    },
    [scrollToLatest],
  );

  const resetForConversation = useCallback(() => {
    nearBottomRef.current = true;
    contentReadyRef.current = false;
    setShowJumpToLatest(false);
  }, []);

  const handleScroll = useCallback(
    (event: NativeSyntheticEvent<NativeScrollEvent>) => {
      const { contentOffset, contentSize, layoutMeasurement } = event.nativeEvent;
      const distanceFromBottom =
        contentSize.height - layoutMeasurement.height - contentOffset.y;
      const nearBottom = distanceFromBottom <= SCROLL_BOTTOM_THRESHOLD;
      nearBottomRef.current = nearBottom;
      setShowJumpToLatest(!nearBottom && itemCount > 0);
    },
    [itemCount],
  );

  const handleContentSizeChange = useCallback(() => {
    if (!contentReadyRef.current || nearBottomRef.current) {
      contentReadyRef.current = true;
      scrollToLatest(true);
    } else if (itemCount > 0) {
      setShowJumpToLatest(true);
    }
  }, [itemCount, scrollToLatest]);

  const handleLayout = useCallback(() => {
    if (contentReadyRef.current) {
      pinToBottomIfNeeded(false);
    }
  }, [pinToBottomIfNeeded]);

  return {
    scrollRef,
    showJumpToLatest,
    scrollToLatest,
    pinToBottomIfNeeded,
    resetForConversation,
    handleScroll,
    handleContentSizeChange,
    handleLayout,
  };
}

function useCodexComposerInput({
  enabled,
  onKeyboardShown,
}: {
  enabled: boolean;
  onKeyboardShown(): void;
}) {
  const inputRef = useRef<TextInput>(null);
  const focusAttemptRef = useRef(0);
  const focusLockUntilRef = useRef(0);
  const blurReleaseTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const refocusTimersRef = useRef<ReturnType<typeof setTimeout>[]>([]);
  const [focused, setFocused] = useState(false);

  const clearBlurReleaseTimer = useCallback(() => {
    if (blurReleaseTimerRef.current) {
      clearTimeout(blurReleaseTimerRef.current);
      blurReleaseTimerRef.current = null;
    }
  }, []);

  const clearRefocusTimers = useCallback(() => {
    refocusTimersRef.current.forEach((timer) => clearTimeout(timer));
    refocusTimersRef.current = [];
  }, []);

  const releaseFocusLock = useCallback(() => {
    focusAttemptRef.current += 1;
    focusLockUntilRef.current = 0;
    clearRefocusTimers();
    clearBlurReleaseTimer();
  }, [clearBlurReleaseTimer, clearRefocusTimers]);

  const restoreFocusIfLocked = useCallback(
    (attempt: number = focusAttemptRef.current) => {
      if (
        enabled &&
        focusAttemptRef.current === attempt &&
        Date.now() <= focusLockUntilRef.current
      ) {
        setFocused(true);
        inputRef.current?.focus();
        return true;
      }
      return false;
    },
    [enabled],
  );

  const focus = useCallback(() => {
    if (!enabled) {
      return;
    }
    const attempt = focusAttemptRef.current + 1;
    focusAttemptRef.current = attempt;
    focusLockUntilRef.current = Date.now() + COMPOSER_FOCUS_LOCK_MS;
    clearRefocusTimers();
    clearBlurReleaseTimer();
    setFocused(true);
    inputRef.current?.focus();
    refocusTimersRef.current = COMPOSER_REFOCUS_DELAYS_MS.map((delay) =>
      setTimeout(() => {
        restoreFocusIfLocked(attempt);
      }, delay),
    );
  }, [clearBlurReleaseTimer, clearRefocusTimers, enabled, restoreFocusIfLocked]);

  const handleFocus = useCallback(() => {
    setFocused(true);
  }, []);

  const handleBlur = useCallback(() => {
    if (Date.now() <= focusLockUntilRef.current && enabled) {
      const attempt = focusAttemptRef.current;
      const timer = setTimeout(() => {
        restoreFocusIfLocked(attempt);
      }, 40);
      refocusTimersRef.current.push(timer);
      return;
    }

    clearBlurReleaseTimer();
    blurReleaseTimerRef.current = setTimeout(() => {
      if (!inputRef.current?.isFocused()) {
        setFocused(false);
      }
      blurReleaseTimerRef.current = null;
    }, 120);
  }, [clearBlurReleaseTimer, enabled, restoreFocusIfLocked]);

  const handleInputStart = useCallback(() => {
    focus();
    return false;
  }, [focus]);

  useEffect(() => {
    const hideSubscription = Keyboard.addListener("keyboardDidHide", () => {
      releaseFocusLock();
      setFocused(false);
    });
    const showSubscription = Keyboard.addListener("keyboardDidShow", () => {
      restoreFocusIfLocked();
      onKeyboardShown();
    });
    return () => {
      hideSubscription.remove();
      showSubscription.remove();
      releaseFocusLock();
    };
  }, [onKeyboardShown, releaseFocusLock, restoreFocusIfLocked]);

  useEffect(() => {
    if (!enabled) {
      releaseFocusLock();
      setFocused(false);
    }
  }, [enabled, releaseFocusLock]);

  return {
    inputRef,
    focused,
    focus,
    handleFocus,
    handleBlur,
    handleInputStart,
  };
}

export function CodexChatSurface({
  serverId,
  agentId,
  agent,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  screenFocused,
  gitDiff,
  onSwitchToTerminal,
  onOpenGitDiff,
}: CodexChatSurfaceProps) {
  const insets = useSafeAreaInsets();
  const conversationCacheKey = `${serverId}:${agentId}`;
  const requestSeqRef = useRef(0);
  const refreshInFlightRef = useRef<RefreshInFlight | null>(null);
  const [conversation, setConversationState] = useState<CodexConversation | null>(
    () => conversationCache.get(conversationCacheKey) ?? null,
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [draft, setDraftState] = useState(() => draftCache.get(conversationCacheKey) ?? "");
  const [attachments, setAttachmentsState] = useState<ComposerAttachment[]>(
    () => attachmentCache.get(conversationCacheKey) ?? [],
  );
  const [sending, setSending] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [slashCommands, setSlashCommands] = useState<CodexSlashCommand[]>(
    () => slashCommandCache.get(serverId) ?? FALLBACK_SLASH_COMMANDS,
  );
  const [chatCommandEvents, setChatCommandEventsState] = useState<ChatCommandEvent[]>(
    () => chatCommandEventCache.get(conversationCacheKey) ?? [],
  );
  const [composerHeight, setComposerHeight] = useState(76);
  const events = conversation?.events ?? [];
  const timeline = usePinnedTimeline(events.length);
  const composerInput = useCodexComposerInput({
    enabled: screenFocused && connectionState === "connected",
    onKeyboardShown: timeline.pinToBottomIfNeeded,
  });
  const {
    scrollRef,
    showJumpToLatest,
    scrollToLatest,
    pinToBottomIfNeeded,
    resetForConversation,
    handleScroll: handleTimelineScroll,
    handleContentSizeChange,
    handleLayout: handleTimelineLayout,
  } = timeline;
  const {
    inputRef,
    focused: composerFocused,
    focus: focusComposer,
    handleFocus: handleComposerFocus,
    handleBlur: handleComposerBlur,
    handleInputStart: handleComposerInputStart,
  } = composerInput;

  const setConversation = useCallback(
    (nextConversation: CodexConversation | null) => {
      if (nextConversation) {
        const cached = conversationCache.get(conversationCacheKey);
        if (nextConversation.available || !cached?.available) {
          conversationCache.set(conversationCacheKey, nextConversation);
        }
      }
      setConversationState(nextConversation);
    },
    [conversationCacheKey],
  );

  const setDraft = useCallback(
    (nextDraft: string) => {
      if (nextDraft) {
        draftCache.set(conversationCacheKey, nextDraft);
      } else {
        draftCache.delete(conversationCacheKey);
      }
      setDraftState(nextDraft);
    },
    [conversationCacheKey],
  );

  const setAttachments = useCallback(
    (nextValue: React.SetStateAction<ComposerAttachment[]>) => {
      setAttachmentsState((current) => {
        const nextAttachments =
          typeof nextValue === "function" ? nextValue(current) : nextValue;
        if (nextAttachments.length > 0) {
          attachmentCache.set(conversationCacheKey, nextAttachments);
        } else {
          attachmentCache.delete(conversationCacheKey);
        }
        return nextAttachments;
      });
    },
    [conversationCacheKey],
  );

  const setChatCommandEvents = useCallback(
    (nextValue: React.SetStateAction<ChatCommandEvent[]>) => {
      setChatCommandEventsState((current) => {
        const nextEvents =
          typeof nextValue === "function" ? nextValue(current) : nextValue;
        const bounded = nextEvents.slice(-12);
        if (bounded.length > 0) {
          chatCommandEventCache.set(conversationCacheKey, bounded);
        } else {
          chatCommandEventCache.delete(conversationCacheKey);
        }
        return bounded;
      });
    },
    [conversationCacheKey],
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
        if (cachedConversation && shouldKeepCachedConversation(cachedConversation, nextConversation)) {
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
    [agent?.command, agent?.cwd, agent?.name, agent?.started_at, agentId, connectionState, serverId, setConversation],
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
    const cachedCommands = slashCommandCache.get(serverId);
    setSlashCommands(
      cachedCommands && cachedCommands.length > 0
        ? cachedCommands
        : FALLBACK_SLASH_COMMANDS,
    );

    if (!screenFocused || connectionState !== "connected") {
      return;
    }

    let cancelled = false;
    void wsClient
      .getCodexSlashCommands(serverId)
      .then((snapshot) => {
        if (cancelled) {
          return;
        }
        const nextCommands = normalizeSlashCommands(snapshot.commands);
        if (nextCommands.length === 0) {
          return;
        }
        slashCommandCache.set(serverId, nextCommands);
        setSlashCommands(nextCommands);
      })
      .catch(() => {
        // The fallback list keeps slash commands usable on older daemons.
      });

    return () => {
      cancelled = true;
    };
  }, [connectionState, screenFocused, serverId]);

  useEffect(() => {
    setConversationState(conversationCache.get(conversationCacheKey) ?? null);
    setError(null);
    setDraftState(draftCache.get(conversationCacheKey) ?? "");
    setAttachmentsState(attachmentCache.get(conversationCacheKey) ?? []);
    setChatCommandEventsState(chatCommandEventCache.get(conversationCacheKey) ?? []);
    refreshInFlightRef.current = null;
    resetForConversation();
  }, [conversationCacheKey, resetForConversation]);

  const unavailable = conversation && !conversation.available;
  const canAttach = connectionState === "connected" && !uploading;
  const canSend =
    connectionState === "connected" &&
    (draft.trim().length > 0 || attachments.length > 0) &&
    !sending &&
    !uploading;

  const statusMeta = useMemo(() => {
    if (connectionIssue) {
      return connectionIssue.title;
    }
    if (connectionState === "connecting") {
      return "Reconnecting";
    }
    if (connectionState !== "connected") {
      return "Offline";
    }
    if (sending || agent?.status === "running" || events.some(isEventRunning)) {
      return "Working";
    }
    if (conversation?.updated_at) {
      return `Updated ${formatTime(conversation.updated_at)}`;
    }
    return "Live";
  }, [agent?.status, connectionIssue, connectionState, conversation?.updated_at, events, sending]);

  const submitTextToCodex = useCallback(
    (text: string, previousDraft: string, previousAttachments: ComposerAttachment[]) => {
      setSending(true);
      setDraft("");
      setAttachments([]);
      scrollToLatest(true);
      try {
        wsClient.sendInput(serverId, agentId, `${text}\n`);
        setTimeout(() => {
          void refreshConversation(false);
          setSending(false);
        }, 600);
      } catch {
        setDraft(previousDraft);
        setAttachments(previousAttachments);
        setSending(false);
      }
    },
    [
      agentId,
      refreshConversation,
      scrollToLatest,
      serverId,
      setAttachments,
      setDraft,
    ],
  );

  const clearComposerForLocalCommand = useCallback(() => {
    setDraft("");
    setAttachments([]);
    scrollToLatest(true);
    setTimeout(() => {
      pinToBottomIfNeeded(true);
    }, SCROLL_TO_BOTTOM_LAYOUT_DELAY_MS);
  }, [pinToBottomIfNeeded, scrollToLatest, setAttachments, setDraft]);

  const openSlashCommandInTerminal = useCallback(
    (command: CodexSlashCommand, rawText?: string) => {
      const text = slashCommandTerminalText(command, rawText);
      const previousDraft = draft;
      const previousAttachments = attachments;
      setDraft("");
      setAttachments([]);
      try {
        wsClient.sendInput(serverId, agentId, `${text}\n`);
        recordChatCommandEvent({
          command,
          tone: "neutral",
          title: "Opened in Terminal",
          detail: command.value,
          body: command.interactive
            ? "This command uses the terminal renderer because it can open prompts, pickers, or terminal-only output."
            : "This command was routed to the terminal renderer.",
        });
        onSwitchToTerminal();
      } catch {
        setDraft(previousDraft);
        setAttachments(previousAttachments);
        recordChatCommandEvent({
          command,
          tone: "failed",
          title: "Command Not Sent",
          detail: command.value,
          body: "Zen could not send this command to the terminal session.",
        });
      }
    },
    [
      agentId,
      attachments,
      draft,
      onSwitchToTerminal,
      recordChatCommandEvent,
      serverId,
      setAttachments,
      setDraft,
    ],
  );

  const runNativeSlashCommand = useCallback(
    async (command: CodexSlashCommand) => {
      clearComposerForLocalCommand();
      switch (command.name) {
        case "status":
          recordChatCommandEvent({
            command,
            tone: connectionState === "connected" ? "success" : "failed",
            title: "Session Status",
            detail: statusMeta,
            body: buildChatStatusCommandBody({
              agent,
              conversation,
              connectionState,
              connectionIssue,
              slashCommands,
            }),
          });
          return;
        case "diff":
          if (onOpenGitDiff || gitDiff?.onPress) {
            (onOpenGitDiff ?? gitDiff?.onPress)?.();
            recordChatCommandEvent({
              command,
              tone: gitDiff?.tone === "error" ? "failed" : "success",
              title: "Opened Git Diff",
              detail: gitDiff?.label || command.value,
              body: gitDiff?.label
                ? `Git diff panel opened. Current summary: ${gitDiff.label}`
                : "Git diff panel opened.",
            });
            return;
          }
          recordChatCommandEvent({
            command,
            tone: "failed",
            title: "Git Diff Unavailable",
            detail: command.value,
            body: "Zen does not have a working directory for this Codex session yet.",
          });
          return;
        case "copy": {
          const latestAssistantMessage = latestAssistantMessageBody(events);
          if (!latestAssistantMessage) {
            recordChatCommandEvent({
              command,
              tone: "failed",
              title: "Nothing to Copy",
              detail: command.value,
              body: "No assistant response is available in the current transcript.",
            });
            return;
          }
          try {
            await Clipboard.setStringAsync(latestAssistantMessage);
            recordChatCommandEvent({
              command,
              tone: "success",
              title: "Copied Last Response",
              detail: `${Array.from(latestAssistantMessage).length} chars`,
              body: "The latest assistant response was copied as markdown.",
            });
          } catch (err: any) {
            recordChatCommandEvent({
              command,
              tone: "failed",
              title: "Copy Failed",
              detail: command.value,
              body: err?.message || "Zen could not write to the clipboard.",
            });
          }
          return;
        }
        default:
          recordChatCommandEvent({
            command,
            tone: "failed",
            title: "Command Not Available",
            detail: command.value,
            body: "Zen does not have a native chat renderer for this slash command yet.",
          });
      }
    },
    [
      agent,
      clearComposerForLocalCommand,
      connectionIssue,
      connectionState,
      conversation,
      events,
      gitDiff,
      onOpenGitDiff,
      recordChatCommandEvent,
      slashCommands,
      statusMeta,
    ],
  );

  const showTerminalRequiredAction = useCallback(
    (
      command: CodexSlashCommand,
      rawText: string,
      composedText: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      Alert.alert(
        `${command.value} needs Terminal`,
        slashCommandTerminalMessage(command),
        [
          { text: "Cancel", style: "cancel" },
          {
            text: "Send Anyway",
            onPress: () =>
              submitTextToCodex(composedText, previousDraft, previousAttachments),
          },
          {
            text: "Open Terminal",
            onPress: () => openSlashCommandInTerminal(command, rawText),
          },
        ],
      );
    },
    [openSlashCommandInTerminal, submitTextToCodex],
  );

  const showUnsupportedSlashCommand = useCallback((command: CodexSlashCommand) => {
    Alert.alert(
      `${command.value} is not available`,
      "This command is hidden or internal in Codex and is not exposed in the chat renderer.",
      [{ text: "OK", style: "cancel" }],
    );
  }, []);

  const showUnknownSlashCommand = useCallback(
    (
      command: CodexSlashCommand,
      rawText: string,
      composedText: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      Alert.alert(
        `${command.value} is not in the catalog`,
        "Zen cannot tell whether this slash command is interactive. Open it in Terminal, or send it as a normal message.",
        [
          { text: "Cancel", style: "cancel" },
          {
            text: "Send as Message",
            onPress: () =>
              submitTextToCodex(composedText, previousDraft, previousAttachments),
          },
          {
            text: "Open Terminal",
            onPress: () => openSlashCommandInTerminal(command, rawText),
          },
        ],
      );
    },
    [openSlashCommandInTerminal, submitTextToCodex],
  );

  const showSlashCommandAttachmentAlert = useCallback(
    (
      command: CodexSlashCommand,
      composedText: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      Alert.alert(
        `${command.value} cannot use attachments here`,
        "Run the slash command without attachments, or send this as a normal message.",
        [
          { text: "Cancel", style: "cancel" },
          {
            text: "Send as Message",
            onPress: () =>
              submitTextToCodex(composedText, previousDraft, previousAttachments),
          },
        ],
      );
    },
    [submitTextToCodex],
  );

  const routeSlashCommandSubmission = useCallback(
    (
      request: SlashCommandRequest,
      composedText: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      const { command, rawText, known } = request;
      if (previousAttachments.length > 0) {
        showSlashCommandAttachmentAlert(
          command,
          composedText,
          previousDraft,
          previousAttachments,
        );
        return true;
      }
      if (requiresSlashCommandArgs(command) && !slashCommandHasArgs(rawText, command)) {
        setDraft(`${command.value} `);
        focusComposer();
        recordChatCommandEvent({
          command,
          tone: "failed",
          title: "Command Needs Input",
          detail: command.value,
          body: command.input.placeholder
            ? `Add arguments after ${command.value}: ${command.input.placeholder}`
            : `Add arguments after ${command.value}.`,
        });
        return true;
      }
      if (!known) {
        showUnknownSlashCommand(
          command,
          rawText,
          composedText,
          previousDraft,
          previousAttachments,
        );
        return true;
      }
      switch (command.execution) {
        case "chat-native":
        case "timeline-output":
          void runNativeSlashCommand(command);
          return true;
        case "terminal-required":
          showTerminalRequiredAction(
            command,
            rawText,
            composedText,
            previousDraft,
            previousAttachments,
          );
          return true;
        case "insert-only":
          return false;
        case "unsupported":
          showUnsupportedSlashCommand(command);
          return true;
        default:
          showTerminalRequiredAction(
            command,
            rawText,
            composedText,
            previousDraft,
            previousAttachments,
          );
          return true;
      }
    },
    [
      focusComposer,
      recordChatCommandEvent,
      runNativeSlashCommand,
      setDraft,
      showSlashCommandAttachmentAlert,
      showTerminalRequiredAction,
      showUnknownSlashCommand,
      showUnsupportedSlashCommand,
    ],
  );

  const sendDraft = useCallback(() => {
    const text = buildCodexComposerMessage(draft, attachments);
    if (!text || connectionState !== "connected" || sending || uploading) {
      return;
    }
    const previousDraft = draft;
    const previousAttachments = attachments;
    const slashRequest = slashCommandRequestFromDraft(draft, slashCommands);
    if (
      slashRequest &&
      routeSlashCommandSubmission(
        slashRequest,
        text,
        previousDraft,
        previousAttachments,
      )
    ) {
      return;
    }
    submitTextToCodex(text, previousDraft, previousAttachments);
  }, [
    attachments,
    connectionState,
    draft,
    routeSlashCommandSubmission,
    sending,
    slashCommands,
    submitTextToCodex,
    uploading,
  ]);

  const interruptCodex = useCallback(() => {
    if (connectionState !== "connected" || sending) {
      return;
    }
    setSending(true);
    try {
      wsClient.sendAction(serverId, agentId, "pause");
      setTimeout(() => {
        void refreshConversation(false);
        setSending(false);
      }, 600);
    } catch {
      setSending(false);
    }
  }, [agentId, connectionState, refreshConversation, sending, serverId]);

  const pickSlashCommand = useCallback((command: CodexSlashCommand) => {
    if (attachments.length > 0) {
      setDraft(`${command.value} `);
      focusComposer();
      return;
    }
    if (command.execution === "unsupported") {
      showUnsupportedSlashCommand(command);
      return;
    }
    if (command.execution === "chat-native" && !requiresSlashCommandArgs(command)) {
      void runNativeSlashCommand(command);
      return;
    }
    if (command.execution === "terminal-required" && !requiresSlashCommandArgs(command)) {
      showTerminalRequiredAction(command, command.value, command.value, draft, attachments);
      return;
    }
    setDraft(`${command.value} `);
    focusComposer();
  }, [
    attachments,
    draft,
    focusComposer,
    runNativeSlashCommand,
    setDraft,
    showTerminalRequiredAction,
    showUnsupportedSlashCommand,
  ]);

  const handleUploadAttachment = useCallback(async () => {
    if (!canAttach) {
      return;
    }
    setUploading(true);
    try {
      const attachment = await uploadDocumentForServer(serverId);
      if (!attachment) {
        return;
      }
      setAttachments((current) =>
        [
          ...current,
          {
            ...attachment,
            id: `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`,
          },
        ].slice(-MAX_COMPOSER_ATTACHMENTS),
      );
      focusComposer();
    } catch (err: any) {
      Alert.alert("Upload failed", err?.message || "Could not upload this file.");
    } finally {
      setUploading(false);
    }
  }, [canAttach, focusComposer, serverId]);

  const removeAttachment = useCallback((id: string) => {
    setAttachments((current) => current.filter((attachment) => attachment.id !== id));
  }, []);

  const commandQuery = draft.trimStart();
  const visibleSlashCommands = useMemo(() => {
    return filterSlashCommands(slashCommands, commandQuery);
  }, [commandQuery, slashCommands]);
  const showCommandMenu =
    connectionState === "connected" &&
    commandQuery.startsWith("/") &&
    !commandQuery.includes(" ");
  const timelineItems = useMemo(
    () => mergeChatCommandEventsIntoTimeline(buildZenTimeline(events), chatCommandEvents),
    [chatCommandEvents, events],
  );
  const showStopButton =
    connectionState === "connected" &&
    agent?.status === "running" &&
    draft.trim().length === 0 &&
    attachments.length === 0 &&
    !sending;
  const sendActionEnabled = canSend || showStopButton;
  const sendActionIcon = showStopButton ? "square" : "arrow-up";
  const sendActionLabel = showStopButton ? "Stop Codex" : "Send message";
  const composerPlaceholder =
    connectionState === "connected" ? "Message Codex" : "Daemon unavailable";
  const streamingAssistantId = "";
  const composerBottomPadding = Math.max(insets.bottom, 8);
  const composerActive = composerFocused || showCommandMenu;
  const keyboardVerticalOffset =
    Platform.OS === "android" ? insets.top + TERMINAL_ROUTE_BAR_HEIGHT : 0;
  const timelineBottomPadding = 18;
  const timelineContentStyle = useMemo(
    () => [styles.timelineContent, { paddingBottom: timelineBottomPadding }],
    [timelineBottomPadding],
  );
  const handleComposerLayout = useCallback((event: LayoutChangeEvent) => {
    const nextHeight = Math.ceil(event.nativeEvent.layout.height);
    setComposerHeight((previous) =>
      Math.abs(previous - nextHeight) <= 1 ? previous : nextHeight,
    );
  }, []);

  useEffect(() => {
    pinToBottomIfNeeded(false);
  }, [composerHeight, pinToBottomIfNeeded]);

  return (
    <View
      style={[styles.root, { backgroundColor: theme.background }]}
    >
      <View
        style={[
          styles.header,
          {
            borderBottomColor: chrome.border,
            backgroundColor: theme.background,
          },
        ]}
      >
        <View style={styles.headerTitleGroup}>
          <View style={styles.headerTitleRow}>
            <Text style={[styles.headerTitle, { color: chrome.text }]} numberOfLines={1}>
              Codex
            </Text>
            <View
              style={[
                styles.statusDot,
                { backgroundColor: statusColor((agent?.status || "unknown") as AgentStatus) },
              ]}
            />
          </View>
          <Text style={[styles.headerMeta, { color: chrome.textSubtle }]} numberOfLines={1}>
            {statusMeta}
          </Text>
        </View>

        {gitDiff ? (
          <ChatHeaderIconButton
            icon={gitDiff.tone === "loading" ? "sync-outline" : "git-branch-outline"}
            accessibilityLabel="Git diff"
            chrome={chrome}
            color={gitDiff.tone === "dirty" ? chrome.accent : chrome.textMuted}
            onPress={gitDiff.onPress}
          />
        ) : null}

        <ChatHeaderIconButton
          icon="terminal-outline"
          accessibilityLabel="Open terminal renderer"
          chrome={chrome}
          onPress={onSwitchToTerminal}
        />
      </View>

      <KeyboardAvoidingView
        behavior="padding"
        enabled={screenFocused}
        keyboardVerticalOffset={keyboardVerticalOffset}
        style={styles.chatBody}
      >
        <TimelineTextSelectableContext.Provider value={!composerActive}>
          <ScrollView
            ref={scrollRef}
            style={styles.timeline}
            contentContainerStyle={timelineContentStyle}
            scrollIndicatorInsets={{ bottom: timelineBottomPadding }}
            keyboardShouldPersistTaps="handled"
            showsVerticalScrollIndicator={false}
            scrollEventThrottle={80}
            onLayout={handleTimelineLayout}
            onScroll={handleTimelineScroll}
            onContentSizeChange={handleContentSizeChange}
          >
            {loading && timelineItems.length === 0 ? (
              <EmptyState chrome={chrome} title="Loading Codex transcript" busy />
            ) : error && timelineItems.length === 0 ? (
              <EmptyState chrome={chrome} title="Transcript unavailable" body={error} />
            ) : unavailable ? (
              <EmptyState
                chrome={chrome}
                title="Native transcript unavailable"
                body={conversationUnavailableReason(conversation?.reason)}
                actionLabel="Terminal"
                onAction={onSwitchToTerminal}
              />
            ) : timelineItems.length === 0 ? (
              <EmptyState chrome={chrome} title="Waiting for Codex transcript" />
            ) : (
              timelineItems.map((item) => (
                <ZenTimelineItemView
                  key={item.id}
                  serverId={serverId}
                  cwd={conversation?.cwd || agent?.cwd}
                  item={item}
                  chrome={chrome}
                  theme={theme}
                  stream={
                    item.type === "message" &&
                    item.role === "assistant" &&
                    item.id === streamingAssistantId
                  }
                />
              ))
            )}
          </ScrollView>
        </TimelineTextSelectableContext.Provider>

        {showJumpToLatest ? (
          <TouchableOpacity
            accessibilityLabel="Jump to latest"
            style={[
              styles.jumpButton,
              {
                backgroundColor: chrome.surfaceMuted,
                borderColor: chrome.borderStrong,
                bottom: composerHeight + 12,
              },
            ]}
            onPress={() => scrollToLatest(true)}
            activeOpacity={0.82}
          >
            <Ionicons name="arrow-down" size={15} color={chrome.accent} />
            <Text style={[styles.jumpButtonText, { color: chrome.textMuted }]}>Latest</Text>
          </TouchableOpacity>
        ) : null}

        <View
          onLayout={handleComposerLayout}
          style={[
            styles.composer,
            {
              paddingBottom: composerBottomPadding,
              borderTopColor: chrome.border,
              backgroundColor: theme.background,
            },
          ]}
        >
        {showCommandMenu ? (
          <View
            style={[
              styles.quickCommandMenu,
              { backgroundColor: chrome.surface, borderColor: chrome.border },
            ]}
          >
            {visibleSlashCommands.length > 0 ? (
              <ScrollView
                style={styles.quickCommandScroller}
                keyboardShouldPersistTaps="handled"
                showsVerticalScrollIndicator={visibleSlashCommands.length > 5}
              >
                {visibleSlashCommands.map((command) => {
                  const selected = commandQuery === command.value;
                  const icon = slashCommandIcon(command.name);
                  return (
                    <TouchableOpacity
                      key={command.value}
                      accessibilityLabel={`${slashCommandRouteLabel(command)} ${command.value}`}
                      style={[
                        styles.quickCommandRow,
                        selected && { backgroundColor: chrome.surfaceMuted },
                      ]}
                      onPress={() => pickSlashCommand(command)}
                      activeOpacity={0.78}
                    >
                      <View
                        style={[
                          styles.quickCommandIcon,
                          { backgroundColor: chrome.surfaceMuted },
                        ]}
                      >
                        <Ionicons name={icon} size={15} color={chrome.accent} />
                      </View>
                      <View style={styles.quickCommandCopy}>
                        <Text
                          style={[styles.quickCommandTitle, { color: chrome.text }]}
                          numberOfLines={1}
                        >
                          {command.title || slashCommandTitle(command.name)}
                        </Text>
                        <Text
                          style={[
                            styles.quickCommandDescription,
                            { color: chrome.textSubtle },
                          ]}
                          numberOfLines={1}
                        >
                          {command.description}
                        </Text>
                      </View>
                      <View
                        style={[
                          styles.quickCommandBadge,
                          { borderColor: slashCommandRouteColor(command, chrome, theme) },
                        ]}
                      >
                        <Text
                          style={[
                            styles.quickCommandBadgeText,
                            { color: slashCommandRouteColor(command, chrome, theme) },
                          ]}
                          numberOfLines={1}
                        >
                          {slashCommandRouteLabel(command)}
                        </Text>
                      </View>
                      <Text
                        style={[styles.quickCommandValue, { color: chrome.textMuted }]}
                        numberOfLines={1}
                      >
                        {command.value}
                      </Text>
                      <Ionicons
                        name="chevron-forward"
                        size={14}
                        color={chrome.textSubtle}
                      />
                    </TouchableOpacity>
                  );
                })}
              </ScrollView>
            ) : (
              <View style={styles.quickCommandEmpty}>
                <Ionicons name="search-outline" size={15} color={chrome.textSubtle} />
                <Text style={[styles.quickCommandDescription, { color: chrome.textSubtle }]}>
                  No matching command
                </Text>
              </View>
            )}
          </View>
        ) : null}

        {attachments.length > 0 || uploading ? (
          <View style={styles.attachmentRail}>
            <ScrollView
              horizontal
              showsHorizontalScrollIndicator={false}
              keyboardShouldPersistTaps="handled"
              contentContainerStyle={styles.attachmentList}
            >
              {attachments.map((attachment) => (
                <View
                  key={attachment.id}
                  style={[
                    styles.attachmentChip,
                    { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
                  ]}
                >
                  <Ionicons
                    name={
                      looksLikeImagePath(attachment.name)
                        ? "image-outline"
                        : "document-attach-outline"
                    }
                    size={14}
                    color={chrome.textMuted}
                  />
                  <View style={styles.attachmentTextGroup}>
                    <Text
                      style={[styles.attachmentName, { color: chrome.text }]}
                      numberOfLines={1}
                    >
                      {attachment.name}
                    </Text>
                    <Text
                      style={[styles.attachmentPath, { color: chrome.textSubtle }]}
                      numberOfLines={1}
                    >
                      {basename(attachment.path)}
                    </Text>
                  </View>
                  <TouchableOpacity
                    accessibilityLabel={`Remove ${attachment.name}`}
                    style={styles.attachmentRemove}
                    onPress={() => removeAttachment(attachment.id)}
                    activeOpacity={0.72}
                  >
                    <Ionicons name="close" size={13} color={chrome.textSubtle} />
                  </TouchableOpacity>
                </View>
              ))}
              {uploading ? (
                <View
                  style={[
                    styles.attachmentChip,
                    styles.attachmentUploading,
                    { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
                  ]}
                >
                  <ActivityIndicator size="small" color={chrome.accent} />
                  <Text style={[styles.attachmentName, { color: chrome.textMuted }]}>
                    Uploading
                  </Text>
                </View>
              ) : null}
            </ScrollView>
          </View>
        ) : null}

        <View
          collapsable={false}
          style={[
            styles.composerPanel,
            composerActive ? styles.composerPanelFloating : null,
            {
              backgroundColor: composerFocused ? chrome.surfaceActive : chrome.surface,
              borderColor: composerFocused ? chrome.borderStrong : chrome.border,
            },
          ]}
        >
          <ComposerIconButton
            accessibilityLabel="Upload file"
            icon="add"
            chrome={chrome}
            loading={uploading}
            disabled={!canAttach}
            iconColor={canAttach ? chrome.text : chrome.textSubtle}
            onPress={() => void handleUploadAttachment()}
          />

          <View
            collapsable={false}
            onStartShouldSetResponderCapture={handleComposerInputStart}
            style={styles.composerInputWrap}
          >
            <TextInput
              ref={inputRef}
              style={[
                styles.composerInput,
                {
                  color: chrome.text,
                },
              ]}
              value={draft}
              onChangeText={setDraft}
              placeholder={composerPlaceholder}
              placeholderTextColor={chrome.textSubtle}
              selectionColor={chrome.accent}
              multiline
              editable={connectionState === "connected"}
              textAlignVertical="top"
              autoCorrect={false}
              autoCapitalize="none"
              autoComplete="off"
              spellCheck={false}
              keyboardType={Platform.OS === "android" ? "visible-password" : "default"}
              disableFullscreenUI
              importantForAutofill="no"
              selectTextOnFocus={false}
              underlineColorAndroid="transparent"
              showSoftInputOnFocus
              returnKeyType="send"
              enterKeyHint="send"
              submitBehavior="submit"
              blurOnSubmit={false}
              onPressIn={focusComposer}
              onSubmitEditing={sendDraft}
              onFocus={handleComposerFocus}
              onBlur={handleComposerBlur}
            />
          </View>

          <ComposerSendButton
            accessibilityLabel={sendActionLabel}
            icon={sendActionIcon}
            chrome={chrome}
            theme={theme}
            enabled={sendActionEnabled}
            loading={sending}
            compact={showStopButton}
            onPress={showStopButton ? interruptCodex : sendDraft}
          />
        </View>
        </View>
      </KeyboardAvoidingView>
    </View>
  );
}

function normalizeSlashCommands(commands: CodexSlashCommand[]) {
  const seen = new Set<string>();
  const normalized: CodexSlashCommand[] = [];
  for (const command of commands) {
    const name = command.name.trim().replace(/^\//, "");
    const rawValue = command.value.trim();
    const value = rawValue.length > 1 && rawValue.startsWith("/") ? rawValue : `/${name}`;
    if (!name || !value.startsWith("/") || seen.has(value)) {
      continue;
    }
    seen.add(value);
    normalized.push({
      value,
      name,
      title: command.title.trim() || slashCommandTitle(name),
      description: command.description.trim(),
      source: command.source,
      ...normalizeSlashCommandCapability(name, command),
    });
  }
  return normalized.length > 0 ? normalized : FALLBACK_SLASH_COMMANDS;
}

function slashCommandRequestFromDraft(
  draft: string,
  commands: CodexSlashCommand[],
): SlashCommandRequest | null {
  const trimmedStart = draft.trimStart();
  if (!trimmedStart.startsWith("/")) {
    return null;
  }
  const firstLine = trimmedStart.split(/\r?\n/, 1)[0] || "";
  const match = /^\/([a-z][a-z0-9-]*)(?:\s|$)/.exec(firstLine);
  if (!match) {
    return null;
  }
  const name = match[1];
  const command = commands.find((candidate) => candidate.name === name);
  if (command) {
    return { command, rawText: trimmedStart, known: true };
  }
  return {
    command: {
      value: `/${name}`,
      name,
      title: slashCommandTitle(name),
      description: "Unknown Codex slash command",
      source: "draft",
      ...fallbackSlashCommandCapability(name),
    },
    rawText: trimmedStart,
    known: false,
  };
}

function requiresSlashCommandArgs(command: CodexSlashCommand) {
  return (
    command.input.kind === "inline-args" ||
    command.input.kind === "freeform" ||
    command.input.kind === "form"
  );
}

function slashCommandHasArgs(rawText: string, command: CodexSlashCommand) {
  const args = rawText.trimStart().slice(command.value.length).trim();
  return args.length > 0;
}

function slashCommandTerminalText(command: CodexSlashCommand, rawText?: string) {
  const text = rawText?.trim();
  if (text?.startsWith(command.value)) {
    return text;
  }
  return command.value;
}

function slashCommandTerminalMessage(command: CodexSlashCommand) {
  if (command.interactive) {
    return "This command can open Codex prompts, pickers, or terminal-only views. The chat renderer cannot represent that interaction yet.";
  }
  if (command.output.kind === "terminal") {
    return "This command writes terminal-oriented output. Open it in Terminal for correct rendering, or send it anyway as a normal message.";
  }
  return "Zen does not have a native chat renderer for this command yet.";
}

function buildChatStatusCommandBody({
  agent,
  conversation,
  connectionState,
  connectionIssue,
  slashCommands,
}: {
  agent?: Agent;
  conversation: CodexConversation | null;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  slashCommands: CodexSlashCommand[];
}) {
  const nativeCommands = slashCommands.filter((command) => command.chat_supported).length;
  const terminalCommands = slashCommands.filter((command) => command.terminal_supported).length;
  const lines = [
    `Connection: ${connectionState}${connectionIssue ? ` (${connectionIssue.title})` : ""}`,
    `Agent: ${agent?.name || agent?.id || "unknown"}${agent?.status ? ` (${agent.status})` : ""}`,
    `Project: ${agent?.project || conversation?.cwd || agent?.cwd || "unknown"}`,
    `Transcript: ${conversation?.available ? "available" : conversation?.reason || "unavailable"}`,
    `Events: ${conversation?.events.length ?? 0}`,
    `Slash commands: ${slashCommands.length} discovered, ${nativeCommands} chat-native, ${terminalCommands} terminal-capable`,
  ];
  if (conversation?.updated_at) {
    lines.splice(4, 0, `Updated: ${formatTime(conversation.updated_at)}`);
  }
  return lines.join("\n");
}

function latestAssistantMessageBody(events: CodexConversationEvent[]) {
  for (let index = events.length - 1; index >= 0; index--) {
    const event = events[index];
    if (event.kind === "assistant_message" && event.body?.trim()) {
      return event.body.trim();
    }
  }
  return "";
}

function normalizeSlashCommandCapability(
  name: string,
  command: Partial<CodexSlashCommand>,
): LocalSlashCommandCapability {
  const fallback = fallbackSlashCommandCapability(name);
  const execution =
    typeof command.execution === "string" && command.execution.trim()
      ? command.execution.trim()
      : fallback.execution;
  const terminalSupported =
    typeof command.terminal_supported === "boolean"
      ? command.terminal_supported
      : fallback.terminal_supported;
  return {
    category:
      typeof command.category === "string" && command.category.trim()
        ? command.category.trim()
        : fallback.category,
    execution,
    input: {
      kind:
        command.input?.kind && typeof command.input.kind === "string"
          ? command.input.kind
          : fallback.input.kind,
      placeholder:
        typeof command.input?.placeholder === "string"
          ? command.input.placeholder
          : fallback.input.placeholder,
      picker:
        typeof command.input?.picker === "string"
          ? command.input.picker
          : fallback.input.picker,
    },
    output: {
      kind:
        command.output?.kind && typeof command.output.kind === "string"
          ? command.output.kind
          : fallback.output.kind,
    },
    interactive:
      typeof command.interactive === "boolean"
        ? command.interactive
        : fallback.interactive,
    chat_supported:
      typeof command.chat_supported === "boolean"
        ? command.chat_supported
        : fallback.chat_supported,
    terminal_supported: terminalSupported,
  };
}

function fallbackSlashCommandCapability(name: string): LocalSlashCommandCapability {
  switch (name) {
    case "status":
      return chatSlashCapability("session", "status-card");
    case "diff":
      return chatSlashCapability("tools", "diff");
    case "copy":
      return chatSlashCapability("tools", "none");
    case "debug-m-drop":
    case "debug-m-update":
      return {
        category: "debug",
        execution: "unsupported",
        input: { kind: "none" },
        output: { kind: "terminal" },
        interactive: true,
        chat_supported: false,
        terminal_supported: false,
      };
    case "debug-config":
    case "test-approval":
      return terminalSlashCapability("debug", { kind: "none" }, true);
    default:
      return terminalSlashCapability(
        slashCommandDefaultCategory(name),
        slashCommandDefaultInput(name),
        slashCommandDefaultsToInteractive(name),
      );
  }
}

function chatSlashCapability(
  category: string,
  outputKind: CodexSlashCommand["output"]["kind"],
): LocalSlashCommandCapability {
  return {
    category,
    execution: "chat-native",
    input: { kind: "none" },
    output: { kind: outputKind },
    interactive: false,
    chat_supported: true,
    terminal_supported: true,
  };
}

function terminalSlashCapability(
  category: string,
  input: CodexSlashCommand["input"],
  interactive: boolean,
): LocalSlashCommandCapability {
  return {
    category,
    execution: "terminal-required",
    input,
    output: { kind: input.kind === "picker" ? "management-screen" : "terminal" },
    interactive,
    chat_supported: false,
    terminal_supported: true,
  };
}

function slashCommandDefaultCategory(name: string) {
  switch (name) {
    case "model":
    case "fast":
    case "ide":
    case "permissions":
    case "keymap":
    case "setup-default-sandbox":
    case "sandbox-add-read-dir":
    case "vim":
    case "experimental":
    case "title":
    case "statusline":
    case "theme":
    case "pets":
    case "personality":
    case "realtime":
    case "settings":
      return "settings";
    case "resume":
    case "fork":
    case "side":
    case "agent":
    case "subagents":
    case "btw":
      return "navigation";
    case "memories":
    case "skills":
    case "hooks":
    case "mcp":
    case "apps":
    case "plugins":
    case "feedback":
      return "management";
    case "logout":
    case "quit":
    case "exit":
      return "danger";
    case "review":
    case "init":
    case "mention":
    case "raw":
    case "rollout":
    case "ps":
    case "stop":
      return "tools";
    case "approve":
    case "rename":
    case "new":
    case "compact":
    case "plan":
    case "goal":
    case "clear":
      return "session";
    default:
      return name.startsWith("debug-") ? "debug" : "unknown";
  }
}

function slashCommandDefaultInput(name: string): CodexSlashCommand["input"] {
  switch (name) {
    case "fast":
      return { kind: "inline-args", placeholder: "optional speed mode" };
    case "model":
    case "permissions":
    case "resume":
    case "fork":
    case "mention":
    case "theme":
    case "pets":
    case "personality":
    case "agent":
    case "subagents":
      return { kind: "picker", picker: name };
    case "sandbox-add-read-dir":
      return { kind: "inline-args", placeholder: "<absolute_path>" };
    case "mcp":
      return { kind: "inline-args", placeholder: "verbose" };
    case "rename":
      return { kind: "freeform", placeholder: "new thread title" };
    case "goal":
      return { kind: "freeform", placeholder: "goal text" };
    case "side":
    case "btw":
      return { kind: "freeform", placeholder: "side conversation prompt" };
    default:
      return { kind: "none" };
  }
}

function slashCommandDefaultsToInteractive(name: string) {
  const input = slashCommandDefaultInput(name);
  if (input.kind === "picker" || input.kind === "form") {
    return true;
  }
  return [
    "model",
    "fast",
    "ide",
    "permissions",
    "keymap",
    "setup-default-sandbox",
    "vim",
    "experimental",
    "memories",
    "skills",
    "hooks",
    "new",
    "resume",
    "fork",
    "side",
    "raw",
    "mention",
    "title",
    "statusline",
    "theme",
    "pets",
    "mcp",
    "apps",
    "plugins",
    "logout",
    "quit",
    "exit",
    "feedback",
    "clear",
    "personality",
    "realtime",
    "settings",
    "test-approval",
    "agent",
    "subagents",
    "btw",
  ].includes(name);
}

function filterSlashCommands(commands: CodexSlashCommand[], commandQuery: string) {
  if (!commandQuery.startsWith("/")) {
    return [];
  }
  const query = commandQuery.slice(1).toLowerCase();
  if (!query) {
    return commands;
  }

  return commands
    .map((command, index) => {
      const name = command.name.toLowerCase();
      const value = command.value.toLowerCase();
      const title = command.title.toLowerCase();
      const description = command.description.toLowerCase();
      let score = Number.POSITIVE_INFINITY;
      if (name === query || value === `/${query}`) {
        score = 0;
      } else if (name.startsWith(query) || value.startsWith(`/${query}`)) {
        score = 1;
      } else if (title.startsWith(query)) {
        score = 2;
      } else if (name.includes(query) || value.includes(query)) {
        score = 3;
      } else if (title.includes(query)) {
        score = 4;
      } else if (description.includes(query)) {
        score = 5;
      }
      return { command, index, score };
    })
    .filter((entry) => Number.isFinite(entry.score))
    .sort((a, b) => a.score - b.score || a.index - b.index)
    .map((entry) => entry.command);
}

function slashCommandTitle(name: string) {
  return name
    .split("-")
    .filter(Boolean)
    .map((part) => {
      const lower = part.toLowerCase();
      if (lower === "ide" || lower === "mcp") {
        return lower.toUpperCase();
      }
      return `${part.slice(0, 1).toUpperCase()}${part.slice(1)}`;
    })
    .join(" ");
}

function slashCommandIcon(name: string): IoniconName {
  switch (name) {
    case "model":
      return "hardware-chip-outline";
    case "fast":
      return "flash-outline";
    case "ide":
      return "code-slash-outline";
    case "permissions":
    case "approve":
    case "test-approval":
      return "shield-checkmark-outline";
    case "keymap":
      return "keypad-outline";
    case "setup-default-sandbox":
    case "sandbox-add-read-dir":
      return "lock-open-outline";
    case "vim":
      return "create-outline";
    case "experimental":
      return "flask-outline";
    case "memories":
      return "library-outline";
    case "skills":
      return "sparkles-outline";
    case "hooks":
      return "link-outline";
    case "review":
      return "search-outline";
    case "rename":
    case "title":
      return "text-outline";
    case "new":
      return "add-circle-outline";
    case "resume":
      return "play-forward-outline";
    case "fork":
    case "side":
      return "git-branch-outline";
    case "init":
      return "document-text-outline";
    case "compact":
      return "contract-outline";
    case "plan":
      return "list-outline";
    case "goal":
      return "flag-outline";
    case "copy":
      return "copy-outline";
    case "raw":
      return "reorder-four-outline";
    case "diff":
      return "git-compare-outline";
    case "mention":
      return "at-outline";
    case "status":
      return "pulse-outline";
    case "debug-config":
    case "debug-m-drop":
    case "debug-m-update":
      return "bug-outline";
    case "statusline":
      return "reader-outline";
    case "theme":
      return "color-palette-outline";
    case "pets":
      return "happy-outline";
    case "mcp":
      return "server-outline";
    case "apps":
    case "plugins":
      return "extension-puzzle-outline";
    case "logout":
    case "quit":
    case "exit":
      return "exit-outline";
    case "feedback":
      return "chatbox-ellipses-outline";
    case "rollout":
      return "map-outline";
    case "ps":
      return "layers-outline";
    case "stop":
      return "stop-circle-outline";
    case "clear":
      return "trash-outline";
    case "personality":
      return "person-circle-outline";
    case "realtime":
    case "settings":
      return "mic-outline";
    case "agent":
    case "subagents":
      return "people-outline";
    case "btw":
      return "chatbubble-ellipses-outline";
    default:
      return "terminal-outline";
  }
}

function slashCommandRouteLabel(command: CodexSlashCommand) {
  if (command.execution === "unsupported" || !command.terminal_supported && !command.chat_supported) {
    return "Unsupported";
  }
  if (command.execution === "chat-native" || command.execution === "timeline-output") {
    if (command.output.kind === "diff") {
      return "Diff";
    }
    if (command.output.kind === "status-card") {
      return "Status";
    }
    return "Chat";
  }
  if (command.interactive || command.input.kind === "picker" || command.input.kind === "form") {
    return "Interactive";
  }
  if (command.execution === "insert-only") {
    return "Insert";
  }
  return "Terminal";
}

function slashCommandRouteColor(
  command: CodexSlashCommand,
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
) {
  if (command.execution === "unsupported" || !command.terminal_supported && !command.chat_supported) {
    return theme.red;
  }
  if (command.execution === "chat-native" || command.execution === "timeline-output") {
    return theme.green;
  }
  if (command.interactive || command.input.kind === "picker" || command.input.kind === "form") {
    return theme.yellow;
  }
  if (command.execution === "insert-only") {
    return theme.cyan;
  }
  return chrome.textSubtle;
}

function ZenTimelineItemView({
  serverId,
  cwd,
  item,
  chrome,
  theme,
  stream,
}: {
  serverId: string;
  cwd?: string;
  item: ZenTimelineItem;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  stream: boolean;
}) {
  if (item.type === "message") {
    if (item.role === "user") {
      return <ZenUserMessage item={item} chrome={chrome} theme={theme} />;
    }
    return (
      <ZenAssistantMessage
        item={item}
        chrome={chrome}
        theme={theme}
        stream={stream}
      />
    );
  }
  if (item.type === "plan") {
    return <ZenPlanUpdate item={item} chrome={chrome} theme={theme} />;
  }
  return (
    <ZenActivityEvent
      serverId={serverId}
      cwd={cwd}
      item={item}
      chrome={chrome}
      theme={theme}
    />
  );
}

function ZenUserMessage({
  item,
  chrome,
  theme,
}: {
  item: Extract<ZenTimelineItem, { type: "message" }> & { role: "user" };
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const hasBody = item.body.trim().length > 0;
  return (
    <View style={styles.zenUserRow}>
      <View style={[styles.zenUserBubble, { backgroundColor: chrome.surfaceMuted }]}>
        {hasBody ? (
          <MessageBody value={item.body} chrome={chrome} theme={theme} compact />
        ) : null}
        {item.attachments.length > 0 ? (
          <AttachmentPreviewList
            attachments={item.attachments}
            chrome={chrome}
            compact={hasBody}
          />
        ) : null}
      </View>
    </View>
  );
}

function ZenAssistantMessage({
  item,
  chrome,
  theme,
  stream,
}: {
  item: Extract<ZenTimelineItem, { type: "message" }> & { role: "assistant" };
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  stream: boolean;
}) {
  return (
    <View style={styles.zenAssistantRow}>
      <StreamingMessageBody
        value={item.body}
        chrome={chrome}
        theme={theme}
        stream={stream}
      />
    </View>
  );
}

function StreamingMessageBody({
  value,
  chrome,
  theme,
  stream,
}: {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  stream: boolean;
}) {
  const [visibleChars, setVisibleChars] = useState(stream ? 0 : value.length);

  useEffect(() => {
    if (!stream) {
      setVisibleChars(value.length);
      return;
    }
    setVisibleChars((current) => Math.min(current, value.length));
  }, [stream, value.length]);

  useEffect(() => {
    if (!stream || visibleChars >= value.length) {
      return;
    }
    const timer = setTimeout(() => {
      setVisibleChars((current) => Math.min(value.length, current + 18));
    }, 24);
    return () => clearTimeout(timer);
  }, [stream, value.length, visibleChars]);

  const renderedValue = stream ? value.slice(0, visibleChars) : value;
  return (
    <View style={styles.zenAssistantContent}>
      <CodexMarkdownBody
        value={renderedValue}
        chrome={chrome}
        theme={theme}
        streaming={stream && visibleChars < value.length}
      />
      {stream && visibleChars < value.length ? (
        <View style={[styles.zenStreamCursor, { backgroundColor: chrome.accent }]} />
      ) : null}
    </View>
  );
}

function CodexMarkdownBody({
  value,
  chrome,
  theme,
  compact = false,
  streaming = false,
}: {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
  streaming?: boolean;
}) {
  const textSelectable = useContext(TimelineTextSelectableContext);
  const markdown = useMemo(() => prepareCodexMarkdown(value, streaming), [streaming, value]);
  const markdownStyle = useMemo(
    () => codexMarkdownStyle(chrome, theme, compact),
    [chrome, compact, theme],
  );
  const fallback = (
    <MessageBody value={markdown || value} chrome={chrome} theme={theme} compact={compact} />
  );
  const handleLinkPress = useCallback((event: LinkPressEvent) => {
    const url = event.url.trim();
    if (!isSafeMarkdownUrl(url)) {
      return;
    }
    void Linking.openURL(url).catch(() => undefined);
  }, []);

  if (!USE_NATIVE_MARKDOWN_BODY || !markdown) {
    return fallback;
  }

  return (
    <MarkdownErrorBoundary fallback={fallback} resetKey={markdown}>
      <EnrichedMarkdownText
        markdown={markdown}
        markdownStyle={markdownStyle}
        containerStyle={styles.messageBody}
        flavor="github"
        selectable={textSelectable}
        allowFontScaling={false}
        allowTrailingMargin={false}
        enableLinkPreview={false}
        md4cFlags={{ latexMath: false, underline: false }}
        onLinkPress={handleLinkPress}
        streamingAnimation={streaming}
        spoilerOverlay="solid"
      />
    </MarkdownErrorBoundary>
  );
}

function ZenPlanUpdate({
  item,
  chrome,
  theme,
}: {
  item: Extract<ZenTimelineItem, { type: "plan" }>;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  return (
    <View style={styles.zenActivityWrap}>
      <View style={styles.zenActivityRow}>
        <Ionicons name="checkbox-outline" size={13} color={theme.cyan} />
        <Text style={[styles.zenActivityTitle, { color: chrome.textSubtle }]} numberOfLines={1}>
          Updated Plan
        </Text>
      </View>
      <View style={[styles.zenActivityExpanded, styles.zenPlanBlock, { borderColor: chrome.border }]}>
        {item.explanation?.trim() ? (
          <Text style={[styles.zenPlanExplanation, { color: chrome.textSubtle }]}>
            {item.explanation.trim()}
          </Text>
        ) : null}
        {item.steps.length > 0 ? (
          <View style={styles.zenPlanSteps}>
            {item.steps.map((step, index) => (
              <ZenPlanStepRow
                key={`${index}:${step.step}`}
                step={step}
                chrome={chrome}
                theme={theme}
              />
            ))}
          </View>
        ) : (
          <Text style={[styles.zenPlanEmpty, { color: chrome.textSubtle }]}>
            (no steps provided)
          </Text>
        )}
      </View>
    </View>
  );
}

function ZenPlanStepRow({
  step,
  chrome,
  theme,
}: {
  step: CodexPlanStep;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const completed = step.status === "completed";
  const inProgress = step.status === "in_progress";
  const marker = completed ? "✔" : "□";
  const color = completed ? chrome.textSubtle : inProgress ? theme.cyan : chrome.textMuted;
  return (
    <View style={styles.zenPlanStepRow}>
      <Text style={[styles.zenPlanMarker, { color }]}>{marker}</Text>
      <Text
        style={[
          styles.zenPlanStepText,
          completed ? styles.zenPlanStepCompleted : null,
          inProgress ? styles.zenPlanStepActive : null,
          { color },
        ]}
      >
        {step.step}
      </Text>
    </View>
  );
}

function ZenActivityEvent({
  serverId,
  cwd,
  item,
  chrome,
  theme,
}: {
  serverId: string;
  cwd?: string;
  item: Extract<ZenTimelineItem, { type: "activity" }>;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const [expanded, setExpanded] = useState(() => shouldAutoExpandActivity(item));
  const [assetPreviewUri, setAssetPreviewUri] = useState<string | null>(null);
  const [assetPreviewFailed, setAssetPreviewFailed] = useState(false);
  const textSelectable = useContext(TimelineTextSelectableContext);
  const toneColor =
    item.tone === "failed"
      ? theme.red
      : item.tone === "running"
        ? theme.yellow
        : item.tone === "success"
          ? theme.green
          : chrome.textSubtle;

  useEffect(() => {
    let cancelled = false;
    setAssetPreviewUri(null);
    setAssetPreviewFailed(false);
    if (!item.previewPath || !serverId) {
      return () => {
        cancelled = true;
      };
    }
    void wsClient
      .getCodexAsset(serverId, { path: item.previewPath, cwd })
      .then((asset) => {
        if (!cancelled && asset.data_url) {
          setAssetPreviewUri(asset.data_url);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setAssetPreviewFailed(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [cwd, item.previewPath, serverId]);

  const canExpand = Boolean(
    item.body || item.fileSummaries?.length || item.files?.length || item.previewPath,
  );
  return (
    <View style={styles.zenActivityWrap}>
      <TouchableOpacity
        accessibilityLabel={item.title}
        style={styles.zenActivityRow}
        onPress={() => {
          if (canExpand) {
            setExpanded((value) => !value);
          }
        }}
        disabled={!canExpand}
        activeOpacity={0.76}
      >
        {item.tone === "running" ? (
          <ActivityIndicator size="small" color={toneColor} />
        ) : (
          <Ionicons name={item.icon} size={13} color={toneColor} />
        )}
        <Text style={[styles.zenActivityTitle, { color: chrome.textSubtle }]} numberOfLines={1}>
          {item.title}
        </Text>
        {item.detail ? (
          <Text style={[styles.zenActivityDetail, { color: chrome.textSubtle }]} numberOfLines={1}>
            {item.detail}
          </Text>
        ) : null}
        {canExpand ? (
          <Ionicons
            name={expanded ? "chevron-up" : "chevron-down"}
            size={12}
            color={chrome.textSubtle}
          />
        ) : null}
      </TouchableOpacity>

      {expanded ? (
        <View style={[styles.zenActivityExpanded, { borderColor: chrome.border }]}>
          {item.previewPath ? (
            assetPreviewUri ? (
              <Image
                source={{ uri: assetPreviewUri }}
                style={[styles.zenActivityImage, { borderColor: chrome.border }]}
                resizeMode="cover"
              />
            ) : (
              <View style={[styles.zenActivityImagePlaceholder, { borderColor: chrome.border }]}>
                {assetPreviewFailed ? (
                  <Ionicons name="image-outline" size={16} color={chrome.textSubtle} />
                ) : (
                  <ActivityIndicator size="small" color={chrome.textSubtle} />
                )}
              </View>
            )
          ) : null}
          {item.fileSummaries?.length ? (
            <View style={styles.zenDiffFiles}>
              {item.fileSummaries.slice(0, 6).map((file) => (
                <View key={`${file.operation}:${file.path}`} style={styles.zenDiffFileRow}>
                  <Text style={[styles.zenDiffPrefix, { color: chrome.textSubtle }]}>
                    └
                  </Text>
                  <Text
                    style={[styles.zenDiffPath, { color: chrome.textMuted }]}
                    numberOfLines={1}
                  >
                    {patchDisplayPath(file)}
                  </Text>
                  <Text style={[styles.zenDiffAdded, { color: theme.green }]}>
                    +{file.added}
                  </Text>
                  <Text style={[styles.zenDiffRemoved, { color: theme.red }]}>
                    -{file.removed}
                  </Text>
                </View>
              ))}
            </View>
          ) : item.files?.length ? (
            <View style={styles.zenActivityFiles}>
              {item.files.slice(0, 4).map((file) => (
                <Text
                  key={file}
                  style={[styles.zenActivityFileText, { color: chrome.textMuted }]}
                  numberOfLines={1}
                >
                  {file}
                </Text>
              ))}
            </View>
          ) : null}
          {item.body ? (
            <Text
              selectable={textSelectable}
              style={[styles.zenActivityBody, { color: chrome.textSubtle }]}
            >
              {truncateRunes(item.body, 1800)}
            </Text>
          ) : null}
        </View>
      ) : null}
    </View>
  );
}

function AttachmentPreviewList({
  attachments,
  chrome,
  compact,
}: {
  attachments: DisplayAttachment[];
  chrome: TerminalThemeChrome;
  compact?: boolean;
}) {
  return (
    <View style={[styles.zenMessageAttachments, compact ? styles.zenMessageAttachmentsCompact : null]}>
      {attachments.map((attachment) => (
        <View
          key={`${attachment.name}:${attachment.path}`}
          style={[styles.zenAttachmentPill, { borderColor: chrome.border }]}
        >
          <Ionicons
            name={looksLikeImagePath(attachment.name) ? "image-outline" : "document-attach-outline"}
            size={13}
            color={chrome.textSubtle}
          />
          <Text
            style={[styles.zenAttachmentPillText, { color: chrome.textMuted }]}
            numberOfLines={1}
          >
            {attachment.name || basename(attachment.path)}
          </Text>
        </View>
      ))}
    </View>
  );
}

function buildZenTimeline(events: CodexConversationEvent[]): ZenTimelineItem[] {
  const items: ZenTimelineItem[] = [];
  let explorationEntries: ExplorationEntry[] = [];

  const flushExploration = () => {
    if (explorationEntries.length === 0) {
      return;
    }
    items.push(explorationActivityFromEntries(explorationEntries));
    explorationEntries = [];
  };

  for (const event of [...events].sort((left, right) => left.seq - right.seq)) {
    if (event.kind === "user_message" || event.kind === "assistant_message") {
      flushExploration();
      const extracted = extractDisplayMessage(event.body || "");
      if (!extracted.body && extracted.attachments.length === 0) {
        continue;
      }
      items.push({
        type: "message",
        id: event.id || `${event.kind}:${event.seq}`,
        role: event.kind === "user_message" ? "user" : "assistant",
        timestamp: event.timestamp,
        body: extracted.body,
        attachments: extracted.attachments,
      });
      continue;
    }

    if (event.kind === "plan") {
      flushExploration();
      items.push({
        type: "plan",
        id: event.id || `plan:${event.seq}`,
        timestamp: event.timestamp,
        explanation: event.explanation || event.body,
        steps: event.plan ?? [],
      });
      continue;
    }

    if (event.kind === "command") {
      const entry = explorationEntryFromEvent(event);
      if (entry) {
        explorationEntries.push(entry);
        continue;
      }
      flushExploration();
    } else {
      flushExploration();
    }

    const activity = activityFromEvent(event);
    if (activity) {
      items.push(activity);
    }
  }
  flushExploration();
  return items;
}

function mergeChatCommandEventsIntoTimeline(
  timelineItems: ZenTimelineItem[],
  commandEvents: ChatCommandEvent[],
): ZenTimelineItem[] {
  if (commandEvents.length === 0) {
    return timelineItems;
  }
  return [
    ...timelineItems,
    ...commandEvents.map((event) => ({
      type: "activity" as const,
      id: event.id,
      timestamp: event.createdAt,
      title: event.title,
      tone: event.tone,
      icon:
        event.tone === "failed"
          ? "alert-circle-outline"
          : event.tone === "success"
            ? slashCommandIcon(event.command.name)
            : "terminal-outline",
      detail: event.detail,
      body: event.body,
    })),
  ];
}

function activityFromEvent(event: CodexConversationEvent): ZenTimelineItem | null {
  switch (event.kind) {
    case "command": {
      const presentation = commandPresentation(event.command || "");
      const failed = isCommandFailed(event, presentation);
      const running = event.status === "running";
      const command = event.command || "";
      const output = formatOutputPreview(event.body || "", {
        maxLines: COMMAND_OUTPUT_PREVIEW_LINES,
        maxChars: COMMAND_OUTPUT_PREVIEW_CHARS,
      });
      return {
        type: "activity",
        id: event.id || `command:${event.seq}`,
        timestamp: event.timestamp,
        title: commandActivityTitle(command, running, failed, presentation),
        tone: running ? "running" : failed ? "failed" : "success",
        icon: running ? "time-outline" : failed ? "alert-circle-outline" : presentation.icon,
        detail: presentation.detail || commandSummary(command),
        body: output.text || (!running && !failed ? "(no output)" : undefined),
      };
    }
    case "patch": {
      const summary = patchSummaryFromEvent(event);
      return {
        type: "activity",
        id: event.id || `patch:${event.seq}`,
        timestamp: event.timestamp,
        title: summary.title,
        tone: "success",
        icon: "git-compare-outline",
        fileSummaries: summary.files,
        files: summary.files.map((file) => file.path),
        body: summary.files.length > 0 ? undefined : event.body,
      };
    }
    case "tool": {
      const name = event.tool_name || event.title || "tool";
      if (isLowSignalToolEvent(name, event.input || "")) {
        return null;
      }
      const failed = event.status === "failed" || (event.exit_code ?? 0) !== 0;
      const running = event.status === "running";
      const presentation = toolPresentation(event);
      const previewPath = presentation.localImagePath || imagePathFromTool(event);
      const result = formatOutputPreview(event.output || event.body || "", {
        maxLines: TOOL_PAYLOAD_PREVIEW_LINES,
        maxChars: TOOL_PAYLOAD_PREVIEW_CHARS,
      });
      const heading = toolActivityHeading(event, running);
      return {
        type: "activity",
        id: event.id || `tool:${event.seq}`,
        timestamp: event.timestamp,
        title: heading.title,
        tone: running ? "running" : failed ? "failed" : "success",
        icon: presentation.icon,
        detail: heading.detail || presentation.subtitle || compactToolDetail(event),
        body: result.text || undefined,
        previewPath,
      };
    }
    case "commentary": {
      if (!event.body?.trim()) {
        return null;
      }
      return {
        type: "activity",
        id: event.id || `commentary:${event.seq}`,
        timestamp: event.timestamp,
        title: event.title || "Reasoning",
        tone: "running",
        icon: "ellipse-outline",
        body: event.body,
      };
    }
    case "status": {
      const title = [event.title, event.body].filter(Boolean).join(" · ");
      if (!title || isLowSignalStatus(title)) {
        return null;
      }
      return {
        type: "activity",
        id: event.id || `status:${event.seq}`,
        timestamp: event.timestamp,
        title,
        tone: "neutral",
        icon: "ellipse-outline",
      };
    }
    default:
      return null;
  }
}

function explorationEntryFromEvent(event: CodexConversationEvent): ExplorationEntry | null {
  const presentation = commandPresentation(event.command || "");
  if (!presentation.groupable) {
    return null;
  }
  const failed = isCommandFailed(event, presentation);
  const running = event.status === "running";
  return {
    event,
    presentation,
    running,
    failed,
    output: formatOutputPreview(event.body || "", {
      maxLines: 4,
      maxChars: 520,
    }),
  };
}

function explorationActivityFromEntries(entries: ExplorationEntry[]): Extract<ZenTimelineItem, { type: "activity" }> {
  const first = entries[0];
  const last = entries[entries.length - 1] ?? first;
  const running = entries.some((entry) => entry.running);
  const failed = entries.some((entry) => entry.failed);
  const files = uniqueStrings(
    entries
      .map((entry) => entry.presentation.target)
      .filter((value): value is string => Boolean(value)),
  ).slice(0, 12);
  const commandLines = entries.map((entry) => explorationEntryLine(entry));
  const failedOutputs = entries
    .filter((entry) => entry.failed && entry.output.text)
    .flatMap((entry) => [
      "",
      `${entry.presentation.detail || commandSummary(entry.event.command || "") || "Command"} output:`,
      entry.output.text,
    ]);
  const body = cleanDisplayText([...commandLines, ...failedOutputs].join("\n"));
  const detail = summarizeExploration(entries);

  return {
    type: "activity",
    id: `explore:${first?.event.id || first?.event.seq}:${last?.event.id || last?.event.seq}`,
    timestamp: last?.event.timestamp || first?.event.timestamp,
    title: running ? "Exploring" : "Explored",
    tone: running ? "running" : failed ? "failed" : "success",
    icon: failed ? "alert-circle-outline" : running ? "time-outline" : "folder-open-outline",
    detail,
    body: body || undefined,
    files,
  };
}

function explorationEntryLine(entry: ExplorationEntry) {
  const action = entry.presentation.explorationLabel || entry.presentation.doneTitle;
  const target = entry.presentation.detail || commandSummary(entry.event.command || "") || "project";
  const suffix = entry.running ? " (running)" : entry.failed ? " (failed)" : "";
  return `${action} ${target}${suffix}`;
}

function summarizeExploration(entries: ExplorationEntry[]) {
  if (entries.length === 0) {
    return undefined;
  }
  const visibleTargets = uniqueStrings(
    entries
      .map((entry) => entry.presentation.target || entry.presentation.query)
      .filter((value): value is string => Boolean(value)),
  );
  if (visibleTargets.length > 0) {
    const summary = visibleTargets.slice(0, 2).map(shortPath).join(", ");
    const hidden = visibleTargets.length - 2;
    return hidden > 0 ? `${summary} +${hidden}` : summary;
  }
  return `${entries.length} lookup${entries.length === 1 ? "" : "s"}`;
}

function shouldAutoExpandActivity(item: Extract<ZenTimelineItem, { type: "activity" }>) {
  if (
    item.tone === "running" ||
    item.tone === "failed" ||
    item.previewPath ||
    item.fileSummaries?.length ||
    item.files?.length
  ) {
    return true;
  }
  if (!item.body) {
    return false;
  }
  return item.body.length <= 700 && item.body.split("\n").length <= 10;
}

function extractDisplayMessage(value: string): {
  body: string;
  attachments: DisplayAttachment[];
} {
  let body = cleanDisplayText(value);
  const attachments: DisplayAttachment[] = [];

  const tagMatch = ATTACHMENT_TAG_RE.exec(body);
  if (tagMatch) {
    attachments.push(...attachmentsFromTag(tagMatch[1]));
    body = cleanDisplayText(body.replace(tagMatch[0], ""));
  }

  const legacy = stripLegacyUploadedFiles(body);
  body = legacy.body;
  attachments.push(...legacy.attachments);

  return {
    body,
    attachments,
  };
}

function attachmentsFromTag(value: string): DisplayAttachment[] {
  try {
    const parsed = JSON.parse(value.trim());
    const files = Array.isArray(parsed?.files) ? parsed.files : [];
    return files
      .map((file: any) => ({
        name: typeof file?.name === "string" ? file.name.trim() : "",
        path: typeof file?.path === "string" ? file.path.trim() : "",
      }))
      .filter((file: DisplayAttachment) => file.path);
  } catch {
    return [];
  }
}

function stripLegacyUploadedFiles(value: string): {
  body: string;
  attachments: DisplayAttachment[];
} {
  const lines = value.split("\n");
  const keep: string[] = [];
  const attachments: DisplayAttachment[] = [];
  let consuming = false;

  for (const line of lines) {
    if (/^Uploaded files?:\s*$/i.test(line.trim())) {
      consuming = true;
      continue;
    }
    if (consuming) {
      const item = /^-\s*(.*?):\s*(\/\S.*)$/.exec(line.trim());
      if (item) {
        attachments.push({
          name: item[1].trim(),
          path: item[2].trim(),
        });
        continue;
      }
      if (!line.trim()) {
        continue;
      }
      consuming = false;
    }
    keep.push(line);
  }

  return {
    body: cleanDisplayText(keep.join("\n")),
    attachments,
  };
}

function cleanDisplayText(value: string) {
  return value.replace(/\n{3,}/g, "\n\n").trim();
}

function patchSummaryFromEvent(event: CodexConversationEvent): PatchSummary {
  const parsed = parseApplyPatchSummary(event.body || "");
  const fallbackFiles =
    parsed.files.length > 0
      ? parsed.files
      : (event.files ?? []).map((path) => ({
          path,
          operation: "update" as PatchOperation,
          added: 0,
          removed: 0,
        }));
  const files = fallbackFiles.sort((left, right) => left.path.localeCompare(right.path));
  const totalAdded = files.reduce((sum, file) => sum + file.added, 0);
  const totalRemoved = files.reduce((sum, file) => sum + file.removed, 0);
  const title = patchSummaryTitle(files, totalAdded, totalRemoved);
  return { title, files, totalAdded, totalRemoved };
}

function parseApplyPatchSummary(patch: string): PatchSummary {
  const files: PatchFileSummary[] = [];
  let current: PatchFileSummary | null = null;

  const finishCurrent = () => {
    if (!current) {
      return;
    }
    files.push(current);
    current = null;
  };

  for (const rawLine of patch.replace(/\r\n/g, "\n").split("\n")) {
    const line = rawLine.trimEnd();
    const add = /^\*\*\* Add File:\s+(.+)$/.exec(line);
    if (add) {
      finishCurrent();
      current = {
        path: add[1].trim(),
        operation: "add",
        added: 0,
        removed: 0,
      };
      continue;
    }
    const update = /^\*\*\* Update File:\s+(.+)$/.exec(line);
    if (update) {
      finishCurrent();
      current = {
        path: update[1].trim(),
        operation: "update",
        added: 0,
        removed: 0,
      };
      continue;
    }
    const del = /^\*\*\* Delete File:\s+(.+)$/.exec(line);
    if (del) {
      finishCurrent();
      current = {
        path: del[1].trim(),
        operation: "delete",
        added: 0,
        removed: 0,
      };
      continue;
    }
    const move = /^\*\*\* Move to:\s+(.+)$/.exec(line);
    if (move && current) {
      current.movePath = move[1].trim();
      continue;
    }
    if (!current || line.startsWith("***") || line.startsWith("@@")) {
      continue;
    }
    if (line.startsWith("+")) {
      current.added += 1;
    } else if (line.startsWith("-")) {
      current.removed += 1;
    }
  }
  finishCurrent();

  const totalAdded = files.reduce((sum, file) => sum + file.added, 0);
  const totalRemoved = files.reduce((sum, file) => sum + file.removed, 0);
  return {
    title: patchSummaryTitle(files, totalAdded, totalRemoved),
    files,
    totalAdded,
    totalRemoved,
  };
}

function patchSummaryTitle(files: PatchFileSummary[], totalAdded: number, totalRemoved: number) {
  if (files.length === 0) {
    return "Edited files";
  }
  if (files.length === 1) {
    const file = files[0];
    const verb =
      file.operation === "add" ? "Added" : file.operation === "delete" ? "Deleted" : "Edited";
    return `${verb} ${patchDisplayPath(file)} ${lineCountSummary(file.added, file.removed)}`;
  }
  return `Edited ${files.length} files ${lineCountSummary(totalAdded, totalRemoved)}`;
}

function patchDisplayPath(file: PatchFileSummary) {
  return file.movePath ? `${file.path} -> ${file.movePath}` : file.path;
}

function lineCountSummary(added: number, removed: number) {
  return `(+${added} -${removed})`;
}

function truncateRunes(value: string, limit: number) {
  const chars = Array.from(value);
  if (chars.length <= limit) {
    return value;
  }
  return chars.slice(0, Math.max(0, limit - 1)).join("") + "…";
}

function isLowSignalStatus(value: string) {
  return /^(Task started|Goal updated|Patch applied)$/i.test(value.trim());
}

function isLowSignalToolEvent(name: string, input: string) {
  const normalized = name.trim();
  if (normalized === "write_stdin" || normalized.endsWith(".write_stdin")) {
    try {
      const parsed = JSON.parse(input);
      return parsed?.chars === "";
    } catch {
      return false;
    }
  }
  return false;
}

function toolActivityHeading(event: CodexConversationEvent, running: boolean) {
  const name = (event.tool_name || event.title || "tool").trim().replace(/^functions\./, "");
  if (name === "view_image") {
    return {
      title: running ? "Calling" : "Viewed Image",
      detail: compactToolDetail(event),
    };
  }
  if (name === "write_stdin") {
    const interaction = terminalInteractionHeading(event);
    if (interaction) {
      return interaction;
    }
  }
  return {
    title: running ? "Calling" : "Called",
    detail: toolInvocationLabel(event),
  };
}

function terminalInteractionHeading(event: CodexConversationEvent) {
  const input = parseToolPayload(event.input);
  const inputObject = isRecord(input) ? input : {};
  const chars = stringField(inputObject, "chars");
  const command = event.command || "command";
  if (!chars) {
    return {
      title: "Waited for",
      detail: commandSummary(command),
    };
  }
  const preview = truncateRunes(displayControlText(chars), 80);
  return {
    title: "Interacted with",
    detail: [commandSummary(command), preview ? `sent ${preview}` : ""].filter(Boolean).join(", "),
  };
}

function toolInvocationLabel(event: CodexConversationEvent) {
  const rawName = (event.tool_name || event.title || "tool").trim().replace(/^functions\./, "");
  const name = formatToolInvocationName(rawName);
  const input = parseToolPayload(event.input);
  const args = isRecord(input) ? compactToolInvocationArgs(input) : "";
  return `${name}(${args})`;
}

function formatToolInvocationName(name: string) {
  const mcpMatch = /^mcp__([^_]+(?:_[^_]+)*)__+(.+)$/.exec(name);
  if (mcpMatch) {
    return `${mcpMatch[1]}.${mcpMatch[2]}`;
  }
  return name || "tool";
}

function compactToolInvocationArgs(record: Record<string, unknown>) {
  const hidden = new Set(["max_output_tokens", "yield_time_ms", "timeout_ms", "response_length"]);
  const compact: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(record)) {
    if (hidden.has(key)) {
      continue;
    }
    compact[key] = value;
    if (Object.keys(compact).length >= 3) {
      break;
    }
  }
  const text = Object.keys(compact).length > 0 ? JSON.stringify(compact) : "";
  return truncateRunes(text, 120);
}

function compactToolDetail(event: CodexConversationEvent) {
  const parsed = parseToolPayload(event.input);
  if (!isRecord(parsed)) {
    return (event.tool_name || "").trim().replace(/^functions\./, "");
  }
  return (
    stringField(parsed, "path") ||
    stringField(parsed, "url") ||
    stringField(parsed, "target") ||
    stringField(parsed, "query") ||
    (event.tool_name || "").trim().replace(/^functions\./, "")
  );
}

function imagePathFromTool(event: CodexConversationEvent) {
  const parsed = parseToolPayload(event.input);
  if (!isRecord(parsed)) {
    return undefined;
  }
  const path = stringField(parsed, "path") || stringField(parsed, "image_url");
  if (!path || previewableImageUri(path) || !looksLikeImagePath(path)) {
    return undefined;
  }
  return path;
}

function commandPresentation(command: string): CommandPresentation {
  const normalized = cleanDisplayText(command);
  const firstLine = normalized.split("\n").find((line) => line.trim())?.trim() || "";
  const tokens = commandTokens(firstLine);
  const executable = commandExecutable(tokens);
  const lower = firstLine.toLowerCase();
  const fallbackDetail = commandSummary(command);

  if (["cat", "sed", "nl", "less", "head", "tail"].includes(executable)) {
    const target = commandTarget(tokens, executable);
    return {
      kind: "read",
      target,
      detail: target || fallbackDetail,
      icon: "document-text-outline",
      runningTitle: "Reading file",
      doneTitle: "Read file",
      failedTitle: "Read failed",
      groupable: true,
      explorationLabel: "Read",
    };
  }

  if (executable === "ls" || (executable === "find" && !/\s-name\s|\s-iname\s|\s-type\s+f/.test(lower))) {
    const target = commandTarget(tokens, executable) || ".";
    return {
      kind: "list",
      target,
      detail: target,
      icon: "folder-open-outline",
      runningTitle: "Listing files",
      doneTitle: "Listed files",
      failedTitle: "List failed",
      groupable: true,
      explorationLabel: "List",
    };
  }

  if (["rg", "grep", "ag", "ack"].includes(executable) || executable === "find") {
    const query = searchQuery(tokens, executable);
    const target = searchTarget(tokens, executable);
    const detail = [query ? truncateRunes(query, 36) : "", target].filter(Boolean).join(" in ");
    return {
      kind: "search",
      query,
      target,
      detail: detail || fallbackDetail,
      icon: "search-outline",
      runningTitle: "Searching project",
      doneTitle: "Searched project",
      failedTitle: "Search failed",
      groupable: true,
      explorationLabel: "Search",
    };
  }

  if (/\b(go test|bun test|npm test|pnpm test|yarn test|jest|vitest|pytest)\b/.test(lower)) {
    return {
      kind: "test",
      detail: fallbackDetail,
      icon: "checkmark-done-outline",
      runningTitle: "Running",
      doneTitle: "Ran",
      failedTitle: "Ran",
      groupable: false,
    };
  }

  if (/\b(tsc|lint|typecheck|doctor|gradlew|xcodebuild|build|assemble)\b/.test(lower)) {
    return {
      kind: "check",
      detail: fallbackDetail,
      icon: "construct-outline",
      runningTitle: "Running",
      doneTitle: "Ran",
      failedTitle: "Ran",
      groupable: false,
    };
  }

  if (/\bgit\b/.test(lower)) {
    return {
      kind: "git",
      detail: fallbackDetail,
      icon: "git-branch-outline",
      runningTitle: "Running",
      doneTitle: "Ran",
      failedTitle: "Ran",
      groupable: false,
    };
  }

  if (/\b(bun install|npm install|pnpm install|yarn install)\b/.test(lower)) {
    return {
      kind: "install",
      detail: fallbackDetail,
      icon: "download-outline",
      runningTitle: "Running",
      doneTitle: "Ran",
      failedTitle: "Ran",
      groupable: false,
    };
  }

  return {
    kind: "run",
    detail: fallbackDetail,
    icon: "terminal-outline",
    runningTitle: "Running",
    doneTitle: "Ran",
    failedTitle: "Ran",
    groupable: false,
  };
}

function commandActivityTitle(
  command: string,
  running: boolean,
  failed: boolean,
  presentation: CommandPresentation = commandPresentation(command),
) {
  void command;
  void failed;
  void presentation;
  return running ? "Running" : "Ran";
}

function commandSummary(command: string) {
  command = cleanDisplayText(command);
  if (!command) {
    return undefined;
  }
  const firstLine = command.split("\n")[0];
  return truncateRunes(firstLine, 72);
}

function tokenizeShellLike(value: string): string[] {
  const tokens: string[] = [];
  let current = "";
  let quote: "'" | '"' | "" = "";
  let escaping = false;

  for (const char of value) {
    if (escaping) {
      current += char;
      escaping = false;
      continue;
    }
    if (char === "\\") {
      escaping = true;
      continue;
    }
    if (quote) {
      if (char === quote) {
        quote = "";
      } else {
        current += char;
      }
      continue;
    }
    if (char === "'" || char === '"') {
      quote = char;
      continue;
    }
    if (/\s/.test(char)) {
      if (current) {
        tokens.push(current);
        current = "";
      }
      continue;
    }
    current += char;
  }

  if (current) {
    tokens.push(current);
  }
  return tokens;
}

function commandTokens(value: string): string[] {
  const tokens = tokenizeShellLike(value);
  const executable = basename(tokens[0] || "").toLowerCase();
  if (executable === "bash" || executable === "sh" || executable === "zsh") {
    const commandIndex = tokens.findIndex((token) => token === "-c" || token === "-lc");
    if (commandIndex >= 0 && tokens[commandIndex + 1]) {
      return commandTokens(tokens[commandIndex + 1]);
    }
  }
  return tokens;
}

function commandExecutable(tokens: string[]) {
  const executableTokens = tokens.filter((token) => token !== "env");
  while (executableTokens[0]?.includes("=")) {
    executableTokens.shift();
  }
  const executable = executableTokens[0] || "";
  return basename(executable).toLowerCase();
}

function commandTarget(tokens: string[], executable: string) {
  const positional = commandPositionals(tokens, executable);
  if (positional.length === 0) {
    return "";
  }
  if (executable === "sed") {
    return positional.find((token) => !/^\d*,?\d*p$/.test(token) && !/^s[|/]/.test(token)) || positional[positional.length - 1];
  }
  if (executable === "find") {
    return positional[0];
  }
  return positional[positional.length - 1];
}

function commandPositionals(tokens: string[], executable: string) {
  const start = tokens.findIndex((token) => basename(token).toLowerCase() === executable);
  const relevant = start >= 0 ? tokens.slice(start + 1) : tokens.slice(1);
  const positionals: string[] = [];
  for (let index = 0; index < relevant.length; index++) {
    const token = relevant[index];
    if (!token || token === "--") {
      continue;
    }
    if (token.startsWith("-")) {
      const optionTakesValue =
        [
          "-e",
          "-f",
          "-g",
          "--glob",
          "--type",
          "-t",
          "-m",
          "--max-count",
          "-C",
          "-A",
          "-B",
        ].includes(token) && relevant[index + 1] && !relevant[index + 1].startsWith("-");
      if (optionTakesValue) {
        index++;
      }
      continue;
    }
    if (token.includes("=") && positionals.length === 0) {
      continue;
    }
    positionals.push(token);
  }
  return positionals;
}

function searchQuery(tokens: string[], executable: string) {
  const positionals = commandPositionals(tokens, executable);
  if (executable === "find") {
    const nameIndex = tokens.findIndex((token) => token === "-name" || token === "-iname");
    return nameIndex >= 0 ? tokens[nameIndex + 1] || "" : positionals.slice(1).join(" ");
  }
  return positionals[0] || "";
}

function searchTarget(tokens: string[], executable: string) {
  const positionals = commandPositionals(tokens, executable);
  if (executable === "find") {
    return positionals[0] || ".";
  }
  return positionals.slice(1).join(", ");
}

function isCommandFailed(event: CodexConversationEvent, presentation: CommandPresentation) {
  if (event.status === "failed" || (event.exit_code ?? 0) !== 0) {
    if (presentation.kind === "search" && event.exit_code === 1 && !cleanToolOutput(event.body || "")) {
      return false;
    }
    return true;
  }
  return false;
}

function cleanToolOutput(value: string) {
  value = cleanDisplayText(value);
  if (!value) {
    return "";
  }
  const lines = value.split("\n");
  const outputLine = lines.findIndex((line) => line.trim() === "Output:");
  const bodyLines = outputLine >= 0 ? lines.slice(outputLine + 1) : lines;
  return cleanDisplayText(bodyLines.filter((line) => !isToolMetadataLine(line)).join("\n"));
}

function formatOutputPreview(value: string, options: OutputPreviewOptions): OutputPreview {
  let output = cleanToolOutput(value);
  if (!output) {
    return { text: "", truncated: false };
  }

  output = compactJsonForPreview(output);
  const charLimited = truncateOutputChars(output, options.maxChars);
  const lineLimited = truncateOutputLines(charLimited.text, options.maxLines);
  return {
    text: lineLimited.text,
    truncated: charLimited.truncated || lineLimited.truncated,
  };
}

function compactJsonForPreview(value: string) {
  const trimmed = value.trim();
  if (!/^[\[{]/.test(trimmed)) {
    return value;
  }
  try {
    const parsed = JSON.parse(trimmed);
    const compact = JSON.stringify(parsed);
    return compact
      .replace(/":/g, '": ')
      .replace(/,"/g, ', "');
  } catch {
    return value;
  }
}

function truncateOutputChars(value: string, maxChars: number): OutputPreview {
  const chars = Array.from(value);
  if (chars.length <= maxChars) {
    return { text: value, truncated: false };
  }
  const headCount = Math.max(120, Math.floor(maxChars * 0.58));
  const tailCount = Math.max(80, maxChars - headCount - 80);
  const hidden = chars.length - headCount - tailCount;
  return {
    text: cleanDisplayText(
      [
        chars.slice(0, headCount).join(""),
        `... ${hidden} chars hidden. ${FULL_OUTPUT_HINT}`,
        chars.slice(chars.length - tailCount).join(""),
      ].join("\n"),
    ),
    truncated: true,
  };
}

function truncateOutputLines(value: string, maxLines: number): OutputPreview {
  const lines = value.split("\n");
  if (lines.length <= maxLines) {
    return { text: value, truncated: false };
  }
  const headCount = Math.max(1, Math.ceil(maxLines / 2));
  const tailCount = Math.max(1, Math.floor(maxLines / 2));
  const hidden = lines.length - headCount - tailCount;
  return {
    text: cleanDisplayText(
      [
        ...lines.slice(0, headCount),
        `... +${hidden} lines hidden. ${FULL_OUTPUT_HINT}`,
        ...lines.slice(lines.length - tailCount),
      ].join("\n"),
    ),
    truncated: true,
  };
}

function isToolMetadataLine(line: string) {
  const trimmed = line.trim();
  return (
    trimmed.startsWith("Chunk ID:") ||
    trimmed.startsWith("Wall time:") ||
    trimmed.startsWith("Exit code:") ||
    trimmed.startsWith("Process exited with code ") ||
    trimmed.startsWith("Process running with session ID ") ||
    trimmed.startsWith("Original token count:") ||
    trimmed.startsWith("Total output lines:")
  );
}

function isRecentTimestamp(value?: string) {
  if (!value) {
    return false;
  }
  const time = new Date(value).getTime();
  if (!Number.isFinite(time)) {
    return false;
  }
  return Date.now() - time <= 90_000;
}

function TimelineEvent({
  serverId,
  cwd,
  event,
  chrome,
  theme,
}: {
  serverId: string;
  cwd?: string;
  event: CodexConversationEvent;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  switch (event.kind) {
    case "user_message":
      return (
        <View style={styles.userRow}>
          <View
            style={[
              styles.messageBubble,
              styles.userBubble,
              { backgroundColor: chrome.accentSoft, borderColor: chrome.borderStrong },
            ]}
          >
            <MessageBody value={event.body || ""} chrome={chrome} theme={theme} compact />
          </View>
        </View>
      );
    case "assistant_message":
      return (
        <View style={styles.assistantRow}>
          <View style={[styles.assistantAvatar, { backgroundColor: chrome.accentSoft }]}>
            <Ionicons name="sparkles-outline" size={14} color={chrome.accent} />
          </View>
          <View
            style={[
              styles.assistantBlock,
              { borderColor: chrome.border, backgroundColor: chrome.surface },
            ]}
          >
            {event.title ? (
              <Text style={[styles.messageEyebrow, { color: chrome.textSubtle }]}>
                {event.title}
              </Text>
            ) : null}
            <MessageBody value={event.body || ""} chrome={chrome} theme={theme} />
          </View>
        </View>
      );
    case "command":
      return <CommandEvent event={event} chrome={chrome} theme={theme} />;
    case "tool":
      return <ToolEvent serverId={serverId} cwd={cwd} event={event} chrome={chrome} theme={theme} />;
    case "patch":
      return <PatchEvent event={event} chrome={chrome} theme={theme} />;
    case "commentary":
      return <CommentaryEvent event={event} chrome={chrome} theme={theme} />;
    case "status":
    default:
      return (
        <View style={styles.statusRow}>
          <Ionicons name="ellipse" size={6} color={chrome.textSubtle} />
          <Text style={[styles.statusText, { color: chrome.textMuted }]}>
            {[event.title, event.body].filter(Boolean).join(" · ")}
          </Text>
        </View>
      );
  }
}

function CommentaryEvent({
  event,
  chrome,
  theme,
}: {
  event: CodexConversationEvent;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const [expanded, setExpanded] = useState(false);
  const textSelectable = useContext(TimelineTextSelectableContext);
  const title = event.title || "Reasoning";
  const body = event.body || "";
  const shouldCollapse =
    body.length > COMMENTARY_PREVIEW_CHARS ||
    body.split("\n").length > COMMENTARY_PREVIEW_LINES;

  return (
    <View
      style={[
        styles.commentaryRow,
        { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
      ]}
    >
      <Ionicons name="bulb-outline" size={14} color={theme.yellow} />
      <View style={styles.commentaryContent}>
        <Text style={[styles.commentaryTitle, { color: chrome.textMuted }]} numberOfLines={1}>
          {title}
        </Text>
        {body ? (
          <Text
            selectable={textSelectable}
            style={[styles.commentaryText, { color: chrome.textSubtle }]}
            numberOfLines={expanded ? undefined : COMMENTARY_PREVIEW_LINES}
          >
            {body}
          </Text>
        ) : null}
        {shouldCollapse ? (
          <TouchableOpacity
            style={styles.commentaryToggle}
            onPress={() => setExpanded((value) => !value)}
            activeOpacity={0.76}
          >
            <Text style={[styles.toolToggleText, { color: chrome.accent }]}>
              {expanded ? "Show less" : "Show more"}
            </Text>
            <Ionicons
              name={expanded ? "chevron-up" : "chevron-down"}
              size={14}
              color={chrome.accent}
            />
          </TouchableOpacity>
        ) : null}
      </View>
    </View>
  );
}

function CommandEvent({
  event,
  chrome,
  theme,
}: {
  event: CodexConversationEvent;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const [expanded, setExpanded] = useState(false);
  const textSelectable = useContext(TimelineTextSelectableContext);
  const failed = event.status === "failed" || (event.exit_code ?? 0) !== 0;
  const icon = event.status === "running"
    ? "time-outline"
    : failed
      ? "close-circle-outline"
      : "checkmark-circle-outline";
  const iconColor = event.status === "running"
    ? theme.yellow
    : failed
      ? theme.red
      : theme.green;
  const output = cleanToolOutput(event.body || "");
  const shouldCollapseOutput =
    output.length > COMMAND_OUTPUT_PREVIEW_CHARS ||
    output.split("\n").length > COMMAND_OUTPUT_PREVIEW_LINES;

  return (
    <View
      style={[
        styles.toolBlock,
        { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
      ]}
    >
      <View style={styles.toolHeader}>
        <Ionicons name={icon} size={15} color={iconColor} />
        <Text style={[styles.toolTitle, { color: chrome.textMuted }]} numberOfLines={1}>
          {event.exit_code === undefined ? "Command" : `Command exit ${event.exit_code}`}
        </Text>
        {event.status === "running" ? (
          <ActivityIndicator size="small" color={theme.yellow} />
        ) : null}
      </View>
      <Text
        selectable={textSelectable}
        style={[styles.commandText, { color: chrome.text }]}
        numberOfLines={4}
      >
        {event.command}
      </Text>
      {output ? (
        <Text
          selectable={textSelectable}
          style={[styles.toolBody, { color: chrome.textSubtle, borderColor: chrome.border }]}
          numberOfLines={expanded ? undefined : COMMAND_OUTPUT_PREVIEW_LINES}
        >
          {output}
        </Text>
      ) : null}
      {shouldCollapseOutput ? (
        <TouchableOpacity
          style={styles.toolToggle}
          onPress={() => setExpanded((value) => !value)}
          activeOpacity={0.76}
        >
          <Text style={[styles.toolToggleText, { color: chrome.accent }]}>
            {expanded ? "Show less" : "Show more"}
          </Text>
          <Ionicons
            name={expanded ? "chevron-up" : "chevron-down"}
            size={14}
            color={chrome.accent}
          />
        </TouchableOpacity>
      ) : null}
    </View>
  );
}

function ToolEvent({
  serverId,
  cwd,
  event,
  chrome,
  theme,
}: {
  serverId: string;
  cwd?: string;
  event: CodexConversationEvent;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const [expanded, setExpanded] = useState(false);
  const [assetPreviewUri, setAssetPreviewUri] = useState<string | null>(null);
  const [assetPreviewFailed, setAssetPreviewFailed] = useState(false);
  const textSelectable = useContext(TimelineTextSelectableContext);
  const presentation = useMemo(() => toolPresentation(event), [event]);
  const running = isEventRunning(event);
  const failed = isEventFailed(event);
  const input = event.input || "";
  const output = cleanToolOutput(event.output || event.body || "");
  const imagePreviewUri = presentation.previewUri || assetPreviewUri || "";
  const showRawInput = Boolean(input) && (expanded || presentation.fields.length === 0);
  const inputCollapsed =
    input.length > TOOL_PAYLOAD_PREVIEW_CHARS ||
    input.split("\n").length > TOOL_PAYLOAD_PREVIEW_LINES;
  const outputCollapsed =
    output.length > TOOL_PAYLOAD_PREVIEW_CHARS ||
    output.split("\n").length > TOOL_PAYLOAD_PREVIEW_LINES;
  const shouldCollapsePayload = inputCollapsed || outputCollapsed;
  const statusText = running ? "Running" : failed ? "Failed" : "Done";
  const iconColor = running ? theme.yellow : failed ? theme.red : theme.cyan;
  const outputLabel = failed ? "Error" : presentation.rawOutputLabel;

  useEffect(() => {
    let cancelled = false;
    setAssetPreviewUri(null);
    setAssetPreviewFailed(false);
    if (!presentation.localImagePath || !serverId) {
      return () => {
        cancelled = true;
      };
    }
    void wsClient
      .getCodexAsset(serverId, { path: presentation.localImagePath, cwd })
      .then((asset) => {
        if (!cancelled && asset.data_url) {
          setAssetPreviewUri(asset.data_url);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setAssetPreviewFailed(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [cwd, presentation.localImagePath, serverId]);

  return (
    <View
      style={[
        styles.toolBlock,
        { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
      ]}
    >
      <View style={styles.toolHeader}>
        <Ionicons name={presentation.icon} size={15} color={iconColor} />
        <View style={styles.toolTitleGroup}>
          <Text style={[styles.toolTitle, { color: chrome.textMuted }]} numberOfLines={1}>
            {presentation.title}
          </Text>
          {presentation.subtitle ? (
            <Text style={[styles.toolSubtitle, { color: chrome.textSubtle }]} numberOfLines={1}>
              {presentation.subtitle}
            </Text>
          ) : null}
        </View>
        <View
          style={[
            styles.toolStatusPill,
            {
              backgroundColor: failed
                ? theme.red + "22"
                : running
                  ? theme.yellow + "22"
                  : chrome.surface,
              borderColor: failed ? theme.red : running ? theme.yellow : chrome.border,
            },
          ]}
        >
          <Text
            style={[
              styles.toolStatusText,
              { color: failed ? theme.red : running ? theme.yellow : chrome.textSubtle },
            ]}
          >
            {statusText}
          </Text>
        </View>
        {running ? <ActivityIndicator size="small" color={theme.yellow} /> : null}
      </View>

      {imagePreviewUri ? (
        <Image
          source={{ uri: imagePreviewUri }}
          style={[styles.toolImagePreview, { borderColor: chrome.border }]}
          resizeMode="cover"
        />
      ) : presentation.localImagePath ? (
        <View
          style={[
            styles.toolImagePlaceholder,
            { borderColor: chrome.border, backgroundColor: chrome.surface },
          ]}
        >
          <Ionicons
            name={assetPreviewFailed ? "image-outline" : "sync-outline"}
            size={18}
            color={assetPreviewFailed ? chrome.textSubtle : theme.yellow}
          />
          <Text style={[styles.toolImagePlaceholderText, { color: chrome.textSubtle }]}>
            {assetPreviewFailed ? "Preview unavailable" : "Loading preview"}
          </Text>
        </View>
      ) : null}

      {presentation.fields.length > 0 ? (
        <View style={styles.toolFieldList}>
          {presentation.fields.map((field) => (
            <View key={`${field.label}:${field.value}`} style={styles.toolFieldRow}>
              <Text style={[styles.toolFieldLabel, { color: chrome.textSubtle }]}>
                {field.label}
              </Text>
              <Text
                selectable={textSelectable}
                style={[
                  styles.toolFieldValue,
                  field.mono ? styles.toolFieldValueMono : null,
                  { color: chrome.text },
                ]}
                numberOfLines={3}
              >
                {field.value}
              </Text>
            </View>
          ))}
        </View>
      ) : null}

      {showRawInput ? (
        <View style={styles.toolPayloadSection}>
          <Text style={[styles.toolLabel, { color: chrome.textSubtle }]}>
            {presentation.rawInputLabel}
          </Text>
          <Text
            selectable={textSelectable}
            style={[styles.toolBody, { color: chrome.textSubtle, borderColor: chrome.border }]}
            numberOfLines={expanded ? undefined : TOOL_PAYLOAD_PREVIEW_LINES}
          >
            {input}
          </Text>
        </View>
      ) : null}
      {output ? (
        <View style={styles.toolPayloadSection}>
          <Text style={[styles.toolLabel, { color: chrome.textSubtle }]}>{outputLabel}</Text>
          <Text
            selectable={textSelectable}
            style={[styles.toolBody, { color: chrome.textSubtle, borderColor: chrome.border }]}
            numberOfLines={expanded ? undefined : TOOL_PAYLOAD_PREVIEW_LINES}
          >
            {output}
          </Text>
        </View>
      ) : null}
      {shouldCollapsePayload || (input && presentation.fields.length > 0) ? (
        <TouchableOpacity
          style={styles.toolToggle}
          onPress={() => setExpanded((value) => !value)}
          activeOpacity={0.76}
        >
          <Text style={[styles.toolToggleText, { color: chrome.accent }]}>
            {expanded ? "Show less" : "Show more"}
          </Text>
          <Ionicons
            name={expanded ? "chevron-up" : "chevron-down"}
            size={14}
            color={chrome.accent}
          />
        </TouchableOpacity>
      ) : null}
    </View>
  );
}

function toolPresentation(event: CodexConversationEvent): ToolPresentation {
  const name = (event.tool_name || event.title || "tool").trim() || "tool";
  const input = parseToolPayload(event.input);
  const inputObject = isRecord(input) ? input : {};
  const browserAction = /^browser_/.test(name) ? humanizeToolName(name.replace(/^browser_/, "")) : "";

  if (name === "view_image") {
    const path = stringField(inputObject, "path") || stringField(inputObject, "image_url");
    const previewUri = previewableImageUri(path);
    return {
      title: "Image preview",
      subtitle: path ? basename(path) : undefined,
      icon: "image-outline",
      fields: compactFields([
        { label: "Path", value: path, mono: true },
        { label: "Detail", value: stringField(inputObject, "detail") },
      ]),
      previewUri,
      localImagePath: path && !previewUri ? path : undefined,
      rawInputLabel: "Image request",
      rawOutputLabel: "Result",
    };
  }

  if (name === "write_stdin") {
    const chars = stringField(inputObject, "chars");
    const sessionId = valueField(inputObject, "session_id");
    const title = chars === ""
      ? "Terminal poll"
      : chars === "\u0003"
        ? "Terminal interrupt"
        : "Terminal input";
    const text = chars === "\u0003" ? "Ctrl-C" : displayControlText(chars);
    return {
      title,
      subtitle: sessionId ? `session ${sessionId}` : undefined,
      icon: chars === ""
        ? "sync-outline"
        : chars === "\u0003"
          ? "stop-circle-outline"
          : "return-down-forward-outline",
      fields: compactFields([
        { label: "Session", value: sessionId, mono: true },
        { label: "Text", value: text, mono: true },
        {
          label: "Wait",
          value: valueField(inputObject, "yield_time_ms")
            ? `${valueField(inputObject, "yield_time_ms")} ms`
            : "",
        },
        { label: "Max output", value: valueField(inputObject, "max_output_tokens") },
      ]),
      rawInputLabel: "Terminal request",
      rawOutputLabel: "Terminal output",
    };
  }

  if (browserAction) {
    const browserFile = stringField(inputObject, "filename") || firstString(inputObject.paths);
    const browserPreviewUri = looksLikeImagePath(browserFile)
      ? previewableImageUri(browserFile)
      : undefined;
    return {
      title: `Browser ${browserAction}`,
      subtitle: stringField(inputObject, "element") || stringField(inputObject, "url") || undefined,
      icon: browserToolIcon(name),
      fields: compactFields([
        { label: "Element", value: stringField(inputObject, "element") },
        { label: "Target", value: stringField(inputObject, "target"), mono: true },
        { label: "URL", value: stringField(inputObject, "url"), mono: true },
        { label: "Text", value: stringField(inputObject, "text"), mono: true },
        { label: "Key", value: stringField(inputObject, "key"), mono: true },
        { label: "File", value: browserFile, mono: true },
      ]),
      previewUri: browserPreviewUri,
      localImagePath: browserFile && !browserPreviewUri && looksLikeImagePath(browserFile)
        ? browserFile
        : undefined,
      rawInputLabel: "Browser request",
      rawOutputLabel: "Browser result",
    };
  }

  if (name.includes("query_docs") || name.includes("resolve_library_id")) {
    return {
      title: name.includes("resolve") ? "Library lookup" : "Docs lookup",
      subtitle: stringField(inputObject, "libraryId") || stringField(inputObject, "libraryName") || undefined,
      icon: "library-outline",
      fields: compactFields([
        { label: "Library", value: stringField(inputObject, "libraryId") || stringField(inputObject, "libraryName"), mono: true },
        { label: "Query", value: stringField(inputObject, "query") },
      ]),
      rawInputLabel: "Docs request",
      rawOutputLabel: "Docs result",
    };
  }

  if (name.includes("search_query") || name === "web.run") {
    return {
      title: "Web lookup",
      icon: "search-outline",
      fields: genericToolFields(inputObject),
      rawInputLabel: "Search request",
      rawOutputLabel: "Search result",
    };
  }

  if (name.includes("multi_tool_use.parallel")) {
    const toolUses = Array.isArray(inputObject.tool_uses) ? inputObject.tool_uses : [];
    const names = toolUses
      .map((toolUse) =>
        isRecord(toolUse) && typeof toolUse.recipient_name === "string"
          ? humanizeToolName(toolUse.recipient_name)
          : "",
      )
      .filter(Boolean);
    return {
      title: "Parallel tools",
      subtitle: names.length ? names.slice(0, 2).join(", ") : undefined,
      icon: "git-network-outline",
      fields: compactFields([
        { label: "Calls", value: toolUses.length ? String(toolUses.length) : "" },
        { label: "Tools", value: names.slice(0, 4).join(", ") },
      ]),
      rawInputLabel: "Parallel request",
      rawOutputLabel: "Parallel result",
    };
  }

  if (name.includes("spawn_agent") || name.includes("send_input") || name.includes("wait_agent")) {
    return {
      title: "Agent coordination",
      subtitle: stringField(inputObject, "target") || firstString(inputObject.targets),
      icon: "git-network-outline",
      fields: genericToolFields(inputObject),
      rawInputLabel: "Agent request",
      rawOutputLabel: "Agent result",
    };
  }

  return {
    title: humanizeToolName(name),
    icon: "cube-outline",
    fields: genericToolFields(inputObject),
    rawInputLabel: "Input",
    rawOutputLabel: "Output",
  };
}

function parseToolPayload(value?: string): unknown {
  if (!value) {
    return null;
  }
  try {
    return JSON.parse(value);
  } catch {
    return null;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function stringField(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  return typeof value === "string" ? value.trim() : "";
}

function valueField(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  if (typeof value === "string") {
    return value.trim();
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return "";
}

function firstString(value: unknown): string {
  return Array.isArray(value) && typeof value[0] === "string" ? value[0] : "";
}

function compactFields(fields: ToolField[]): ToolField[] {
  return fields.filter((field) => field.value.trim().length > 0).slice(0, 6);
}

function genericToolFields(record: Record<string, unknown>): ToolField[] {
  const hidden = new Set(["max_output_tokens", "yield_time_ms", "timeout_ms", "response_length"]);
  return Object.entries(record)
    .filter(([key]) => !hidden.has(key))
    .map(([key, value]) => ({
      label: humanizeToolName(key),
      value: summarizeToolValue(value),
      mono: typeof value !== "boolean",
    }))
    .filter((field) => field.value.length > 0)
    .slice(0, 6);
}

function summarizeToolValue(value: unknown): string {
  if (typeof value === "string") {
    return displayControlText(value);
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  if (Array.isArray(value)) {
    const scalar = value.filter((item) => ["string", "number", "boolean"].includes(typeof item));
    if (scalar.length > 0) {
      return scalar.slice(0, 3).map(String).join(", ");
    }
    return `${value.length} item${value.length === 1 ? "" : "s"}`;
  }
  if (isRecord(value)) {
    const keys = Object.keys(value);
    return keys.length ? keys.slice(0, 4).join(", ") : "";
  }
  return "";
}

function displayControlText(value: string): string {
  if (!value) {
    return "";
  }
  return value
    .replace(/\u0003/g, "Ctrl-C")
    .replace(/\n/g, "\\n")
    .replace(/\t/g, "\\t");
}

function humanizeToolName(value: string): string {
  return value
    .replace(/^mcp__/, "")
    .replace(/^functions\./, "")
    .replace(/__/g, " ")
    .replace(/_/g, " ")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/\b\w/g, (letter) => letter.toUpperCase()) || "Tool";
}

function browserToolIcon(name: string): IoniconName {
  if (name.includes("navigate")) {
    return "navigate-outline";
  }
  if (name.includes("click")) {
    return "radio-button-on-outline";
  }
  if (name.includes("type") || name.includes("fill")) {
    return "text-outline";
  }
  if (name.includes("screenshot")) {
    return "camera-outline";
  }
  if (name.includes("snapshot")) {
    return "scan-outline";
  }
  return "globe-outline";
}

function previewableImageUri(value?: string) {
  if (!value) {
    return undefined;
  }
  if (/^(https?:|data:image\/|file:)/.test(value)) {
    return value;
  }
  return undefined;
}

function looksLikeImagePath(value: string) {
  return /\.(png|jpe?g|gif|webp|bmp)$/i.test(value.trim());
}

function buildCodexComposerMessage(
  draft: string,
  attachments: ComposerAttachment[],
) {
  const body = draft.trim();
  if (attachments.length === 0) {
    return body;
  }
  const attachmentBlock = `<zen_attachments>${JSON.stringify({
    files: attachments.map((attachment) => ({
      name: attachment.name,
      path: attachment.path,
    })),
  })}</zen_attachments>`;
  return [body, attachmentBlock].filter(Boolean).join("\n\n");
}

function basename(value: string) {
  const parts = value.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || value;
}

function shortPath(value: string) {
  const trimmed = value.trim();
  if (!trimmed) {
    return "";
  }
  const parts = trimmed.split(/[\\/]/).filter(Boolean);
  if (parts.length <= 2) {
    return trimmed;
  }
  return `${parts[parts.length - 2]}/${parts[parts.length - 1]}`;
}

function uniqueStrings(values: string[]) {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const value of values) {
    const normalized = value.trim();
    if (!normalized || seen.has(normalized)) {
      continue;
    }
    seen.add(normalized);
    result.push(normalized);
  }
  return result;
}

function isEventRunning(event: CodexConversationEvent) {
  return event.status === "running";
}

function isEventFailed(event: CodexConversationEvent) {
  return event.status === "failed" || (event.exit_code ?? 0) !== 0;
}

function PatchEvent({
  event,
  chrome,
  theme,
}: {
  event: CodexConversationEvent;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const [expanded, setExpanded] = useState(false);
  const textSelectable = useContext(TimelineTextSelectableContext);
  const visibleFiles = event.files?.slice(0, 5) ?? [];
  const hiddenFileCount = Math.max((event.files?.length ?? 0) - visibleFiles.length, 0);
  const patch = event.body || "";
  return (
    <View
      style={[
        styles.toolBlock,
        { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
      ]}
    >
      <View style={styles.toolHeader}>
        <Ionicons name="construct-outline" size={15} color={theme.cyan} />
        <Text style={[styles.toolTitle, { color: chrome.textMuted }]} numberOfLines={1}>
          {event.title || "Patch"}
        </Text>
      </View>
      {visibleFiles.length ? (
        <View style={styles.fileList}>
          {visibleFiles.map((file) => (
            <View
              key={file}
              style={[
                styles.filePill,
                { borderColor: chrome.border, backgroundColor: chrome.surface },
              ]}
            >
              <Text style={[styles.filePillText, { color: chrome.textMuted }]} numberOfLines={1}>
                {file}
              </Text>
            </View>
          ))}
          {hiddenFileCount > 0 ? (
            <View
              style={[
                styles.filePill,
                { borderColor: chrome.border, backgroundColor: chrome.surface },
              ]}
            >
              <Text style={[styles.filePillText, { color: chrome.textMuted }]}>
                +{hiddenFileCount}
              </Text>
            </View>
          ) : null}
        </View>
      ) : null}
      {patch && expanded ? (
        <Text
          selectable={textSelectable}
          style={[styles.toolBody, { color: chrome.textSubtle, borderColor: chrome.border }]}
          numberOfLines={COMMAND_OUTPUT_PREVIEW_LINES}
        >
          {patch}
        </Text>
      ) : null}
      {patch ? (
        <TouchableOpacity
          style={styles.toolToggle}
          onPress={() => setExpanded((value) => !value)}
          activeOpacity={0.76}
        >
          <Text style={[styles.toolToggleText, { color: chrome.accent }]}>
            {expanded ? "Hide patch" : "View patch"}
          </Text>
          <Ionicons
            name={expanded ? "chevron-up" : "chevron-down"}
            size={14}
            color={chrome.accent}
          />
        </TouchableOpacity>
      ) : null}
    </View>
  );
}

type MessageBlock =
  | { type: "heading"; level: number; text: string }
  | { type: "paragraph"; text: string }
  | { type: "list"; items: string[] }
  | { type: "code"; text: string }
  | { type: "quote"; text: string };

type InlinePart = {
  text: string;
  kind?: "bold" | "code" | "link";
};

function MessageBody({
  value,
  chrome,
  theme,
  compact = false,
}: {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
}) {
  const textSelectable = useContext(TimelineTextSelectableContext);
  const blocks = useMemo(() => parseMessageBlocks(value), [value]);
  if (blocks.length === 0) {
    return null;
  }
  return (
    <View style={styles.messageBody}>
      {blocks.map((block, index) => {
        const isLast = index === blocks.length - 1;
        switch (block.type) {
          case "heading":
            return (
              <Text
                key={index}
                selectable={textSelectable}
                style={[
                  styles.messageHeading,
                  block.level <= 2 ? styles.messageHeadingLarge : null,
                  { color: chrome.text },
                  isLast ? styles.messageBlockLast : null,
                ]}
              >
                {renderInlineMessage(block.text, chrome, theme)}
              </Text>
            );
          case "list":
            return (
              <View
                key={index}
                style={[styles.messageList, isLast ? styles.messageBlockLast : null]}
              >
                {block.items.map((item, itemIndex) => (
                  <View key={itemIndex} style={styles.messageListItem}>
                    <Text
                      selectable={textSelectable}
                      style={[styles.messageBullet, { color: chrome.textSubtle }]}
                    >
                      •
                    </Text>
                    <Text
                      selectable={textSelectable}
                      style={[
                        styles.messageText,
                        styles.messageBlockLast,
                        { color: chrome.text },
                      ]}
                    >
                      {renderInlineMessage(item, chrome, theme)}
                    </Text>
                  </View>
                ))}
              </View>
            );
          case "code":
            return (
              <Text
                key={index}
                selectable={textSelectable}
                style={[
                  styles.messageCodeBlock,
                  {
                    color: chrome.text,
                    backgroundColor: compact ? chrome.surface : theme.black,
                    borderColor: chrome.border,
                  },
                  isLast ? styles.messageBlockLast : null,
                ]}
              >
                {block.text}
              </Text>
            );
          case "quote":
            return (
              <View
                key={index}
                style={[
                  styles.messageQuote,
                  { borderLeftColor: chrome.borderStrong },
                  isLast ? styles.messageBlockLast : null,
                ]}
              >
                <Text
                  selectable={textSelectable}
                  style={[styles.messageQuoteText, { color: chrome.textMuted }]}
                >
                  {renderInlineMessage(block.text, chrome, theme)}
                </Text>
              </View>
            );
          case "paragraph":
          default:
            return (
              <Text
                key={index}
                selectable={textSelectable}
                style={[
                  styles.messageText,
                  { color: chrome.text },
                  isLast ? styles.messageBlockLast : null,
                ]}
              >
                {renderInlineMessage(block.text, chrome, theme)}
              </Text>
            );
        }
      })}
    </View>
  );
}

function parseMessageBlocks(value: string): MessageBlock[] {
  const lines = value.replace(/<!--[\s\S]*?-->/g, "").replace(/\r\n/g, "\n").split("\n");
  const blocks: MessageBlock[] = [];
  let paragraph: string[] = [];
  let list: string[] = [];
  let quote: string[] = [];
  let code: string[] | null = null;

  const flushParagraph = () => {
    const text = paragraph.join(" ").trim();
    if (text) {
      blocks.push({ type: "paragraph", text });
    }
    paragraph = [];
  };
  const flushList = () => {
    if (list.length > 0) {
      blocks.push({ type: "list", items: list });
    }
    list = [];
  };
  const flushQuote = () => {
    const text = quote.join(" ").trim();
    if (text) {
      blocks.push({ type: "quote", text });
    }
    quote = [];
  };
  const flushOpenBlocks = () => {
    flushParagraph();
    flushList();
    flushQuote();
  };

  for (const rawLine of lines) {
    const line = rawLine.trimEnd();
    const trimmed = line.trim();

    if (code) {
      if (/^```/.test(trimmed)) {
        blocks.push({ type: "code", text: code.join("\n").replace(/\n+$/, "") });
        code = null;
      } else {
        code.push(rawLine);
      }
      continue;
    }

    if (/^```/.test(trimmed)) {
      flushOpenBlocks();
      code = [];
      continue;
    }

    if (!trimmed) {
      flushOpenBlocks();
      continue;
    }

    const heading = /^(#{1,4})\s+(.+)$/.exec(trimmed);
    if (heading) {
      flushOpenBlocks();
      blocks.push({
        type: "heading",
        level: heading[1].length,
        text: heading[2].trim(),
      });
      continue;
    }

    const listItem = /^(?:[-*]|\d+\.)\s+(.+)$/.exec(trimmed);
    if (listItem) {
      flushParagraph();
      flushQuote();
      list.push(listItem[1].trim());
      continue;
    }

    const quoteItem = /^>\s?(.+)$/.exec(trimmed);
    if (quoteItem) {
      flushParagraph();
      flushList();
      quote.push(quoteItem[1].trim());
      continue;
    }

    flushList();
    flushQuote();
    paragraph.push(trimmed);
  }

  if (code) {
    blocks.push({ type: "code", text: code.join("\n").replace(/\n+$/, "") });
  }
  flushOpenBlocks();
  return blocks;
}

function renderInlineMessage(
  text: string,
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
) {
  return tokenizeInlineMessage(text).map((part, index) => {
    if (part.kind === "bold") {
      return (
        <Text key={index} style={[styles.messageBold, { color: chrome.text }]}>
          {part.text}
        </Text>
      );
    }
    if (part.kind === "code") {
      return (
        <Text
          key={index}
          style={[
            styles.messageInlineCode,
            { color: theme.green, backgroundColor: chrome.surfaceMuted },
          ]}
        >
          {part.text}
        </Text>
      );
    }
    if (part.kind === "link") {
      return (
        <Text key={index} style={[styles.messageLink, { color: chrome.accent }]}>
          {part.text}
        </Text>
      );
    }
    return part.text;
  });
}

function prepareCodexMarkdown(value: string, streaming: boolean) {
  let markdown = value
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/\r\n/g, "\n")
    .trim();
  if (!markdown) {
    return "";
  }
  if (streaming) {
    markdown = remend(markdown, STREAMING_REMEND_OPTIONS);
  }
  return stripMarkdownImages(markdown);
}

function stripMarkdownImages(value: string) {
  return value.replace(/!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g, (_match, alt, url) => {
    const label = String(alt || "").trim();
    const href = String(url || "").trim();
    if (!href) {
      return label;
    }
    return label ? `[${label}](${href})` : href;
  });
}

function isSafeMarkdownUrl(value: string) {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

function codexMarkdownStyle(
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
  compact: boolean,
): MarkdownStyle {
  const text = {
    color: chrome.text,
    fontFamily: Typography.uiFont,
    fontSize: 14,
    lineHeight: 20,
    marginTop: 0,
    marginBottom: compact ? 6 : 7,
  };
  const heading = {
    color: chrome.text,
    fontFamily: Typography.uiFontMedium,
    lineHeight: 20,
    marginTop: 0,
    marginBottom: 7,
  };
  return {
    paragraph: text,
    h1: { ...heading, fontSize: 16, lineHeight: 22 },
    h2: { ...heading, fontSize: 15, lineHeight: 21 },
    h3: { ...heading, fontSize: 14, lineHeight: 20 },
    h4: { ...heading, fontSize: 14, lineHeight: 20 },
    h5: { ...heading, fontSize: 13, lineHeight: 19 },
    h6: { ...heading, fontSize: 13, lineHeight: 19, color: chrome.textMuted },
    strong: {
      color: chrome.text,
      fontFamily: Typography.uiFontMedium,
      fontWeight: "normal",
    },
    em: {
      color: chrome.text,
      fontFamily: Typography.uiFont,
      fontStyle: "italic",
    },
    link: {
      color: chrome.accent,
      fontFamily: Typography.uiFontMedium,
      underline: false,
    },
    code: {
      color: theme.green,
      backgroundColor: chrome.surfaceMuted,
      borderColor: chrome.border,
      fontFamily: Typography.terminalFont,
      fontSize: 13,
    },
    codeBlock: {
      color: chrome.text,
      backgroundColor: compact ? chrome.surface : theme.black,
      borderColor: chrome.border,
      borderRadius: 7,
      borderWidth: StyleSheet.hairlineWidth,
      fontFamily: Typography.terminalFont,
      fontSize: 12,
      lineHeight: 17,
      marginTop: 2,
      marginBottom: 9,
      padding: 10,
    },
    blockquote: {
      color: chrome.textMuted,
      backgroundColor: "transparent",
      borderColor: chrome.borderStrong,
      borderWidth: 2,
      fontFamily: Typography.uiFont,
      fontSize: 13,
      gapWidth: 9,
      lineHeight: 19,
      marginTop: 0,
      marginBottom: 8,
    },
    list: {
      color: chrome.text,
      bulletColor: chrome.textSubtle,
      markerColor: chrome.textSubtle,
      markerFontWeight: "normal",
      fontFamily: Typography.uiFont,
      fontSize: 14,
      gapWidth: 7,
      lineHeight: 20,
      marginLeft: 0,
      marginTop: 0,
      marginBottom: 8,
    },
    table: {
      color: chrome.text,
      borderColor: chrome.border,
      borderRadius: 7,
      borderWidth: StyleSheet.hairlineWidth,
      cellPaddingHorizontal: 8,
      cellPaddingVertical: 6,
      fontFamily: Typography.uiFont,
      fontSize: 12,
      headerBackgroundColor: chrome.surfaceMuted,
      headerFontFamily: Typography.uiFontMedium,
      headerTextColor: chrome.text,
      lineHeight: 17,
      marginTop: 2,
      marginBottom: 9,
      rowEvenBackgroundColor: chrome.surface,
      rowOddBackgroundColor: chrome.surfaceMuted,
    },
    taskList: {
      borderColor: chrome.borderStrong,
      checkboxBorderRadius: 4,
      checkboxSize: 15,
      checkedColor: theme.green,
      checkedStrikethrough: true,
      checkedTextColor: chrome.textMuted,
      checkmarkColor: theme.background,
    },
    thematicBreak: {
      color: chrome.border,
      height: StyleSheet.hairlineWidth,
      marginTop: 8,
      marginBottom: 10,
    },
    math: {
      color: chrome.text,
      backgroundColor: chrome.surfaceMuted,
      fontSize: 13,
      marginTop: 4,
      marginBottom: 8,
      padding: 8,
      textAlign: "left",
    },
    inlineMath: {
      color: theme.cyan,
    },
    spoiler: {
      color: chrome.surfaceMuted,
      solid: { borderRadius: 4 },
    },
  };
}

function tokenizeInlineMessage(text: string): InlinePart[] {
  const pattern = /(`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\))/g;
  const parts: InlinePart[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = pattern.exec(text)) !== null) {
    if (match.index > lastIndex) {
      parts.push({ text: text.slice(lastIndex, match.index) });
    }
    const token = match[0];
    if (token.startsWith("`")) {
      parts.push({ kind: "code", text: token.slice(1, -1) });
    } else if (token.startsWith("**")) {
      parts.push({ kind: "bold", text: token.slice(2, -2) });
    } else {
      const label = /^\[([^\]]+)\]/.exec(token)?.[1] || token;
      parts.push({ kind: "link", text: label });
    }
    lastIndex = match.index + token.length;
  }

  if (lastIndex < text.length) {
    parts.push({ text: text.slice(lastIndex) });
  }
  return parts;
}

function EmptyState({
  chrome,
  title,
  body,
  busy = false,
  actionLabel,
  onAction,
}: {
  chrome: TerminalThemeChrome;
  title: string;
  body?: string;
  busy?: boolean;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <View style={styles.emptyState}>
      {busy ? <ActivityIndicator color={chrome.accent} /> : null}
      <Text style={[styles.emptyTitle, { color: chrome.text }]}>{title}</Text>
      {body ? (
        <Text style={[styles.emptyBody, { color: chrome.textMuted }]}>{body}</Text>
      ) : null}
      {actionLabel && onAction ? (
        <TouchableOpacity
          style={[
            styles.emptyAction,
            { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
          ]}
          onPress={onAction}
          activeOpacity={0.82}
        >
          <Ionicons name="terminal-outline" size={15} color={chrome.textMuted} />
          <Text style={[styles.emptyActionText, { color: chrome.textMuted }]}>
            {actionLabel}
          </Text>
        </TouchableOpacity>
      ) : null}
    </View>
  );
}

function conversationUnavailableReason(reason?: string) {
  switch (reason) {
    case "not_codex":
      return "This session is not a Codex process.";
    case "missing_cwd":
      return "The daemon has not captured this session directory yet.";
    case "transcript_not_found":
      return "Codex has not written a matching local transcript for this session.";
    case "agent_not_found":
      return "The daemon no longer sees this session.";
    case "session_not_ready":
      return "This new terminal is still being indexed by the daemon.";
    default:
      return "Open the terminal renderer for the raw session.";
  }
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

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    minHeight: 0,
    position: "relative",
  },
  header: {
    minHeight: 40,
    borderBottomWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingVertical: 5,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  headerTitleGroup: {
    flex: 1,
    minWidth: 0,
  },
  headerTitleRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
  },
  headerTitle: {
    fontSize: 14,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },
  headerMeta: {
    marginTop: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
  },
  statusDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  timeline: {
    flex: 1,
    minHeight: 0,
  },
  chatBody: {
    flex: 1,
    minHeight: 0,
  },
  timelineContent: {
    paddingHorizontal: 16,
    paddingTop: 14,
    paddingBottom: 22,
  },
  zenUserRow: {
    marginBottom: 16,
    flexDirection: "row",
    justifyContent: "flex-end",
  },
  zenUserBubble: {
    maxWidth: "86%",
    borderRadius: 8,
    paddingHorizontal: 12,
    paddingVertical: 9,
  },
  zenAssistantRow: {
    marginBottom: 18,
    paddingRight: 10,
  },
  zenAssistantContent: {
    minWidth: 0,
  },
  zenStreamCursor: {
    width: 6,
    height: 16,
    borderRadius: 3,
    opacity: 0.65,
  },
  zenMessageAttachments: {
    gap: 6,
  },
  zenMessageAttachmentsCompact: {
    marginTop: 8,
  },
  zenAttachmentPill: {
    alignSelf: "flex-start",
    maxWidth: "100%",
    minHeight: 28,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 8,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  zenAttachmentPillText: {
    flexShrink: 1,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
  zenActivityWrap: {
    marginBottom: 10,
    paddingLeft: 1,
  },
  zenActivityRow: {
    alignSelf: "flex-start",
    minHeight: 24,
    maxWidth: "100%",
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    opacity: 0.78,
  },
  zenActivityTitle: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
  zenActivityDetail: {
    flexShrink: 1,
    maxWidth: 210,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  zenActivityExpanded: {
    marginTop: 6,
    marginLeft: 19,
    maxWidth: "92%",
    borderLeftWidth: StyleSheet.hairlineWidth,
    paddingLeft: 10,
    paddingVertical: 4,
  },
  zenActivityBody: {
    marginTop: 6,
    fontSize: 11,
    lineHeight: 16,
    fontFamily: Typography.terminalFont,
  },
  zenActivityFiles: {
    gap: 4,
  },
  zenActivityFileText: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  zenDiffFiles: {
    gap: 5,
  },
  zenDiffFileRow: {
    minWidth: 0,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
  },
  zenDiffPrefix: {
    width: 10,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  zenDiffPath: {
    flex: 1,
    minWidth: 0,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  zenDiffAdded: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  zenDiffRemoved: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  zenActivityImage: {
    width: "100%",
    height: 150,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
  },
  zenActivityImagePlaceholder: {
    height: 96,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  zenPlanBlock: {
    paddingVertical: 2,
  },
  zenPlanExplanation: {
    marginBottom: 7,
    fontSize: 12,
    lineHeight: 17,
    fontStyle: "italic",
    fontFamily: Typography.uiFont,
  },
  zenPlanSteps: {
    gap: 6,
  },
  zenPlanStepRow: {
    minWidth: 0,
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 7,
  },
  zenPlanMarker: {
    width: 14,
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },
  zenPlanStepText: {
    flex: 1,
    minWidth: 0,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  zenPlanStepActive: {
    fontFamily: Typography.uiFontMedium,
  },
  zenPlanStepCompleted: {
    textDecorationLine: "line-through",
  },
  zenPlanEmpty: {
    fontSize: 12,
    lineHeight: 17,
    fontStyle: "italic",
    fontFamily: Typography.uiFont,
  },
  userRow: {
    flexDirection: "row",
    justifyContent: "flex-end",
    marginBottom: 10,
  },
  messageBubble: {
    maxWidth: "88%",
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  userBubble: {
    alignSelf: "flex-end",
  },
  assistantRow: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 8,
    marginBottom: 10,
  },
  assistantAvatar: {
    width: 26,
    height: 26,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
    marginTop: 1,
  },
  assistantBlock: {
    flex: 1,
    minWidth: 0,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingVertical: 10,
  },
  messageEyebrow: {
    marginBottom: 5,
    fontSize: 10,
    lineHeight: 13,
    textTransform: "uppercase",
    fontFamily: Typography.uiFontMedium,
  },
  messageBody: {
    minWidth: 0,
  },
  messageText: {
    marginBottom: 7,
    fontSize: 14,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
  },
  messageHeading: {
    marginBottom: 7,
    fontSize: 14,
    lineHeight: 19,
    fontFamily: Typography.uiFontMedium,
  },
  messageHeadingLarge: {
    fontSize: 15,
    lineHeight: 20,
  },
  messageList: {
    marginBottom: 8,
    gap: 4,
  },
  messageListItem: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 7,
  },
  messageBullet: {
    width: 9,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
  },
  messageCodeBlock: {
    marginTop: 2,
    marginBottom: 9,
    borderRadius: 7,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
    paddingHorizontal: 10,
    paddingVertical: 8,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
  },
  messageQuote: {
    marginBottom: 8,
    borderLeftWidth: 2,
    paddingLeft: 9,
  },
  messageQuoteText: {
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
  },
  messageBold: {
    fontFamily: Typography.uiFontMedium,
  },
  messageInlineCode: {
    fontFamily: Typography.terminalFont,
    fontSize: 13,
    lineHeight: 18,
  },
  messageLink: {
    fontFamily: Typography.uiFontMedium,
  },
  messageBlockLast: {
    marginBottom: 0,
  },
  toolBlock: {
    marginLeft: 34,
    marginBottom: 10,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 11,
    paddingVertical: 9,
  },
  toolHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
  },
  toolTitleGroup: {
    flex: 1,
    minWidth: 0,
  },
  toolTitle: {
    minWidth: 0,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
  toolSubtitle: {
    flex: 1,
    minWidth: 0,
    marginTop: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
  },
  toolStatusPill: {
    minHeight: 20,
    borderRadius: 7,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 7,
    alignItems: "center",
    justifyContent: "center",
  },
  toolStatusText: {
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFontMedium,
  },
  commandText: {
    marginTop: 6,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
  },
  toolBody: {
    marginTop: 7,
    borderLeftWidth: StyleSheet.hairlineWidth,
    paddingLeft: 9,
    fontSize: 11,
    lineHeight: 16,
    fontFamily: Typography.terminalFont,
  },
  toolPayloadSection: {
    marginTop: 8,
  },
  toolImagePreview: {
    marginTop: 9,
    width: "100%",
    height: 160,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  toolImagePlaceholder: {
    marginTop: 9,
    minHeight: 96,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
  },
  toolImagePlaceholderText: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
  toolFieldList: {
    marginTop: 8,
    gap: 6,
  },
  toolFieldRow: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 8,
  },
  toolFieldLabel: {
    width: 72,
    flexShrink: 0,
    fontSize: 10,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
    textTransform: "uppercase",
  },
  toolFieldValue: {
    flex: 1,
    minWidth: 0,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFont,
  },
  toolFieldValueMono: {
    fontFamily: Typography.terminalFont,
    fontSize: 11,
    lineHeight: 16,
  },
  toolLabel: {
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFontMedium,
    textTransform: "uppercase",
  },
  toolToggle: {
    alignSelf: "flex-start",
    marginTop: 8,
    minHeight: 28,
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
  toolToggleText: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  fileList: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 6,
    marginTop: 8,
  },
  filePill: {
    maxWidth: "100%",
    borderRadius: 7,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 8,
    paddingVertical: 4,
  },
  filePillText: {
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.terminalFont,
  },
  statusRow: {
    marginBottom: 10,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 4,
  },
  statusText: {
    flex: 1,
    minWidth: 0,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.uiFont,
  },
  commentaryRow: {
    marginLeft: 34,
    marginBottom: 10,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
    paddingVertical: 8,
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 7,
  },
  commentaryText: {
    flex: 1,
    minWidth: 0,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.uiFont,
  },
  commentaryContent: {
    flex: 1,
    minWidth: 0,
  },
  commentaryTitle: {
    marginBottom: 2,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
  commentaryToggle: {
    alignSelf: "flex-start",
    marginTop: 5,
    minHeight: 24,
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
  jumpButton: {
    position: "absolute",
    right: 14,
    bottom: 76,
    minHeight: 32,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
    zIndex: 4,
  },
  jumpButtonText: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  emptyState: {
    minHeight: 260,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
  },
  emptyTitle: {
    marginTop: 10,
    fontSize: 16,
    lineHeight: 21,
    textAlign: "center",
    fontFamily: Typography.uiFontMedium,
  },
  emptyBody: {
    marginTop: 7,
    fontSize: 12,
    lineHeight: 18,
    textAlign: "center",
    fontFamily: Typography.uiFont,
  },
  emptyAction: {
    marginTop: 14,
    minHeight: 36,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 7,
  },
  emptyActionText: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
  composer: {
    paddingHorizontal: 12,
    paddingTop: 8,
  },
  composerPanel: {
    minHeight: 50,
    borderRadius: 25,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 4,
    paddingRight: 6,
    paddingVertical: 4,
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
  composerPanelFloating: {
    shadowColor: "#000000",
    shadowOffset: { width: 0, height: 8 },
    shadowOpacity: 0.16,
    shadowRadius: 18,
    elevation: 10,
  },
  composerInputWrap: {
    flex: 1,
    minHeight: 40,
    maxHeight: 110,
    justifyContent: "center",
  },
  composerInput: {
    width: "100%",
    minHeight: 40,
    maxHeight: 110,
    paddingHorizontal: 4,
    paddingTop: 9,
    paddingBottom: 7,
    fontSize: 15,
    lineHeight: 21,
    fontFamily: Typography.uiFont,
    includeFontPadding: false,
  },
  attachmentRail: {
    marginBottom: 7,
  },
  attachmentList: {
    minHeight: 38,
    alignItems: "center",
    gap: 7,
    paddingHorizontal: 2,
  },
  attachmentChip: {
    maxWidth: 220,
    minHeight: 36,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 9,
    paddingRight: 5,
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
  },
  attachmentUploading: {
    paddingRight: 10,
  },
  attachmentTextGroup: {
    flex: 1,
    minWidth: 0,
  },
  attachmentName: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
  attachmentPath: {
    marginTop: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.terminalFont,
  },
  attachmentRemove: {
    width: 24,
    height: 28,
    alignItems: "center",
    justifyContent: "center",
  },
  quickCommandMenu: {
    marginBottom: 8,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  quickCommandScroller: {
    maxHeight: 330,
  },
  quickCommandRow: {
    minHeight: 50,
    flexDirection: "row",
    alignItems: "center",
    gap: 9,
    paddingHorizontal: 10,
    paddingVertical: 7,
  },
  quickCommandIcon: {
    width: 28,
    height: 28,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
  },
  quickCommandCopy: {
    flex: 1,
    minWidth: 0,
  },
  quickCommandTitle: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
  quickCommandDescription: {
    marginTop: 1,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
  quickCommandValue: {
    maxWidth: 72,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  quickCommandBadge: {
    maxWidth: 86,
    minHeight: 22,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 7,
    alignItems: "center",
    justifyContent: "center",
  },
  quickCommandBadgeText: {
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFontMedium,
  },
  quickCommandEmpty: {
    minHeight: 44,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 12,
  },
});
