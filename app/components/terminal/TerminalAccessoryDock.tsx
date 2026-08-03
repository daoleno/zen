import React from "react";
import { StyleSheet, View, type LayoutChangeEvent } from "react-native";
import type { TerminalThemePalette } from "../../constants/terminalThemes";
import { TerminalAccessoryBar } from "./TerminalAccessoryBar";
import type { TerminalSurfaceHandle } from "./TerminalSurface";

interface TerminalAccessoryDockProps {
  terminalRef: React.RefObject<TerminalSurfaceHandle | null>;
  uploadOwnerKey: string | null;
  serverId: string;
  serverUrl: string;
  daemonId: string;
  theme: TerminalThemePalette;
  keyboardVisible: boolean;
  ctrlArmed: boolean;
  bottomOffset: number;
  onCtrlArmedChange(next: boolean): void;
  onLayout(event: LayoutChangeEvent): void;
}

export function TerminalAccessoryDock({
  terminalRef,
  uploadOwnerKey,
  serverId,
  serverUrl,
  daemonId,
  theme,
  keyboardVisible,
  ctrlArmed,
  bottomOffset,
  onCtrlArmedChange,
  onLayout,
}: TerminalAccessoryDockProps) {
  return (
    <View
      onLayout={onLayout}
      style={[
        styles.inputShell,
        styles.inputShellDock,
        { bottom: bottomOffset },
      ]}
    >
      <TerminalAccessoryBar
        terminalRef={terminalRef}
        uploadOwnerKey={uploadOwnerKey}
        serverId={serverId}
        serverUrl={serverUrl}
        daemonId={daemonId}
        theme={theme}
        keyboardVisible={keyboardVisible}
        ctrlArmed={ctrlArmed}
        onCtrlArmedChange={onCtrlArmedChange}
      />
    </View>
  );
}

const styles = StyleSheet.create({
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
