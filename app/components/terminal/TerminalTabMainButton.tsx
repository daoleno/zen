import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography, statusColor } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { AgentKindIcon } from "./AgentKindIcon";
import type { TerminalTabDescriptor } from "./TerminalTopBar";

interface TerminalTabMainButtonProps {
  tab: TerminalTabDescriptor;
  chrome: TerminalThemeChrome;
  onOpenTab(id: string): void;
}

export function TerminalTabMainButton({
  tab,
  chrome,
  onOpenTab,
}: TerminalTabMainButtonProps) {
  return (
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
  );
}

const styles = StyleSheet.create({
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
});
