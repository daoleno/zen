import React from "react";
import {
  ScrollView,
  StyleSheet,
  View,
} from "react-native";
import type { AgentStatus } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TerminalTopBarChromeButton } from "./TerminalTopBarChromeButton";
import { TerminalTabScroller } from "./TerminalTabScroller";

export interface TerminalTabDescriptor {
  id: string;
  name: string;
  status: AgentStatus;
  kind: "terminal" | "claude" | "codex";
  pinned: boolean;
  active: boolean;
}

export interface TerminalTopBarProps {
  tabs: TerminalTabDescriptor[];
  backgroundColor: string;
  chrome: TerminalThemeChrome;
  tabScrollRef: React.RefObject<ScrollView | null>;
  menuAnchorRef: React.RefObject<View | null>;
  onBack(): void;
  onOpenTab(id: string): void;
  onOpenMenu(): void;
  onNewTerminal(): void;
  onTabLayout(id: string, layout: { x: number; width: number }): void;
}

export function TerminalTopBar({
  tabs,
  backgroundColor,
  chrome,
  tabScrollRef,
  menuAnchorRef,
  onBack,
  onOpenTab,
  onOpenMenu,
  onNewTerminal,
  onTabLayout,
}: TerminalTopBarProps) {
  return (
    <View
      style={[
        styles.topBar,
        { backgroundColor },
      ]}
    >
      <TerminalTopBarChromeButton
        accessibilityLabel="Back"
        chrome={chrome}
        icon="chevron-back"
        onPress={onBack}
      />

      <TerminalTabScroller
        tabs={tabs}
        chrome={chrome}
        tabScrollRef={tabScrollRef}
        menuAnchorRef={menuAnchorRef}
        onOpenTab={onOpenTab}
        onOpenMenu={onOpenMenu}
        onTabLayout={onTabLayout}
      />

      <TerminalTopBarChromeButton
        accessibilityLabel="New terminal"
        chrome={chrome}
        icon="add"
        onPress={onNewTerminal}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  topBar: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 8,
    paddingTop: 2,
    paddingBottom: 4,
  },
});
