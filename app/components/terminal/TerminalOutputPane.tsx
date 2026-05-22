import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
  type LayoutChangeEvent,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemeName,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { TerminalAccessoryDock } from "./TerminalAccessoryDock";
import {
  TerminalSurface,
  type TerminalSurfaceHandle,
} from "./TerminalSurface";
import { TerminalOutputStateCard } from "./TerminalOutputStateCard";

interface GitDiffChip {
  label: string;
  tone: "clean" | "dirty" | "error" | "loading";
  onPress(): void;
}

interface TerminalOutputPaneProps {
  sessionKey: string | null;
  serverId: string;
  agentId: string;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  themeName: TerminalThemeName;
  terminalRef: React.RefObject<TerminalSurfaceHandle | null>;
  ctrlArmed: boolean;
  onCtrlArmedChange(next: boolean): void;
  canRenderTerminal: boolean;
  shouldMountTerminalSurface: boolean;
  terminalStateAccent: string;
  terminalStateBusy: boolean;
  terminalStateTitle: string;
  terminalStateDetail: string;
  terminalStateHint: string;
  hasTerminalRoute: boolean;
  isCodexAgent: boolean;
  outputBottomInset: number;
  accessoryVisible: boolean;
  accessoryBottomOffset: number;
  serverUrl: string;
  daemonId: string;
  keyboardVisible: boolean;
  gitDiff?: GitDiffChip | null;
  onSwitchToChat(): void;
  onRetryConnection(): void;
  onAccessoryLayout(event: LayoutChangeEvent): void;
}

export function TerminalOutputPane({
  sessionKey,
  serverId,
  agentId,
  theme,
  chrome,
  themeName,
  terminalRef,
  ctrlArmed,
  onCtrlArmedChange,
  canRenderTerminal,
  shouldMountTerminalSurface,
  terminalStateAccent,
  terminalStateBusy,
  terminalStateTitle,
  terminalStateDetail,
  terminalStateHint,
  hasTerminalRoute,
  isCodexAgent,
  outputBottomInset,
  accessoryVisible,
  accessoryBottomOffset,
  serverUrl,
  daemonId,
  keyboardVisible,
  gitDiff,
  onSwitchToChat,
  onRetryConnection,
  onAccessoryLayout,
}: TerminalOutputPaneProps) {
  return (
    <>
      <View
        style={[
          styles.output,
          { backgroundColor: theme.background },
          outputBottomInset > 0 ? { paddingBottom: outputBottomInset } : null,
        ]}
      >
        {shouldMountTerminalSurface && sessionKey && serverId && agentId ? (
          <TerminalSurface
            key={sessionKey}
            ref={terminalRef}
            serverId={serverId}
            targetId={agentId}
            themeName={themeName}
            ctrlArmed={ctrlArmed}
            onCtrlArmedChange={onCtrlArmedChange}
          />
        ) : null}
        {canRenderTerminal ? null : (
          <TerminalOutputStateCard
            accent={terminalStateAccent}
            busy={terminalStateBusy}
            title={terminalStateTitle}
            detail={terminalStateDetail}
            hint={terminalStateHint}
            showRetry={hasTerminalRoute}
            chrome={chrome}
            theme={theme}
            onRetry={onRetryConnection}
          />
        )}
        {isCodexAgent ? (
          <TouchableOpacity
            accessibilityLabel="Open Codex Chat renderer"
            style={[
              styles.codexChatSwitchButton,
              {
                backgroundColor: chrome.surfaceMuted,
                borderColor: chrome.borderStrong,
              },
            ]}
            onPress={onSwitchToChat}
            activeOpacity={0.82}
          >
            <Ionicons name="sparkles-outline" size={14} color={chrome.accent} />
            <Text
              style={[
                styles.codexChatSwitchText,
                { color: chrome.textMuted },
              ]}
            >
              Chat
            </Text>
          </TouchableOpacity>
        ) : null}
      </View>

      {accessoryVisible ? (
        <TerminalAccessoryDock
          terminalRef={terminalRef}
          serverUrl={serverUrl}
          daemonId={daemonId}
          theme={theme}
          gitDiff={gitDiff}
          keyboardVisible={keyboardVisible}
          ctrlArmed={ctrlArmed}
          bottomOffset={accessoryBottomOffset}
          onCtrlArmedChange={onCtrlArmedChange}
          onLayout={onAccessoryLayout}
        />
      ) : null}
    </>
  );
}

const styles = StyleSheet.create({
  codexChatSwitchButton: {
    position: "absolute",
    top: 10,
    right: 10,
    minHeight: 32,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    zIndex: 8,
  },
  codexChatSwitchText: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  output: {
    flex: 1,
    minHeight: 0,
    paddingTop: 4,
  },
});
