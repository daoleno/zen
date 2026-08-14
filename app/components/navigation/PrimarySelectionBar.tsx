import React, {
  createContext,
  useCallback,
  useContext,
  useId,
  useLayoutEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useIsFocused } from "expo-router";
import {
  clearPrimarySelectionBar,
  registerPrimarySelectionBar,
  type PrimarySelectionBarRegistration,
} from "./primarySelectionBarState";

/**
 * Full-width top-chrome swap channel: a page can register a selection-mode
 * node (Cancel + selected count + destructive Terminate) that replaces the
 * drawer shell's normal app bar (menu + tab switch + page action). The
 * registration is owner-scoped and clears automatically on blur and unmount,
 * mirroring PrimaryPageAction.
 */

interface PrimarySelectionBarDispatch {
  clear(ownerId: string): void;
  register(ownerId: string, content: ReactNode): void;
}

const PrimarySelectionBarStateContext =
  createContext<PrimarySelectionBarRegistration<ReactNode> | null>(null);

const PrimarySelectionBarDispatchContext =
  createContext<PrimarySelectionBarDispatch | null>(null);

export function PrimarySelectionBarProvider({
  children,
}: {
  children: ReactNode;
}) {
  const [entry, setEntry] = useState<PrimarySelectionBarRegistration<ReactNode> | null>(
    null,
  );
  const register = useCallback((ownerId: string, content: ReactNode) => {
    setEntry((current) => {
      if (
        current != null &&
        current.ownerId === ownerId &&
        current.content === content
      ) {
        return current;
      }
      return registerPrimarySelectionBar(current, ownerId, content);
    });
  }, []);
  const clear = useCallback((ownerId: string) => {
    setEntry((current) => clearPrimarySelectionBar(current, ownerId));
  }, []);
  const dispatch = useMemo<PrimarySelectionBarDispatch>(
    () => ({ clear, register }),
    [clear, register],
  );

  return (
    <PrimarySelectionBarDispatchContext.Provider value={dispatch}>
      <PrimarySelectionBarStateContext.Provider value={entry}>
        {children}
      </PrimarySelectionBarStateContext.Provider>
    </PrimarySelectionBarDispatchContext.Provider>
  );
}

/**
 * Register the selection-mode app bar node while the owning page is focused.
 * Pass null to release. Memoize the node when the parent re-renders often.
 */
export function usePrimarySelectionBar(node: ReactNode | null): void {
  const dispatch = useContext(PrimarySelectionBarDispatchContext);
  const ownerId = useId();
  const focused = useIsFocused();

  useLayoutEffect(() => {
    if (dispatch == null) {
      return;
    }
    if (!focused) {
      dispatch.clear(ownerId);
      return;
    }
    if (node == null || node === false) {
      dispatch.clear(ownerId);
      return;
    }
    dispatch.register(ownerId, node);
    return () => {
      dispatch.clear(ownerId);
    };
  }, [dispatch, focused, node, ownerId]);

  if (dispatch == null) {
    throw new Error(
      "usePrimarySelectionBar must be used inside PrimaryDrawerShell",
    );
  }
}

/** The registered selection node, or null for the normal app bar. */
export function usePrimarySelectionBarContent(): ReactNode {
  const entry = useContext(PrimarySelectionBarStateContext);
  return entry?.content ?? null;
}
