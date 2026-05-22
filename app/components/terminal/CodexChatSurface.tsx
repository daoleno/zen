import React, {
  useCallback,
  useEffect,
  useState,
} from "react";
import {
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
import { CodexChatComposer } from "./CodexChatComposer";
import { useCodexChatController } from "./CodexChatController";
import { CodexChatHeader } from "./CodexChatHeader";
import { useCodexChatSession } from "./CodexChatSession";
import { CodexChatTimelineSection } from "./CodexChatTimelineSection";
import { useCodexSlashCommands } from "./CodexSlashCommands";
import {
  useCodexComposerPresentation,
  useCodexComposerInput,
  usePinnedTimeline,
} from "./CodexChatSurfaceHooks";

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

  const composerPresentation = useCodexComposerPresentation({
    draft,
    slashCommands,
    connectionState,
    agentStatus: agent?.status,
    attachmentCount: attachments.length,
    sending,
    canSend,
    composerFocused,
    safeAreaTop: insets.top,
    safeAreaBottom: insets.bottom,
  });
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
        keyboardVerticalOffset={composerPresentation.keyboardVerticalOffset}
        style={styles.chatBody}
      >
        <CodexChatTimelineSection
          serverId={serverId}
          agentCwd={agent?.cwd}
          conversation={conversation}
          events={events}
          chatCommandEvents={chatCommandEvents}
          loading={loading}
          error={error}
          composerActive={composerPresentation.active}
          composerHeight={composerHeight}
          scrollRef={scrollRef}
          showJumpToLatest={showJumpToLatest}
          chrome={chrome}
          theme={theme}
          onLayout={handleTimelineLayout}
          onScroll={handleTimelineScroll}
          onContentSizeChange={handleContentSizeChange}
          onScrollToLatest={scrollToLatest}
          onUnavailableAction={onSwitchToTerminal}
        />

        <CodexChatComposer
          inputRef={inputRef}
          draft={draft}
          placeholder={composerPresentation.placeholder}
          editable={connectionState === "connected"}
          focused={composerFocused}
          floating={composerPresentation.active}
          canAttach={canAttach}
          uploading={uploading}
          sendEnabled={composerPresentation.sendEnabled}
          sending={sending}
          sendIcon={composerPresentation.sendIcon}
          sendLabel={composerPresentation.sendLabel}
          compactSendIcon={composerPresentation.showStopButton}
          bottomPadding={composerPresentation.bottomPadding}
          showCommandMenu={composerPresentation.showCommandMenu}
          commandQuery={composerPresentation.commandQuery}
          commands={composerPresentation.visibleSlashCommands}
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
          onSendPress={
            composerPresentation.showStopButton ? interruptCodex : sendDraft
          }
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
