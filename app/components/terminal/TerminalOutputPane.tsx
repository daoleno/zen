import React from "react";
import * as Clipboard from "expo-clipboard";
import {
  Pressable,
  StyleSheet,
  Text,
  View,
  type LayoutChangeEvent,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { TerminalAccessoryDock } from "./TerminalAccessoryDock";
import { TerminalSurface, type TerminalSurfaceHandle } from "./TerminalSurface";
import type { SkillsHandoffFailure } from "./TerminalSurface.types";
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
  onRetryConnection(): void;
  onAccessoryLayout(event: LayoutChangeEvent): void;
  skillsHandoffToken?: string;
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
  onRetryConnection,
  onAccessoryLayout,
  skillsHandoffToken,
}: TerminalOutputPaneProps) {
  const [skillsFailure, setSkillsFailure] =
    React.useState<SkillsHandoffFailure | null>(null);
  const hasTerminalTarget = Boolean(sessionKey && serverId && agentId);
  const shouldAutoResumeTerminal =
    terminalSurfaceActive && canRenderTerminal && hasTerminalTarget;

  React.useEffect(() => {
    setSkillsFailure(null);
  }, [sessionKey]);

  React.useEffect(() => {
    if (!shouldMountTerminalSurface || !hasTerminalTarget) {
      return;
    }

    if (!terminalSurfaceActive) {
      terminalRef.current?.blur();
      return;
    }

    const frame = requestAnimationFrame(() => {
      // resumeInput wakes the WebView compositor then restores live input.
      terminalRef.current?.resumeInput();
    });
    return () => {
      cancelAnimationFrame(frame);
    };
  }, [
    agentId,
    hasTerminalTarget,
    serverId,
    sessionKey,
    shouldAutoResumeTerminal,
    shouldMountTerminalSurface,
    terminalRef,
    terminalSurfaceActive,
  ]);

  return (
    <>
      <View
        collapsable={false}
        pointerEvents={terminalSurfaceActive ? "auto" : "none"}
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
            skillsHandoffToken={skillsHandoffToken}
            onSkillsHandoffFailure={setSkillsFailure}
          />
        ) : null}
        {skillsFailure ? (
          <View
            accessibilityRole="alert"
            style={[
              styles.skillsFailure,
              {
                backgroundColor: chrome.surface,
                borderColor: chrome.border,
              },
            ]}
          >
            <Text style={[styles.skillsFailureTitle, { color: chrome.text }]}>
              {skillsFailure.kind === "not-submitted"
                ? "Skills command was not submitted."
                : "Skills command submission was not confirmed."}
            </Text>
            <Text
              style={[styles.skillsFailureDetail, { color: chrome.textMuted }]}
            >
              Review the Terminal before running it manually.
            </Text>
            <Text
              selectable
              numberOfLines={3}
              style={[styles.skillsFailureCommand, { color: chrome.text }]}
            >
              {skillsFailure.command.command}
            </Text>
            <Pressable
              accessibilityRole="button"
              onPress={() =>
                void Clipboard.setStringAsync(skillsFailure.command.command)
              }
              style={[styles.skillsFailureCopy, { borderColor: chrome.border }]}
            >
              <Text
                style={[styles.skillsFailureCopyText, { color: chrome.text }]}
              >
                Copy command
              </Text>
            </Pressable>
          </View>
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
          uploadOwnerKey={sessionKey}
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
  skillsFailure: {
    position: "absolute",
    left: 12,
    right: 12,
    top: 12,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 12,
    padding: 12,
    gap: 6,
  },
  skillsFailureTitle: { fontSize: 14, fontWeight: "600" },
  skillsFailureDetail: { fontSize: 12, lineHeight: 17 },
  skillsFailureCommand: {
    fontFamily: "monospace",
    fontSize: 11,
    lineHeight: 16,
  },
  skillsFailureCopy: {
    alignSelf: "flex-start",
    minHeight: 36,
    justifyContent: "center",
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 8,
    paddingHorizontal: 12,
  },
  skillsFailureCopyText: { fontSize: 12, fontWeight: "600" },
});
