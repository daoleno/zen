import { useMemo } from "react";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { Agent, ConnectionState } from "../../store/agents";
import type { WorkItem } from "../../store/work";
import { presentAgent } from "../../services/agentPresentation";
import {
  DefaultCodexRenderMode,
  type StoredAgentAliases,
  type StoredCodexRenderMode,
  type StoredCodexRenderModes,
  type StoredTerminalTabs,
} from "../../services/storage";
import { findLinkedWork } from "./TerminalScreenModel";

interface UseTerminalRouteModelInput {
  serverId: string;
  agentId: string;
  sessionKey: string | null;
  agentByKey: ReadonlyMap<string, Agent>;
  workByKey: Record<string, WorkItem>;
  agentAliases: StoredAgentAliases;
  terminalTabs: StoredTerminalTabs;
  serverConnections: Record<string, ConnectionState>;
  serverConnectionIssues: Record<string, ConnectionIssue | null>;
  codexRenderModes: StoredCodexRenderModes;
}

export function useTerminalRouteModel({
  serverId,
  agentId,
  sessionKey,
  agentByKey,
  workByKey,
  agentAliases,
  terminalTabs,
  serverConnections,
  serverConnectionIssues,
  codexRenderModes,
}: UseTerminalRouteModelInput) {
  const agent = sessionKey ? agentByKey.get(sessionKey) : undefined;
  const gitDiffCwd = typeof agent?.cwd === "string" ? agent.cwd.trim() : "";
  const presentedAgent = useMemo(
    () =>
      presentAgent(
        agent || { name: "", summary: "", last_output_lines: [] },
        sessionKey ? agentAliases[sessionKey] : undefined,
      ),
    [agent, agentAliases, sessionKey],
  );
  const linkedWork = useMemo(
    () => findLinkedWork(workByKey, serverId, agentId),
    [agentId, serverId, workByKey],
  );
  const activePinned = sessionKey
    ? terminalTabs.pinned.includes(sessionKey)
    : false;
  const displayName = presentedAgent.title;
  const connectionState = serverId
    ? serverConnections[serverId] || "offline"
    : "offline";
  const connectionIssue = serverId
    ? serverConnectionIssues[serverId] || null
    : null;
  const hasTerminalRoute = Boolean(sessionKey && serverId && agentId);
  const isCodexAgent = presentedAgent.kind === "codex";
  const codexRenderMode: StoredCodexRenderMode = sessionKey
    ? codexRenderModes[sessionKey] ?? DefaultCodexRenderMode
    : DefaultCodexRenderMode;
  const showCodexChat =
    hasTerminalRoute && isCodexAgent && codexRenderMode === "chat";

  return {
    activePinned,
    agent,
    codexRenderMode,
    connectionIssue,
    connectionState,
    displayName,
    gitDiffCwd,
    hasTerminalRoute,
    isCodexAgent,
    linkedWork,
    presentedAgent,
    showCodexChat,
  };
}
