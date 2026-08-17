import React, {
  createContext,
  useContext,
  useReducer,
  type ReactNode,
} from "react";

export type CalendarKind =
  "event" | "reminder" | "deadline" | "scheduled_action";
export type CalendarStatus =
  "scheduled" | "waiting" | "running" | "completed" | "failed" | "cancelled";
export type CalendarRecurrence = "none" | "daily" | "weekly" | "weekdays";
export interface CalendarRun {
  id: string;
  scheduled_for: string;
  started_at: string;
  finished_at?: string;
  status: CalendarStatus;
  manual?: boolean;
  work_id?: string;
  agent_session?: string;
  result?: string;
  failure_reason?: string;
}
export interface CalendarItem {
  id: string;
  title: string;
  kind: CalendarKind;
  status: CalendarStatus;
  start_at?: string;
  end_at?: string;
  notify_at?: string;
  due_at?: string;
  timezone: string;
  recurrence: CalendarRecurrence;
  notes?: string;
  action_instruction?: string;
  action_cwd?: string;
  source_thread_id?: string;
  source_message_id?: string;
  linked_work_id?: string;
  failure_reason?: string;
  next_at: string;
  created_at: string;
  updated_at: string;
  cancelled_at?: string;
  revision: number;
  runs?: CalendarRun[];
}
export interface ServerCalendar {
  serverId: string;
  serverName: string;
  serverUrl: string;
  hydrated: boolean;
  items: CalendarItem[];
}
export interface CalendarState {
  byServer: Record<string, ServerCalendar>;
}
export type ServerCalendarItem = CalendarItem & {
  serverId: string;
  serverName: string;
};
export const initialCalendarState: CalendarState = { byServer: {} };
type Action =
  | {
      type: "CALENDAR_SNAPSHOT";
      serverId: string;
      serverName: string;
      serverUrl: string;
      items: CalendarItem[];
    }
  | {
      type: "CALENDAR_CHANGED";
      serverId: string;
      serverName: string;
      serverUrl: string;
      item: CalendarItem;
    }
  | { type: "REMOVE_SERVER"; serverId: string };

export function calendarReducer(
  state: CalendarState,
  action: Action,
): CalendarState {
  switch (action.type) {
    case "CALENDAR_SNAPSHOT":
      return {
        byServer: {
          ...state.byServer,
          [action.serverId]: {
            serverId: action.serverId,
            serverName: action.serverName,
            serverUrl: action.serverUrl,
            hydrated: true,
            items: sortItems(action.items),
          },
        },
      };
    case "CALENDAR_CHANGED": {
      const current = state.byServer[action.serverId];
      const items =
        current?.items.filter((item) => item.id !== action.item.id) ?? [];
      return {
        byServer: {
          ...state.byServer,
          [action.serverId]: {
            serverId: action.serverId,
            serverName: action.serverName,
            serverUrl: action.serverUrl,
            hydrated: true,
            items: sortItems([...items, action.item]),
          },
        },
      };
    }
    case "REMOVE_SERVER": {
      const byServer = { ...state.byServer };
      delete byServer[action.serverId];
      return { byServer };
    }
  }
}
function sortItems(items: CalendarItem[]) {
  return [...items].sort(
    (a, b) => Date.parse(a.next_at) - Date.parse(b.next_at),
  );
}

export function selectCurrentServerCalendar(
  state: CalendarState,
  currentServerId: string | null | undefined,
): ServerCalendar | null {
  const serverId = currentServerId?.trim() ?? "";
  return serverId ? state.byServer[serverId] ?? null : null;
}

export function selectCurrentServerCalendarItems(
  state: CalendarState,
  currentServerId: string | null | undefined,
): ServerCalendarItem[] {
  const server = selectCurrentServerCalendar(state, currentServerId);
  if (!server) return [];
  return server.items.map((item) => ({
    ...item,
    serverId: server.serverId,
    serverName: server.serverName,
  }));
}

const StateContext = createContext<CalendarState | null>(null);
const DispatchContext = createContext<React.Dispatch<Action> | null>(null);
export function CalendarProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(calendarReducer, initialCalendarState);
  return (
    <DispatchContext.Provider value={dispatch}>
      <StateContext.Provider value={state}>{children}</StateContext.Provider>
    </DispatchContext.Provider>
  );
}
export function useCalendar() {
  const state = useContext(StateContext);
  if (!state)
    throw new Error("useCalendar must be used within CalendarProvider");
  return state;
}
export function useCalendarDispatch() {
  const dispatch = useContext(DispatchContext);
  if (!dispatch)
    throw new Error("useCalendarDispatch must be used within CalendarProvider");
  return dispatch;
}
