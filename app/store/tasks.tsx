import React, { createContext, useContext, useReducer, ReactNode } from "react";
import type { IssueStatus, IssuePriority } from "../constants/tokens";
import { normalizeIssuePrefix } from "../services/taskIdentity";

export type { IssueStatus, IssuePriority };

export interface Attachment {
  name: string;
  path: string;
}

export interface TaskComment {
  id: string;
  body: string;
  attachments: Attachment[];
  authorKind: string;
  authorLabel: string;
  parentId?: string;
  deliveryMode?: string;
  runId?: string;
  agentSessionId?: string;
  targetLabel?: string;
  createdAt: number;
}

export interface Task {
  id: string;
  identifierPrefix: string;
  number: number;
  serverId: string;
  serverName: string;
  title: string;
  description: string;
  attachments: Attachment[];
  status: IssueStatus;
  priority: IssuePriority;
  labels: string[];
  projectId?: string;
  dueDate?: string;
  cwd?: string;
  currentRunId?: string;
  lastRunStatus?: string;
  comments: TaskComment[];
  createdAt: number;
  updatedAt: number;
}

export interface Run {
  id: string;
  serverId: string;
  serverName: string;
  taskId: string;
  attemptNumber: number;
  status: string;
  executionMode: string;
  executorKind?: string;
  executorLabel?: string;
  agentSessionId?: string;
  promptSnapshot?: string;
  summary?: string;
  lastError?: string;
  waitingReason?: string;
  startedAt?: number;
  endedAt?: number;
  createdAt: number;
  updatedAt: number;
}

export interface Guidance {
  preamble: string;
  constraints: string[];
}

export interface Project {
  id: string;
  serverId: string;
  key: string;
  name: string;
  icon: string;
  repoRoot?: string;
  worktreeRoot?: string;
  baseBranch?: string;
}

interface State {
  tasks: Task[];
  runs: Run[];
  projects: Project[];
  guidance: Record<string, Guidance>;
}

type RawTask = {
  id: string;
  identifier_prefix?: string;
  number?: number;
  title: string;
  description?: string;
  attachments?: RawAttachment[];
  status: IssueStatus;
  priority?: number;
  labels?: string[];
  project_id?: string;
  due_date?: string;
  cwd?: string;
  current_run_id?: string;
  last_run_status?: string;
  comments?: RawTaskComment[];
  created_at?: string | number;
  updated_at?: string | number;
};

type RawTaskComment = {
  id: string;
  body?: string;
  attachments?: RawAttachment[];
  author_kind?: string;
  author_label?: string;
  parent_id?: string;
  delivery_mode?: string;
  run_id?: string;
  agent_session_id?: string;
  target_label?: string;
  created_at?: string | number;
};

type RawAttachment = {
  name?: string;
  path?: string;
};

type RawRun = {
  id: string;
  task_id: string;
  attempt_number?: number;
  status: string;
  execution_mode?: string;
  executor_kind?: string;
  executor_label?: string;
  agent_session_id?: string;
  prompt_snapshot?: string;
  summary?: string;
  last_error?: string;
  waiting_reason?: string;
  started_at?: string | number;
  ended_at?: string | number;
  created_at?: string | number;
  updated_at?: string | number;
};

type RawProject = {
  id: string;
  key?: string;
  name: string;
  icon?: string;
  repo_root?: string;
  worktree_root?: string;
  base_branch?: string;
};

type Action =
  | {
      type: "UPSERT_SERVER_TASKS";
      serverId: string;
      serverName: string;
      tasks: RawTask[];
    }
  | {
      type: "TASK_CREATED";
      serverId: string;
      serverName: string;
      task: RawTask;
    }
  | {
      type: "TASK_UPDATED";
      serverId: string;
      serverName: string;
      task: RawTask;
    }
  | { type: "TASK_DELETED"; serverId: string; taskId: string }
  | {
      type: "UPSERT_SERVER_RUNS";
      serverId: string;
      serverName: string;
      runs: RawRun[];
    }
  | { type: "RUN_CREATED"; serverId: string; serverName: string; run: RawRun }
  | { type: "RUN_UPDATED"; serverId: string; serverName: string; run: RawRun }
  | { type: "UPSERT_SERVER_PROJECTS"; serverId: string; projects: RawProject[] }
  | { type: "PROJECT_CREATED"; serverId: string; project: RawProject }
  | { type: "PROJECT_UPDATED"; serverId: string; project: RawProject }
  | { type: "PROJECT_DELETED"; serverId: string; projectId: string }
  | { type: "SET_GUIDANCE"; serverId: string; guidance: Guidance }
  | { type: "REMOVE_SERVER"; serverId: string };

const initialState: State = {
  tasks: [],
  runs: [],
  projects: [],
  guidance: {},
};

function normalizeTask(
  raw: RawTask,
  serverId: string,
  serverName: string,
): Task {
  return {
    id: raw.id,
    identifierPrefix: normalizeIssuePrefix(raw.identifier_prefix),
    number: raw.number || 0,
    serverId,
    serverName,
    title: raw.title,
    description: raw.description || "",
    attachments: Array.isArray(raw.attachments)
      ? raw.attachments.map(normalizeAttachment)
      : [],
    status: raw.status,
    priority: (raw.priority || 0) as IssuePriority,
    labels: raw.labels || [],
    projectId: raw.project_id,
    dueDate: raw.due_date || "",
    cwd: raw.cwd,
    currentRunId: raw.current_run_id,
    lastRunStatus: raw.last_run_status,
    comments: Array.isArray(raw.comments)
      ? raw.comments.map(normalizeTaskComment)
      : [],
    createdAt: normalizeTimestamp(raw.created_at),
    updatedAt: normalizeTimestamp(raw.updated_at),
  };
}

function normalizeTaskComment(raw: RawTaskComment): TaskComment {
  return {
    id: raw.id,
    body: raw.body || "",
    attachments: Array.isArray(raw.attachments)
      ? raw.attachments.map(normalizeAttachment)
      : [],
    authorKind: raw.author_kind || "user",
    authorLabel: raw.author_label || "",
    parentId: raw.parent_id,
    deliveryMode: raw.delivery_mode,
    runId: raw.run_id,
    agentSessionId: raw.agent_session_id,
    targetLabel: raw.target_label,
    createdAt: normalizeTimestamp(raw.created_at),
  };
}

function normalizeAttachment(raw: RawAttachment): Attachment {
  return {
    name: raw.name || "",
    path: raw.path || "",
  };
}

function normalizeRun(raw: RawRun, serverId: string, serverName: string): Run {
  return {
    id: raw.id,
    serverId,
    serverName,
    taskId: raw.task_id,
    attemptNumber: raw.attempt_number || 0,
    status: raw.status,
    executionMode: raw.execution_mode || "",
    executorKind: raw.executor_kind,
    executorLabel: raw.executor_label,
    agentSessionId: raw.agent_session_id,
    promptSnapshot: raw.prompt_snapshot,
    summary: raw.summary,
    lastError: raw.last_error,
    waitingReason: raw.waiting_reason,
    startedAt: raw.started_at ? normalizeTimestamp(raw.started_at) : undefined,
    endedAt: raw.ended_at ? normalizeTimestamp(raw.ended_at) : undefined,
    createdAt: normalizeTimestamp(raw.created_at),
    updatedAt: normalizeTimestamp(raw.updated_at),
  };
}

function normalizeProject(raw: RawProject, serverId: string): Project {
  return {
    id: raw.id,
    serverId,
    key: normalizeIssuePrefix(raw.key),
    name: raw.name,
    icon: raw.icon || "",
    repoRoot: raw.repo_root || "",
    worktreeRoot: raw.worktree_root || "",
    baseBranch: raw.base_branch || "",
  };
}

function normalizeTimestamp(value?: string | number): number {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value > 10_000_000_000 ? value : value * 1000;
  }
  if (typeof value === "string") {
    const parsed = Date.parse(value);
    if (!Number.isNaN(parsed)) return parsed;
  }
  return Date.now();
}

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case "UPSERT_SERVER_TASKS": {
      const normalized = action.tasks.map((t) =>
        normalizeTask(t, action.serverId, action.serverName),
      );
      return {
        ...state,
        tasks: [
          ...state.tasks.filter((t) => t.serverId !== action.serverId),
          ...normalized,
        ],
      };
    }
    case "TASK_CREATED": {
      const task = normalizeTask(
        action.task,
        action.serverId,
        action.serverName,
      );
      return {
        ...state,
        tasks: [
          ...state.tasks.filter(
            (t) => !(t.id === task.id && t.serverId === task.serverId),
          ),
          task,
        ],
      };
    }
    case "TASK_UPDATED": {
      const task = normalizeTask(
        action.task,
        action.serverId,
        action.serverName,
      );
      const exists = state.tasks.some(
        (t) => t.id === task.id && t.serverId === task.serverId,
      );
      return {
        ...state,
        tasks: exists
          ? state.tasks.map((t) =>
              t.id === task.id && t.serverId === task.serverId ? task : t,
            )
          : [...state.tasks, task],
      };
    }
    case "TASK_DELETED":
      return {
        ...state,
        tasks: state.tasks.filter(
          (t) => !(t.id === action.taskId && t.serverId === action.serverId),
        ),
      };
    case "UPSERT_SERVER_RUNS": {
      const normalized = action.runs.map((r) =>
        normalizeRun(r, action.serverId, action.serverName),
      );
      return {
        ...state,
        runs: [
          ...state.runs.filter((r) => r.serverId !== action.serverId),
          ...normalized,
        ],
      };
    }
    case "RUN_CREATED": {
      const run = normalizeRun(action.run, action.serverId, action.serverName);
      return {
        ...state,
        runs: [
          ...state.runs.filter(
            (r) => !(r.id === run.id && r.serverId === run.serverId),
          ),
          run,
        ],
      };
    }
    case "RUN_UPDATED": {
      const run = normalizeRun(action.run, action.serverId, action.serverName);
      const exists = state.runs.some(
        (r) => r.id === run.id && r.serverId === run.serverId,
      );
      return {
        ...state,
        runs: exists
          ? state.runs.map((r) =>
              r.id === run.id && r.serverId === run.serverId ? run : r,
            )
          : [...state.runs, run],
      };
    }
    case "UPSERT_SERVER_PROJECTS": {
      const normalized = action.projects.map((p) =>
        normalizeProject(p, action.serverId),
      );
      return {
        ...state,
        projects: [
          ...state.projects.filter((p) => p.serverId !== action.serverId),
          ...normalized,
        ],
      };
    }
    case "PROJECT_CREATED": {
      const project = normalizeProject(action.project, action.serverId);
      return {
        ...state,
        projects: [
          ...state.projects.filter(
            (p) => !(p.id === project.id && p.serverId === project.serverId),
          ),
          project,
        ],
      };
    }
    case "PROJECT_UPDATED": {
      const project = normalizeProject(action.project, action.serverId);
      const exists = state.projects.some(
        (p) => p.id === project.id && p.serverId === project.serverId,
      );
      return {
        ...state,
        projects: exists
          ? state.projects.map((p) =>
              p.id === project.id && p.serverId === project.serverId
                ? project
                : p,
            )
          : [...state.projects, project],
      };
    }
    case "PROJECT_DELETED":
      return {
        ...state,
        tasks: state.tasks.map((task) =>
          task.serverId === action.serverId && task.projectId === action.projectId
            ? { ...task, projectId: "" }
            : task,
        ),
        projects: state.projects.filter(
          (p) => !(p.id === action.projectId && p.serverId === action.serverId),
        ),
      };
    case "SET_GUIDANCE":
      return {
        ...state,
        guidance: { ...state.guidance, [action.serverId]: action.guidance },
      };
    case "REMOVE_SERVER":
      return {
        ...state,
        tasks: state.tasks.filter((t) => t.serverId !== action.serverId),
        runs: state.runs.filter((r) => r.serverId !== action.serverId),
        projects: state.projects.filter((p) => p.serverId !== action.serverId),
        guidance: Object.fromEntries(
          Object.entries(state.guidance).filter(
            ([id]) => id !== action.serverId,
          ),
        ),
      };
    default:
      return state;
  }
}

const TaskContext = createContext<{
  state: State;
  dispatch: React.Dispatch<Action>;
} | null>(null);

export function TaskProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, initialState);
  return (
    <TaskContext.Provider value={{ state, dispatch }}>
      {children}
    </TaskContext.Provider>
  );
}

export function useTasks() {
  const ctx = useContext(TaskContext);
  if (!ctx) throw new Error("useTasks must be used within TaskProvider");
  return ctx;
}
