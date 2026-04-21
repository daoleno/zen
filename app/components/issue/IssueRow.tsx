import React from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import { useRouter } from "expo-router";
import { Colors, Typography } from "../../constants/tokens";
import type { Issue } from "../../store/issues";

export function statusGlyph(issue: Issue): string {
  if (issue.frontmatter.done) return "✓";
  if (issue.frontmatter.dispatched) return "▶";
  return "●";
}

export function relativeTime(iso: string): string {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) {
    return "";
  }

  const diffSeconds = Math.floor((Date.now() - then) / 1000);
  if (diffSeconds < 60) return "just now";
  if (diffSeconds < 3600) return `${Math.floor(diffSeconds / 60)}m`;
  if (diffSeconds < 86_400) return `${Math.floor(diffSeconds / 3600)}h`;
  return `${Math.floor(diffSeconds / 86_400)}d`;
}

export function IssueRow({ issue }: { issue: Issue }) {
  const router = useRouter();

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
      <Text style={styles.glyph}>{statusGlyph(issue)}</Text>
      <View style={styles.body}>
        <Text style={styles.title} numberOfLines={1}>
          {issue.title || "(untitled)"}
        </Text>
        <Text style={styles.meta} numberOfLines={1}>
          {issue.serverName} · {issue.project} · {relativeTime(issue.frontmatter.created)}
        </Text>
      </View>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 18,
    paddingVertical: 14,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: Colors.bgElevated,
    backgroundColor: Colors.bgPrimary,
  },
  rowPressed: {
    opacity: 0.65,
  },
  glyph: {
    width: 18,
    color: Colors.textSecondary,
    fontFamily: Typography.terminalFontBold,
    fontSize: 14,
    textAlign: "center",
  },
  body: {
    flex: 1,
  },
  title: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 16,
  },
  meta: {
    marginTop: 4,
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
  },
});
