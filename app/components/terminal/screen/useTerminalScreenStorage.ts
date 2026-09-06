import { useEffect, useState } from "react";
import {
  getWorkerAliases,
  getInterfaceRenderModes,
  getRecentWorkerOpens,
  markWorkerOpened,
  type StoredWorkerAliases,
  type StoredInterfaceRenderMode,
  type StoredInterfaceRenderModes,
  type StoredRecentWorkerOpens,
} from "../../../services/storage";
import { useCurrentServer } from "../../../store/currentServer";

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
  const { currentServer } = useCurrentServer();
  const server = currentServer?.id === serverId ? currentServer : null;
  const [workerAliases, setWorkerAliases] = useState<StoredWorkerAliases>({});
  const [interfaceRenderModes, setInterfaceRenderModes] =
    useState<StoredInterfaceRenderModes>(() =>
      sessionKey && initialInterfaceRenderMode
        ? { [sessionKey]: initialInterfaceRenderMode }
        : {},
    );
  const [recentWorkerOpens, setRecentWorkerOpens] =
    useState<StoredRecentWorkerOpens>({});

  useEffect(() => {
    let cancelled = false;

    (async () => {
      const [
        storedRecentOpens,
        storedAliases,
        storedInterfaceRenderModes,
      ] = await Promise.all([
        getRecentWorkerOpens(),
        getWorkerAliases(),
        getInterfaceRenderModes(),
      ]);
      const nextInterfaceRenderModes =
        sessionKey && initialInterfaceRenderMode
          ? {
              ...storedInterfaceRenderModes,
              [sessionKey]: initialInterfaceRenderMode,
            }
          : storedInterfaceRenderModes;

      const openedAt = sessionKey ? Date.now() : 0;
      if (sessionKey) {
        void markWorkerOpened(sessionKey, openedAt);
      }

      if (!cancelled) {
        setWorkerAliases(storedAliases);
        setInterfaceRenderModes(nextInterfaceRenderModes);
        setRecentWorkerOpens(
          sessionKey
            ? {
                ...storedRecentOpens,
                [sessionKey]: openedAt,
              }
            : storedRecentOpens,
        );
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [initialInterfaceRenderMode, serverId, sessionKey]);

  return {
    workerAliases,
    setWorkerAliases,
    interfaceRenderModes,
    setInterfaceRenderModes,
    recentWorkerOpens,
    setRecentWorkerOpens,
    server,
  };
}
