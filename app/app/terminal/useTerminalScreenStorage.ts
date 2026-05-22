import { useEffect, useState } from "react";
import {
  getAgentAliases,
  getCodexRenderModes,
  getRecentAgentOpens,
  getServers,
  getTerminalTabs,
  getTerminalTheme,
  markAgentOpened,
  syncTerminalTabsWithLiveSessions,
  touchTerminalTab,
  type StoredAgentAliases,
  type StoredCodexRenderModes,
  type StoredRecentAgentOpens,
  type StoredServer,
  type StoredTerminalTabs,
} from "../../services/storage";
import type { TerminalThemePreference } from "../../constants/terminalThemes";
import { DefaultTerminalThemePreference } from "../../constants/terminalThemes";
import { EMPTY_TABS } from "./TerminalScreenModel";

interface UseTerminalScreenStorageInput {
  serverId: string;
  sessionKey: string | null;
  hydratedServerIds: string[];
  liveAgentKeys: string[];
}

export function useTerminalScreenStorage({
  serverId,
  sessionKey,
  hydratedServerIds,
  liveAgentKeys,
}: UseTerminalScreenStorageInput) {
  const [themePreference, setThemePreference] = useState<TerminalThemePreference>(
    DefaultTerminalThemePreference,
  );
  const [agentAliases, setAgentAliases] = useState<StoredAgentAliases>({});
  const [codexRenderModes, setCodexRenderModes] =
    useState<StoredCodexRenderModes>({});
  const [recentAgentOpens, setRecentAgentOpens] =
    useState<StoredRecentAgentOpens>({});
  const [terminalTabs, setTerminalTabs] =
    useState<StoredTerminalTabs>(EMPTY_TABS);
  const [server, setServer] = useState<StoredServer | null>(null);
  const [servers, setServers] = useState<StoredServer[]>([]);

  useEffect(() => {
    let cancelled = false;

    (async () => {
      const [
        storedTheme,
        storedRecentOpens,
        storedAliases,
        storedServers,
        storedCodexRenderModes,
      ] =
        await Promise.all([
          getTerminalTheme(),
          getRecentAgentOpens(),
          getAgentAliases(),
          getServers(),
          getCodexRenderModes(),
        ]);
      const storedServer = serverId
        ? storedServers.find((current) => current.id === serverId) || null
        : null;

      if (!sessionKey) {
        const storedTabs = await getTerminalTabs();
        if (!cancelled) {
          setThemePreference(storedTheme);
          setAgentAliases(storedAliases);
          setCodexRenderModes(storedCodexRenderModes);
          setRecentAgentOpens(storedRecentOpens);
          setTerminalTabs(storedTabs);
          setServer(storedServer);
          setServers(storedServers);
        }
        return;
      }

      const openedAt = Date.now();
      const nextTabs = await touchTerminalTab(sessionKey);
      void markAgentOpened(sessionKey, openedAt);

      if (!cancelled) {
        setThemePreference(storedTheme);
        setAgentAliases(storedAliases);
        setCodexRenderModes(storedCodexRenderModes);
        setRecentAgentOpens({
          ...storedRecentOpens,
          [sessionKey]: openedAt,
        });
        setTerminalTabs(nextTabs);
        setServer(storedServer);
        setServers(storedServers);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [serverId, sessionKey]);

  useEffect(() => {
    if (hydratedServerIds.length === 0) return;

    let cancelled = false;

    (async () => {
      const nextTabs = await syncTerminalTabsWithLiveSessions(
        liveAgentKeys,
        hydratedServerIds,
      );
      if (!cancelled) {
        setTerminalTabs(nextTabs);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [hydratedServerIds, liveAgentKeys]);

  return {
    themePreference,
    agentAliases,
    setAgentAliases,
    codexRenderModes,
    setCodexRenderModes,
    recentAgentOpens,
    setRecentAgentOpens,
    terminalTabs,
    setTerminalTabs,
    server,
    setServer,
    servers,
  };
}
