import { useEffect, useMemo } from "react";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type { AgentStatus } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { Agent, ConnectionState } from "../../store/agents";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { CodexChatBodyProps } from "./CodexChatBody";
import { useCodexChatController } from "./CodexChatController";
import type { CodexChatHeaderProps } from "./CodexChatHeader";
import { useCodexChatSession } from "./CodexChatSession";
import { useCodexSlashCommands } from "./CodexSlashCommands";
import { useCodexChatBodyProps } from "./useCodexChatBodyProps";
import {
  useCodexComposerInput,
  useCodexComposerPresentation,
  usePinnedTimeline,
} from "./CodexChatSurfaceHooks";

interface GitDiffAction {
  label: string;
  tone: "clean" | "dirty" | "error" | "loading";
  onPress(): void;
}

interface UseCodexChatSurfaceStateInput {
  serverId: string;
  agentId: string;
  agent?: Agent;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  gitDiff?: GitDiffAction | null;
  onSwitchToTerminal(): void;
  onOpenGitDiff?: () => void;
}

interface CodexChatSurfaceState {
  headerProps: CodexChatHeaderProps;
  bodyProps: CodexChatBodyProps;
}

export function useCodexChatSurfaceState({
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
}: UseCodexChatSurfaceStateInput): CodexChatSurfaceState {
  const insets = useSafeAreaInsets();
  const slashCommands = useCodexSlashCommands({
    serverId,
    connectionState,
    screenFocused,
  });
  const session = useCodexChatSession({
    serverId,
    agentId,
    agent,
    connectionState,
    screenFocused,
  });
  const {
    cacheKey: conversationCacheKey,
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
  } = session;
  const events = conversation?.events ?? [];
  const timeline = usePinnedTimeline(events.length);
  const composerInput = useCodexComposerInput({
    enabled: screenFocused && connectionState === "connected",
    onKeyboardShown: timeline.pinToBottomIfNeeded,
  });
  const controller = useCodexChatController({
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
    scrollToLatest: timeline.scrollToLatest,
    pinToBottomIfNeeded: timeline.pinToBottomIfNeeded,
    focusComposer: composerInput.focus,
  });

  useEffect(() => {
    timeline.resetForConversation();
  }, [conversationCacheKey, timeline.resetForConversation]);

  const composerPresentation = useCodexComposerPresentation({
    draft,
    slashCommands,
    connectionState,
    agentStatus: agent?.status,
    attachmentCount: attachments.length,
    sending: controller.sending,
    canSend: controller.canSend,
    composerFocused: composerInput.focused,
    safeAreaTop: insets.top,
    safeAreaBottom: insets.bottom,
  });
  const bodyProps = useCodexChatBodyProps({
    screenFocused,
    serverId,
    agent,
    connectionState,
    conversation,
    events,
    chatCommandEvents,
    loading,
    error,
    draft,
    attachments,
    composerPresentation,
    timeline,
    composerInput,
    controller,
    chrome,
    theme,
    onSwitchToTerminal,
    setDraft,
  });
  const headerProps = useMemo(
    () => ({
      status: (agent?.status || "unknown") as AgentStatus,
      statusMeta: controller.statusMeta,
      theme,
      chrome,
      gitDiff,
      onSwitchToTerminal,
    }),
    [
      agent?.status,
      chrome,
      controller.statusMeta,
      gitDiff,
      onSwitchToTerminal,
      theme,
    ],
  );

  return {
    headerProps,
    bodyProps,
  };
}
