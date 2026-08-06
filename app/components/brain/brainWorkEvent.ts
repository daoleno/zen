/** Canonical Brain Work card domain for conversation/timeline presentation. */
export type BrainWorkEventKind =
  | "session.done"
  | "session.failed"
  | "session.needs_input"
  | "session.stale";

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
};
