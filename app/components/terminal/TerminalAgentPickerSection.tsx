import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Typography, statusColor } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { presentAgent } from "../../services/agentPresentation";
import type { AgentDirectorySection } from "../../services/serverSelection";
import { AgentKindIcon } from "./AgentKindIcon";

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

      {section.data.map((item) => {
        const isActive = item.key === activeSessionKey;
        const presented = presentAgent(item, agentAliases[item.key]);
        const meta = [
          presented.typeLabel,
          showServerNames ? item.serverName : null,
        ]
          .filter(Boolean)
          .join(" · ");

        return (
          <TouchableOpacity
            key={item.key}
            style={[
              styles.agentRow,
              { borderBottomColor: chrome.border },
              isActive && styles.agentRowActive,
            ]}
            onPress={() => onOpenAgent(item.key)}
            activeOpacity={0.84}
          >
            <AgentKindIcon kind={presented.kind} size={15} />
            <View style={styles.agentRowBody}>
              <Text
                style={[styles.agentRowTitle, { color: chrome.text }]}
                numberOfLines={1}
              >
                {presented.title}
              </Text>
              <Text
                style={[styles.agentRowMeta, { color: chrome.textMuted }]}
                numberOfLines={1}
              >
                {meta}
              </Text>
            </View>
            <View
              style={[
                styles.agentRowStatusDot,
                { backgroundColor: statusColor(item.status) },
              ]}
            />
          </TouchableOpacity>
        );
      })}
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
  agentRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingVertical: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  agentRowActive: {
    opacity: 1,
  },
  agentRowBody: {
    flex: 1,
    minWidth: 0,
    gap: 2,
  },
  agentRowStatusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  agentRowTitle: {
    flex: 1,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  agentRowMeta: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
    opacity: 0.55,
  },
});
