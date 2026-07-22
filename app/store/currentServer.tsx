import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  getCurrentServerId,
  getServers,
  setCurrentServerId,
  type StoredServer,
} from "../services/storage";
import {
  enqueueCurrentServerPersistence,
  resolveCurrentServerId,
  selectCurrentServer,
} from "../services/currentServerSelection";

interface CurrentServerContextValue {
  currentServer: StoredServer | null;
  currentServerId: string | null;
  hydrated: boolean;
  servers: StoredServer[];
  refreshServers(preferredServerId?: string): Promise<void>;
  switchCurrentServer(serverId: string): Promise<void>;
}

const CurrentServerContext = createContext<CurrentServerContextValue | null>(
  null,
);

export function CurrentServerProvider({ children }: { children: ReactNode }) {
  const [servers, setServers] = useState<StoredServer[]>([]);
  const [currentServerId, setCurrentServerIdState] = useState<string | null>(
    null,
  );
  const [hydrated, setHydrated] = useState(false);
  const currentServerIdRef = useRef<string | null>(null);
  const refreshGenerationRef = useRef(0);
  const persistenceTailRef = useRef<Promise<void>>(Promise.resolve());

  const commitCurrentServerId = useCallback((nextId: string | null) => {
    if (currentServerIdRef.current === nextId) {
      return;
    }
    currentServerIdRef.current = nextId;
    setCurrentServerIdState(nextId);
  }, []);

  const persistCurrentServerId = useCallback((nextId: string | null) => {
    const queued = enqueueCurrentServerPersistence(
      persistenceTailRef.current,
      () => setCurrentServerId(nextId),
    );
    persistenceTailRef.current = queued.tail;
    return queued.result;
  }, []);

  const refreshServers = useCallback(
    async (preferredServerId?: string) => {
      const refreshGeneration = ++refreshGenerationRef.current;
      await persistenceTailRef.current;
      const [savedServers, persistedServerId] = await Promise.all([
        getServers(),
        getCurrentServerId(),
      ]);
      if (refreshGeneration !== refreshGenerationRef.current) {
        return;
      }
      const nextId = resolveCurrentServerId(
        savedServers,
        preferredServerId || currentServerIdRef.current || persistedServerId,
      );
      setServers(savedServers);
      commitCurrentServerId(nextId);
      setHydrated(true);
      if (nextId !== persistedServerId) {
        await persistCurrentServerId(nextId);
      }
    },
    [commitCurrentServerId, persistCurrentServerId],
  );

  useEffect(() => {
    void refreshServers();
  }, [refreshServers]);

  const switchCurrentServer = useCallback(
    async (serverId: string) => {
      const normalizedId = serverId.trim();
      if (
        !normalizedId ||
        !servers.some((server) => server.id === normalizedId)
      ) {
        throw new Error("That server configuration is no longer available.");
      }
      refreshGenerationRef.current += 1;
      await persistCurrentServerId(normalizedId);
      commitCurrentServerId(normalizedId);
    },
    [commitCurrentServerId, persistCurrentServerId, servers],
  );

  const currentServer = useMemo(
    () => selectCurrentServer(servers, currentServerId),
    [currentServerId, servers],
  );
  const value = useMemo<CurrentServerContextValue>(
    () => ({
      currentServer,
      currentServerId,
      hydrated,
      refreshServers,
      servers,
      switchCurrentServer,
    }),
    [
      currentServer,
      currentServerId,
      hydrated,
      refreshServers,
      servers,
      switchCurrentServer,
    ],
  );

  return (
    <CurrentServerContext.Provider value={value}>
      {children}
    </CurrentServerContext.Provider>
  );
}

export function useCurrentServer() {
  const context = useContext(CurrentServerContext);
  if (!context) {
    throw new Error(
      "useCurrentServer must be used within CurrentServerProvider",
    );
  }
  return context;
}
