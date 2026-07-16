export type CodexConversationEventKind =
  | "user_message"
  | "assistant_message"
  | "commentary"
  | "command"
  | "tool"
  | "web_search"
  | "patch"
  | "plan"
  | "status";

export type CodexConversationRole = "user" | "assistant";
export type CodexPlanStepStatus = "pending" | "in_progress" | "completed";

export type StructuredTurnStatus =
  | "queued"
  | "running"
  | "completed"
  | "failed"
  | "interrupted"
  | "cancelled";

/**
 * Provider-neutral executor lifecycle. Transcript events are deliberately not
 * part of this record: partial text and tool rendering cannot open or settle a
 * turn.
 */
export interface StructuredTurn {
  id: string;
  status: StructuredTurnStatus;
  started_at: string;
  settled_at?: string;
  control_id?: string;
}

export interface CodexPlanStep {
  step: string;
  status: CodexPlanStepStatus;
}

export interface CodexConversationEvent {
  id: string;
  seq: number;
  timestamp?: string;
  kind: CodexConversationEventKind;
  role?: CodexConversationRole;
  title?: string;
  body?: string;
  command?: string;
  tool_name?: string;
  input?: string;
  output?: string;
  call_id?: string;
  exit_code?: number;
  status?: string;
  partial?: boolean;
  transient?: boolean;
  files?: string[];
  explanation?: string;
  plan?: CodexPlanStep[];
  source?: string;
  position?: number;
  event_revision?: number;
  activity_id?: string;
  submission_id?: string;
  submission_state?: "accepted" | "queued" | "delivered" | "unconfirmed" | "rejected";
}

export interface CodexConversation {
  available: boolean;
  reason?: string;
  source?: string;
  path?: string;
  session_id?: string;
  cwd?: string;
  updated_at?: string;
  active?: boolean;
  turn_epoch?: string;
  turn_revision?: number;
  turn?: StructuredTurn;
  /** The sole owner of Working, timer, and Stop. */
  activity?: StructuredTurn;
  queued_turns?: StructuredTurn[];
  events: CodexConversationEvent[];
}

export function normalizeCodexConversation(value: any): CodexConversation {
  const conversation = value && typeof value === "object" ? value : {};
  return {
    available: Boolean(conversation.available),
    reason:
      typeof conversation.reason === "string" ? conversation.reason : undefined,
    source:
      typeof conversation.source === "string" ? conversation.source : undefined,
    path: typeof conversation.path === "string" ? conversation.path : undefined,
    session_id:
      typeof conversation.session_id === "string"
        ? conversation.session_id
        : undefined,
    cwd: typeof conversation.cwd === "string" ? conversation.cwd : undefined,
    updated_at:
      typeof conversation.updated_at === "string"
        ? conversation.updated_at
        : undefined,
    active:
      typeof conversation.active === "boolean"
        ? conversation.active
        : undefined,
    turn_epoch:
      typeof conversation.turn_epoch === "string" && conversation.turn_epoch
        ? conversation.turn_epoch
        : undefined,
    turn_revision:
      typeof conversation.turn_revision === "number" &&
        Number.isFinite(conversation.turn_revision) &&
        conversation.turn_revision >= 0
        ? conversation.turn_revision
        : undefined,
    turn: normalizeStructuredTurn(conversation.turn),
    activity: normalizeStructuredTurn(conversation.activity),
    queued_turns: Array.isArray(conversation.queued_turns)
      ? conversation.queued_turns
          .map((turn: unknown) => normalizeStructuredTurn(turn))
          .filter((turn: StructuredTurn | undefined): turn is StructuredTurn =>
            Boolean(turn),
          )
      : [],
    events: Array.isArray(conversation.events)
      ? conversation.events.map(normalizeCodexConversationEvent).filter(Boolean)
      : [],
  };
}

export function normalizeStructuredTurn(
  value: unknown,
): StructuredTurn | undefined {
  const turn = value && typeof value === "object"
    ? value as Record<string, unknown>
    : null;
  if (
    !turn ||
    typeof turn.id !== "string" ||
    !turn.id.trim() ||
    typeof turn.started_at !== "string" ||
    !turn.started_at ||
    !Number.isFinite(Date.parse(turn.started_at))
  ) {
    return undefined;
  }
  const status = normalizeStructuredTurnStatus(turn.status);
  if (!status) {
    return undefined;
  }
  return {
    id: turn.id.trim(),
    status,
    started_at: turn.started_at,
    settled_at:
      typeof turn.settled_at === "string" &&
      turn.settled_at &&
      Number.isFinite(Date.parse(turn.settled_at))
        ? turn.settled_at
        : undefined,
    control_id:
      typeof turn.control_id === "string" && turn.control_id.trim()
        ? turn.control_id.trim()
        : undefined,
  };
}

export function isStructuredTurnRunning(
  turn?: StructuredTurn | null,
): turn is StructuredTurn & { status: "running" } {
  return turn?.status === "running";
}

export function isStructuredTurnTerminal(
  turn?: StructuredTurn | null,
): boolean {
  return Boolean(
    turn &&
      (turn.status === "completed" ||
        turn.status === "failed" ||
        turn.status === "interrupted" ||
        turn.status === "cancelled"),
  );
}

function normalizeStructuredTurnStatus(
  value: unknown,
): StructuredTurnStatus | undefined {
  switch (value) {
    case "queued":
    case "running":
    case "completed":
    case "failed":
    case "interrupted":
    case "cancelled":
      return value;
    default:
      return undefined;
  }
}

function normalizeCodexConversationEvent(
  value: any,
): CodexConversationEvent | null {
  const event = value && typeof value === "object" ? value : {};
  const kind = normalizeKind(event.kind);
  if (!kind) {
    return null;
  }
  const id = typeof event.id === "string" && event.id ? event.id : `${kind}:${event.seq ?? ""}`;
  return {
    id,
    seq: typeof event.seq === "number" && Number.isFinite(event.seq) ? event.seq : 0,
    timestamp:
      typeof event.timestamp === "string" ? event.timestamp : undefined,
    kind,
    role:
      event.role === "user" || event.role === "assistant"
        ? event.role
        : undefined,
    title: typeof event.title === "string" ? event.title : undefined,
    body: typeof event.body === "string" ? event.body : undefined,
    command:
      typeof event.command === "string" ? event.command : undefined,
    tool_name:
      typeof event.tool_name === "string" ? event.tool_name : undefined,
    input: typeof event.input === "string" ? event.input : undefined,
    output: typeof event.output === "string" ? event.output : undefined,
    call_id:
      typeof event.call_id === "string" ? event.call_id : undefined,
    exit_code:
      typeof event.exit_code === "number" && Number.isFinite(event.exit_code)
        ? event.exit_code
        : undefined,
    status: typeof event.status === "string" ? event.status : undefined,
    partial:
      typeof event.partial === "boolean" ? event.partial : undefined,
    transient:
      typeof event.transient === "boolean" ? event.transient : undefined,
    files: Array.isArray(event.files)
      ? event.files.filter((file: unknown): file is string => typeof file === "string")
      : undefined,
    explanation:
      typeof event.explanation === "string" ? event.explanation : undefined,
    plan: Array.isArray(event.plan)
      ? event.plan
          .map(normalizePlanStep)
          .filter((step: CodexPlanStep | null): step is CodexPlanStep => Boolean(step))
      : undefined,
    source: typeof event.source === "string" ? event.source : undefined,
    position: normalizeNonnegativeNumber(event.position),
    event_revision: normalizeNonnegativeNumber(event.event_revision),
    activity_id:
      typeof event.activity_id === "string" && event.activity_id
        ? event.activity_id
        : undefined,
    submission_id:
      typeof event.submission_id === "string" && event.submission_id
        ? event.submission_id
        : undefined,
    submission_state: normalizeSubmissionState(event.submission_state),
  };
}

function normalizeNonnegativeNumber(value: unknown) {
  return typeof value === "number" && Number.isFinite(value) && value >= 0
    ? value
    : undefined;
}

function normalizeSubmissionState(
  value: unknown,
): CodexConversationEvent["submission_state"] {
  switch (value) {
    case "accepted":
    case "queued":
    case "delivered":
    case "unconfirmed":
    case "rejected":
      return value;
    default:
      return undefined;
  }
}

function normalizePlanStep(value: any): CodexPlanStep | null {
  const step = value && typeof value === "object" ? value : {};
  if (typeof step.step !== "string" || !step.step.trim()) {
    return null;
  }
  const status =
    step.status === "completed" ||
    step.status === "in_progress" ||
    step.status === "pending"
      ? step.status
      : "pending";
  return {
    step: step.step.trim(),
    status,
  };
}

function normalizeKind(value: unknown): CodexConversationEventKind | null {
  switch (value) {
    case "user_message":
    case "assistant_message":
    case "commentary":
    case "command":
    case "tool":
    case "web_search":
    case "patch":
    case "plan":
    case "status":
      return value;
    default:
      return null;
  }
}
