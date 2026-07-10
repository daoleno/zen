import { useEffect, useLayoutEffect, useRef, type RefObject } from "react";
import {
  AccessibilityInfo,
  findNodeHandle,
  Platform,
  type View,
} from "react-native";

interface DrawerFocusContainmentOptions {
  closeButtonRef: RefObject<View | null>;
  drawerRef: RefObject<View | null>;
  drawerVisible: boolean;
  menuButtonRef: RefObject<View | null>;
  onMenuFocusRestored(): void;
  primaryRef: RefObject<View | null>;
  restoreMenuFocus: boolean;
  routeFocused: boolean;
}

const FOCUSABLE_SELECTOR = [
  "a[href]:not([tabindex='-1'])",
  "button:not([disabled]):not([tabindex='-1'])",
  "input:not([disabled]):not([tabindex='-1'])",
  "select:not([disabled]):not([tabindex='-1'])",
  "textarea:not([disabled]):not([tabindex='-1'])",
  "[tabindex]:not([tabindex='-1'])",
].join(",");

function asElement(node: View | null): HTMLElement | null {
  return Platform.OS === "web" ? (node as unknown as HTMLElement | null) : null;
}

function setInert(node: View | null, inert: boolean): void {
  const element = asElement(node);
  if (element == null) {
    return;
  }
  element.inert = inert;
  if (inert) {
    element.setAttribute("inert", "");
  } else {
    element.removeAttribute("inert");
  }
}

function focusHost(node: View | null): boolean {
  if (node == null) {
    return false;
  }
  if (Platform.OS === "web") {
    const element = asElement(node);
    if (element == null || !element.isConnected) {
      return false;
    }
    element.focus({ preventScroll: true });
    return document.activeElement === element;
  }
  const reactTag = findNodeHandle(node);
  if (reactTag == null) {
    return false;
  }
  AccessibilityInfo.setAccessibilityFocus(reactTag);
  return true;
}

function getFocusableElements(drawer: HTMLElement): HTMLElement[] {
  return Array.from(drawer.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
    (element) =>
      element.getAttribute("aria-hidden") !== "true" &&
      !element.hasAttribute("disabled") &&
      !element.inert &&
      element.getClientRects().length > 0,
  );
}

export function useDrawerFocusContainment({
  closeButtonRef,
  drawerRef,
  drawerVisible,
  menuButtonRef,
  onMenuFocusRestored,
  primaryRef,
  restoreMenuFocus,
  routeFocused,
}: DrawerFocusContainmentOptions): void {
  const previousVisibleRef = useRef(false);

  useLayoutEffect(() => {
    setInert(primaryRef.current, drawerVisible);
    setInert(drawerRef.current, !drawerVisible);

    const enteringDrawer = drawerVisible && !previousVisibleRef.current;
    previousVisibleRef.current = drawerVisible;

    if (enteringDrawer) {
      focusHost(closeButtonRef.current);
      return;
    }

    if (!drawerVisible && restoreMenuFocus && routeFocused) {
      if (focusHost(menuButtonRef.current)) {
        onMenuFocusRestored();
      }
    }
  }, [
    closeButtonRef,
    drawerRef,
    drawerVisible,
    menuButtonRef,
    onMenuFocusRestored,
    primaryRef,
    restoreMenuFocus,
    routeFocused,
  ]);

  useEffect(
    () => () => {
      setInert(primaryRef.current, false);
      setInert(drawerRef.current, false);
    },
    [drawerRef, primaryRef],
  );

  useEffect(() => {
    if (Platform.OS !== "web" || !drawerVisible) {
      return;
    }
    const drawer = asElement(drawerRef.current);
    if (drawer == null) {
      return;
    }

    const focusCloseButton = () => {
      focusHost(closeButtonRef.current);
    };
    const handleFocusIn = (event: FocusEvent) => {
      if (!(event.target instanceof Node) || drawer.contains(event.target)) {
        return;
      }
      focusCloseButton();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Tab") {
        return;
      }
      const focusable = getFocusableElements(drawer);
      if (focusable.length === 0) {
        event.preventDefault();
        focusCloseButton();
        return;
      }
      const activeElement = document.activeElement;
      const activeIndex = focusable.findIndex(
        (element) => element === activeElement,
      );
      const atStart = activeIndex <= 0;
      const atEnd = activeIndex === focusable.length - 1;
      if (event.shiftKey && atStart) {
        event.preventDefault();
        focusable[focusable.length - 1]?.focus({ preventScroll: true });
      } else if (!event.shiftKey && (activeIndex < 0 || atEnd)) {
        event.preventDefault();
        focusable[0]?.focus({ preventScroll: true });
      }
    };

    document.addEventListener("focusin", handleFocusIn, true);
    document.addEventListener("keydown", handleKeyDown, true);
    return () => {
      document.removeEventListener("focusin", handleFocusIn, true);
      document.removeEventListener("keydown", handleKeyDown, true);
    };
  }, [closeButtonRef, drawerRef, drawerVisible]);
}
