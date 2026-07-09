import type { ViewStyle } from "react-native";
import type { ChatLayout } from "../../theme/types";
import type { MessageGroupPosition } from "./CodexTimelineGrouping";

const LARGE = 18;
const SMALL = 6;
const CHATGPT_RADIUS = 20;

export function userBubbleRadii(
  position: MessageGroupPosition,
): Pick<ViewStyle, "borderTopLeftRadius" | "borderTopRightRadius" | "borderBottomLeftRadius" | "borderBottomRightRadius"> {
  switch (position) {
    case "first":
      return {
        borderTopLeftRadius: LARGE,
        borderTopRightRadius: LARGE,
        borderBottomLeftRadius: LARGE,
        borderBottomRightRadius: SMALL,
      };
    case "middle":
      return {
        borderTopLeftRadius: LARGE,
        borderTopRightRadius: SMALL,
        borderBottomLeftRadius: LARGE,
        borderBottomRightRadius: SMALL,
      };
    case "last":
      return {
        borderTopLeftRadius: LARGE,
        borderTopRightRadius: SMALL,
        borderBottomLeftRadius: LARGE,
        borderBottomRightRadius: LARGE,
      };
    default:
      return {
        borderTopLeftRadius: LARGE,
        borderTopRightRadius: LARGE,
        borderBottomLeftRadius: LARGE,
        borderBottomRightRadius: SMALL,
      };
  }
}

export function assistantBubbleRadii(
  position: MessageGroupPosition,
): Pick<ViewStyle, "borderTopLeftRadius" | "borderTopRightRadius" | "borderBottomLeftRadius" | "borderBottomRightRadius"> {
  switch (position) {
    case "first":
      return {
        borderTopLeftRadius: LARGE,
        borderTopRightRadius: LARGE,
        borderBottomLeftRadius: LARGE,
        borderBottomRightRadius: LARGE,
      };
    case "middle":
      return {
        borderTopLeftRadius: SMALL,
        borderTopRightRadius: LARGE,
        borderBottomLeftRadius: SMALL,
        borderBottomRightRadius: LARGE,
      };
    case "last":
      return {
        borderTopLeftRadius: SMALL,
        borderTopRightRadius: LARGE,
        borderBottomLeftRadius: SMALL,
        borderBottomRightRadius: LARGE,
      };
    default:
      return {
        borderTopLeftRadius: SMALL,
        borderTopRightRadius: LARGE,
        borderBottomLeftRadius: SMALL,
        borderBottomRightRadius: LARGE,
      };
  }
}

export function chatgptUserBubbleRadii(): Pick<
  ViewStyle,
  "borderTopLeftRadius" | "borderTopRightRadius" | "borderBottomLeftRadius" | "borderBottomRightRadius"
> {
  return {
    borderTopLeftRadius: CHATGPT_RADIUS,
    borderTopRightRadius: CHATGPT_RADIUS,
    borderBottomLeftRadius: CHATGPT_RADIUS,
    borderBottomRightRadius: CHATGPT_RADIUS,
  };
}

export function messageRowSpacing(
  compactTop: boolean,
  compactBottom: boolean,
  layout: ChatLayout = "telegram",
  role: "user" | "assistant" = "user",
) {
  if (layout === "chatgpt") {
    return {
      marginTop: compactTop ? 6 : 0,
      marginBottom: compactBottom ? 6 : 18,
    };
  }

  // Assistant stays document-flow: tighter within a turn, airier between turns.
  if (role === "assistant") {
    return {
      marginTop: compactTop ? 1 : 0,
      marginBottom: compactBottom ? 4 : 14,
    };
  }

  return {
    marginTop: compactTop ? 2 : 0,
    marginBottom: compactBottom ? 3 : 11,
  };
}
