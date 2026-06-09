import { useEffect, useState } from "react";
import {
  getAgentAliases,
  getCodexRenderModes,
  getRecentAgentOpens,
  getServers,
  markAgentOpened,
  type StoredAgentAliases,
  type StoredCodexRenderMode,
  type StoredCodexRenderModes,
  type StoredRecentAgentOpens,
  type StoredServer,
} from "../../services/storage";

interface UseTerminalScreenStorageInput {
  serverId: string;
  sessionKey: string | null;
  initialCodexRenderMode?: StoredCodexRenderMode;
}

export function useTerminalScreenStorage({
  serverId,
  sessionKey,
  initialCodexRenderMode,
}: UseTerminalScreenStorageInput) {
  const [agentAliases, setAgentAliases] = useState<StoredAgentAliases>({});
  const [codexRenderModes, setCodexRenderModes] =
    useState<StoredCodexRenderModes>(() =>
      sessionKey && initialCodexRenderMode
        ? { [sessionKey]: initialCodexRenderMode }
        : {},
    );
  const [recentAgentOpens, setRecentAgentOpens] =
    useState<StoredRecentAgentOpens>({});
  const [server, setServer] = useState<StoredServer | null>(null);
  const [servers, setServers] = useState<StoredServer[]>([]);

  useEffect(() => {
    let cancelled = false;

    (async () => {
      const [
        storedRecentOpens,
        storedAliases,
        storedServers,
        storedCodexRenderModes,
      ] =
        await Promise.all([
          getRecentAgentOpens(),
          getAgentAliases(),
          getServers(),
          getCodexRenderModes(),
        ]);
      const storedServer = serverId
        ? storedServers.find((current) => current.id === serverId) || null
        : null;
      const nextCodexRenderModes =
        sessionKey && initialCodexRenderMode
          ? {
              ...storedCodexRenderModes,
              [sessionKey]: initialCodexRenderMode,
            }
          : storedCodexRenderModes;

      const openedAt = sessionKey ? Date.now() : 0;
      if (sessionKey) {
        void markAgentOpened(sessionKey, openedAt);
      }

      if (!cancelled) {
        setAgentAliases(storedAliases);
        setCodexRenderModes(nextCodexRenderModes);
        setRecentAgentOpens(
          sessionKey
            ? {
                ...storedRecentOpens,
                [sessionKey]: openedAt,
              }
            : storedRecentOpens,
        );
        setServer(storedServer);
        setServers(storedServers);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [initialCodexRenderMode, serverId, sessionKey]);

  return {
    agentAliases,
    setAgentAliases,
    codexRenderModes,
    setCodexRenderModes,
    recentAgentOpens,
    setRecentAgentOpens,
    server,
    setServer,
    servers,
  };
}
