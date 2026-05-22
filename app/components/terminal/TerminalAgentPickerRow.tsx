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
import type { Agent } from "../../store/agents";
import { AgentKindIcon } from "./AgentKindIcon";

interface TerminalAgentPickerRowProps {
  agent: Agent;
  alias?: string;
  active: boolean;
  showServerName: boolean;
  chrome: TerminalThemeChrome;
  onOpenAgent(sessionKey: string): void;
}

export function TerminalAgentPickerRow({
  agent,
  alias,
  active,
  showServerName,
  chrome,
  onOpenAgent,
}: TerminalAgentPickerRowProps) {
  const presented = presentAgent(agent, alias);
  const meta = [presented.typeLabel, showServerName ? agent.serverName : null]
    .filter(Boolean)
    .join(" · ");

  return (
    <TouchableOpacity
      style={[
        styles.agentRow,
        { borderBottomColor: chrome.border },
        active && styles.agentRowActive,
      ]}
      onPress={() => onOpenAgent(agent.key)}
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
          { backgroundColor: statusColor(agent.status) },
        ]}
      />
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
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
