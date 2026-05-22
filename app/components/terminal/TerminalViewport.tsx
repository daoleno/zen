import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
  type LayoutChangeEvent,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Colors, Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemeName,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { Agent, ConnectionState } from "../../store/agents";
import { CodexChatSurface } from "./CodexChatSurface";
import { TerminalAccessoryBar } from "./TerminalAccessoryBar";
import {
  TerminalSurface,
  type TerminalSurfaceHandle,
} from "./TerminalSurface";

interface GitDiffChip {
  label: string;
  tone: "clean" | "dirty" | "error" | "loading";
  onPress(): void;
}

interface TerminalViewportProps {
  showCodexChat: boolean;
  sessionKey: string | null;
  serverId: string;
  agentId: string;
  agent?: Agent;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  themeName: TerminalThemeName;
  screenFocused: boolean;
  gitDiff?: GitDiffChip | null;
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
  onSwitchToTerminal(): void;
  onSwitchToChat(): void;
  onOpenGitDiff(): void;
  onRetryConnection(): void;
  onAccessoryLayout(event: LayoutChangeEvent): void;
}

export function TerminalViewport({
  showCodexChat,
  sessionKey,
  serverId,
  agentId,
  agent,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  themeName,
  screenFocused,
  gitDiff,
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
  onSwitchToTerminal,
  onSwitchToChat,
  onOpenGitDiff,
  onRetryConnection,
  onAccessoryLayout,
}: TerminalViewportProps) {
  const viewport =
    showCodexChat && sessionKey && serverId && agentId ? (
      <CodexChatSurface
        key={`codex-chat:${sessionKey}`}
        serverId={serverId}
        agentId={agentId}
        agent={agent}
        connectionState={connectionState}
        connectionIssue={connectionIssue}
        theme={theme}
        chrome={chrome}
        screenFocused={screenFocused}
        gitDiff={gitDiff}
        onSwitchToTerminal={onSwitchToTerminal}
        onOpenGitDiff={onOpenGitDiff}
      />
    ) : (
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
            <View style={styles.terminalState}>
              <View
                style={[
                  styles.terminalStateCard,
                  {
                    backgroundColor: chrome.surface,
                    borderColor: terminalStateAccent,
                  },
                ]}
              >
                {terminalStateBusy ? (
                  <ActivityIndicator color={terminalStateAccent} />
                ) : (
                  <View
                    style={[
                      styles.terminalStateDot,
                      { backgroundColor: terminalStateAccent },
                    ]}
                  />
                )}
                <Text style={[styles.terminalStateTitle, { color: chrome.text }]}>
                  {terminalStateTitle}
                </Text>
                <Text
                  style={[
                    styles.terminalStateDetail,
                    { color: chrome.textMuted },
                  ]}
                >
                  {terminalStateDetail}
                </Text>
                <Text
                  style={[
                    styles.terminalStateHint,
                    { color: chrome.textSubtle },
                  ]}
                >
                  {terminalStateHint}
                </Text>
                {hasTerminalRoute ? (
                  <TouchableOpacity
                    style={[
                      styles.terminalStateAction,
                      { backgroundColor: chrome.accent },
                    ]}
                    onPress={onRetryConnection}
                    activeOpacity={0.84}
                  >
                    <Text
                      style={[
                        styles.terminalStateActionText,
                        { color: theme.background },
                      ]}
                    >
                      Retry Connection
                    </Text>
                  </TouchableOpacity>
                ) : null}
              </View>
            </View>
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
          <View
            onLayout={onAccessoryLayout}
            style={[
              styles.inputShell,
              styles.inputShellDock,
              { bottom: accessoryBottomOffset },
            ]}
          >
            <TerminalAccessoryBar
              terminalRef={terminalRef}
              serverUrl={serverUrl}
              daemonId={daemonId}
              theme={theme}
              gitDiff={gitDiff}
              keyboardVisible={keyboardVisible}
              ctrlArmed={ctrlArmed}
              onCtrlArmedChange={onCtrlArmedChange}
            />
          </View>
        ) : null}
      </>
    );

  return (
    <View style={[styles.terminalStage, { backgroundColor: theme.background }]}>
      <View style={[styles.terminalShell, { backgroundColor: theme.background }]}>
        <View style={styles.terminalContent}>{viewport}</View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  terminalStage: {
    flex: 1,
    minHeight: 0,
    overflow: "hidden",
    justifyContent: "center",
  },
  terminalShell: {
    flex: 1,
    minHeight: 0,
  },
  terminalContent: {
    flex: 1,
    minHeight: 0,
    position: "relative",
  },
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
  terminalState: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 18,
    paddingBottom: 32,
  },
  terminalStateCard: {
    width: "100%",
    maxWidth: 360,
    alignItems: "center",
    paddingHorizontal: 18,
    paddingVertical: 20,
    borderRadius: 20,
    borderWidth: 1,
    backgroundColor: "rgba(17,22,31,0.9)",
  },
  terminalStateDot: {
    width: 10,
    height: 10,
    borderRadius: 5,
  },
  terminalStateTitle: {
    marginTop: 12,
    color: Colors.textPrimary,
    fontSize: 18,
    fontFamily: Typography.uiFontMedium,
    textAlign: "center",
  },
  terminalStateDetail: {
    marginTop: 8,
    color: "#D6DFEC",
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
    textAlign: "center",
  },
  terminalStateHint: {
    marginTop: 8,
    color: "#8E9DB2",
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
    textAlign: "center",
  },
  terminalStateAction: {
    marginTop: 16,
    minHeight: 38,
    paddingHorizontal: 14,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: Colors.accent,
  },
  terminalStateActionText: {
    color: Colors.bgPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  inputShell: {
    backgroundColor: "transparent",
  },
  inputShellDock: {
    position: "absolute",
    left: 0,
    right: 0,
    bottom: 0,
    zIndex: 8,
  },
});
