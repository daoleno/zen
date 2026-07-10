import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  View,
} from "react-native";
import * as Haptics from "expo-haptics";
import { TypeScale, Typography } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { presentAgent } from "../../services/agentPresentation";
import type { Agent } from "../../store/agents";
import { AgentKindIcon } from "./AgentKindIcon";
import { AnimatedPressable } from "../ui/AnimatedPressable";

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
    <AnimatedPressable
      accessibilityRole="button"
      accessibilityLabel={`Open ${presented.title}, ${meta}`}
      accessibilityState={{ selected: active }}
      style={[
        styles.agentRow,
        {
          borderBottomColor: chrome.border,
          backgroundColor: active ? chrome.surfaceActive : "transparent",
        },
      ]}
      preset="press"
      scale={0.99}
      onPress={() => {
        Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
        onOpenAgent(agent.key);
      }}
    >
      <AgentKindIcon
        kind={presented.kind}
        flavor={presented.terminalFlavor}
        size={15}
      />
      <View style={styles.agentRowBody}>
        <View style={styles.agentRowTitleLine}>
          <Text
            style={[styles.agentRowTitle, { color: chrome.text }]}
            numberOfLines={1}
          >
            {presented.title}
          </Text>
          {agent.delegated ? (
            <View style={[styles.brainBadge, { borderColor: chrome.border }]}>
              <Text style={[styles.brainBadgeText, { color: chrome.textMuted }]}>
                Brain
              </Text>
            </View>
          ) : null}
        </View>
        <Text
          style={[styles.agentRowMeta, { color: chrome.textMuted }]}
          numberOfLines={1}
        >
          {meta}
        </Text>
      </View>
      {agent.status === "running" ? (
        <View style={styles.agentRowStatusIndicator}>
          <ActivityIndicator
            size="small"
            color={chrome.accent}
            style={styles.agentRowStatusSpinner}
          />
        </View>
      ) : (
        <View
          style={[
            styles.agentRowStatusDot,
            { backgroundColor: chrome.textMuted },
          ]}
        />
      )}
    </AnimatedPressable>
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
  agentRowBody: {
    flex: 1,
    minWidth: 0,
    gap: 2,
  },
  agentRowTitleLine: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    minWidth: 0,
  },
  agentRowStatusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  agentRowStatusIndicator: {
    width: 14,
    height: 14,
    alignItems: "center",
    justifyContent: "center",
  },
  agentRowStatusSpinner: {
    transform: [{ scale: 0.55 }],
  },
  agentRowTitle: {
    ...TypeScale.compact,
    flexShrink: 1,
    minWidth: 0,
    fontFamily: Typography.uiFontMedium,
  },
  brainBadge: {
    height: 16,
    paddingHorizontal: 5,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 4,
    alignItems: "center",
    justifyContent: "center",
  },
  brainBadgeText: {
    ...TypeScale.micro,
    fontFamily: Typography.uiFontMedium,
    includeFontPadding: false,
  },
  agentRowMeta: {
    ...TypeScale.caption,
  },
});
