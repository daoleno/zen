import React, { createContext, useContext, useReducer, type ReactNode } from "react";

export type Frontmatter = {
  id: string;
  kind?: string;
  created: string;
  done?: string | null;
  started?: string | null;
  status?: string;
  title?: string;
  outcome?: string;
  summary?: string;
  progress?: string[];
  friction?: string;
  cause?: string;
  insight?: string;
  next?: string;
  agent_source?: string;
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
  digestProviderByServer: Record<string, string>;
};

export const initialWorkState: WorkState = {
  byKey: {},
  byProject: {},
  executorsByServer: {},
  digestProviderByServer: {},
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
      digestProvider?: string;
    }
  | {
      type: "WORK_ITEM_CHANGED";
      serverId: string;
      serverName: string;
      serverUrl: string;
      workItem: RawWorkItem;
    }
  | { type: "WORK_ITEM_DELETED"; serverId: string; id?: string; path?: string }
  | {
      type: "EXECUTORS_LOADED";
      serverId: string;
      executors: string[];
      digestProvider?: string;
    }
  | { type: "WORK_DIGEST_PROVIDER_SET"; serverId: string; provider: string }
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
      outcome:
        typeof frontmatter.outcome === "string"
          ? frontmatter.outcome.trim()
          : undefined,
      summary:
        typeof frontmatter.summary === "string"
          ? frontmatter.summary.trim()
          : undefined,
      progress: Array.isArray(frontmatter.progress)
        ? frontmatter.progress.filter((item): item is string => typeof item === "string")
        : undefined,
      friction:
        typeof frontmatter.friction === "string"
          ? frontmatter.friction.trim()
          : undefined,
      cause:
        typeof frontmatter.cause === "string"
          ? frontmatter.cause.trim()
          : undefined,
      insight:
        typeof frontmatter.insight === "string"
          ? frontmatter.insight.trim()
          : undefined,
      next:
        typeof frontmatter.next === "string"
          ? frontmatter.next.trim()
          : undefined,
      agent_source:
        typeof frontmatter.agent_source === "string"
          ? frontmatter.agent_source.trim()
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
      const executorsChanged = !stringArraysEqual(
        state.executorsByServer[action.serverId] ?? [],
        action.executors,
      );
      const digestProviderChanged = Boolean(
        action.digestProvider &&
        state.digestProviderByServer[action.serverId] !== action.digestProvider,
      );
      if (!itemChanged && !executorsChanged && !digestProviderChanged) {
        return state;
      }
      return {
        byKey: itemChanged ? nextByKey : state.byKey,
        byProject: itemChanged ? groupByProject(nextByKey) : state.byProject,
        executorsByServer: {
          ...state.executorsByServer,
          [action.serverId]: action.executors,
        },
        digestProviderByServer: {
          ...state.digestProviderByServer,
          ...(action.digestProvider
            ? { [action.serverId]: action.digestProvider }
            : {}),
        },
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
    case "EXECUTORS_LOADED":
      if (
        stringArraysEqual(state.executorsByServer[action.serverId] ?? [], action.executors) &&
        (!action.digestProvider || state.digestProviderByServer[action.serverId] === action.digestProvider)
      ) {
        return state;
      }
      return {
        ...state,
        executorsByServer: {
          ...state.executorsByServer,
          [action.serverId]: action.executors,
        },
        digestProviderByServer: {
          ...state.digestProviderByServer,
          ...(action.digestProvider
            ? { [action.serverId]: action.digestProvider }
            : {}),
        },
      };
    case "WORK_DIGEST_PROVIDER_SET":
      if (!action.provider) {
        return state;
      }
      if (state.digestProviderByServer[action.serverId] === action.provider) {
        return state;
      }
      return {
        ...state,
        digestProviderByServer: {
          ...state.digestProviderByServer,
          [action.serverId]: action.provider,
        },
      };
    case "REMOVE_SERVER": {
      const hasServerItems = Object.values(state.byKey).some(
        (value) => value.serverId === action.serverId,
      );
      if (
        !hasServerItems &&
        !(action.serverId in state.executorsByServer) &&
        !(action.serverId in state.digestProviderByServer)
      ) {
        return state;
      }
      const nextByKey = Object.fromEntries(
        Object.entries(state.byKey).filter(([, value]) => value.serverId !== action.serverId),
      );
      return {
        byKey: nextByKey,
        byProject: groupByProject(nextByKey),
        executorsByServer: Object.fromEntries(
          Object.entries(state.executorsByServer).filter(([serverId]) => serverId !== action.serverId),
        ),
        digestProviderByServer: Object.fromEntries(
          Object.entries(state.digestProviderByServer).filter(([serverId]) => serverId !== action.serverId),
        ),
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
      mentionsEqual(left.mentions, right.mentions) &&
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
      left.outcome === right.outcome &&
      left.summary === right.summary &&
      stringArraysEqual(left.progress ?? [], right.progress ?? []) &&
      left.friction === right.friction &&
      left.cause === right.cause &&
      left.insight === right.insight &&
      left.next === right.next &&
      left.agent_source === right.agent_source &&
      left.agent_session === right.agent_session &&
      left.cwd === right.cwd &&
      left.command === right.command &&
      left.ai_provider === right.ai_provider &&
      left.ai_updated === right.ai_updated &&
      left.ai_hash === right.ai_hash &&
      left.ai_error === right.ai_error &&
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
  "outcome",
  "summary",
  "progress",
  "friction",
  "cause",
  "insight",
  "next",
  "agent_source",
  "agent_session",
  "cwd",
  "command",
  "ai_provider",
  "ai_updated",
  "ai_hash",
  "ai_error",
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

function mentionsEqual(left: Mention[], right: Mention[]): boolean {
  if (left === right) {
    return true;
  }
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    const leftMention = left[index];
    const rightMention = right[index];
    if (
      leftMention?.role !== rightMention?.role ||
      leftMention?.session !== rightMention?.session ||
      leftMention?.index !== rightMention?.index
    ) {
      return false;
    }
  }
  return true;
}

function stringArraysEqual(left: string[], right: string[]): boolean {
  if (left === right) {
    return true;
  }
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) {
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
