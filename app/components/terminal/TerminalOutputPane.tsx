import React from "react";
import {
  StyleSheet,
  View,
  type LayoutChangeEvent,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { TerminalAccessoryDock } from "./TerminalAccessoryDock";
import {
  TerminalSurface,
  type TerminalSurfaceHandle,
} from "./TerminalSurface";
import { TerminalOutputStateCard } from "./TerminalOutputStateCard";

interface TerminalOutputPaneProps {
  sessionKey: string | null;
  serverId: string;
  agentId: string;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
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
  outputBottomInset: number;
  accessoryVisible: boolean;
  accessoryBottomOffset: number;
  serverUrl: string;
  daemonId: string;
  keyboardVisible: boolean;
  onRetryConnection(): void;
  onAccessoryLayout(event: LayoutChangeEvent): void;
}

function TerminalOutputPaneImpl({
  sessionKey,
  serverId,
  agentId,
  theme,
  chrome,
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
  outputBottomInset,
  accessoryVisible,
  accessoryBottomOffset,
  serverUrl,
  daemonId,
  keyboardVisible,
  onRetryConnection,
  onAccessoryLayout,
}: TerminalOutputPaneProps) {
  const shouldAutoResumeTerminal =
    shouldMountTerminalSurface &&
    canRenderTerminal &&
    Boolean(sessionKey && serverId && agentId);

  React.useEffect(() => {
    if (!shouldAutoResumeTerminal) {
      return;
    }
    const frame = requestAnimationFrame(() => {
      terminalRef.current?.resumeInput();
    });
    return () => {
      cancelAnimationFrame(frame);
    };
  }, [agentId, serverId, sessionKey, shouldAutoResumeTerminal, terminalRef]);

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
            theme={theme}
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
      </View>

      {accessoryVisible ? (
        <TerminalAccessoryDock
          terminalRef={terminalRef}
          serverUrl={serverUrl}
          daemonId={daemonId}
          theme={theme}
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

export const TerminalOutputPane = React.memo(TerminalOutputPaneImpl);

const styles = StyleSheet.create({
  output: {
    flex: 1,
    minHeight: 0,
    paddingTop: 4,
  },
});
