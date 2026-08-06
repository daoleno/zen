import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type View } from "react-native";
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

  useEffect(() => {
    closeMenu();
  }, [closeMenu, sessionKey]);

  return {
    closeMenu,
    menuAnchorRef,
    menuPosition,
    menuVisible,
    openMenu,
  };
}
