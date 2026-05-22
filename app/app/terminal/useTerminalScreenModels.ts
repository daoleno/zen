import type { ConnectionIssue } from "../../services/connectionIssue";
import type { Agent, ConnectionState } from "../../store/agents";
import type { WorkItem } from "../../store/work";
import type {
  StoredAgentAliases,
  StoredCodexRenderModes,
  StoredTerminalTabs,
} from "../../services/storage";
import type { TerminalThemePreference } from "../../constants/terminalThemes";
import { useTerminalGitDiff } from "../../components/terminal/useTerminalGitDiff";
import { useTerminalRouteModel } from "./useTerminalRouteModel";
import { useTerminalThemeChrome } from "./useTerminalThemeChrome";
import { useTerminalViewportModel } from "./useTerminalViewportModel";

interface UseTerminalScreenModelsInput {
  serverId: string;
  agentId: string;
  sessionKey: string | null;
  screenFocused: boolean;
  themePreference: TerminalThemePreference;
  agentByKey: ReadonlyMap<string, Agent>;
  workByKey: Record<string, WorkItem>;
  agentAliases: StoredAgentAliases;
  terminalTabs: StoredTerminalTabs;
  serverConnections: Record<string, ConnectionState>;
  serverConnectionIssues: Record<string, ConnectionIssue | null>;
  codexRenderModes: StoredCodexRenderModes;
}

export function useTerminalScreenModels({
  serverId,
  agentId,
  sessionKey,
  screenFocused,
  themePreference,
  agentByKey,
  workByKey,
  agentAliases,
  terminalTabs,
  serverConnections,
  serverConnectionIssues,
  codexRenderModes,
}: UseTerminalScreenModelsInput) {
  const theme = useTerminalThemeChrome(themePreference);
  const route = useTerminalRouteModel({
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
  });
  const viewport = useTerminalViewportModel({
    hasTerminalRoute: route.hasTerminalRoute,
    showCodexChat: route.showCodexChat,
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
