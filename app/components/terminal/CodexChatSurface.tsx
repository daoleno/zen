import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Alert,
  Platform,
  StyleSheet,
  View,
  type LayoutChangeEvent,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Clipboard from "expo-clipboard";
import { KeyboardAvoidingView } from "react-native-keyboard-controller";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type { AgentStatus } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { Agent, ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import {
  uploadDocumentForServer,
  type UploadedAttachment,
} from "../../services/uploads";
import { wsClient, type CodexSlashCommand } from "../../services/websocket";
import { CodexChatComposer } from "./CodexChatComposer";
import { CodexChatHeader } from "./CodexChatHeader";
import {
  SCROLL_TO_BOTTOM_LAYOUT_DELAY_MS,
  useCodexComposerInput,
  usePinnedTimeline,
} from "./CodexChatSurfaceHooks";
import {
  type PatchFileSummary,
  type PatchOperation,
} from "./CodexTimelineActivity";
import {
  type DisplayAttachment,
} from "./CodexTimelineMessage";
import {
  type ZenTimelineItem,
} from "./CodexTimelineItemView";
import { CodexTimelineView } from "./CodexTimelineView";
import {
  slashCommandIcon,
  slashCommandTitle,
} from "./codexSlashCommandPresentation";

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
const MAX_COMPOSER_ATTACHMENTS = 8;
const FULL_OUTPUT_HINT = "Open Terminal for full output.";
const TERMINAL_ROUTE_BAR_HEIGHT = 38;

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

type ToolPresentation = {
  subtitle?: string;
  icon: IoniconName;
  localImagePath?: string;
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
  const loadTimelineAssetPreview = useCallback(
    async (path: string) => {
      const asset = await wsClient.getCodexAsset(serverId, {
        path,
        cwd: conversation?.cwd || agent?.cwd,
      });
      return asset.data_url || null;
    },
    [agent?.cwd, conversation?.cwd, serverId],
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
      <CodexChatHeader
        status={(agent?.status || "unknown") as AgentStatus}
        statusMeta={statusMeta}
        theme={theme}
        chrome={chrome}
        gitDiff={gitDiff}
        onSwitchToTerminal={onSwitchToTerminal}
      />

      <KeyboardAvoidingView
        behavior="padding"
        enabled={screenFocused}
        keyboardVerticalOffset={keyboardVerticalOffset}
        style={styles.chatBody}
      >
        <CodexTimelineView
          scrollRef={scrollRef}
          items={timelineItems}
          loading={loading}
          error={error}
          unavailable={unavailable}
          unavailableReason={conversationUnavailableReason(conversation?.reason)}
          textSelectable={!composerActive}
          showJumpToLatest={showJumpToLatest}
          jumpButtonBottom={composerHeight + 12}
          streamingAssistantId={streamingAssistantId}
          chrome={chrome}
          theme={theme}
          onLayout={handleTimelineLayout}
          onScroll={handleTimelineScroll}
          onContentSizeChange={handleContentSizeChange}
          onJumpToLatest={() => scrollToLatest(true)}
          onUnavailableAction={onSwitchToTerminal}
          loadAssetPreview={loadTimelineAssetPreview}
          formatPatchPath={patchDisplayPath}
          truncateBody={truncateRunes}
        />

        <CodexChatComposer
          inputRef={inputRef}
          draft={draft}
          placeholder={composerPlaceholder}
          editable={connectionState === "connected"}
          focused={composerFocused}
          floating={composerActive}
          canAttach={canAttach}
          uploading={uploading}
          sendEnabled={sendActionEnabled}
          sending={sending}
          sendIcon={sendActionIcon}
          sendLabel={sendActionLabel}
          compactSendIcon={showStopButton}
          bottomPadding={composerBottomPadding}
          showCommandMenu={showCommandMenu}
          commandQuery={commandQuery}
          commands={visibleSlashCommands}
          attachments={attachments}
          chrome={chrome}
          theme={theme}
          onLayout={handleComposerLayout}
          onSelectCommand={pickSlashCommand}
          onRemoveAttachment={removeAttachment}
          onDraftChange={setDraft}
          onUploadPress={() => void handleUploadAttachment()}
          onInputPress={focusComposer}
          onInputFocus={handleComposerFocus}
          onInputBlur={handleComposerBlur}
          onInputStart={handleComposerInputStart}
          onSubmit={sendDraft}
          onSendPress={showStopButton ? interruptCodex : sendDraft}
        />
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

function isEventRunning(event: CodexConversationEvent) {
  return event.status === "running";
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

function toolPresentation(event: CodexConversationEvent): ToolPresentation {
  const name = (event.tool_name || event.title || "tool").trim() || "tool";
  const input = parseToolPayload(event.input);
  const inputObject = isRecord(input) ? input : {};
  const browserAction = /^browser_/.test(name) ? humanizeToolName(name.replace(/^browser_/, "")) : "";

  if (name === "view_image") {
    const path = stringField(inputObject, "path") || stringField(inputObject, "image_url");
    const previewUri = previewableImageUri(path);
    return {
      subtitle: path ? basename(path) : undefined,
      icon: "image-outline",
      localImagePath: path && !previewUri ? path : undefined,
    };
  }

  if (name === "write_stdin") {
    const chars = stringField(inputObject, "chars");
    const sessionId = valueField(inputObject, "session_id");
    return {
      subtitle: sessionId ? `session ${sessionId}` : undefined,
      icon: chars === ""
        ? "sync-outline"
        : chars === "\u0003"
          ? "stop-circle-outline"
          : "return-down-forward-outline",
    };
  }

  if (browserAction) {
    const browserFile = stringField(inputObject, "filename") || firstString(inputObject.paths);
    const browserPreviewUri = looksLikeImagePath(browserFile)
      ? previewableImageUri(browserFile)
      : undefined;
    return {
      subtitle: stringField(inputObject, "element") || stringField(inputObject, "url") || undefined,
      icon: browserToolIcon(name),
      localImagePath: browserFile && !browserPreviewUri && looksLikeImagePath(browserFile)
        ? browserFile
        : undefined,
    };
  }

  if (name.includes("query_docs") || name.includes("resolve_library_id")) {
    return {
      subtitle: stringField(inputObject, "libraryId") || stringField(inputObject, "libraryName") || undefined,
      icon: "library-outline",
    };
  }

  if (name.includes("search_query") || name === "web.run") {
    return {
      icon: "search-outline",
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
      subtitle: names.length ? names.slice(0, 2).join(", ") : undefined,
      icon: "git-network-outline",
    };
  }

  if (name.includes("spawn_agent") || name.includes("send_input") || name.includes("wait_agent")) {
    return {
      subtitle: stringField(inputObject, "target") || firstString(inputObject.targets),
      icon: "git-network-outline",
    };
  }

  return {
    icon: "cube-outline",
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
  chatBody: {
    flex: 1,
    minHeight: 0,
  },
});
