import React from "react";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface InterfaceTimelineActivityExpandIconProps {
  expanded: boolean;
  chrome: TerminalThemeChrome;
}

export function InterfaceTimelineActivityExpandIcon({
  expanded,
  chrome,
}: InterfaceTimelineActivityExpandIconProps) {
  return (
    <Ionicons
      name={expanded ? "chevron-up" : "chevron-down"}
      size={12}
      color={chrome.textMuted}
    />
  );
}
