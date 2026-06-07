import React from "react";
import type { TextProps } from "react-native";

type TimelineSelectableTextProps = Pick<
  TextProps,
  "selectable" | "onPressIn" | "onLongPress" | "onPressOut"
>;

export interface TimelineTextSelectableContextValue {
  selectable: boolean;
  onTextSelectionGestureStart?: TextProps["onPressIn"];
  onTextSelectionGestureEnd?: TextProps["onPressOut"];
}

export const TimelineTextSelectableContext =
  React.createContext<TimelineTextSelectableContextValue>({
    selectable: true,
  });

export function useTimelineSelectableTextProps(): TimelineSelectableTextProps {
  const {
    selectable,
    onTextSelectionGestureStart,
    onTextSelectionGestureEnd,
  } = React.useContext(TimelineTextSelectableContext);

  return React.useMemo(() => {
    if (!selectable) {
      return { selectable: false };
    }
    return {
      selectable: true,
      onPressIn: onTextSelectionGestureStart,
      onLongPress: onTextSelectionGestureStart,
      onPressOut: onTextSelectionGestureEnd,
    };
  }, [
    onTextSelectionGestureEnd,
    onTextSelectionGestureStart,
    selectable,
  ]);
}
