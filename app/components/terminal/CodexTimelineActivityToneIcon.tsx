import React from "react";
import {
  ActivityIndicator,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type {
  TimelineActivityIconName,
  ZenActivityTimelineItem,
} from "./CodexTimelineActivityTypes";

interface CodexTimelineActivityToneIconProps {
  tone: ZenActivityTimelineItem["tone"];
  icon: TimelineActivityIconName;
  color: string;
}

export function CodexTimelineActivityToneIcon({
  tone,
  icon,
  color,
}: CodexTimelineActivityToneIconProps) {
  if (tone === "running") {
    return <ActivityIndicator size="small" color={color} />;
  }

  return <Ionicons name={icon} size={13} color={color} />;
}
