import type { BrainActiveWork } from "../../store/brain";
import type { WorkGraphOwner } from "./workRelationshipGraphModel";

export const GRAPH_OWNER_RUNNING: WorkGraphOwner = {
  sessionId: "brain-agent-release:@7",
  label: "Release review",
  status: "running",
  delegated: true,
  updatedAt: Date.parse("2026-08-10T09:30:00Z"),
};

export const GRAPH_OWNER_CORRECTION: WorkGraphOwner = {
  sessionId: "brain-agent-mobile:@11",
  label: "Mobile checks",
  status: "running",
  delegated: true,
  needsAttention: true,
  updatedAt: Date.parse("2026-08-10T09:31:00Z"),
};

export const GRAPH_OWNER_FAILED: WorkGraphOwner = {
  sessionId: "brain-agent-export:@4",
  label: "Export checks",
  status: "failed",
  delegated: true,
  updatedAt: Date.parse("2026-08-10T09:32:00Z"),
};

export const GRAPH_RAW_SESSION_WAKE_REF =
  `session:${GRAPH_OWNER_RUNNING.sessionId}:turn:` +
  `${GRAPH_OWNER_RUNNING.sessionId}:turn:provider-turn-9`;

export const GRAPH_PRODUCTION_WORK: BrainActiveWork[] = [
  graphWork({
    work_id: "owned-running",
    title: "Prepare the mobile release candidate",
    status: "running",
    progress_mode: "owned",
    owner_session_id: GRAPH_OWNER_RUNNING.sessionId,
    owner_delegated: true,
  }),
  graphWork({
    work_id: "typed-session-wait",
    title: "Summarize delegated findings",
    status: "waiting",
    progress_mode: "waiting",
    wait_for: GRAPH_RAW_SESSION_WAKE_REF,
    wake: {
      kind: "session_terminal",
      ref: GRAPH_RAW_SESSION_WAKE_REF,
    },
  }),
  graphWork({
    work_id: "user-wait",
    title: "Choose the release note emphasis",
    status: "waiting",
    progress_mode: "waiting",
    wait_for: "brain-thread:release-thread",
    wake: { kind: "user_input", ref: "brain-thread:release-thread" },
  }),
  graphWork({
    work_id: "calendar-wait",
    title: "Publish the scheduled report",
    status: "waiting",
    progress_mode: "waiting",
    wait_for: "calendar:daily-report:run-19",
    wake: {
      kind: "calendar_result",
      ref: "calendar:daily-report:run-19",
    },
  }),
  graphWork({
    work_id: "review-ready",
    title: "Review the relationship wording",
    status: "running",
    progress_mode: "ready",
    attention_pending: true,
  }),
  graphWork({
    work_id: "correction",
    title: "Correct compact Android spacing",
    status: "running",
    progress_mode: "owned",
    owner_session_id: GRAPH_OWNER_CORRECTION.sessionId,
    owner_delegated: true,
  }),
  graphWork({
    work_id: "failed-owner",
    title: "Verify the iOS export",
    status: "running",
    progress_mode: "owned",
    owner_session_id: GRAPH_OWNER_FAILED.sessionId,
    owner_delegated: true,
  }),
  graphWork({
    work_id: "ownerless-contradiction",
    title: "Resolve the missing owner",
    status: "running",
    progress_mode: "owned",
  }),
  graphWork({
    work_id: "failed-finalization",
    title: "Close delegated export checks",
    status: "done",
    progress_mode: undefined,
    session_finalizations: [
      {
        session_id: GRAPH_OWNER_FAILED.sessionId,
        delegated: true,
        state: "failed",
        attempts: 2,
        last_error: "provider teardown did not settle",
        updated_at: "2026-08-10T09:35:00Z",
      },
    ],
    unread_result: true,
  }),
  graphWork({
    work_id: "historical-finished",
    title: "Historical finished Work",
    status: "done",
    progress_mode: undefined,
    attention_pending: false,
    unread_result: false,
  }),
];

export const GRAPH_PRODUCTION_OWNERS: WorkGraphOwner[] = [
  GRAPH_OWNER_RUNNING,
  GRAPH_OWNER_CORRECTION,
  GRAPH_OWNER_FAILED,
];

export function graphWork(
  overrides: Partial<BrainActiveWork> = {},
): BrainActiveWork {
  return {
    work_id: "work-default",
    revision: 1,
    title: "Prepare release",
    status: "running",
    progress_mode: "ready",
    attention_pending: true,
    unread_result: false,
    ...overrides,
  };
}
