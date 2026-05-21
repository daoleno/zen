export type CodexConversationEventKind =
  | "user_message"
  | "assistant_message"
  | "commentary"
  | "command"
  | "tool"
  | "patch"
  | "plan"
  | "status";

export type CodexConversationRole = "user" | "assistant";
export type CodexPlanStepStatus = "pending" | "in_progress" | "completed";

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
  files?: string[];
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
    events: Array.isArray(conversation.events)
      ? conversation.events.map(normalizeCodexConversationEvent).filter(Boolean)
      : [],
  };
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
  };
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
    case "patch":
    case "plan":
    case "status":
      return value;
    default:
      return null;
  }
}
