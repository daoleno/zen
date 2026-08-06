import React from "react";
import { StyleSheet, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { ACTIVITY_HEADER_ICON_SLOT } from "./activityHeaderTextMetrics";
import type {
  TimelineActivityIconName,
  ZenActivityTimelineItem,
} from "./InterfaceTimelineActivityTypes";
import { ComposerLoadingDots } from "./ComposerLoadingDots";

interface InterfaceTimelineActivityToneIconProps {
  tone: ZenActivityTimelineItem["tone"];
  icon: TimelineActivityIconName;
  activityKind?: ZenActivityTimelineItem["activityKind"];
  color: string;
}

export function InterfaceTimelineActivityToneIcon({
  tone,
  icon,
  activityKind,
  color,
}: InterfaceTimelineActivityToneIconProps) {
  if (tone === "running") {
    return (
      <View style={styles.slot}>
        <ComposerLoadingDots color={color} size={7} />
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
    width: ACTIVITY_HEADER_ICON_SLOT,
    height: ACTIVITY_HEADER_ICON_SLOT,
    alignItems: "center",
    justifyContent: "center",
  },
});
