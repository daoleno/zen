import React from "react";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface InterfaceTimelineActivityExpandIconProps {
  expanded: boolean;
  chrome: TerminalThemeChrome;
}

/**
 * Native disclosure convention: a forward chevron while collapsed, a down
 * chevron once the details are open.
 */
export function InterfaceTimelineActivityExpandIcon({
  expanded,
  chrome,
}: InterfaceTimelineActivityExpandIconProps) {
  return (
    <Ionicons
      name={expanded ? "chevron-down" : "chevron-forward"}
      size={13}
      color={expanded ? chrome.textMuted : chrome.textSubtle}
    />
  );
}
