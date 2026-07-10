export type DrawerPhase =
  | "closed"
  | "dragging-open"
  | "settling-open"
  | "open"
  | "dragging-closed"
  | "settling-closed";

export type DrawerFocusReturn = "menu" | "none";

export type DrawerState =
  | { phase: "closed"; focusReturn: DrawerFocusReturn }
  | { phase: "dragging-open" }
  | { phase: "settling-open" }
  | { phase: "open" }
  | { phase: "dragging-closed" }
  | { phase: "settling-closed"; focusReturn: DrawerFocusReturn };

export type DrawerStateEvent =
  | { type: "gesture-activated"; anchor: "closed" | "open" }
  | {
      type: "settle-started";
      target: "closed" | "open";
      focusReturn: DrawerFocusReturn;
    }
  | {
      type: "settled";
      target: "closed" | "open";
      focusReturn: DrawerFocusReturn;
    }
  | { type: "reset" }
  | { type: "focus-returned" };

export const INITIAL_DRAWER_STATE: DrawerState = {
  phase: "closed",
  focusReturn: "none",
};

export function transitionDrawerState(
  state: DrawerState,
  event: DrawerStateEvent,
): DrawerState {
  switch (event.type) {
    case "gesture-activated":
      if (event.anchor === "closed" && state.phase === "closed") {
        return { phase: "dragging-open" };
      }
      if (event.anchor === "open" && state.phase === "open") {
        return { phase: "dragging-closed" };
      }
      return state;
    case "settle-started":
      if (event.target === "open") {
        if (state.phase === "open" || state.phase === "settling-open") {
          return state;
        }
        return { phase: "settling-open" };
      }
      if (state.phase === "closed" || state.phase === "settling-closed") {
        return state;
      }
      return {
        phase: "settling-closed",
        focusReturn: event.focusReturn,
      };
    case "settled":
      return event.target === "open"
        ? { phase: "open" }
        : { phase: "closed", focusReturn: event.focusReturn };
    case "reset":
      if (state.phase === "closed" && state.focusReturn === "none") {
        return state;
      }
      return INITIAL_DRAWER_STATE;
    case "focus-returned":
      if (state.phase !== "closed" || state.focusReturn === "none") {
        return state;
      }
      return INITIAL_DRAWER_STATE;
  }
}

export function isDrawerVisible(state: DrawerState): boolean {
  return state.phase !== "closed";
}
