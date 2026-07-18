import React, {
  createContext,
  useCallback,
  useContext,
  useLayoutEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import type { Animated } from "react-native";

export type PrimaryPagerPositionNode = Animated.AnimatedInterpolation<number>;

interface PrimaryPagerPositionContextValue {
  position: PrimaryPagerPositionNode | null;
}

const PrimaryPagerPositionContext =
  createContext<PrimaryPagerPositionContextValue>({ position: null });

const PrimaryPagerPositionPublishContext = createContext<
  ((position: PrimaryPagerPositionNode) => void) | null
>(null);

/**
 * Holds the Pager's exact Animated position object (not a numeric sample).
 * Publishing the node once is fine; per-frame numeric updates must not go
 * through React state.
 */
export function PrimaryPagerPositionProvider({
  children,
}: {
  children: ReactNode;
}) {
  const [position, setPosition] = useState<PrimaryPagerPositionNode | null>(
    null,
  );
  const publish = useCallback((next: PrimaryPagerPositionNode) => {
    setPosition((current) => (current === next ? current : next));
  }, []);
  const value = useMemo(() => ({ position }), [position]);

  return (
    <PrimaryPagerPositionPublishContext.Provider value={publish}>
      <PrimaryPagerPositionContext.Provider value={value}>
        {children}
      </PrimaryPagerPositionContext.Provider>
    </PrimaryPagerPositionPublishContext.Provider>
  );
}

export function usePrimaryPagerPosition(): PrimaryPagerPositionNode | null {
  return useContext(PrimaryPagerPositionContext).position;
}

/**
 * Invisible TopTabs tabBar bridge: mounts so it can publish `position`, then
 * renders nothing (visible control lives in the shared Header).
 */
export function PrimaryPagerPositionBridge({
  position,
}: {
  position: PrimaryPagerPositionNode;
}) {
  const publish = useContext(PrimaryPagerPositionPublishContext);

  useLayoutEffect(() => {
    publish?.(position);
  }, [position, publish]);

  return null;
}
