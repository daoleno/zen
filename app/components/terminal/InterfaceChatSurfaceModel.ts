import type { ConnectionState } from "../../store/agents";
import {
  buildChatComposerPlaceholder,
  chatAgentSupportsSlashCommands,
} from "../../services/chatComposerPresentation";
import type { AgentKind } from "../../services/agentPresentation";
import type { ChatLayout } from "../../theme/types";
import type { CodexSlashCommand } from "../../services/websocket";
import type { ProviderActivity } from "../../services/codexConversation";
import { filterSlashCommands } from "./CodexSlashCommands";
import { resolveComposerSendAction } from "./composerSendAction";

export interface InterfaceComposerPresentation {
  commandQuery: string;
  visibleSlashCommands: CodexSlashCommand[];
  showCommandMenu: boolean;
  showCommandList: boolean;
  showComposerActions: boolean;
  showActionMenuButton: boolean;
  actionMenuIcon: "add" | "happy-outline";
  showAttachmentRail: boolean;
  composerActionButtonEnabled: boolean;
  showStopButton: boolean;
  stopEnabled: boolean;
  stopLabel: string;
  stopLoading: boolean;
  providerActivityStartedAt?: string;
  sendEnabled: boolean;
  sendLabel: string;
  placeholder: string;
  bottomPadding: number;
  keyboardVerticalOffset: number;
  composerLayout: ChatLayout;
}

export interface InterfaceComposerPresentationInput {
  draft: string;
  slashCommands: CodexSlashCommand[];
  agentKind: AgentKind;
  connectionState: ConnectionState;
  runningActivity?: ProviderActivity;
  attachmentCount: number;
  interrupting: boolean;
  canSend: boolean;
  elapsedStartedAt?: string;
  actionMenuPinned: boolean;
  safeAreaBottom: number;
  placeholder?: string;
  keyboardVerticalOffset?: number;
  composerBottomInset?: number;
  composerLayout?: ChatLayout;
}

export function buildInterfaceComposerPresentation({
  draft,
  slashCommands,
  agentKind,
  connectionState,
  runningActivity,
  attachmentCount,
  interrupting,
  canSend,
  elapsedStartedAt,
  actionMenuPinned,
  safeAreaBottom,
  placeholder,
  keyboardVerticalOffset,
  composerBottomInset,
  composerLayout = "telegram",
}: InterfaceComposerPresentationInput): InterfaceComposerPresentation {
  const commandQuery = draft.trimStart();
  const slashCommandsEnabled = chatAgentSupportsSlashCommands(agentKind);
  const slashQueryActive =
    slashCommandsEnabled &&
    commandQuery.startsWith("/") &&
    !commandQuery.includes(" ") &&
    !commandQuery.includes("\n");
  const normalDraftActive = commandQuery.length > 0 && !slashQueryActive;
  const visibleSlashCommands = slashCommandsEnabled
    ? filterSlashCommands(slashCommands, slashQueryActive ? commandQuery : "/")
    : [];
  const showComposerActions =
    connectionState === "connected" && actionMenuPinned;
  const showCommandList =
    slashCommandsEnabled &&
    connectionState === "connected" &&
    (actionMenuPinned || slashQueryActive) &&
    (slashQueryActive || !normalDraftActive) &&
    (slashQueryActive || visibleSlashCommands.length > 0);
  const showCommandMenu = showComposerActions || showCommandList;
  const composerActionButtonEnabled = connectionState === "connected";
  const sendAction = resolveComposerSendAction({
    canSend,
    connected: connectionState === "connected",
    elapsedStartedAt,
    hasComposerContent: draft.trim().length > 0 || attachmentCount > 0,
    interrupting,
    activityRunning: Boolean(runningActivity),
  });
  const { showStopButton } = sendAction;

  return {
    commandQuery,
    visibleSlashCommands,
    showCommandMenu,
    showCommandList,
    showComposerActions,
    showActionMenuButton: true,
    actionMenuIcon: "add",
    showAttachmentRail: true,
    composerActionButtonEnabled,
    showStopButton,
    stopEnabled: sendAction.stopEnabled,
    stopLabel: sendAction.stopLabel,
    stopLoading: interrupting,
    providerActivityStartedAt: sendAction.providerActivityStartedAt,
    sendEnabled: sendAction.sendEnabled,
    sendLabel: sendAction.sendLabel,
    placeholder: buildChatComposerPlaceholder({
      agentKind,
      connectionState,
      slashQueryActive,
      explicitPlaceholder: placeholder,
    }),
    bottomPadding: composerBottomInset ?? Math.max(safeAreaBottom, 8),
    keyboardVerticalOffset: keyboardVerticalOffset ?? 0,
    composerLayout,
  };
}
