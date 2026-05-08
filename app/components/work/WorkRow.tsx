import React, { useMemo } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { useRouter } from "expo-router";
import { Colors, Typography, useAppColors } from "../../constants/tokens";
import type { WorkItem } from "../../store/work";

type GlyphInfo = { glyph: string; color: string; label: string };
export type WorkStatus = "queued" | "running" | "blocked" | "done" | "failed" | "unknown";

export function workItemStatus(item: WorkItem): WorkStatus {
  const raw =
    typeof item.frontmatter.status === "string"
      ? item.frontmatter.status.trim().toLowerCase()
      : "";
  switch (raw) {
    case "failed":
      return "failed";
    case "blocked":
      return "blocked";
    case "done":
      return "done";
    case "running":
      return "running";
    case "unknown":
      return "unknown";
    default:
      break;
  }
  if (item.frontmatter.done) {
    return "done";
  }
  if (item.frontmatter.started) {
    return "running";
  }
  return "queued";
}

export function statusGlyph(item: WorkItem, colors: typeof Colors = Colors): GlyphInfo {
  switch (workItemStatus(item)) {
    case "failed":
      return { glyph: "×", color: colors.statusFailed, label: "Failed" };
    case "blocked":
      return { glyph: "!", color: colors.statusBlocked, label: "Blocked" };
    case "done":
      return { glyph: "✓", color: colors.statusDone, label: "Done" };
    case "running":
      return { glyph: "▸", color: colors.statusRunning, label: "Running" };
    case "unknown":
      return { glyph: "?", color: colors.statusUnknown, label: "Unknown" };
    case "queued":
      return { glyph: "●", color: colors.accent, label: "Queued" };
  }
}

export function relativeTime(iso: string): string {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) {
    return "";
  }
  const diff = Math.floor((Date.now() - then) / 1000);
  if (diff < 60) return "now";
  if (diff < 3600) return `${Math.floor(diff / 60)}m`;
  if (diff < 86_400) return `${Math.floor(diff / 3600)}h`;
  if (diff < 86_400 * 30) return `${Math.floor(diff / 86_400)}d`;
  return `${Math.floor(diff / (86_400 * 30))}mo`;
}

export function workItemTitle(item: WorkItem): string {
  return item.frontmatter.title?.trim() || item.title || "Analyzing work session";
}

export function workItemSummary(item: WorkItem): string {
  if (item.frontmatter.summary?.trim()) {
    return item.frontmatter.summary.trim();
  }
  if (item.frontmatter.ai_error?.trim()) {
    return "AI digest unavailable. The daemon will retry when this session changes.";
  }
  return "Waiting for AI digest.";
}

export function WorkRow({ item }: { item: WorkItem }) {
  const router = useRouter();
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const { glyph, color, label } = statusGlyph(item, colors);
  const status = workItemStatus(item);
  const done = status === "done";
  const timeSource = item.mtime || item.frontmatter.created;
  const title = workItemTitle(item);
  const summary = workItemSummary(item);
  const next = item.frontmatter.next?.trim();
  const provider = item.frontmatter.ai_provider?.trim();
  const time = relativeTime(timeSource);

  return (
    <Pressable
      onPress={() =>
        router.push({
          pathname: "/work/[id]",
          params: { id: item.id, serverId: item.serverId },
        })
      }
      style={({ pressed }) => [styles.row, pressed && styles.rowPressed]}
    >
      <View style={[styles.statusRail, { backgroundColor: color }]} />
      <View style={styles.copy}>
        <View style={styles.topLine}>
          <View style={[styles.statusPill, { borderColor: color }]}>
            <Text style={[styles.statusGlyph, { color }]}>{glyph}</Text>
            <Text style={[styles.statusLabel, { color }]}>{label}</Text>
          </View>
          {provider ? <Text style={styles.provider}>{provider}</Text> : null}
          {time ? <Text style={styles.time}>{time}</Text> : null}
        </View>
        <Text
          style={[styles.title, done && styles.titleDone]}
          numberOfLines={2}
        >
          {title}
        </Text>
        <Text style={styles.summary} numberOfLines={2}>
          {summary}
        </Text>
        {next ? (
          <Text style={styles.next} numberOfLines={1}>
            Next: {next}
          </Text>
        ) : null}
      </View>
    </Pressable>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
  row: {
    flexDirection: "row",
    alignItems: "stretch",
    gap: 11,
    minHeight: 86,
    paddingVertical: 10,
    backgroundColor: colors.bgPrimary,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.borderSubtle,
  },
  rowPressed: {
    backgroundColor: colors.surfacePressed,
  },
  statusRail: {
    width: 3,
    borderRadius: 2,
    opacity: 0.82,
  },
  copy: {
    flex: 1,
    minWidth: 0,
  },
  topLine: {
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
    minHeight: 18,
    marginBottom: 4,
  },
  statusPill: {
    height: 18,
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    paddingHorizontal: 6,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 9,
    backgroundColor: colors.surfaceSubtle,
  },
  statusGlyph: {
    fontFamily: Typography.terminalFontBold,
    fontSize: 9,
    lineHeight: 12,
  },
  statusLabel: {
    fontFamily: Typography.terminalFont,
    fontSize: 9,
    lineHeight: 12,
    textTransform: "uppercase",
  },
  provider: {
    color: colors.textSecondary,
    fontFamily: Typography.terminalFont,
    fontSize: 10,
    lineHeight: 13,
    opacity: 0.48,
  },
  title: {
    color: colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 14,
    lineHeight: 19,
    opacity: 0.94,
  },
  titleDone: {
    color: colors.textSecondary,
    textDecorationLine: "line-through",
    opacity: 0.62,
  },
  summary: {
    marginTop: 4,
    color: colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
    lineHeight: 17,
    opacity: 0.76,
  },
  next: {
    marginTop: 5,
    color: colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 11,
    lineHeight: 15,
    opacity: 0.54,
  },
  time: {
    color: colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 10,
    lineHeight: 13,
    marginLeft: "auto",
    opacity: 0.48,
  },
  });
}
