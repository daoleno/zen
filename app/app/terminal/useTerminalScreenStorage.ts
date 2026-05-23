import { useEffect, useState } from "react";
import {
  getAgentAliases,
  getCodexRenderModes,
  getRecentAgentOpens,
  getServers,
  markAgentOpened,
  type StoredAgentAliases,
  type StoredCodexRenderModes,
  type StoredRecentAgentOpens,
  type StoredServer,
} from "../../services/storage";

interface UseTerminalScreenStorageInput {
  serverId: string;
  sessionKey: string | null;
}

export function useTerminalScreenStorage({
  serverId,
  sessionKey,
}: UseTerminalScreenStorageInput) {
  const [agentAliases, setAgentAliases] = useState<StoredAgentAliases>({});
  const [codexRenderModes, setCodexRenderModes] =
    useState<StoredCodexRenderModes>({});
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

      const openedAt = sessionKey ? Date.now() : 0;
      if (sessionKey) {
        void markAgentOpened(sessionKey, openedAt);
      }

      if (!cancelled) {
        setAgentAliases(storedAliases);
        setCodexRenderModes(storedCodexRenderModes);
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
  }, [serverId, sessionKey]);

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
