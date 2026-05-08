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

export function WorkRow({ item }: { item: WorkItem }) {
  const router = useRouter();
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const { glyph, color, label } = statusGlyph(item, colors);
  const status = workItemStatus(item);
  const done = status === "done";
  const showStatus = status !== "queued";
  const timeSource = item.mtime || item.frontmatter.created;
  const mentionCount = item.mentions.length;

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
      <Text style={[styles.glyph, { color }]}>{glyph}</Text>
      <View style={styles.copy}>
        <Text
          style={[styles.title, done && styles.titleDone]}
          numberOfLines={1}
        >
          {item.title || "(untitled work)"}
        </Text>
        {showStatus || mentionCount > 0 ? (
          <View style={styles.metaLine}>
            {showStatus ? <Text style={[styles.status, { color }]}>{label}</Text> : null}
            {showStatus && mentionCount > 0 ? <Text style={styles.metaDot}>·</Text> : null}
            {mentionCount > 0 ? (
              <Text style={styles.meta}>@{mentionCount}</Text>
            ) : null}
          </View>
        ) : null}
      </View>
      <Text style={styles.time}>{relativeTime(timeSource)}</Text>
    </Pressable>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    minHeight: 40,
    paddingVertical: 6,
    backgroundColor: colors.bgPrimary,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.borderSubtle,
  },
  rowPressed: {
    backgroundColor: colors.surfacePressed,
  },
  glyph: {
    fontFamily: Typography.terminalFontBold,
    fontSize: 10,
    lineHeight: 18,
    textAlign: "center",
    width: 16,
    opacity: 0.88,
  },
  copy: {
    flex: 1,
    minWidth: 0,
  },
  title: {
    color: colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 13,
    lineHeight: 17,
    opacity: 0.92,
  },
  titleDone: {
    color: colors.textSecondary,
    textDecorationLine: "line-through",
    opacity: 0.62,
  },
  metaLine: {
    marginTop: 1,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
  },
  status: {
    fontFamily: Typography.terminalFont,
    fontSize: 10,
    lineHeight: 13,
    opacity: 0.78,
  },
  metaDot: {
    color: colors.textSecondary,
    fontFamily: Typography.terminalFont,
    fontSize: 10,
    lineHeight: 13,
    opacity: 0.35,
  },
  meta: {
    color: colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 10,
    lineHeight: 13,
    opacity: 0.5,
  },
  time: {
    color: colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 11,
    lineHeight: 14,
    minWidth: 26,
    textAlign: "right",
    opacity: 0.44,
  },
  });
}
