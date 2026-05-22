import React from "react";
import {
  StyleSheet,
  TouchableOpacity,
  View,
} from "react-native";
import type { LayoutChangeEvent } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TerminalTabMainButton } from "./TerminalTabMainButton";
import type { TerminalTabDescriptor } from "./TerminalTopBar";

interface TerminalTabPillProps {
  tab: TerminalTabDescriptor;
  chrome: TerminalThemeChrome;
  menuAnchorRef: React.RefObject<View | null>;
  onOpenTab(id: string): void;
  onOpenMenu(): void;
  onLayout(event: LayoutChangeEvent): void;
}

export function TerminalTabPill({
  tab,
  chrome,
  menuAnchorRef,
  onOpenTab,
  onOpenMenu,
  onLayout,
}: TerminalTabPillProps) {
  return (
    <View
      style={[
        styles.tabPill,
        tab.active && [
          styles.tabPillActive,
          { backgroundColor: chrome.surfaceMuted },
        ],
      ]}
      onLayout={onLayout}
    >
      <TerminalTabMainButton
        tab={tab}
        chrome={chrome}
        onOpenTab={onOpenTab}
      />

      {tab.active ? (
        <View ref={menuAnchorRef} collapsable={false}>
          <TouchableOpacity
            style={styles.tabMenuButton}
            onPress={onOpenMenu}
            activeOpacity={0.75}
          >
            <Ionicons
              name="ellipsis-vertical"
              size={15}
              color={chrome.textMuted}
            />
          </TouchableOpacity>
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  tabPill: {
    minWidth: 110,
    maxWidth: 200,
    height: 30,
    borderRadius: 8,
    paddingLeft: 8,
    paddingRight: 4,
    marginRight: 2,
    flexDirection: "row",
    alignItems: "center",
  },
  tabPillActive: {
    borderRadius: 8,
  },
  tabMenuButton: {
    width: 22,
    height: 22,
    borderRadius: 6,
    alignItems: "center",
    justifyContent: "center",
  },
});
