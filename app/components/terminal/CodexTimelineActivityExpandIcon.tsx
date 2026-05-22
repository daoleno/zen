import React from "react";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface CodexTimelineActivityExpandIconProps {
  expanded: boolean;
  chrome: TerminalThemeChrome;
}

export function CodexTimelineActivityExpandIcon({
  expanded,
  chrome,
}: CodexTimelineActivityExpandIconProps) {
  return (
    <Ionicons
      name={expanded ? "chevron-up" : "chevron-down"}
      size={12}
      color={chrome.textSubtle}
    />
  );
}
