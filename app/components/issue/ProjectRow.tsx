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
  return (
    <View style={styles.row}>
      <TouchableOpacity
        style={styles.mainAction}
        onPress={onPress}
        activeOpacity={0.82}
      >
        <View style={styles.header}>
          <View style={styles.copy}>
            <Text style={styles.name} numberOfLines={1}>
              {name}
            </Text>
            <Text style={styles.meta} numberOfLines={1}>
              {meta}
            </Text>
          </View>

          <View style={styles.trailing}>
            <Text style={styles.issueCount}>{issueCount}</Text>
            <Ionicons
              name="chevron-forward"
              size={14}
              color={Colors.textSecondary}
            />
          </View>
        </View>

        <View style={styles.metrics}>
          <MetricPill label="Active" value={activeCount} tone={Colors.accent} />
          <MetricPill
            label="Backlog"
            value={backlogCount}
            tone={Colors.textSecondary}
          />
          <MetricPill label="Done" value={doneCount} tone={Colors.statusDone} />
        </View>
      </TouchableOpacity>

      <TouchableOpacity
        style={styles.moreButton}
        onPress={onMore}
        activeOpacity={0.82}
      >
        <Ionicons
          name="ellipsis-horizontal"
          size={16}
          color={Colors.textSecondary}
        />
      </TouchableOpacity>
    </View>
  );
}

function MetricPill({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone: string;
}) {
  if (value <= 0) {
    return null;
  }

  return (
    <View style={[styles.metricPill, { backgroundColor: `${tone}14` }]}>
      <Text style={[styles.metricText, { color: tone }]}>
        {label} {value}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 10,
  },
  mainAction: {
    flex: 1,
    paddingVertical: 14,
    gap: 10,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  copy: {
    flex: 1,
    gap: 4,
  },
  name: {
    color: Colors.textPrimary,
    fontSize: 14,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },
  meta: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  trailing: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  issueCount: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.terminalFont,
  },
  metrics: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 8,
  },
  metricPill: {
    minHeight: 24,
    paddingHorizontal: 9,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
  },
  metricText: {
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
  },
  moreButton: {
    width: 32,
    height: 32,
    marginTop: 10,
    borderRadius: 16,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.04)",
  },
});
