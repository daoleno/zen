import React from "react";
import type { TextProps } from "react-native";

type TimelineSelectableTextProps = Pick<
  TextProps,
  "selectable" | "onLongPress" | "onPressOut"
>;

export interface TimelineTextSelectableContextValue {
  selectable: boolean;
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
    onTextSelectionGestureStart,
    onTextSelectionGestureEnd,
  } = React.useContext(TimelineTextSelectableContext);

  return React.useMemo(() => {
    if (!selectable) {
      return { selectable: false };
    }
    return {
      selectable: true,
      onLongPress: onTextSelectionGestureStart,
      onPressOut: onTextSelectionGestureEnd,
    };
  }, [
    onTextSelectionGestureEnd,
    onTextSelectionGestureStart,
    selectable,
  ]);
}
