import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type ScrollView, type View } from "react-native";
import {
  buildMenuPosition,
  type MenuAnchorLayout,
} from "./TerminalScreenModel";

interface UseTerminalChromeLayoutInput {
  sessionKey: string | null;
  windowWidth: number;
  popoverWidth: number;
}

export function useTerminalChromeLayout({
  sessionKey,
  windowWidth,
  popoverWidth,
}: UseTerminalChromeLayoutInput) {
  const [menuVisible, setMenuVisible] = useState(false);
  const [menuAnchor, setMenuAnchor] = useState<MenuAnchorLayout | null>(null);
  const tabScrollRef = useRef<ScrollView>(null);
  const tabLayoutsRef = useRef<Map<string, { x: number; width: number }>>(
    new Map(),
  );
  const menuAnchorRef = useRef<View | null>(null);

  const menuPosition = useMemo(
    () => buildMenuPosition(menuAnchor, windowWidth, popoverWidth),
    [menuAnchor, popoverWidth, windowWidth],
  );

  const closeMenu = useCallback(() => {
    setMenuVisible(false);
    setMenuAnchor(null);
  }, []);

  const openMenu = useCallback(() => {
    const anchor = menuAnchorRef.current;
    if (!anchor) {
      setMenuAnchor(null);
      setMenuVisible(true);
      return;
    }

    anchor.measureInWindow((x, y, width, height) => {
      setMenuAnchor({ x, y, width, height });
      setMenuVisible(true);
    });
  }, []);

  const handleTabLayout = useCallback(
    (tabId: string, layout: { x: number; width: number }) => {
      tabLayoutsRef.current.set(tabId, layout);
    },
    [],
  );

  useEffect(() => {
    closeMenu();
  }, [closeMenu, sessionKey]);

  useEffect(() => {
    if (!sessionKey) return;
    const layout = tabLayoutsRef.current.get(sessionKey);
    if (layout && tabScrollRef.current) {
      const scrollTo = Math.max(0, layout.x - 40);
      tabScrollRef.current.scrollTo({ x: scrollTo, animated: true });
    }
  }, [sessionKey]);

  return {
    closeMenu,
    handleTabLayout,
    menuAnchorRef,
    menuPosition,
    menuVisible,
    openMenu,
    tabScrollRef,
  };
}
