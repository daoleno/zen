import React, { createContext, useContext, useReducer, type ReactNode } from "react";

export type Frontmatter = {
  id: string;
  kind?: string;
  created: string;
  done?: string | null;
  started?: string | null;
  status?: string;
  title?: string;
  agent_session?: string;
  extra?: Record<string, unknown>;
  [key: string]: unknown;
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
  mtime: string;
};

export type WorkState = {
  byKey: Record<string, WorkItem>;
  byProject: Record<string, string[]>;
};

export const initialWorkState: WorkState = {
  byKey: {},
  byProject: {},
};

type RawWorkItem = {
  id: string;
  path?: string;
  project?: string;
  title?: string;
  body?: string;
  frontmatter?: Partial<Frontmatter> | null;
  mtime?: string | number | Date | null;
};

type Action =
  | {
      type: "WORK_ITEMS_SNAPSHOT";
      serverId: string;
      serverName: string;
      serverUrl: string;
      workItems: RawWorkItem[];
    }
  | {
      type: "WORK_ITEM_CHANGED";
      serverId: string;
      serverName: string;
      serverUrl: string;
      workItem: RawWorkItem;
    }
  | { type: "WORK_ITEM_DELETED"; serverId: string; id?: string; path?: string }
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
      kind:
        typeof frontmatter.kind === "string"
          ? frontmatter.kind.trim()
          : undefined,
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
      agent_session:
        typeof frontmatter.agent_session === "string"
          ? frontmatter.agent_session
          : undefined,
    },
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
      const previousServerItemCount = Object.values(state.byKey).filter(
        (item) => item.serverId === action.serverId,
      ).length;
      let itemChanged = previousServerItemCount !== action.workItems.length;
      const nextByKey = Object.fromEntries(
        Object.entries(state.byKey).filter(([key]) => !key.startsWith(`${action.serverId}:`)),
      );
      for (const rawItem of action.workItems) {
        const normalized = normalizeWorkItem(rawItem, action.serverId, action.serverName, action.serverUrl);
        const previous = state.byKey[normalized.key];
        if (previous && workItemsEqual(previous, normalized)) {
          nextByKey[normalized.key] = previous;
        } else {
          itemChanged = true;
          nextByKey[normalized.key] = normalized;
        }
      }
      if (!itemChanged) {
        return state;
      }
      return {
        byKey: nextByKey,
        byProject: groupByProject(nextByKey),
      };
    }
    case "WORK_ITEM_CHANGED": {
      const normalized = normalizeWorkItem(action.workItem, action.serverId, action.serverName, action.serverUrl);
      const previous = state.byKey[normalized.key];
      if (previous && workItemsEqual(previous, normalized)) {
        return state;
      }
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
      let deleted = false;
      if (action.id) {
        const key = makeWorkItemKey(action.serverId, action.id);
        deleted = key in nextByKey;
        delete nextByKey[key];
      } else if (action.path) {
        for (const [key, value] of Object.entries(nextByKey)) {
          if (value.serverId === action.serverId && value.path === action.path) {
            delete nextByKey[key];
            deleted = true;
          }
        }
      }
      if (!deleted) {
        return state;
      }
      return {
        ...state,
        byKey: nextByKey,
        byProject: groupByProject(nextByKey),
      };
    }
    case "REMOVE_SERVER": {
      const hasServerItems = Object.values(state.byKey).some(
        (value) => value.serverId === action.serverId,
      );
      if (!hasServerItems) {
        return state;
      }
      const nextByKey = Object.fromEntries(
        Object.entries(state.byKey).filter(([, value]) => value.serverId !== action.serverId),
      );
      return {
        byKey: nextByKey,
        byProject: groupByProject(nextByKey),
      };
    }
    default:
      return state;
  }
}

function workItemsEqual(left: WorkItem, right: WorkItem): boolean {
  return (
    left === right ||
    (
      left.key === right.key &&
      left.serverId === right.serverId &&
      left.serverName === right.serverName &&
      left.serverUrl === right.serverUrl &&
      left.id === right.id &&
      left.path === right.path &&
      left.project === right.project &&
      left.title === right.title &&
      left.body === right.body &&
      frontmatterEqual(left.frontmatter, right.frontmatter) &&
      left.mtime === right.mtime
    )
  );
}

function frontmatterEqual(left: Frontmatter, right: Frontmatter): boolean {
  return (
    left === right ||
    (
      left.id === right.id &&
      left.kind === right.kind &&
      left.created === right.created &&
      left.done === right.done &&
      left.started === right.started &&
      left.status === right.status &&
      left.title === right.title &&
      left.agent_session === right.agent_session &&
      left.extra === right.extra &&
      extraFrontmatterFieldsEqual(left, right)
    )
  );
}

const knownFrontmatterKeys = new Set([
  "id",
  "kind",
  "created",
  "done",
  "started",
  "status",
  "title",
  "agent_session",
  "extra",
]);

function extraFrontmatterFieldsEqual(
  left: Frontmatter,
  right: Frontmatter,
): boolean {
  const leftKeys = Object.keys(left).filter(key => !knownFrontmatterKeys.has(key));
  const rightKeys = Object.keys(right).filter(key => !knownFrontmatterKeys.has(key));
  if (leftKeys.length !== rightKeys.length) {
    return false;
  }
  for (const key of leftKeys) {
    if (!Object.prototype.hasOwnProperty.call(right, key) || left[key] !== right[key]) {
      return false;
    }
  }
  return true;
}

const WorkStateContext = createContext<WorkState | null>(null);
const WorkDispatchContext = createContext<React.Dispatch<Action> | null>(null);

export function WorkProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(workReducer, initialWorkState);
  return (
    <WorkDispatchContext.Provider value={dispatch}>
      <WorkStateContext.Provider value={state}>
        {children}
      </WorkStateContext.Provider>
    </WorkDispatchContext.Provider>
  );
}

export function useWork() {
  const state = useContext(WorkStateContext);
  const dispatch = useContext(WorkDispatchContext);
  if (!state || !dispatch) {
    throw new Error("useWork must be used within WorkProvider");
  }
  return { state, dispatch };
}

export function useWorkDispatch() {
  const dispatch = useContext(WorkDispatchContext);
  if (!dispatch) {
    throw new Error("useWorkDispatch must be used within WorkProvider");
  }
  return dispatch;
}
