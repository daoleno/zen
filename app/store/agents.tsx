import React, { createContext, useContext, useReducer, ReactNode } from 'react';
import { AgentStatus } from '../constants/tokens';

export interface Agent {
  id: string;
  name: string;
  status: AgentStatus;
  project?: string;
  summary: string;
  last_output_lines: string[];
  updated_at: number;
}

interface State {
  agents: Agent[];
  connected: boolean;
  selectedAgentId: string | null;
}

type Action =
  | { type: 'SET_AGENTS'; agents: Agent[] }
  | { type: 'UPDATE_OUTPUT'; agent_id: string; lines: string[] }
  | { type: 'STATE_CHANGE'; agent_id: string; old: string; new_state: string }
  | { type: 'SET_CONNECTED'; connected: boolean }
  | { type: 'SELECT_AGENT'; id: string | null };

const initialState: State = {
  agents: [],
  connected: false,
  selectedAgentId: null,
};

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'SET_AGENTS':
      return { ...state, agents: action.agents };
    case 'UPDATE_OUTPUT': {
      const agents = state.agents.map(a => {
        if (a.id !== action.agent_id) return a;
        return {
          ...a,
          last_output_lines: [...a.last_output_lines, ...action.lines].slice(-1000),
          updated_at: Date.now() / 1000,
        };
      });
      return { ...state, agents };
    }
    case 'STATE_CHANGE': {
      const agents = state.agents.map(a => {
        if (a.id !== action.agent_id) return a;
        return { ...a, status: action.new_state as AgentStatus, updated_at: Date.now() / 1000 };
      });
      return { ...state, agents };
    }
    case 'SET_CONNECTED':
      return { ...state, connected: action.connected };
    case 'SELECT_AGENT':
      return { ...state, selectedAgentId: action.id };
    default:
      return state;
  }
}

const AgentContext = createContext<{ state: State; dispatch: React.Dispatch<Action> } | null>(null);

export function AgentProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, initialState);
  return (
    <AgentContext.Provider value={{ state, dispatch }}>
      {children}
    </AgentContext.Provider>
  );
}

export function useAgents() {
  const ctx = useContext(AgentContext);
  if (!ctx) throw new Error('useAgents must be used within AgentProvider');
  return ctx;
}
