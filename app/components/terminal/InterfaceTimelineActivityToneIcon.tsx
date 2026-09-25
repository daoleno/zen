import React from "react";
import { StyleSheet, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { ContinuousCorners } from "../../constants/tokens";
import { ACTIVITY_HEADER_ICON_SLOT } from "./activityHeaderTextMetrics";
import type {
  TimelineActivityIconName,
  ZenActivityTimelineItem,
} from "./InterfaceTimelineActivityTypes";
import { ComposerLoadingDots } from "./ComposerLoadingDots";
import { chromeTint } from "./composerMaterial";

interface InterfaceTimelineActivityToneIconProps {
  tone: ZenActivityTimelineItem["tone"];
  icon: TimelineActivityIconName;
  activityKind?: ZenActivityTimelineItem["activityKind"];
  color: string;
}

/**
 * Leading tool mark: a small tone-tinted squircle in the shared 18 pt slot.
 * It is the only colored element of a collapsed tool row, so tool activity
 * reads as a quiet margin annotation beside the prose, not as content.
 */
export function InterfaceTimelineActivityToneIcon({
  tone,
  icon,
  activityKind,
  color,
}: InterfaceTimelineActivityToneIconProps) {
  const tint = chromeTint(color, 0.14, "transparent");
  if (tone === "running") {
    return (
      <View style={[styles.slot, { backgroundColor: tint }]}>
        <ComposerLoadingDots color={color} size={6} />
      </View>
    );
  }

  return (
    <View style={[styles.slot, { backgroundColor: tint }]}>
      <Ionicons
        name={icon}
        size={activityKind === "reasoning" ? 12 : 11}
        color={color}
      />
    </View>
  );
}

/** Shared by the Plan header so every timeline annotation shares one rail. */
export const ACTIVITY_TONE_ICON_RADIUS = 6;

const styles = StyleSheet.create({
  slot: {
    width: ACTIVITY_HEADER_ICON_SLOT,
    height: ACTIVITY_HEADER_ICON_SLOT,
    borderRadius: ACTIVITY_TONE_ICON_RADIUS,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
  },
});
