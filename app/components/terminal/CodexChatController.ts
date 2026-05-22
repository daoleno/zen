import {
  useCallback,
  useMemo,
  useState,
  type SetStateAction,
} from "react";
import { Alert } from "react-native";
import * as Clipboard from "expo-clipboard";
import type { Agent, ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import { uploadDocumentForServer } from "../../services/uploads";
import { wsClient, type CodexSlashCommand } from "../../services/websocket";
import {
  type ChatCommandEvent,
  type ComposerAttachment,
} from "./CodexChatSession";
import {
  requiresSlashCommandArgs,
  slashCommandHasArgs,
  slashCommandRequestFromDraft,
  slashCommandTerminalMessage,
  slashCommandTerminalText,
  type SlashCommandRequest,
} from "./CodexSlashCommands";
import { SCROLL_TO_BOTTOM_LAYOUT_DELAY_MS } from "./CodexChatSurfaceHooks";
import {
  isEventRunning,
  latestAssistantMessageBody,
} from "./CodexTimelineModel";

const MAX_COMPOSER_ATTACHMENTS = 8;

interface GitDiffAction {
  label: string;
  tone: "clean" | "dirty" | "error" | "loading";
  onPress(): void;
}

interface UseCodexChatControllerInput {
  serverId: string;
  agentId: string;
  agent?: Agent;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  draft: string;
  setDraft(value: string): void;
  attachments: ComposerAttachment[];
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  slashCommands: CodexSlashCommand[];
  gitDiff?: GitDiffAction | null;
  onSwitchToTerminal(): void;
  onOpenGitDiff?: () => void;
  recordChatCommandEvent(
    event: Omit<ChatCommandEvent, "id" | "createdAt">,
  ): void;
  refreshConversation(showLoading?: boolean): Promise<void>;
  scrollToLatest(animated?: boolean, delay?: number): void;
  pinToBottomIfNeeded(animated?: boolean, delay?: number): void;
  focusComposer(): void;
}

export function useCodexChatController({
  serverId,
  agentId,
  agent,
  connectionState,
  connectionIssue,
  conversation,
  events,
  draft,
  setDraft,
  attachments,
  setAttachments,
  slashCommands,
  gitDiff,
  onSwitchToTerminal,
  onOpenGitDiff,
  recordChatCommandEvent,
  refreshConversation,
  scrollToLatest,
  pinToBottomIfNeeded,
  focusComposer,
}: UseCodexChatControllerInput) {
  const [sending, setSending] = useState(false);
  const [uploading, setUploading] = useState(false);

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
  }, [
    agent?.status,
    connectionIssue,
    connectionState,
    conversation?.updated_at,
    events,
    sending,
  ]);

  const canAttach = connectionState === "connected" && !uploading;
  const canSend =
    connectionState === "connected" &&
    (draft.trim().length > 0 || attachments.length > 0) &&
    !sending &&
    !uploading;

  const submitTextToCodex = useCallback(
    (
      text: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
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
              submitTextToCodex(
                composedText,
                previousDraft,
                previousAttachments,
              ),
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
              submitTextToCodex(
                composedText,
                previousDraft,
                previousAttachments,
              ),
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
              submitTextToCodex(
                composedText,
                previousDraft,
                previousAttachments,
              ),
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

  const pickSlashCommand = useCallback(
    (command: CodexSlashCommand) => {
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
      if (
        command.execution === "terminal-required" &&
        !requiresSlashCommandArgs(command)
      ) {
        showTerminalRequiredAction(
          command,
          command.value,
          command.value,
          draft,
          attachments,
        );
        return;
      }
      setDraft(`${command.value} `);
      focusComposer();
    },
    [
      attachments,
      draft,
      focusComposer,
      runNativeSlashCommand,
      setDraft,
      showTerminalRequiredAction,
      showUnsupportedSlashCommand,
    ],
  );

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
  }, [canAttach, focusComposer, serverId, setAttachments]);

  const removeAttachment = useCallback(
    (id: string) => {
      setAttachments((current) =>
        current.filter((attachment) => attachment.id !== id),
      );
    },
    [setAttachments],
  );

  return {
    sending,
    uploading,
    statusMeta,
    canAttach,
    canSend,
    sendDraft,
    interruptCodex,
    pickSlashCommand,
    handleUploadAttachment,
    removeAttachment,
  };
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

export function conversationUnavailableReason(reason?: string) {
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

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}
