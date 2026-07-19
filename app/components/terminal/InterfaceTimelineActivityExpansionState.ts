import { useCallback, useReducer } from "react";

export interface TimelineActivityExpansionState {
  readonly eventId: string;
  readonly userChoice?: boolean;
}

export interface ToggleTimelineActivityExpansionAction {
  readonly eventId: string;
  readonly defaultExpanded: boolean;
}

export function createTimelineActivityExpansionState(
  eventId: string,
): TimelineActivityExpansionState {
  requireStableEventId(eventId);
  return { eventId };
}

export function reduceTimelineActivityExpansion(
  state: TimelineActivityExpansionState,
  action: ToggleTimelineActivityExpansionAction,
): TimelineActivityExpansionState {
  const expanded = resolveTimelineActivityExpansion(
    state,
    action.eventId,
    action.defaultExpanded,
  );
  return {
    eventId: action.eventId,
    userChoice: !expanded,
  };
}

export function resolveTimelineActivityExpansion(
  state: TimelineActivityExpansionState,
  eventId: string,
  defaultExpanded: boolean,
): boolean {
  requireStableEventId(eventId);
  return state.eventId === eventId && state.userChoice !== undefined
    ? state.userChoice
    : defaultExpanded;
}

/**
 * Expansion intent belongs to the local activity row. InterfaceTimelineView keys
 * FlatList rows by event ID, so same-ID streaming upserts retain this state and
 * a new ID mounts with its own default. If virtualization later remounts an
 * off-window row, it intentionally returns to its current default; preserving
 * choices beyond that boundary would require an explicitly timeline-owned
 * provider rather than hidden item metadata.
 */
export function useTimelineActivityExpansion(
  eventId: string,
  defaultExpanded: boolean,
) {
  const [state, dispatch] = useReducer(
    reduceTimelineActivityExpansion,
    eventId,
    createTimelineActivityExpansionState,
  );
  const expanded = resolveTimelineActivityExpansion(
    state,
    eventId,
    defaultExpanded,
  );
  return {
    expanded,
    detailsExpanded: expanded,
    toggle: useCallback(() => {
      dispatch({ eventId, defaultExpanded });
    }, [defaultExpanded, eventId]),
  };
}

function requireStableEventId(eventId: string): void {
  if (!eventId) {
    throw new Error("Timeline activity expansion requires a stable event id");
  }
}
