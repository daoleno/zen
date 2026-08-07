import type { ConnectionIssue } from "../../../services/connectionIssue";
import type { Agent, ConnectionState } from "../../../store/agents";
import type { BrainAgentRef } from "../../../store/brain";
import type { WorkItem } from "../../../store/work";
import type {
  StoredAgentAliases,
  StoredInterfaceRenderModes,
} from "../../../services/storage";
import { useTerminalGitDiff } from "../useTerminalGitDiff";
import { useTerminalRouteModel } from "./useTerminalRouteModel";
import { useTerminalThemeChrome } from "./useTerminalThemeChrome";
import { useTerminalViewportModel } from "./useTerminalViewportModel";
import type { TerminalRouteSessionHint } from "./useTerminalScreenLocalState";

interface UseTerminalScreenModelsInput {
  serverId: string;
  agentId: string;
  sessionKey: string | null;
  routeSessionHint: TerminalRouteSessionHint;
  screenFocused: boolean;
  agentByKey: ReadonlyMap<string, Agent>;
  workByKey: Record<string, WorkItem>;
  agentAliases: StoredAgentAliases;
  serverConnections: Record<string, ConnectionState>;
  serverConnectionIssues: Record<string, ConnectionIssue | null>;
  interfaceRenderModes: StoredInterfaceRenderModes;
  brainHostAgent?: BrainAgentRef | null;
  brainHostServerId?: string | null;
}

export function useTerminalScreenModels({
  serverId,
  agentId,
  sessionKey,
  routeSessionHint,
  screenFocused,
  agentByKey,
  workByKey,
  agentAliases,
  serverConnections,
  serverConnectionIssues,
  interfaceRenderModes,
  brainHostAgent,
  brainHostServerId,
}: UseTerminalScreenModelsInput) {
  const theme = useTerminalThemeChrome();
  const route = useTerminalRouteModel({
    serverId,
    agentId,
    sessionKey,
    routeSessionHint,
    agentByKey,
    workByKey,
    agentAliases,
    serverConnections,
    serverConnectionIssues,
    interfaceRenderModes,
    brainHostAgent,
    brainHostServerId,
  });
  const viewport = useTerminalViewportModel({
    hasTerminalRoute: route.hasTerminalRoute,
    showInterfaceChat: route.showInterfaceChat,
    screenFocused,
    connectionState: route.connectionState,
    connectionIssue: route.connectionIssue,
    terminalTheme: theme.terminalTheme,
    chromeColors: theme.chromeColors,
  });
  const gitDiff = useTerminalGitDiff({
    serverId,
    agentId,
    cwd: route.gitDiffCwd,
    connectionState: route.connectionState,
    hasTerminalRoute: route.hasTerminalRoute,
    screenFocused,
  });

  return {
    gitDiff,
    route,
    theme,
    viewport,
  };
}
