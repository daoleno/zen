import React from "react";
import { StyleSheet, Text, TouchableOpacity, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Colors, Typography } from "../../constants/tokens";

interface ProjectRowProps {
  name: string;
  meta: string;
  issueCount: number;
  activeCount: number;
  backlogCount: number;
  doneCount: number;
  onPress: () => void;
  onMore: () => void;
}

export function ProjectRow({
  name,
  meta,
  issueCount,
  activeCount,
  backlogCount,
  doneCount,
  onPress,
  onMore,
}: ProjectRowProps) {
  const countParts: string[] = [];
  if (activeCount > 0) countParts.push(`${activeCount} active`);
  if (backlogCount > 0) countParts.push(`${backlogCount} backlog`);
  if (doneCount > 0) countParts.push(`${doneCount} done`);
  const countLine = countParts.join('  ·  ');

  const metaLine = [meta, countLine].filter(Boolean).join('  ·  ');

  return (
    <View style={styles.row}>
      <TouchableOpacity
        style={styles.mainAction}
        onPress={onPress}
        activeOpacity={0.82}
      >
        <View style={styles.header}>
          <Text style={styles.name} numberOfLines={1}>
            {name}
          </Text>

          <View style={styles.trailing}>
            {issueCount > 0 ? (
              <Text style={styles.issueCount}>{issueCount}</Text>
            ) : null}
            <Ionicons
              name="chevron-forward"
              size={13}
              color={Colors.textSecondary}
            />
          </View>
        </View>

        {metaLine ? (
          <Text style={styles.meta} numberOfLines={1}>
            {metaLine}
          </Text>
        ) : null}
      </TouchableOpacity>

      <TouchableOpacity
        style={styles.moreButton}
        onPress={onMore}
        activeOpacity={0.82}
      >
        <Ionicons
          name="ellipsis-horizontal"
          size={15}
          color={Colors.textSecondary}
        />
      </TouchableOpacity>
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
  mainAction: {
    flex: 1,
    paddingVertical: 14,
    gap: 5,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
  },
  name: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 14,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },
  trailing: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
  issueCount: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.terminalFont,
  },
  meta: {
    color: Colors.textSecondary,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
  moreButton: {
    width: 30,
    height: 30,
    borderRadius: 15,
    alignItems: "center",
    justifyContent: "center",
  },
});
