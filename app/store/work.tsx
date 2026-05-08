import React, { createContext, useContext, useReducer, type ReactNode } from "react";

export type Frontmatter = {
  id: string;
  created: string;
  done?: string | null;
  started?: string | null;
  status?: string;
  title?: string;
  summary?: string;
  progress?: string[];
  next?: string;
  agent_session?: string;
  cwd?: string;
  command?: string;
  ai_provider?: string;
  ai_updated?: string | null;
  ai_hash?: string;
  ai_error?: string;
  extra?: Record<string, unknown>;
  [key: string]: unknown;
};

export type Mention = {
  role: string;
  session?: string;
  index: number;
};

export type WorkItem = {
  key: string;
  serverId: string;
  serverName: string;
  serverUrl: string;
  id: string;
  path: string;
  project: string;
  title: string;
  body: string;
  frontmatter: Frontmatter;
  mentions: Mention[];
  mtime: string;
};

export type WorkState = {
  byKey: Record<string, WorkItem>;
  byProject: Record<string, string[]>;
  executorsByServer: Record<string, string[]>;
};

export const initialWorkState: WorkState = {
  byKey: {},
  byProject: {},
  executorsByServer: {},
};

type RawWorkItem = {
  id: string;
  path?: string;
  project?: string;
  title?: string;
  body?: string;
  frontmatter?: Partial<Frontmatter> | null;
  mentions?: Mention[] | null;
  mtime?: string | number | Date | null;
};

type Action =
  | {
      type: "WORK_ITEMS_SNAPSHOT";
      serverId: string;
      serverName: string;
      serverUrl: string;
      workItems: RawWorkItem[];
      executors: string[];
    }
  | {
      type: "WORK_ITEM_CHANGED";
      serverId: string;
      serverName: string;
      serverUrl: string;
      workItem: RawWorkItem;
    }
  | { type: "WORK_ITEM_DELETED"; serverId: string; id?: string; path?: string }
  | { type: "EXECUTORS_LOADED"; serverId: string; executors: string[] }
  | { type: "REMOVE_SERVER"; serverId: string };

function makeWorkItemKey(serverId: string, itemId: string) {
  return `${serverId}:${itemId}`;
}

function makeProjectKey(item: Pick<WorkItem, "serverId" | "project">) {
  return `${item.serverId}:${item.project}`;
}

function normalizeTimestamp(value: RawWorkItem["mtime"]): string {
  if (typeof value === "string" && value.trim()) {
    const parsed = Date.parse(value);
    return Number.isNaN(parsed) ? value : new Date(parsed).toISOString();
  }

  if (typeof value === "number" && Number.isFinite(value)) {
    const millis = value > 10_000_000_000 ? value : value * 1000;
    return new Date(millis).toISOString();
  }

  if (value instanceof Date) {
    return value.toISOString();
  }

  return new Date(0).toISOString();
}

function normalizeWorkItem(
  raw: RawWorkItem,
  serverId: string,
  serverName: string,
  serverUrl: string,
): WorkItem {
  const id = String(raw.id || "");
  const frontmatter = raw.frontmatter || {};
  const created = typeof frontmatter.created === "string"
    ? frontmatter.created
    : new Date(0).toISOString();
  return {
    key: makeWorkItemKey(serverId, id),
    serverId,
    serverName,
    serverUrl,
    id,
    path: raw.path || "",
    project: raw.project || "inbox",
    title: raw.title || "",
    body: raw.body || "",
    frontmatter: {
      ...frontmatter,
      id: typeof frontmatter.id === "string" && frontmatter.id ? frontmatter.id : id,
      created,
      done: typeof frontmatter.done === "string" ? frontmatter.done : frontmatter.done ?? null,
      started: typeof frontmatter.started === "string" ? frontmatter.started : frontmatter.started ?? null,
      status:
        typeof frontmatter.status === "string"
          ? frontmatter.status.trim()
          : undefined,
      title:
        typeof frontmatter.title === "string"
          ? frontmatter.title.trim()
          : undefined,
      summary:
        typeof frontmatter.summary === "string"
          ? frontmatter.summary.trim()
          : undefined,
      progress: Array.isArray(frontmatter.progress)
        ? frontmatter.progress.filter((item): item is string => typeof item === "string")
        : undefined,
      next:
        typeof frontmatter.next === "string"
          ? frontmatter.next.trim()
          : undefined,
      agent_session:
        typeof frontmatter.agent_session === "string"
          ? frontmatter.agent_session
          : undefined,
      cwd:
        typeof frontmatter.cwd === "string" ? frontmatter.cwd.trim() : undefined,
      command:
        typeof frontmatter.command === "string"
          ? frontmatter.command.trim()
          : undefined,
      ai_provider:
        typeof frontmatter.ai_provider === "string"
          ? frontmatter.ai_provider.trim()
          : undefined,
      ai_updated:
        typeof frontmatter.ai_updated === "string"
          ? frontmatter.ai_updated
          : frontmatter.ai_updated ?? null,
      ai_hash:
        typeof frontmatter.ai_hash === "string"
          ? frontmatter.ai_hash.trim()
          : undefined,
      ai_error:
        typeof frontmatter.ai_error === "string"
          ? frontmatter.ai_error.trim()
          : undefined,
    },
    mentions: Array.isArray(raw.mentions) ? raw.mentions : [],
    mtime: normalizeTimestamp(raw.mtime),
  };
}

function groupByProject(byKey: Record<string, WorkItem>) {
  const out: Record<string, string[]> = {};
  for (const current of Object.values(byKey)) {
    const key = makeProjectKey(current);
    if (!out[key]) {
      out[key] = [];
    }
    out[key].push(current.key);
  }

  for (const key of Object.keys(out)) {
    out[key].sort((left, right) => {
      const leftItem = byKey[left];
      const rightItem = byKey[right];
      const leftCreated = Date.parse(leftItem?.frontmatter.created || "");
      const rightCreated = Date.parse(rightItem?.frontmatter.created || "");
      return (Number.isNaN(rightCreated) ? 0 : rightCreated) - (Number.isNaN(leftCreated) ? 0 : leftCreated);
    });
  }

  return out;
}

export function workReducer(state: WorkState, action: Action): WorkState {
  switch (action.type) {
    case "WORK_ITEMS_SNAPSHOT": {
      const nextByKey = Object.fromEntries(
        Object.entries(state.byKey).filter(([key]) => !key.startsWith(`${action.serverId}:`)),
      );
      for (const rawItem of action.workItems) {
        const normalized = normalizeWorkItem(rawItem, action.serverId, action.serverName, action.serverUrl);
        nextByKey[normalized.key] = normalized;
      }
      return {
        byKey: nextByKey,
        byProject: groupByProject(nextByKey),
        executorsByServer: {
          ...state.executorsByServer,
          [action.serverId]: action.executors,
        },
      };
    }
    case "WORK_ITEM_CHANGED": {
      const normalized = normalizeWorkItem(action.workItem, action.serverId, action.serverName, action.serverUrl);
      const nextByKey = {
        ...state.byKey,
        [normalized.key]: normalized,
      };
      return {
        ...state,
        byKey: nextByKey,
        byProject: groupByProject(nextByKey),
      };
    }
    case "WORK_ITEM_DELETED": {
      const nextByKey = { ...state.byKey };
      if (action.id) {
        delete nextByKey[makeWorkItemKey(action.serverId, action.id)];
      } else if (action.path) {
        for (const [key, value] of Object.entries(nextByKey)) {
          if (value.serverId === action.serverId && value.path === action.path) {
            delete nextByKey[key];
          }
        }
      }
      return {
        ...state,
        byKey: nextByKey,
        byProject: groupByProject(nextByKey),
      };
    }
    case "EXECUTORS_LOADED":
      return {
        ...state,
        executorsByServer: {
          ...state.executorsByServer,
          [action.serverId]: action.executors,
        },
      };
    case "REMOVE_SERVER": {
      const nextByKey = Object.fromEntries(
        Object.entries(state.byKey).filter(([, value]) => value.serverId !== action.serverId),
      );
      return {
        byKey: nextByKey,
        byProject: groupByProject(nextByKey),
        executorsByServer: Object.fromEntries(
          Object.entries(state.executorsByServer).filter(([serverId]) => serverId !== action.serverId),
        ),
      };
    }
    default:
      return state;
  }
}

const WorkContext = createContext<{
  state: WorkState;
  dispatch: React.Dispatch<Action>;
} | null>(null);

export function WorkProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(workReducer, initialWorkState);
  return (
    <WorkContext.Provider value={{ state, dispatch }}>
      {children}
    </WorkContext.Provider>
  );
}

export function useWork() {
  const ctx = useContext(WorkContext);
  if (!ctx) {
    throw new Error("useWork must be used within WorkProvider");
  }
  return ctx;
}
