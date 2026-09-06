import type { ConnectionIssue } from "../../../services/connectionIssue";
import type { Worker, ConnectionState } from "../../../store/workers";
import type { BrainWorkerRef } from "../../../store/brain";
import type { WorkItem } from "../../../store/work";
import type {
  StoredWorkerAliases,
  StoredInterfaceRenderModes,
} from "../../../services/storage";
import { useTerminalGitDiff } from "../useTerminalGitDiff";
import { useTerminalRouteModel } from "./useTerminalRouteModel";
import { useTerminalThemeChrome } from "./useTerminalThemeChrome";
import { useTerminalViewportModel } from "./useTerminalViewportModel";
import type { TerminalRouteSessionHint } from "./useTerminalScreenLocalState";

interface UseTerminalScreenModelsInput {
  serverId: string;
  workerId: string;
  sessionKey: string | null;
  routeSessionHint: TerminalRouteSessionHint;
  screenFocused: boolean;
  workerByKey: ReadonlyMap<string, Worker>;
  workByKey: Record<string, WorkItem>;
  workerAliases: StoredWorkerAliases;
  serverConnections: Record<string, ConnectionState>;
  serverConnectionIssues: Record<string, ConnectionIssue | null>;
  interfaceRenderModes: StoredInterfaceRenderModes;
  brainHostWorker?: BrainWorkerRef | null;
  brainHostServerId?: string | null;
}

export function useTerminalScreenModels({
  serverId,
  workerId,
  sessionKey,
  routeSessionHint,
  screenFocused,
  workerByKey,
  workByKey,
  workerAliases,
  serverConnections,
  serverConnectionIssues,
  interfaceRenderModes,
  brainHostWorker,
  brainHostServerId,
}: UseTerminalScreenModelsInput) {
  const theme = useTerminalThemeChrome();
  const route = useTerminalRouteModel({
    serverId,
    workerId,
    sessionKey,
    routeSessionHint,
    workerByKey,
    workByKey,
    workerAliases,
    serverConnections,
    serverConnectionIssues,
    interfaceRenderModes,
    brainHostWorker,
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
    workerId,
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
