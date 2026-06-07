import React from "react";
import type { TextProps } from "react-native";

type TimelineSelectableTextProps = Pick<
  TextProps,
  "selectable" | "onPressIn" | "onLongPress" | "onPressOut"
>;

export interface TimelineTextSelectableContextValue {
  selectable: boolean;
  onTextSelectionPressIn?: () => void;
  onTextSelectionGestureStart?: () => void;
  onTextSelectionGestureEnd?: () => void;
}

export const TimelineTextSelectableContext =
  React.createContext<TimelineTextSelectableContextValue>({
    selectable: true,
  });

export function useTimelineSelectableTextProps(): TimelineSelectableTextProps {
  const {
    selectable,
    onTextSelectionPressIn,
    onTextSelectionGestureStart,
    onTextSelectionGestureEnd,
  } = React.useContext(TimelineTextSelectableContext);

  return React.useMemo(() => {
    return {
      selectable,
      onPressIn: onTextSelectionPressIn,
      onLongPress: onTextSelectionGestureStart,
      onPressOut: onTextSelectionGestureEnd,
    };
  }, [
    onTextSelectionGestureEnd,
    onTextSelectionGestureStart,
    onTextSelectionPressIn,
    selectable,
  ]);
}
