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

export type ProviderActivityStatus =
  "running" | "completed" | "failed" | "interrupted" | "cancelled";

/**
 * Provider-neutral executor lifecycle. Transcript events are deliberately not
 * part of this record: partial text and tool rendering cannot open or settle
 * Activity.
 */
export interface ProviderActivity {
  id: string;
  status: ProviderActivityStatus;
  started_at: string;
  settled_at?: string;
}

export interface CodexPlanStep {
  step: string;
  status: CodexPlanStepStatus;
}

export type CodexConversationFileOperation = "add" | "delete" | "update";

export interface CodexConversationFileChange {
  path: string;
  move_path?: string;
  operation: CodexConversationFileOperation;
  additions?: number;
  deletions?: number;
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
  file_changes?: CodexConversationFileChange[];
  explanation?: string;
  plan?: CodexPlanStep[];
  source?: string;
}

export interface CodexConversation {
  available: boolean;
  reason?: string;
  source?: string;
  path?: string;
  session_id?: string;
  cwd?: string;
  updated_at?: string;
  /** The provider's sole lifecycle fact for Working, timer, and Stop. */
  activity?: ProviderActivity;
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
    activity: normalizeProviderActivity(conversation.activity),
    events: Array.isArray(conversation.events)
      ? conversation.events.map(normalizeCodexConversationEvent).filter(Boolean)
      : [],
  };
}

export function normalizeProviderActivity(
  value: unknown,
): ProviderActivity | undefined {
  const activity =
    value && typeof value === "object"
      ? (value as Record<string, unknown>)
      : null;
  if (
    !activity ||
    typeof activity.id !== "string" ||
    !activity.id.trim() ||
    typeof activity.started_at !== "string" ||
    !activity.started_at ||
    !Number.isFinite(Date.parse(activity.started_at))
  ) {
    return undefined;
  }
  const status = normalizeProviderActivityStatus(activity.status);
  if (!status) {
    return undefined;
  }
  return {
    id: activity.id.trim(),
    status,
    started_at: activity.started_at,
    settled_at:
      typeof activity.settled_at === "string" &&
      activity.settled_at &&
      Number.isFinite(Date.parse(activity.settled_at))
        ? activity.settled_at
        : undefined,
  };
}

export function isProviderActivityRunning(
  activity?: ProviderActivity | null,
): activity is ProviderActivity & { status: "running" } {
  return activity?.status === "running";
}

export function isProviderActivityTerminal(
  activity?: ProviderActivity | null,
): boolean {
  return Boolean(
    activity &&
    (activity.status === "completed" ||
      activity.status === "failed" ||
      activity.status === "interrupted" ||
      activity.status === "cancelled"),
  );
}

function normalizeProviderActivityStatus(
  value: unknown,
): ProviderActivityStatus | undefined {
  switch (value) {
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
  const id =
    typeof event.id === "string" && event.id
      ? event.id
      : `${kind}:${event.seq ?? ""}`;
  return {
    id,
    seq:
      typeof event.seq === "number" && Number.isFinite(event.seq)
        ? event.seq
        : 0,
    timestamp:
      typeof event.timestamp === "string" ? event.timestamp : undefined,
    kind,
    role:
      event.role === "user" || event.role === "assistant"
        ? event.role
        : undefined,
    title: typeof event.title === "string" ? event.title : undefined,
    body: typeof event.body === "string" ? event.body : undefined,
    command: typeof event.command === "string" ? event.command : undefined,
    tool_name:
      typeof event.tool_name === "string" ? event.tool_name : undefined,
    input: typeof event.input === "string" ? event.input : undefined,
    output: typeof event.output === "string" ? event.output : undefined,
    call_id: typeof event.call_id === "string" ? event.call_id : undefined,
    exit_code:
      typeof event.exit_code === "number" && Number.isFinite(event.exit_code)
        ? event.exit_code
        : undefined,
    status: typeof event.status === "string" ? event.status : undefined,
    partial: typeof event.partial === "boolean" ? event.partial : undefined,
    transient:
      typeof event.transient === "boolean" ? event.transient : undefined,
    files: Array.isArray(event.files)
      ? event.files.filter(
          (file: unknown): file is string => typeof file === "string",
        )
      : undefined,
    file_changes: Array.isArray(event.file_changes)
      ? event.file_changes
          .map(normalizeFileChange)
          .filter(
            (
              change: CodexConversationFileChange | null,
            ): change is CodexConversationFileChange => Boolean(change),
          )
      : undefined,
    explanation:
      typeof event.explanation === "string" ? event.explanation : undefined,
    plan: Array.isArray(event.plan)
      ? event.plan
          .map(normalizePlanStep)
          .filter((step: CodexPlanStep | null): step is CodexPlanStep =>
            Boolean(step),
          )
      : undefined,
    source: typeof event.source === "string" ? event.source : undefined,
  };
}

function normalizeFileChange(
  value: unknown,
): CodexConversationFileChange | null {
  const change =
    value && typeof value === "object"
      ? (value as Record<string, unknown>)
      : null;
  if (!change || typeof change.path !== "string" || !change.path.trim()) {
    return null;
  }
  const operation = normalizeFileOperation(change.operation);
  if (!operation) {
    return null;
  }
  const additions = normalizeLineCount(change.additions);
  const deletions = normalizeLineCount(change.deletions);
  return {
    path: change.path.trim(),
    move_path:
      typeof change.move_path === "string" && change.move_path.trim()
        ? change.move_path.trim()
        : undefined,
    operation,
    additions,
    deletions,
  };
}

function normalizeFileOperation(
  value: unknown,
): CodexConversationFileOperation | undefined {
  switch (value) {
    case "add":
    case "delete":
    case "update":
      return value;
    default:
      return undefined;
  }
}

function normalizeLineCount(value: unknown): number | undefined {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0
    ? value
    : undefined;
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
