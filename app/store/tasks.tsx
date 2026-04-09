import React, { createContext, useContext, useReducer, ReactNode } from 'react';
import type { IssueStatus, IssuePriority } from '../constants/tokens';

export type { IssueStatus, IssuePriority };

export interface Task {
  id: string;
  number: number;
  serverId: string;
  serverName: string;
  title: string;
  description: string;
  status: IssueStatus;
  priority: IssuePriority;
  labels: string[];
  projectId?: string;
  skillId?: string;
  agentId?: string;
  agentStatus?: string;
  cwd?: string;
  createdAt: number;
  updatedAt: number;
}

export interface Skill {
  id: string;
  serverId: string;
  serverName: string;
  name: string;
  icon: string;
  agentCmd: string;
  prompt: string;
  cwd?: string;
}

export interface Guidance {
  preamble: string;
  constraints: string[];
}

export interface Project {
  id: string;
  serverId: string;
  name: string;
  icon: string;
}

interface State {
  tasks: Task[];
  skills: Skill[];
  projects: Project[];
  guidance: Record<string, Guidance>;
}

type RawTask = {
  id: string;
  number?: number;
  title: string;
  description?: string;
  status: IssueStatus;
  priority?: number;
  labels?: string[];
  project_id?: string;
  skill_id?: string;
  agent_id?: string;
  agent_status?: string;
  cwd?: string;
  created_at?: string | number;
  updated_at?: string | number;
};

type RawSkill = {
  id: string;
  name: string;
  icon?: string;
  agent_cmd: string;
  prompt: string;
  cwd?: string;
};

type RawProject = {
  id: string;
  name: string;
  icon?: string;
};

type Action =
  | { type: 'UPSERT_SERVER_TASKS'; serverId: string; serverName: string; tasks: RawTask[] }
  | { type: 'TASK_CREATED'; serverId: string; serverName: string; task: RawTask }
  | { type: 'TASK_UPDATED'; serverId: string; serverName: string; task: RawTask }
  | { type: 'TASK_DELETED'; serverId: string; taskId: string }
  | { type: 'UPSERT_SERVER_SKILLS'; serverId: string; serverName: string; skills: RawSkill[] }
  | { type: 'UPSERT_SERVER_PROJECTS'; serverId: string; projects: RawProject[] }
  | { type: 'PROJECT_CREATED'; serverId: string; project: RawProject }
  | { type: 'PROJECT_DELETED'; serverId: string; projectId: string }
  | { type: 'SET_GUIDANCE'; serverId: string; guidance: Guidance }
  | { type: 'REMOVE_SERVER'; serverId: string };

const initialState: State = {
  tasks: [],
  skills: [],
  projects: [],
  guidance: {},
};

function normalizeTask(raw: RawTask, serverId: string, serverName: string): Task {
  return {
    id: raw.id,
    number: raw.number || 0,
    serverId,
    serverName,
    title: raw.title,
    description: raw.description || '',
    status: raw.status,
    priority: (raw.priority || 0) as IssuePriority,
    labels: raw.labels || [],
    projectId: raw.project_id,
    skillId: raw.skill_id,
    agentId: raw.agent_id,
    agentStatus: raw.agent_status,
    cwd: raw.cwd,
    createdAt: normalizeTimestamp(raw.created_at),
    updatedAt: normalizeTimestamp(raw.updated_at),
  };
}

function normalizeSkill(raw: RawSkill, serverId: string, serverName: string): Skill {
  return {
    id: raw.id,
    serverId,
    serverName,
    name: raw.name,
    icon: raw.icon || '',
    agentCmd: raw.agent_cmd,
    prompt: raw.prompt,
    cwd: raw.cwd,
  };
}

function normalizeProject(raw: RawProject, serverId: string): Project {
  return {
    id: raw.id,
    serverId,
    name: raw.name,
    icon: raw.icon || '',
  };
}

function normalizeTimestamp(value?: string | number): number {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value > 10_000_000_000 ? value : value * 1000;
  }
  if (typeof value === 'string') {
    const parsed = Date.parse(value);
    if (!Number.isNaN(parsed)) return parsed;
  }
  return Date.now();
}

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'UPSERT_SERVER_TASKS': {
      const normalized = action.tasks.map(t => normalizeTask(t, action.serverId, action.serverName));
      return {
        ...state,
        tasks: [
          ...state.tasks.filter(t => t.serverId !== action.serverId),
          ...normalized,
        ],
      };
    }
    case 'TASK_CREATED': {
      const task = normalizeTask(action.task, action.serverId, action.serverName);
      return {
        ...state,
        tasks: [...state.tasks.filter(t => !(t.id === task.id && t.serverId === task.serverId)), task],
      };
    }
    case 'TASK_UPDATED': {
      const task = normalizeTask(action.task, action.serverId, action.serverName);
      const exists = state.tasks.some(t => t.id === task.id && t.serverId === task.serverId);
      return {
        ...state,
        tasks: exists
          ? state.tasks.map(t => (t.id === task.id && t.serverId === task.serverId ? task : t))
          : [...state.tasks, task],
      };
    }
    case 'TASK_DELETED':
      return {
        ...state,
        tasks: state.tasks.filter(t => !(t.id === action.taskId && t.serverId === action.serverId)),
      };
    case 'UPSERT_SERVER_SKILLS': {
      const normalized = action.skills.map(s => normalizeSkill(s, action.serverId, action.serverName));
      return {
        ...state,
        skills: [
          ...state.skills.filter(s => s.serverId !== action.serverId),
          ...normalized,
        ],
      };
    }
    case 'UPSERT_SERVER_PROJECTS': {
      const normalized = action.projects.map(p => normalizeProject(p, action.serverId));
      return {
        ...state,
        projects: [
          ...state.projects.filter(p => p.serverId !== action.serverId),
          ...normalized,
        ],
      };
    }
    case 'PROJECT_CREATED': {
      const project = normalizeProject(action.project, action.serverId);
      return {
        ...state,
        projects: [...state.projects.filter(p => !(p.id === project.id && p.serverId === project.serverId)), project],
      };
    }
    case 'PROJECT_DELETED':
      return {
        ...state,
        projects: state.projects.filter(p => !(p.id === action.projectId && p.serverId === action.serverId)),
      };
    case 'SET_GUIDANCE':
      return {
        ...state,
        guidance: { ...state.guidance, [action.serverId]: action.guidance },
      };
    case 'REMOVE_SERVER':
      return {
        ...state,
        tasks: state.tasks.filter(t => t.serverId !== action.serverId),
        skills: state.skills.filter(s => s.serverId !== action.serverId),
        projects: state.projects.filter(p => p.serverId !== action.serverId),
        guidance: Object.fromEntries(
          Object.entries(state.guidance).filter(([id]) => id !== action.serverId),
        ),
      };
    default:
      return state;
  }
}

const TaskContext = createContext<{ state: State; dispatch: React.Dispatch<Action> } | null>(null);

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
  if (!ctx) throw new Error('useTasks must be used within TaskProvider');
  return ctx;
}
