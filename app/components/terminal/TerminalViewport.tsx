import React from "react";
import { StyleSheet, View, type LayoutChangeEvent } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { Agent, ConnectionState } from "../../store/agents";
import { InterfaceChatSurface } from "./InterfaceChatSurface";
import type { InterfaceChatAgentInfo } from "./InterfaceChatSession";
import { CHAT_HEADER_HEIGHT, CHAT_HEADER_OUTER_GAP } from "./chatChromeMetrics";
import { TerminalOutputPane } from "./TerminalOutputPane";
import type { TerminalSurfaceHandle } from "./TerminalSurface";

export interface TerminalViewportProps {
  showInterfaceChat: boolean;
  initialComposerFocusGrant: string | null;
  sessionKey: string | null;
  serverId: string;
  agentId: string;
  agent?: Agent;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  terminalRef: React.RefObject<TerminalSurfaceHandle | null>;
  ctrlArmed: boolean;
  onCtrlArmedChange(next: boolean): void;
  canRenderTerminal: boolean;
  shouldMountTerminalSurface: boolean;
  terminalSurfaceActive: boolean;
  terminalStateAccent: string;
  terminalStateBusy: boolean;
  terminalStateTitle: string;
  terminalStateDetail: string;
  terminalStateHint: string;
  hasTerminalRoute: boolean;
  outputBottomInset: number;
  accessoryVisible: boolean;
  accessoryBottomOffset: number;
  serverUrl: string;
  daemonId: string;
  keyboardVisible: boolean;
  onSwitchToTerminal(): void;
  onRetryConnection(): void;
  onAccessoryLayout(event: LayoutChangeEvent): void;
  onConsumeInitialComposerFocus(): void;
  skillsHandoffToken?: string;
}

function TerminalViewportImpl({
  showInterfaceChat,
  initialComposerFocusGrant,
  sessionKey,
  serverId,
  agentId,
  agent,
  connectionState,
  connectionIssue,
  theme,
  chrome,
  screenFocused,
  terminalRef,
  ctrlArmed,
  onCtrlArmedChange,
  canRenderTerminal,
  shouldMountTerminalSurface,
  terminalSurfaceActive,
  terminalStateAccent,
  terminalStateBusy,
  terminalStateTitle,
  terminalStateDetail,
  terminalStateHint,
  hasTerminalRoute,
  outputBottomInset,
  accessoryVisible,
  accessoryBottomOffset,
  serverUrl,
  daemonId,
  keyboardVisible,
  onSwitchToTerminal,
  onRetryConnection,
  onAccessoryLayout,
  onConsumeInitialComposerFocus,
  skillsHandoffToken,
}: TerminalViewportProps) {
  const interfaceChatAgentInfo = React.useMemo<
    InterfaceChatAgentInfo | undefined
  >(
    () =>
      agent
        ? {
            status: agent.status,
            summary: agent.summary,
            phase: agent.phase,
            attention: agent.attention,
            taskClass: agent.task_class,
            eventKind: agent.event_kind,
            detailsJson: agent.details_json,
            needsAttention: agent.needs_attention,
            lastOutputLines: agent.last_output_lines,
            cwd: agent.cwd,
            command: agent.command,
            name: agent.name,
            startedAt: agent.started_at,
            processId: agent.process_id,
          }
        : undefined,
    [
      agent?.command,
      agent?.cwd,
      agent?.attention,
      agent?.details_json,
      agent?.event_kind,
      agent?.name,
      agent?.needs_attention,
      agent?.phase,
      agent?.process_id,
      agent?.started_at,
      agent?.status,
      agent?.summary,
      agent?.task_class,
      agent?.last_output_lines,
    ],
  );

  return (
    <View style={[styles.terminalStage, { backgroundColor: theme.background }]}>
      <View
        style={[styles.terminalShell, { backgroundColor: theme.background }]}
      >
        <View style={styles.terminalContent}>
          <TerminalOutputPane
            sessionKey={sessionKey}
            serverId={serverId}
            agentId={agentId}
            theme={theme}
            chrome={chrome}
            terminalRef={terminalRef}
            ctrlArmed={ctrlArmed}
            onCtrlArmedChange={onCtrlArmedChange}
            canRenderTerminal={canRenderTerminal}
            shouldMountTerminalSurface={shouldMountTerminalSurface}
            terminalSurfaceActive={terminalSurfaceActive}
            terminalStateAccent={terminalStateAccent}
            terminalStateBusy={terminalStateBusy}
            terminalStateTitle={terminalStateTitle}
            terminalStateDetail={terminalStateDetail}
            terminalStateHint={terminalStateHint}
            hasTerminalRoute={hasTerminalRoute}
            outputBottomInset={outputBottomInset}
            accessoryVisible={accessoryVisible}
            accessoryBottomOffset={accessoryBottomOffset}
            serverUrl={serverUrl}
            daemonId={daemonId}
            keyboardVisible={keyboardVisible}
            onRetryConnection={onRetryConnection}
            onAccessoryLayout={onAccessoryLayout}
            skillsHandoffToken={skillsHandoffToken}
          />

          {showInterfaceChat && sessionKey && serverId && agentId ? (
            <View pointerEvents="auto" style={styles.chatOverlay}>
              <InterfaceChatSurface
                key={`interface-chat:${sessionKey}`}
                visible
                serverId={serverId}
                agentId={agentId}
                agentInfo={interfaceChatAgentInfo}
                connectionState={connectionState}
                connectionIssue={connectionIssue}
                theme={theme}
                chrome={chrome}
                screenFocused={screenFocused}
                initialComposerFocusGrant={initialComposerFocusGrant}
                topChromeInset={CHAT_HEADER_HEIGHT + CHAT_HEADER_OUTER_GAP * 2}
                onSwitchToTerminal={onSwitchToTerminal}
                onConsumeInitialComposerFocus={onConsumeInitialComposerFocus}
              />
            </View>
          ) : null}
        </View>
      </View>
    </View>
  );
}

export const TerminalViewport = React.memo(TerminalViewportImpl);

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
  chatOverlay: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    zIndex: 12,
  },
});
