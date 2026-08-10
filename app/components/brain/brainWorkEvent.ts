/** Canonical Brain Work card domain for conversation/timeline presentation. */
export type BrainWorkEventKind =
  | "session.done"
  | "session.failed"
  | "session.needs_input"
  | "session.stale"
  | "session.uncertain"
  | "session.ownership_lost";

export type BrainWorkResultEvent = {
  event_id: string;
  kind: BrainWorkEventKind;
  work_id: string;
  work_title: string;
  summary: string;
  session_id?: string;
  session_name?: string;
  occurred_at: string;
  unread: boolean;
  review_state: "queued" | "reserved" | "reviewing" | "resolved";
  session_state:
    | "open"
    | "closing"
    | "finalized"
    | "close_failed"
    | "not_required";
  current_result: boolean;
};
