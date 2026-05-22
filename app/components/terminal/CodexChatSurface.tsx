import React, {
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import {
  Platform,
  StyleSheet,
  View,
  type LayoutChangeEvent,
} from "react-native";
import { KeyboardAvoidingView } from "react-native-keyboard-controller";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import type { AgentStatus } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { Agent, ConnectionState } from "../../store/agents";
import type { ConnectionIssue } from "../../services/connectionIssue";
import { wsClient } from "../../services/websocket";
import { CodexChatComposer } from "./CodexChatComposer";
import { useCodexChatController } from "./CodexChatController";
import { conversationUnavailableReason } from "./CodexChatControllerModel";
import { CodexChatHeader } from "./CodexChatHeader";
import { useCodexChatSession } from "./CodexChatSession";
import {
  filterSlashCommands,
  useCodexSlashCommands,
} from "./CodexSlashCommands";
import {
  useCodexComposerInput,
  usePinnedTimeline,
} from "./CodexChatSurfaceHooks";
import { CodexTimelineView } from "./CodexTimelineView";
import {
  buildZenTimeline,
  mergeChatCommandEventsIntoTimeline,
  patchDisplayPath,
  truncateRunes,
} from "./CodexTimelineModel";

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

const TERMINAL_ROUTE_BAR_HEIGHT = 38;

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
  const slashCommands = useCodexSlashCommands({
    serverId,
    connectionState,
    screenFocused,
  });
  const [composerHeight, setComposerHeight] = useState(76);
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
  const {
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
  } = useCodexChatController({
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
  });

  useEffect(() => {
    resetForConversation();
  }, [conversationCacheKey, resetForConversation]);

  const unavailable = conversation && !conversation.available;
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
