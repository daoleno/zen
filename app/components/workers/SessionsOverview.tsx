import React, { memo } from "react";
import { Pressable, StyleSheet, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import {
  ContinuousCorners,
  Radii,
  useAppTheme,
  type AppColors,
} from "../../constants/tokens";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type {
  WorkActivityListModel,
  WorkActivityRow,
  WorkActivityTone,
} from "../work/workActivityListModel";
import { AppText } from "../ui/AppText";
import { GlassSurface } from "../ui/GlassSurface";
import { InlineNotice } from "../ui/InlineNotice";
import { StatusPill, type StatusTone } from "../ui/StatusPill";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

export type SessionFilter = "all" | "running" | "attention";

export interface SessionCounts {
  all: number;
  running: number;
  attention: number;
}

interface SessionsOverviewProps {
  serverName: string | null;
  connection: "connected" | "connecting" | "offline";
  issue: ConnectionIssue | null;
  counts: SessionCounts;
  filter: SessionFilter;
  onChangeFilter(filter: SessionFilter): void;
  work: WorkActivityListModel;
  onActivateWork(row: WorkActivityRow): void;
  onOpenWorkActivity(): void;
  canCreate: boolean;
  creating: boolean;
  onCreate(): void;
  onOpenServices(): void;
  onRetry(): void;
  /** Filters only make sense once there is a list to narrow. */
  showFilters: boolean;
}

const WORK_PREVIEW_LIMIT = 3;

/**
 * Sessions page header: where you are (server + connection), what needs you
 * (Work), the next actions, and a filter over the list below.
 */
function SessionsOverviewComponent({
  serverName,
  connection,
  issue,
  counts,
  filter,
  onChangeFilter,
  work,
  onActivateWork,
  onOpenWorkActivity,
  canCreate,
  creating,
  onCreate,
  onOpenServices,
  onRetry,
  showFilters,
}: SessionsOverviewProps) {
  const { colors, theme } = useAppTheme();
  const connected = connection === "connected";
  const statusTone: StatusTone = issue
    ? "danger"
    : connected
      ? "success"
      : connection === "connecting"
        ? "warning"
        : "neutral";
  const statusLabel =
    issue?.title ??
    (connected ? "Connected" : connection === "connecting" ? "Connecting" : "Offline");
  const workRows = [...work.attention, ...work.active];
  const summary = sessionSummary(counts);

  return (
    <View style={styles.root}>
      <View style={styles.titleBlock}>
        <AppText variant="display" accessibilityRole="header">
          Sessions
        </AppText>
        <View style={styles.statusLine}>
          <StatusPill label={statusLabel} tone={statusTone} live={connection === "connecting"} />
          {serverName ? (
            <AppText variant="caption" tone="tertiary" numberOfLines={1} style={styles.serverName}>
              {serverName}
            </AppText>
          ) : null}
        </View>
        {summary ? (
          <AppText variant="compact" tone="secondary" numberOfLines={1}>
            {summary}
          </AppText>
        ) : null}
      </View>

      {serverName && !connected && connection !== "connecting" ? (
        <InlineNotice
          tone={issue ? "danger" : "warning"}
          title={issue?.title ?? "Server offline"}
          detail={issue?.detail ?? "Sessions below may be out of date."}
          action={{ label: "Retry", onPress: onRetry }}
        />
      ) : null}

      <View style={styles.actions}>
        <QuickAction
          icon={creating ? "hourglass-outline" : "add"}
          label={creating ? "Starting" : "New session"}
          prominent
          disabled={!canCreate || creating}
          onPress={onCreate}
          colors={colors}
          tint={theme.materials.tint}
        />
        <QuickAction
          icon="globe-outline"
          label="Services"
          disabled={!connected}
          onPress={onOpenServices}
          colors={colors}
          tint={theme.materials.tint}
        />
        <QuickAction
          icon="pulse-outline"
          label="Work"
          badge={work.attention.length > 0 ? work.attention.length : null}
          onPress={onOpenWorkActivity}
          colors={colors}
          tint={theme.materials.tint}
        />
      </View>

      {workRows.length > 0 ? (
        <GlassSurface material="thin" radius={Radii.card} elevation="card" style={styles.workCard}>
          <View style={styles.workHeader}>
            <AppText variant="label" tone="tertiary" accessibilityRole="header">
              Work in progress
            </AppText>
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={work.accessibilityLabel}
              accessibilityHint="Opens all Work activity"
              hitSlop={10}
              onPress={onOpenWorkActivity}
            >
              <AppText variant="label" tone="accent">
                See all
              </AppText>
            </Pressable>
          </View>
          {workRows.slice(0, WORK_PREVIEW_LIMIT).map((row, index) => (
            <WorkPreviewRow
              key={row.id}
              row={row}
              first={index === 0}
              colors={colors}
              separator={theme.materials.separator}
              onPress={row.action === "none" ? undefined : () => onActivateWork(row)}
            />
          ))}
          {workRows.length > WORK_PREVIEW_LIMIT ? (
            <AppText variant="caption" tone="tertiary" style={styles.workMore}>
              {`+${workRows.length - WORK_PREVIEW_LIMIT} more`}
            </AppText>
          ) : null}
        </GlassSurface>
      ) : null}

      {showFilters ? (
        <View style={styles.filters} accessibilityRole="tablist">
          <FilterChip label="All" count={counts.all} selected={filter === "all"} onPress={() => onChangeFilter("all")} colors={colors} />
          <FilterChip label="Running" count={counts.running} selected={filter === "running"} onPress={() => onChangeFilter("running")} colors={colors} />
          <FilterChip label="Needs you" count={counts.attention} selected={filter === "attention"} onPress={() => onChangeFilter("attention")} colors={colors} attention={counts.attention > 0} />
        </View>
      ) : null}
    </View>
  );
}

export const SessionsOverview = memo(SessionsOverviewComponent);

function sessionSummary(counts: SessionCounts): string | null {
  if (counts.all === 0) return null;
  const parts = [`${counts.all} ${counts.all === 1 ? "session" : "sessions"}`];
  if (counts.running > 0) parts.push(`${counts.running} running`);
  if (counts.attention > 0) parts.push(`${counts.attention} need you`);
  return parts.join(" · ");
}

function QuickAction({
  icon,
  label,
  prominent = false,
  disabled = false,
  badge = null,
  onPress,
  colors,
  tint,
}: {
  icon: IoniconName;
  label: string;
  prominent?: boolean;
  disabled?: boolean;
  badge?: number | null;
  onPress(): void;
  colors: AppColors;
  tint: string;
}) {
  const fill = disabled ? colors.disabledSurface : prominent ? colors.accent : colors.bgSurface;
  const ink = disabled ? colors.disabledText : prominent ? colors.textOnAccent : colors.accentStrong;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={badge ? `${label}, ${badge} need you` : label}
      accessibilityState={{ disabled }}
      disabled={disabled}
      android_ripple={{ color: colors.surfacePressed }}
      onPress={() => {
        void Haptics.selectionAsync();
        onPress();
      }}
      style={({ pressed }) => [
        styles.action,
        { backgroundColor: fill },
        pressed ? styles.actionPressed : null,
      ]}
    >
      <View style={[styles.actionGlyph, { backgroundColor: prominent && !disabled ? "rgba(255,255,255,0.18)" : tint }]}>
        <Ionicons name={icon} size={19} color={ink} />
      </View>
      <AppText variant="label" numberOfLines={1} style={{ color: prominent && !disabled ? colors.textOnAccent : colors.textPrimary }}>
        {label}
      </AppText>
      {badge ? (
        <View style={[styles.badge, { backgroundColor: colors.statusBlocked }]}>
          <AppText variant="micro" style={{ color: colors.textOnAccent }}>
            {badge}
          </AppText>
        </View>
      ) : null}
    </Pressable>
  );
}

function workToneColor(tone: WorkActivityTone, colors: AppColors): string {
  switch (tone) {
    case "attention":
      return colors.statusBlocked;
    case "accent":
      return colors.accentStrong;
    default:
      return colors.textTertiary;
  }
}

function WorkPreviewRow({
  row,
  first,
  colors,
  separator,
  onPress,
}: {
  row: WorkActivityRow;
  first: boolean;
  colors: AppColors;
  separator: string;
  onPress?: () => void;
}) {
  const toneColor = workToneColor(row.tone, colors);
  const detail = row.owner ? `${row.statusLabel} · ${row.owner.title}` : row.statusLabel;
  return (
    <Pressable
      accessibilityRole={onPress ? "button" : undefined}
      accessibilityLabel={`${row.title}, ${detail}`}
      disabled={!onPress}
      onPress={onPress}
      android_ripple={onPress ? { color: colors.surfacePressed } : undefined}
      style={({ pressed }) => [
        styles.workRow,
        !first && { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: separator },
        pressed ? { backgroundColor: colors.surfacePressed } : null,
      ]}
    >
      <View style={[styles.workDot, { backgroundColor: toneColor }]} />
      <View style={styles.workCopy}>
        <AppText variant="body" numberOfLines={1}>
          {row.title}
        </AppText>
        <AppText variant="caption" numberOfLines={1} style={{ color: row.tone === "attention" ? toneColor : colors.textTertiary }}>
          {detail}
        </AppText>
      </View>
      {onPress ? <Ionicons name="chevron-forward" size={16} color={colors.textTertiary} /> : null}
    </Pressable>
  );
}

function FilterChip({
  label,
  count,
  selected,
  attention = false,
  onPress,
  colors,
}: {
  label: string;
  count: number;
  selected: boolean;
  attention?: boolean;
  onPress(): void;
  colors: AppColors;
}) {
  return (
    <Pressable
      accessibilityRole="tab"
      accessibilityLabel={`${label}, ${count}`}
      accessibilityState={{ selected }}
      hitSlop={{ top: 6, bottom: 6 }}
      onPress={() => {
        if (selected) return;
        void Haptics.selectionAsync();
        onPress();
      }}
      style={[
        styles.chip,
        {
          backgroundColor: selected ? colors.textPrimary : colors.surfaceSubtle,
        },
      ]}
    >
      <AppText variant="label" style={{ color: selected ? colors.bgPrimary : colors.textSecondary }}>
        {label}
      </AppText>
      <AppText
        variant="micro"
        style={{
          color: selected ? colors.bgPrimary : attention ? colors.statusBlocked : colors.textTertiary,
        }}
      >
        {count}
      </AppText>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  root: {
    paddingHorizontal: 16,
    paddingTop: 8,
    paddingBottom: 6,
    gap: 16,
  },
  titleBlock: {
    gap: 6,
  },
  statusLine: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  serverName: {
    flexShrink: 1,
  },
  actions: {
    flexDirection: "row",
    gap: 10,
  },
  action: {
    flex: 1,
    minHeight: 76,
    borderRadius: Radii.lg,
    ...ContinuousCorners,
    paddingHorizontal: 12,
    paddingVertical: 11,
    justifyContent: "space-between",
    gap: 8,
    overflow: "hidden",
  },
  actionPressed: {
    opacity: 0.85,
    transform: [{ scale: 0.98 }],
  },
  actionGlyph: {
    width: 30,
    height: 30,
    borderRadius: 15,
    alignItems: "center",
    justifyContent: "center",
  },
  badge: {
    position: "absolute",
    top: 10,
    right: 10,
    minWidth: 20,
    height: 20,
    borderRadius: 10,
    paddingHorizontal: 6,
    alignItems: "center",
    justifyContent: "center",
  },
  workCard: {
    overflow: "hidden",
  },
  workHeader: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 16,
    paddingTop: 12,
    paddingBottom: 4,
  },
  workRow: {
    minHeight: 56,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 9,
  },
  workDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  workCopy: {
    flex: 1,
    minWidth: 0,
  },
  workMore: {
    paddingHorizontal: 16,
    paddingBottom: 12,
  },
  filters: {
    flexDirection: "row",
    gap: 8,
  },
  chip: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    minHeight: 34,
    paddingHorizontal: 14,
    borderRadius: Radii.pill,
  },
});
