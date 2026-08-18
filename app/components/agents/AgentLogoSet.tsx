import React from "react";
import { Ionicons } from "@expo/vector-icons";
import { StyleSheet, Text, View } from "react-native";
import { TypeScale, useAppColors } from "../../constants/tokens";
import type { AgentKind } from "../../services/agentPresentation";
import type { ManagedSkillAgent } from "../../services/skillsManagement";
import { skillAgentLabel } from "../../services/skillsManagement";
import { AgentKindIcon } from "../terminal/AgentKindIcon";

export interface AgentLogoSetProps {
  agents: readonly string[];
  size?: number;
  showLabels?: boolean;
  maxVisible?: number;
  accessibilityLabel?: string;
}

const KNOWN_AGENTS = new Set<ManagedSkillAgent>([
  "codex",
  "claude-code",
  "cursor",
  "grok",
  "opencode",
  "pi",
]);

export function AgentLogoSet({
  agents,
  size = 18,
  showLabels = false,
  maxVisible,
  accessibilityLabel,
}: AgentLogoSetProps) {
  const colors = useAppColors();
  const uniqueAgents = [...new Set(agents.map((agent) => agent.trim()))].filter(
    Boolean,
  );
  const labels = uniqueAgents.map(agentLabel);
  const visibleAgents =
    maxVisible === undefined ? uniqueAgents : uniqueAgents.slice(0, maxVisible);
  const label =
    accessibilityLabel ||
    (labels.length
      ? `Available to ${labels.join(", ")}`
      : "Available to an unknown Agent");

  if (uniqueAgents.length === 0) {
    return (
      <View
        accessible
        accessibilityRole="image"
        accessibilityLabel={label}
        style={styles.row}
      >
        <UnknownAgent size={size} label="Unknown" />
      </View>
    );
  }

  return (
    <View
      accessible
      accessibilityRole="image"
      accessibilityLabel={label}
      style={styles.row}
    >
      {visibleAgents.map((agent) =>
        isManagedSkillAgent(agent) ? (
          <View key={agent} accessible={false} style={styles.item}>
            <AgentKindIcon kind={agentKind(agent)} size={size} />
            {showLabels ? (
              <Text style={[styles.label, { color: colors.textSecondary }]}>
                {skillAgentLabel(agent)}
              </Text>
            ) : null}
          </View>
        ) : (
          <UnknownAgent
            key={agent}
            size={size}
            label={agent}
            showLabel={showLabels}
          />
        ),
      )}
    </View>
  );
}

function UnknownAgent({
  size,
  label,
  showLabel = true,
}: {
  size: number;
  label: string;
  showLabel?: boolean;
}) {
  const colors = useAppColors();
  return (
    <View accessible={false} style={styles.item}>
      <Ionicons
        name="help-circle-outline"
        size={size}
        color={colors.textTertiary}
      />
      {showLabel ? (
        <Text style={[styles.label, { color: colors.textTertiary }]}>
          {label}
        </Text>
      ) : null}
    </View>
  );
}

function isManagedSkillAgent(agent: string): agent is ManagedSkillAgent {
  return KNOWN_AGENTS.has(agent as ManagedSkillAgent);
}

function agentLabel(agent: string): string {
  return isManagedSkillAgent(agent) ? skillAgentLabel(agent) : agent;
}

export function agentKind(agent: ManagedSkillAgent): AgentKind {
  return agent === "claude-code" ? "claude" : agent;
}

const styles = StyleSheet.create({
  row: {
    minHeight: 28,
    flexDirection: "row",
    flexWrap: "wrap",
    alignItems: "center",
    gap: 8,
  },
  item: {
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
  },
  label: TypeScale.compact,
});
