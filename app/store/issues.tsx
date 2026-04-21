import React, { createContext, useContext, useReducer, type ReactNode } from "react";

export type Frontmatter = {
  id: string;
  created: string;
  done?: string | null;
  dispatched?: string | null;
  agent_session?: string;
  extra?: Record<string, unknown>;
  [key: string]: unknown;
};

export type Mention = {
  role: string;
  session?: string;
  index: number;
};

export type Issue = {
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

export type IssuesState = {
  byKey: Record<string, Issue>;
  byProject: Record<string, string[]>;
  executorsByServer: Record<string, string[]>;
};

export const initialIssuesState: IssuesState = {
  byKey: {},
  byProject: {},
  executorsByServer: {},
};

type RawIssue = {
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
      type: "ISSUES_SNAPSHOT";
      serverId: string;
      serverName: string;
      serverUrl: string;
      issues: RawIssue[];
      executors: string[];
    }
  | {
      type: "ISSUE_CHANGED";
      serverId: string;
      serverName: string;
      serverUrl: string;
      issue: RawIssue;
    }
  | { type: "ISSUE_DELETED"; serverId: string; id?: string; path?: string }
  | { type: "EXECUTORS_LOADED"; serverId: string; executors: string[] }
  | { type: "REMOVE_SERVER"; serverId: string };

function makeIssueKey(serverId: string, issueId: string) {
  return `${serverId}:${issueId}`;
}

function makeProjectKey(issue: Pick<Issue, "serverId" | "project">) {
  return `${issue.serverId}:${issue.project}`;
}

function normalizeTimestamp(value: RawIssue["mtime"]): string {
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

function normalizeIssue(
  raw: RawIssue,
  serverId: string,
  serverName: string,
  serverUrl: string,
): Issue {
  const id = String(raw.id || "");
  const frontmatter = raw.frontmatter || {};
  const created = typeof frontmatter.created === "string"
    ? frontmatter.created
    : new Date(0).toISOString();
  return {
    key: makeIssueKey(serverId, id),
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
      dispatched:
        typeof frontmatter.dispatched === "string"
          ? frontmatter.dispatched
          : frontmatter.dispatched ?? null,
      agent_session:
        typeof frontmatter.agent_session === "string"
          ? frontmatter.agent_session
          : undefined,
    },
    mentions: Array.isArray(raw.mentions) ? raw.mentions : [],
    mtime: normalizeTimestamp(raw.mtime),
  };
}

function groupByProject(byKey: Record<string, Issue>) {
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
      const leftIssue = byKey[left];
      const rightIssue = byKey[right];
      const leftCreated = Date.parse(leftIssue?.frontmatter.created || "");
      const rightCreated = Date.parse(rightIssue?.frontmatter.created || "");
      return (Number.isNaN(rightCreated) ? 0 : rightCreated) - (Number.isNaN(leftCreated) ? 0 : leftCreated);
    });
  }

  return out;
}

export function issuesReducer(state: IssuesState, action: Action): IssuesState {
  switch (action.type) {
    case "ISSUES_SNAPSHOT": {
      const nextByKey = Object.fromEntries(
        Object.entries(state.byKey).filter(([key]) => !key.startsWith(`${action.serverId}:`)),
      );
      for (const rawIssue of action.issues) {
        const normalized = normalizeIssue(rawIssue, action.serverId, action.serverName, action.serverUrl);
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
    case "ISSUE_CHANGED": {
      const normalized = normalizeIssue(action.issue, action.serverId, action.serverName, action.serverUrl);
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
    case "ISSUE_DELETED": {
      const nextByKey = { ...state.byKey };
      if (action.id) {
        delete nextByKey[makeIssueKey(action.serverId, action.id)];
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

const IssuesContext = createContext<{
  state: IssuesState;
  dispatch: React.Dispatch<Action>;
} | null>(null);

export function IssuesProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(issuesReducer, initialIssuesState);
  return (
    <IssuesContext.Provider value={{ state, dispatch }}>
      {children}
    </IssuesContext.Provider>
  );
}

export function useIssues() {
  const ctx = useContext(IssuesContext);
  if (!ctx) {
    throw new Error("useIssues must be used within IssuesProvider");
  }
  return ctx;
}
