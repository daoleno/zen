import React, { createContext, useContext, useMemo, type ReactNode } from "react";
import type { DrawerPhase } from "./primaryDrawerState";

interface PrimarySurfaceState {
  drawerPhase: DrawerPhase;
  routeFocused: boolean;
}

interface PrimarySurfaceInteractionProviderProps extends PrimarySurfaceState {
  children: ReactNode;
}

export interface PrimarySurfaceInteractiveInputs {
  connected: boolean;
  coveringModal: boolean;
  editable: boolean;
}

const PrimarySurfaceInteractionContext =
  createContext<PrimarySurfaceState | null>(null);

export function PrimarySurfaceInteractionProvider({
  children,
  drawerPhase,
  routeFocused,
}: PrimarySurfaceInteractionProviderProps) {
  const value = useMemo(
    () => ({ drawerPhase, routeFocused }),
    [drawerPhase, routeFocused],
  );
  return (
    <PrimarySurfaceInteractionContext.Provider value={value}>
      {children}
    </PrimarySurfaceInteractionContext.Provider>
  );
}

export function usePrimarySurfaceInteractive({
  connected,
  coveringModal,
  editable,
}: PrimarySurfaceInteractiveInputs): boolean {
  const state = useContext(PrimarySurfaceInteractionContext);
  if (state == null) {
    throw new Error(
      "usePrimarySurfaceInteractive must be used inside PrimaryDrawerShell",
    );
  }
  return (
    state.routeFocused &&
    state.drawerPhase === "closed" &&
    !coveringModal &&
    connected &&
    editable
  );
}
