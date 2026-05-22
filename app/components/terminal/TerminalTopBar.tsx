import React from "react";
import {
  ScrollView,
  StyleSheet,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { AgentStatus } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TerminalTabPill } from "./TerminalTabPill";

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
      <TouchableOpacity
        onPress={onBack}
        style={styles.chromeButton}
        activeOpacity={0.75}
      >
        <Ionicons name="chevron-back" size={20} color={chrome.textMuted} />
      </TouchableOpacity>

      <ScrollView
        ref={tabScrollRef}
        horizontal
        showsHorizontalScrollIndicator={false}
        style={styles.tabScroller}
        contentContainerStyle={styles.tabScrollerContent}
      >
        {tabs.map((tab) => (
          <TerminalTabPill
            key={tab.id}
            tab={tab}
            chrome={chrome}
            menuAnchorRef={menuAnchorRef}
            onOpenTab={onOpenTab}
            onOpenMenu={onOpenMenu}
            onLayout={(event) => {
              const { x, width } = event.nativeEvent.layout;
              onTabLayout(tab.id, { x, width });
            }}
          />
        ))}
      </ScrollView>

      <TouchableOpacity
        onPress={onNewTerminal}
        style={styles.chromeButton}
        activeOpacity={0.75}
      >
        <Ionicons name="add" size={20} color={chrome.textMuted} />
      </TouchableOpacity>
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
  chromeButton: {
    width: 32,
    height: 32,
    alignItems: "center",
    justifyContent: "center",
  },
  tabScroller: {
    flex: 1,
    marginHorizontal: 4,
  },
  tabScrollerContent: {
    paddingRight: 2,
  },
});
