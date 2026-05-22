import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import type { LayoutChangeEvent } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography, statusColor } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { AgentKindIcon } from "./AgentKindIcon";
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
      <TouchableOpacity
        style={styles.tabMainButton}
        onPress={() => onOpenTab(tab.id)}
        activeOpacity={0.84}
      >
        <AgentKindIcon kind={tab.kind} size={10} />
        <View style={styles.tabLabelWrapper}>
          <Text
            style={[
              styles.tabLabel,
              { color: tab.active ? chrome.text : chrome.textSubtle },
            ]}
            numberOfLines={1}
          >
            {tab.name}
          </Text>
        </View>
        {tab.pinned ? (
          <Ionicons
            name="bookmark"
            size={10}
            color={tab.active ? chrome.textMuted : chrome.textSubtle}
          />
        ) : null}
        <View
          style={[
            styles.tabStatusDot,
            { backgroundColor: statusColor(tab.status) },
            !tab.active && styles.tabStatusDotInactive,
          ]}
        />
      </TouchableOpacity>

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
  tabMainButton: {
    flex: 1,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
  },
  tabStatusDot: {
    width: 5,
    height: 5,
    borderRadius: 2.5,
    marginLeft: 6,
  },
  tabStatusDotInactive: {
    opacity: 0.45,
  },
  tabLabelWrapper: {
    flex: 1,
    justifyContent: "center",
    marginRight: 4,
    paddingTop: 1,
  },
  tabLabel: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
    includeFontPadding: false,
  },
  tabMenuButton: {
    width: 22,
    height: 22,
    borderRadius: 6,
    alignItems: "center",
    justifyContent: "center",
  },
});
