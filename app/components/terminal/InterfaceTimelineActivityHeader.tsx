import React, { useCallback, useRef, useState } from "react";
import {
  StyleSheet,
  Text,
  View,
  type GestureResponderEvent,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ContinuousCorners } from "../../constants/tokens";
import { chromeTint } from "./composerMaterial";
import {
  ACTIVITY_HEADER_DETAIL_FONT,
  ACTIVITY_HEADER_DETAIL_GAP,
  ACTIVITY_HEADER_ROW_MIN_HEIGHT,
  ACTIVITY_HEADER_ROW_PADDING_VERTICAL,
  ACTIVITY_HEADER_TITLE_FONT,
  activityHeaderCopyLineBoxHeight,
  activityHeaderSharedTextStyle,
} from "./activityHeaderTextMetrics";
import type { TimelineActivityIconName } from "./InterfaceTimelineActivityTypes";
import type { ZenActivityTimelineItem } from "./InterfaceTimelineActivityTypes";
import { InterfaceTimelineActivityExpandIcon } from "./InterfaceTimelineActivityExpandIcon";
import { InterfaceTimelineActivityToneIcon } from "./InterfaceTimelineActivityToneIcon";
import {
  toolDisclosureMovedBeyondSlop,
  toolDisclosureShouldCommitToggle,
} from "./timelineDisclosurePress";

interface InterfaceTimelineActivityHeaderProps {
  title: string;
  tone: "neutral" | "running" | "success" | "failed";
  icon: TimelineActivityIconName;
  activityKind?: ZenActivityTimelineItem["activityKind"];
  detail?: string;
  canExpand: boolean;
  expanded: boolean;
  toneColor: string;
  chrome: TerminalThemeChrome;
  accessibilityLabel?: string;
  onPress(): void;
}

type DisclosureTouch = {
  startX: number;
  startY: number;
  moved: boolean;
  active: boolean;
};

/**
 * Tool disclosure control. Owns the responder until the user drags past touch
 * slop so content-driven parent scroll cannot cancel an intentional tap after
 * press feedback. Expansion state remains on the activity row; this only
 * delivers a clean onPress.
 */

export function InterfaceTimelineActivityHeader({
  title,
  tone,
  icon,
  activityKind,
  detail,
  canExpand,
  expanded,
  toneColor,
  chrome,
  accessibilityLabel,
  onPress,
}: InterfaceTimelineActivityHeaderProps) {
  const [pressed, setPressed] = useState(false);
  const touchRef = useRef<DisclosureTouch>({
    startX: 0,
    startY: 0,
    moved: false,
    active: false,
  });

  const labelParts = [accessibilityLabel || title];
  if (detail) {
    labelParts.push(detail);
  }
  const normalizedDetail = detail?.toLowerCase() || "";
  if (
    tone === "failed" &&
    !normalizedDetail.includes("failed") &&
    !normalizedDetail.includes("blocked")
  ) {
    labelParts.push("failed");
  } else if (
    tone === "running" &&
    !normalizedDetail.includes("running") &&
    !normalizedDetail.includes("waiting")
  ) {
    labelParts.push("in progress");
  }
  if (canExpand) {
    labelParts.push(expanded ? "expanded" : "collapsed");
  }

  const resetTouch = useCallback(() => {
    touchRef.current.active = false;
    touchRef.current.moved = false;
    setPressed(false);
  }, []);

  const handleResponderRelease = useCallback(() => {
    const touch = touchRef.current;
    const shouldCommit = toolDisclosureShouldCommitToggle({
      canExpand,
      gestureActive: touch.active,
      userMovedBeyondSlop: touch.moved,
    });
    resetTouch();
    if (shouldCommit) {
      onPress();
    }
  }, [canExpand, onPress, resetTouch]);

  const handleStartShouldSetResponder = useCallback(
    () => canExpand,
    [canExpand],
  );

  const handleResponderGrant = useCallback((event: GestureResponderEvent) => {
    touchRef.current = {
      startX: event.nativeEvent.pageX,
      startY: event.nativeEvent.pageY,
      moved: false,
      active: true,
    };
    setPressed(true);
  }, []);

  const handleResponderMove = useCallback((event: GestureResponderEvent) => {
    const touch = touchRef.current;
    if (!touch.active || touch.moved) {
      return;
    }
    if (
      toolDisclosureMovedBeyondSlop(
        touch.startX,
        touch.startY,
        event.nativeEvent.pageX,
        event.nativeEvent.pageY,
      )
    ) {
      touch.moved = true;
      setPressed(false);
    }
  }, []);

  const handleAccessibilityActivate = useCallback(() => {
    if (canExpand) {
      onPress();
    }
  }, [canExpand, onPress]);

  return (
    <View
      accessible
      accessibilityLabel={labelParts.join(", ")}
      accessibilityRole="button"
      accessibilityState={{
        disabled: !canExpand,
        expanded: canExpand ? expanded : undefined,
      }}
      accessibilityActions={canExpand ? [{ name: "activate" }] : undefined}
      onAccessibilityAction={(event) => {
        if (event.nativeEvent.actionName === "activate") {
          handleAccessibilityActivate();
        }
      }}
      style={[
        styles.row,
        pressed
          ? {
              backgroundColor: chromeTint(
                chrome.text,
                0.06,
                chrome.surfaceActive,
              ),
            }
          : null,
      ]}
      onStartShouldSetResponder={handleStartShouldSetResponder}
      onMoveShouldSetResponder={() => false}
      onResponderGrant={handleResponderGrant}
      onResponderMove={handleResponderMove}
      onResponderRelease={handleResponderRelease}
      onResponderTerminate={resetTouch}
      onResponderTerminationRequest={() => touchRef.current.moved}
      // Enlarge the press target while keeping durable responder ownership.
      hitSlop={{ top: 8, bottom: 8, left: 4, right: 4 }}
    >
      <InterfaceTimelineActivityToneIcon
        tone={tone}
        icon={icon}
        activityKind={activityKind}
        color={toneColor}
      />
      <View style={styles.copy} pointerEvents="none">
        <Text
          style={[
            styles.title,
            { color: tone === "failed" ? toneColor : chrome.textMuted },
          ]}
          numberOfLines={1}
          ellipsizeMode="tail"
        >
          {title}
        </Text>
        {detail ? (
          <Text
            style={[styles.detail, { color: chrome.textSubtle }]}
            numberOfLines={1}
            ellipsizeMode="tail"
          >
            {detail}
          </Text>
        ) : null}
      </View>
      {canExpand ? (
        <View pointerEvents="none">
          <InterfaceTimelineActivityExpandIcon
            expanded={expanded}
            chrome={chrome}
          />
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  // The press tint bleeds 6 pt past the prose column so the tone mark stays
  // optically aligned with the message text while the highlight reads as a
  // rounded, native list-row press.
  row: {
    alignSelf: "stretch",
    minHeight: ACTIVITY_HEADER_ROW_MIN_HEIGHT,
    marginHorizontal: -6,
    paddingHorizontal: 6,
    borderRadius: 9,
    ...ContinuousCorners,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingVertical: ACTIVITY_HEADER_ROW_PADDING_VERTICAL,
  },
  copy: {
    flex: 1,
    minWidth: 0,
    height: activityHeaderCopyLineBoxHeight(),
    flexDirection: "row",
    alignItems: "center",
  },
  title: {
    ...activityHeaderSharedTextStyle,
    flex: 1,
    minWidth: 0,
    flexShrink: 1,
    fontFamily: ACTIVITY_HEADER_TITLE_FONT,
    fontWeight: "500",
  },
  detail: {
    ...activityHeaderSharedTextStyle,
    maxWidth: "46%",
    flexShrink: 0,
    minWidth: 0,
    marginLeft: ACTIVITY_HEADER_DETAIL_GAP,
    fontFamily: ACTIVITY_HEADER_DETAIL_FONT,
    fontWeight: "400",
  },
});
