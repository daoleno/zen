import React from "react";
import { StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ContinuousCorners } from "../../constants/tokens";
import {
  ACTIVITY_HEADER_ICON_SLOT,
  ACTIVITY_HEADER_ROW_MIN_HEIGHT,
  ACTIVITY_HEADER_TITLE_FONT,
  activityHeaderSharedTextStyle,
} from "./activityHeaderTextMetrics";
import { ACTIVITY_TONE_ICON_RADIUS } from "./InterfaceTimelineActivityToneIcon";
import { chromeTint } from "./composerMaterial";

/**
 * Plan annotation header. Shares the tool row's tone-mark slot, caption line
 * box and gap so plans and tools hang from one leading rail.
 */
export function ZenPlanHeader({
  accentColor,
  chrome,
}: {
  accentColor: string;
  chrome: TerminalThemeChrome;
}) {
  return (
    <View style={styles.row}>
      <View
        style={[
          styles.mark,
          { backgroundColor: chromeTint(accentColor, 0.14, "transparent") },
        ]}
      >
        <Ionicons name="checkbox-outline" size={11} color={accentColor} />
      </View>
      <Text
        style={[styles.title, { color: chrome.textMuted }]}
        numberOfLines={1}
      >
        Updated Plan
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    alignSelf: "flex-start",
    minHeight: ACTIVITY_HEADER_ROW_MIN_HEIGHT,
    maxWidth: "100%",
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  mark: {
    width: ACTIVITY_HEADER_ICON_SLOT,
    height: ACTIVITY_HEADER_ICON_SLOT,
    borderRadius: ACTIVITY_TONE_ICON_RADIUS,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
  },
  title: {
    ...activityHeaderSharedTextStyle,
    fontFamily: ACTIVITY_HEADER_TITLE_FONT,
    fontWeight: "500",
  },
});
