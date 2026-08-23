import React, { useCallback, useMemo } from "react";
import {
  ActivityIndicator,
  Modal,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useReducedMotion } from "react-native-reanimated";
import { SafeAreaView } from "react-native-safe-area-context";
import {
  isAgentSessionListFreshForConnection,
  useAgents,
  type Agent,
} from "../../store/agents";
import { useBrain } from "../../store/brain";
import { useCurrentServer } from "../../store/currentServer";
import type { StoredAgentAliases } from "../../services/storage";
import { presentAgent } from "../../services/agentPresentation";
import { agentStatusLabel } from "../../services/agentStatusPresentation";
import { TypeScale, UiTextMetrics, useAppTheme } from "../../constants/tokens";
import type { ResolvedZenTheme } from "../../theme";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import {
  buildWorkActivityListModel,
  type WorkActivityRow,
} from "./workActivityListModel";
import { resolveWorkObservatoryMotion } from "./workSignalObservatoryInteraction";

type WorkSignalObservatoryProps = {
  visible: boolean;
  aliases: StoredAgentAliases;
  onClose(): void;
  onOpenSession(agent: Agent): void;
  onOpenBrain(): void;
};

export function WorkSignalObservatory({
  visible,
  aliases,
  onClose,
  onOpenSession,
  onOpenBrain,
}: WorkSignalObservatoryProps) {
  const { theme } = useAppTheme();
  const reducedMotion = useReducedMotion();
  const motion = resolveWorkObservatoryMotion(reducedMotion);
  const styles = useMemo(() => createStyles(theme), [theme]);
  const { state: agentState } = useAgents();
  const { state: brainState } = useBrain();
  const { currentServer, currentServerId, hydrated } = useCurrentServer();
  const serverId = currentServerId;
  const brain = serverId ? brainState.byServer[serverId] : undefined;
  const currentAgents = useMemo(
    () => agentState.agents.filter((agent) => agent.serverId === serverId),
    [agentState.agents, serverId],
  );
  const agentById = useMemo(
    () => new Map(currentAgents.map((agent) => [agent.id, agent] as const)),
    [currentAgents],
  );
  const owners = useMemo(
    () =>
      currentAgents.map((agent) => ({
        sessionId: agent.id,
        title: presentAgent(agent, aliases[agent.key]).title,
        status: agent.status,
        delegated: agent.delegated === true,
      })),
    [aliases, currentAgents],
  );
  const model = useMemo(
    () =>
      buildWorkActivityListModel({
        work: brain?.current_work ?? [],
        owners,
        historicalResultCount: brain?.work_backlog?.historical_results ?? 0,
      }),
    [brain?.current_work, brain?.work_backlog?.historical_results, owners],
  );
  const connectionState = serverId
    ? agentState.serverConnections[serverId] ?? "offline"
    : "offline";
  const ready = Boolean(
    hydrated &&
      currentServer &&
      brain?.hydrated &&
      serverId &&
      isAgentSessionListFreshForConnection(agentState, serverId),
  );
  const activateRow = useCallback(
    (row: WorkActivityRow) => {
      if (row.action === "open_session" && row.owner) {
        const agent = agentById.get(row.owner.sessionId);
        if (agent) {
          onClose();
          onOpenSession(agent);
        }
        return;
      }
      if (row.action === "open_brain") {
        onClose();
        onOpenBrain();
      }
    },
    [agentById, onClose, onOpenBrain, onOpenSession],
  );

  return (
    <Modal
      visible={visible}
      animationType={motion.modalAnimationType}
      presentationStyle="fullScreen"
      onRequestClose={onClose}
    >
      <SafeAreaView style={styles.root} edges={["top", "bottom"]}>
        <View style={styles.topBar}>
          <View style={styles.titleBlock}>
            <Text style={styles.title}>Work</Text>
            <Text style={styles.subtitle} numberOfLines={1}>
              {currentServer?.name || "Current server"}
            </Text>
          </View>
          <AnimatedPressable
            style={styles.closeButton}
            preset="press"
            scale={0.94}
            onPress={onClose}
            accessibilityRole="button"
            accessibilityLabel="Close Work"
            hitSlop={8}
          >
            <Ionicons name="close" size={21} color={theme.colors.textPrimary} />
          </AnimatedPressable>
        </View>

        {!ready ? (
          <View style={styles.state}>
            {connectionState === "connected" ? (
              <ActivityIndicator color={theme.colors.accent} />
            ) : (
              <Ionicons
                name="cloud-offline-outline"
                size={22}
                color={theme.colors.textTertiary}
              />
            )}
            <Text style={styles.stateText}>
              {connectionState === "connected"
                ? "Updating Work"
                : "Work unavailable"}
            </Text>
          </View>
        ) : (
          <ScrollView
            style={styles.scroll}
            contentContainerStyle={styles.content}
            showsVerticalScrollIndicator={false}
          >
            <WorkSummary model={model} styles={styles} />
            <WorkSection
              title="Needs you"
              rows={model.attention}
              styles={styles}
              theme={theme}
              onActivate={activateRow}
            />
            <WorkSection
              title="Active"
              rows={model.active}
              styles={styles}
              theme={theme}
              onActivate={activateRow}
            />
            <WorkSection
              title="Recent"
              rows={model.recent}
              styles={styles}
              theme={theme}
              onActivate={activateRow}
            />
            {model.historicalResultCount > 0 ? (
              <View
                accessible
                accessibilityRole="summary"
                accessibilityLabel={`${model.historicalResultCount} historical Work results`}
                style={styles.historyRow}
              >
                <Ionicons
                  name="archive-outline"
                  size={18}
                  color={theme.colors.textTertiary}
                />
                <Text style={styles.historyText}>History</Text>
                <Text style={styles.historyCount}>
                  {model.historicalResultCount}
                </Text>
              </View>
            ) : null}
            {model.totalVisible === 0 && model.historicalResultCount === 0 ? (
              <View style={styles.state}>
                <Ionicons
                  name="checkmark-circle-outline"
                  size={22}
                  color={theme.colors.statusDone}
                />
                <Text style={styles.stateText}>No Work in progress</Text>
              </View>
            ) : null}
          </ScrollView>
        )}
      </SafeAreaView>
    </Modal>
  );
}

function WorkSummary({
  model,
  styles,
}: {
  model: ReturnType<typeof buildWorkActivityListModel>;
  styles: ReturnType<typeof createStyles>;
}) {
  return (
    <View
      accessible
      accessibilityRole="summary"
      accessibilityLabel={model.accessibilityLabel}
      style={styles.summary}
    >
      <Text style={styles.summaryCount}>{model.attention.length}</Text>
      <Text style={styles.summaryLabel}>Needs you</Text>
      <View style={styles.summaryDivider} />
      <Text style={styles.summaryCount}>{model.active.length}</Text>
      <Text style={styles.summaryLabel}>Active</Text>
      <View style={styles.summaryDivider} />
      <Text style={styles.summaryCount}>
        {model.recent.length + model.historicalResultCount}
      </Text>
      <Text style={styles.summaryLabel}>History</Text>
    </View>
  );
}

function WorkSection({
  title,
  rows,
  styles,
  theme,
  onActivate,
}: {
  title: string;
  rows: WorkActivityRow[];
  styles: ReturnType<typeof createStyles>;
  theme: ResolvedZenTheme;
  onActivate(row: WorkActivityRow): void;
}) {
  if (rows.length === 0) return null;
  return (
    <View style={styles.section}>
      <Text style={styles.sectionTitle}>{title}</Text>
      {rows.map((row, index) => (
        <WorkActivityRowView
          key={row.id}
          row={row}
          first={index === 0}
          styles={styles}
          theme={theme}
          onPress={row.action === "none" ? undefined : () => onActivate(row)}
        />
      ))}
    </View>
  );
}

function WorkActivityRowView({
  row,
  first,
  styles,
  theme,
  onPress,
}: {
  row: WorkActivityRow;
  first: boolean;
  styles: ReturnType<typeof createStyles>;
  theme: ResolvedZenTheme;
  onPress?: () => void;
}) {
  const statusColor = workToneColor(row, theme);
  const label = [
    row.statusLabel,
    `Work ${row.title}`,
    row.owner
      ? `Delegated agent ${row.owner.title}, ${agentStatusLabel(row.owner.status)}`
      : undefined,
    row.unread ? "Unread result" : undefined,
  ]
    .filter(Boolean)
    .join(". ");
  const rowStyle = [
    styles.row,
    !first ? styles.rowBorder : null,
    row.lifecycle === "needs_you" ? styles.rowAttention : null,
  ];
  const content = (
    <>
      <Ionicons
        name={workIcon(row)}
        size={19}
        color={statusColor}
        style={styles.rowIcon}
      />
      <View style={styles.rowBody}>
        <View style={styles.rowTitleLine}>
          <Text style={styles.rowTitle} numberOfLines={2}>
            {row.title}
          </Text>
          <Text style={[styles.rowStatus, { color: statusColor }]}>
            {row.statusLabel}
          </Text>
        </View>
        {row.owner ? (
          <View style={styles.ownerLine}>
            <Ionicons
              name="terminal-outline"
              size={13}
              color={theme.colors.textTertiary}
            />
            <Text style={styles.ownerText} numberOfLines={1}>
              {row.owner.title} · {agentStatusLabel(row.owner.status)}
            </Text>
          </View>
        ) : null}
      </View>
      {onPress ? (
        <Ionicons
          name="chevron-forward"
          size={16}
          color={theme.colors.textTertiary}
        />
      ) : null}
    </>
  );
  if (!onPress) {
    return (
      <View accessible accessibilityLabel={label} style={rowStyle}>
        {content}
      </View>
    );
  }
  return (
    <AnimatedPressable
      style={rowStyle}
      preset="press"
      scale={0.99}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={label}
    >
      {content}
    </AnimatedPressable>
  );
}

function workToneColor(row: WorkActivityRow, theme: ResolvedZenTheme): string {
  switch (row.tone) {
    case "attention":
      return theme.colors.warning;
    case "accent":
      return theme.colors.accent;
    case "danger":
      return theme.colors.statusFailed;
    case "neutral":
      return row.terminal
        ? theme.colors.statusDone
        : theme.colors.textSecondary;
  }
}

function workIcon(row: WorkActivityRow) {
  switch (row.lifecycle) {
    case "needs_you":
      return "help-circle-outline" as const;
    case "reviewing":
      return "eye-outline" as const;
    case "ready":
      return "checkmark-circle-outline" as const;
    case "working":
      return "ellipsis-horizontal-circle-outline" as const;
    case "waiting":
      return "time-outline" as const;
    case "done":
      return "checkmark-circle-outline" as const;
    case "cancelled":
      return "close-circle-outline" as const;
  }
}

function createStyles(theme: ResolvedZenTheme) {
  const colors = theme.colors;
  return StyleSheet.create({
    root: { flex: 1, backgroundColor: colors.bgPrimary },
    topBar: {
      minHeight: 64,
      paddingHorizontal: 16,
      flexDirection: "row",
      alignItems: "center",
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    titleBlock: { flex: 1, minWidth: 0 },
    title: {
      ...UiTextMetrics,
      ...TypeScale.title,
      color: colors.textPrimary,
    },
    subtitle: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textTertiary,
    },
    closeButton: {
      width: 44,
      height: 44,
      alignItems: "center",
      justifyContent: "center",
    },
    scroll: { flex: 1 },
    content: {
      width: "100%",
      maxWidth: 720,
      alignSelf: "center",
      paddingHorizontal: 16,
      paddingTop: 14,
      paddingBottom: 32,
    },
    summary: {
      minHeight: 42,
      flexDirection: "row",
      alignItems: "center",
      gap: 7,
      paddingBottom: 12,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    summaryCount: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textPrimary,
    },
    summaryLabel: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textSecondary,
    },
    summaryDivider: {
      width: 1,
      height: 14,
      marginHorizontal: 3,
      backgroundColor: colors.borderSubtle,
    },
    section: { paddingTop: 18 },
    sectionTitle: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textSecondary,
      marginBottom: 5,
    },
    row: {
      minHeight: 68,
      flexDirection: "row",
      alignItems: "center",
      gap: 10,
      paddingVertical: 10,
      paddingHorizontal: 4,
    },
    rowBorder: {
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    rowAttention: {
      backgroundColor: colors.warningSoft,
      paddingHorizontal: 8,
      borderRadius: 8,
    },
    rowIcon: { alignSelf: "flex-start", marginTop: 2 },
    rowBody: { flex: 1, minWidth: 0 },
    rowTitleLine: {
      flexDirection: "row",
      alignItems: "flex-start",
      gap: 8,
    },
    rowTitle: {
      ...UiTextMetrics,
      ...TypeScale.body,
      flex: 1,
      minWidth: 0,
      color: colors.textPrimary,
    },
    rowStatus: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      flexShrink: 0,
      letterSpacing: 0,
    },
    ownerLine: {
      flexDirection: "row",
      alignItems: "center",
      gap: 5,
      marginTop: 3,
    },
    ownerText: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      flex: 1,
      minWidth: 0,
      color: colors.textTertiary,
    },
    historyRow: {
      minHeight: 52,
      marginTop: 18,
      flexDirection: "row",
      alignItems: "center",
      gap: 9,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    historyText: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      flex: 1,
      color: colors.textSecondary,
    },
    historyCount: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: colors.textTertiary,
    },
    state: {
      flex: 1,
      minHeight: 220,
      alignItems: "center",
      justifyContent: "center",
      gap: 10,
    },
    stateText: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textTertiary,
    },
  });
}
