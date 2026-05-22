import React from "react";
import {
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Typography } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { AgentDirectorySection } from "../../services/serverSelection";
import { TerminalAgentPickerRow } from "./TerminalAgentPickerRow";

interface TerminalAgentPickerSectionProps {
  section: AgentDirectorySection;
  activeSessionKey: string | null;
  showServerNames: boolean;
  agentAliases: Record<string, string | undefined>;
  chrome: TerminalThemeChrome;
  onOpenAgent(sessionKey: string): void;
}

export function TerminalAgentPickerSection({
  section,
  activeSessionKey,
  showServerNames,
  agentAliases,
  chrome,
  onOpenAgent,
}: TerminalAgentPickerSectionProps) {
  return (
    <View style={styles.section}>
      <View style={styles.sectionHeader}>
        <View style={styles.sectionBody}>
          <Text
            style={[styles.sectionTitle, { color: chrome.text }]}
            numberOfLines={1}
          >
            {section.title}
          </Text>
          <Text
            style={[styles.sectionSubtitle, { color: chrome.textMuted }]}
            numberOfLines={1}
          >
            {section.subtitle}
          </Text>
        </View>
        <Text
          style={[
            styles.sectionCount,
            {
              color: chrome.textMuted,
              backgroundColor: chrome.surfaceMuted,
            },
          ]}
        >
          {section.data.length}
        </Text>
      </View>

      {section.data.map((item) => (
        <TerminalAgentPickerRow
          key={item.key}
          agent={item}
          alias={agentAliases[item.key]}
          active={item.key === activeSessionKey}
          showServerName={showServerNames}
          chrome={chrome}
          onOpenAgent={onOpenAgent}
        />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  section: {
    paddingTop: 18,
  },
  sectionHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingBottom: 10,
  },
  sectionBody: {
    flex: 1,
    minWidth: 0,
  },
  sectionTitle: {
    fontSize: 15,
    lineHeight: 20,
    fontFamily: Typography.uiFontMedium,
  },
  sectionSubtitle: {
    marginTop: 2,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
    opacity: 0.55,
  },
  sectionCount: {
    minWidth: 24,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 999,
    overflow: "hidden",
    textAlign: "center",
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFontMedium,
  },
});
