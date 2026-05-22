import React from "react";
import {
  ScrollView,
  StyleSheet,
  Text,
} from "react-native";
import { Typography } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { AgentDirectorySection } from "../../services/serverSelection";
import { TerminalAgentPickerSection } from "./TerminalAgentPickerSection";

interface TerminalAgentPickerListProps {
  sections: AgentDirectorySection[];
  agentCount: number;
  activeSessionKey: string | null;
  showServerNames: boolean;
  agentAliases: Record<string, string | undefined>;
  chrome: TerminalThemeChrome;
  onOpenAgent(sessionKey: string): void;
}

export function TerminalAgentPickerList({
  sections,
  agentCount,
  activeSessionKey,
  showServerNames,
  agentAliases,
  chrome,
  onOpenAgent,
}: TerminalAgentPickerListProps) {
  return (
    <ScrollView
      style={styles.sheetScroll}
      contentContainerStyle={styles.sheetScrollContent}
      showsVerticalScrollIndicator={false}
    >
      {agentCount === 0 ? (
        <Text style={[styles.sheetEmpty, { color: chrome.textMuted }]}>
          No agents available.
        </Text>
      ) : (
        sections.map((section) => (
          <TerminalAgentPickerSection
            key={section.key}
            section={section}
            activeSessionKey={activeSessionKey}
            showServerNames={showServerNames}
            agentAliases={agentAliases}
            chrome={chrome}
            onOpenAgent={onOpenAgent}
          />
        ))
      )}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  sheetScroll: {
    marginTop: 4,
  },
  sheetScrollContent: {
    paddingBottom: 8,
  },
  sheetEmpty: {
    color: "#7D8CA0",
    fontSize: 13,
    fontFamily: Typography.uiFont,
    paddingVertical: 12,
  },
});
