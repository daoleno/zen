import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type {
  TimelineActivityIconName,
  ZenActivityTimelineItem,
} from "./CodexTimelineActivityTypes";

interface CodexTimelineActivityToneIconProps {
  tone: ZenActivityTimelineItem["tone"];
  icon: TimelineActivityIconName;
  activityKind?: ZenActivityTimelineItem["activityKind"];
  color: string;
}

export function CodexTimelineActivityToneIcon({
  tone,
  icon,
  activityKind,
  color,
}: CodexTimelineActivityToneIconProps) {
  if (tone === "running") {
    return (
      <View style={styles.slot}>
        <ActivityIndicator size="small" color={color} />
      </View>
    );
  }

  return (
    <View style={styles.slot}>
      <Ionicons
        name={icon}
        size={activityKind === "reasoning" ? 15 : 14}
        color={color}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  slot: {
    width: 18,
    height: 18,
    alignItems: "center",
    justifyContent: "center",
  },
});
