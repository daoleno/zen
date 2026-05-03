import React, { useMemo } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { useRouter } from "expo-router";
import { Colors, Typography, useAppColors } from "../../constants/tokens";
import type { Issue } from "../../store/issues";

type GlyphInfo = { glyph: string; color: string; label: string };

export function statusGlyph(issue: Issue, colors: typeof Colors = Colors): GlyphInfo {
  if (issue.frontmatter.done) {
    return { glyph: "✓", color: colors.textSecondary, label: "Done" };
  }
  if (issue.frontmatter.dispatched) {
    return { glyph: "▸", color: colors.statusRunning, label: "Sent" };
  }
  return { glyph: "●", color: colors.accent, label: "Open" };
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

export function IssueRow({ issue }: { issue: Issue }) {
  const router = useRouter();
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const { glyph, color, label } = statusGlyph(issue, colors);
  const done = !!issue.frontmatter.done;
  const sent = !!issue.frontmatter.dispatched;
  const timeSource = issue.mtime || issue.frontmatter.created;
  const mentionCount = issue.mentions.length;

  return (
    <Pressable
      onPress={() =>
        router.push({
          pathname: "/issue/[id]",
          params: { id: issue.id, serverId: issue.serverId },
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
          {issue.title || "(untitled)"}
        </Text>
        {sent || mentionCount > 0 ? (
          <View style={styles.metaLine}>
            {sent ? <Text style={[styles.status, { color }]}>{label}</Text> : null}
            {sent && mentionCount > 0 ? <Text style={styles.metaDot}>·</Text> : null}
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
