import React from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { useRouter } from "expo-router";
import { Colors, Spacing, Typography } from "../../constants/tokens";
import type { Issue } from "../../store/issues";

type GlyphInfo = { glyph: string; color: string };

export function statusGlyph(issue: Issue): GlyphInfo {
  if (issue.frontmatter.done) {
    return { glyph: "✓", color: Colors.textSecondary };
  }
  if (issue.frontmatter.dispatched) {
    return { glyph: "▸", color: Colors.statusRunning };
  }
  return { glyph: "●", color: Colors.accent };
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
  const { glyph, color } = statusGlyph(issue);
  const done = !!issue.frontmatter.done;
  const timeSource = issue.mtime || issue.frontmatter.created;

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
      <Text
        style={[styles.title, done && styles.titleDone]}
        numberOfLines={1}
      >
        {issue.title || "(untitled)"}
      </Text>
      <Text style={styles.time}>{relativeTime(timeSource)}</Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.md,
    paddingHorizontal: Spacing.lg,
    height: 44,
    backgroundColor: Colors.bgPrimary,
  },
  rowPressed: {
    backgroundColor: Colors.bgSurface,
  },
  glyph: {
    width: 14,
    fontFamily: Typography.terminalFontBold,
    fontSize: 14,
    textAlign: "center",
  },
  title: {
    flex: 1,
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 15,
  },
  titleDone: {
    color: Colors.textSecondary,
    textDecorationLine: "line-through",
  },
  time: {
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
    minWidth: 28,
    textAlign: "right",
  },
});
