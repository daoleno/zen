import React, {
  createContext,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from "react";

/**
 * The app is single-session-view: exactly one routed Session is displayed at
 * a time. This store records that Session (server + agent) as the "current
 * Session" so feature surfaces that live outside the terminal route — the
 * Settings Providers screen — can act on it (e.g. a Settings Provider switch
 * activates the exact new Provider + current model on this Session).
 *
 * Semantics: last-focused wins. The terminal route writes on every
 * server/agent rebind and keeps the value across screen pop, so opening
 * Settings from anywhere keeps the Session the user was last looking at. The
 * value is presentation state only — never durable. The durable truths stay
 * the catalog client default (preferred Provider) and the daemon route
 * binding (Session route), both of which are restored deterministically on
 * restart; this store only decides which live Session a Settings action
 * activates.
 */
export type CurrentSessionRoute = {
  serverId: string;
  agentId: string;
};

interface CurrentSessionValue {
  currentSession: CurrentSessionRoute | null;
  setCurrentSession(session: CurrentSessionRoute | null): void;
}

const CurrentSessionContext = createContext<CurrentSessionValue | null>(null);

export function CurrentSessionProvider({ children }: { children: ReactNode }) {
  const [currentSession, setCurrentSession] =
    useState<CurrentSessionRoute | null>(null);
  const value = useMemo(
    () => ({ currentSession, setCurrentSession }),
    [currentSession],
  );
  return (
    <CurrentSessionContext.Provider value={value}>
      {children}
    </CurrentSessionContext.Provider>
  );
}

export function useCurrentSession(): CurrentSessionValue {
  const context = useContext(CurrentSessionContext);
  if (!context) {
    throw new Error(
      "useCurrentSession must be used within CurrentSessionProvider",
    );
  }
  return context;
}
