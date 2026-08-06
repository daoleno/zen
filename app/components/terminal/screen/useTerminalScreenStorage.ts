import { useEffect, useState } from "react";
import {
  getAgentAliases,
  getInterfaceRenderModes,
  getRecentAgentOpens,
  getServers,
  markAgentOpened,
  type StoredAgentAliases,
  type StoredInterfaceRenderMode,
  type StoredInterfaceRenderModes,
  type StoredRecentAgentOpens,
  type StoredServer,
} from "../../../services/storage";

interface UseTerminalScreenStorageInput {
  serverId: string;
  sessionKey: string | null;
  initialInterfaceRenderMode?: StoredInterfaceRenderMode;
}

export function useTerminalScreenStorage({
  serverId,
  sessionKey,
  initialInterfaceRenderMode,
}: UseTerminalScreenStorageInput) {
  const [agentAliases, setAgentAliases] = useState<StoredAgentAliases>({});
  const [interfaceRenderModes, setInterfaceRenderModes] =
    useState<StoredInterfaceRenderModes>(() =>
      sessionKey && initialInterfaceRenderMode
        ? { [sessionKey]: initialInterfaceRenderMode }
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
        storedInterfaceRenderModes,
      ] = await Promise.all([
        getRecentAgentOpens(),
        getAgentAliases(),
        getServers(),
        getInterfaceRenderModes(),
      ]);
      const storedServer = serverId
        ? storedServers.find((current) => current.id === serverId) || null
        : null;
      const nextInterfaceRenderModes =
        sessionKey && initialInterfaceRenderMode
          ? {
              ...storedInterfaceRenderModes,
              [sessionKey]: initialInterfaceRenderMode,
            }
          : storedInterfaceRenderModes;

      const openedAt = sessionKey ? Date.now() : 0;
      if (sessionKey) {
        void markAgentOpened(sessionKey, openedAt);
      }

      if (!cancelled) {
        setAgentAliases(storedAliases);
        setInterfaceRenderModes(nextInterfaceRenderModes);
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
  }, [initialInterfaceRenderMode, serverId, sessionKey]);

  return {
    agentAliases,
    setAgentAliases,
    interfaceRenderModes,
    setInterfaceRenderModes,
    recentAgentOpens,
    setRecentAgentOpens,
    server,
    setServer,
    servers,
  };
}
