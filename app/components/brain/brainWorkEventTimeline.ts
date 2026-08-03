import type { BrainWorkResultEvent } from "../../store/brain";
import type { BrainWorkEventTimelineItem } from "./BrainWorkEventCard";

export function projectCanonicalBrainWorkResultEvents({
  events,
  displayedThreadId,
  currentThreadId,
  readOnly,
  openSessionIds,
  onActivate,
}: {
  events: BrainWorkResultEvent[];
  displayedThreadId?: string;
  currentThreadId?: string;
  readOnly: boolean;
  openSessionIds: ReadonlySet<string>;
  onActivate: (
    event: BrainWorkResultEvent,
    canOpenSession: boolean,
  ) => void;
}): BrainWorkEventTimelineItem[] {
  if (
    readOnly ||
    !displayedThreadId ||
    displayedThreadId !== currentThreadId
  ) {
    return [];
  }
  return events.map((event) => {
    const canOpenSession = Boolean(
      event.session_id && openSessionIds.has(event.session_id),
    );
    return {
      type: "brain-work-event",
      id: event.event_id,
      timestamp: event.occurred_at,
      event,
      onPress:
        event.unread || canOpenSession
          ? () => onActivate(event, canOpenSession)
          : undefined,
    };
  });
}
