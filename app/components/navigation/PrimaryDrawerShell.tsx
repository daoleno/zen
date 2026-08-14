import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from "react";
import {
  Keyboard,
  Platform,
  Pressable,
  StyleSheet,
  View,
  useWindowDimensions,
  type View as ViewInstance,
} from "react-native";
import { useIsFocused } from "expo-router";
import { Drawer } from "react-native-drawer-layout";
import type { PanGesture } from "react-native-gesture-handler";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useAppColors } from "../../constants/tokens";
import type { PrimaryRouteName } from "../../services/interactionTrace";
import { NavMenuIcon } from "./PrimaryNavIcons";
import {
  PrimaryAppBarPageAction,
  PrimaryPageActionProvider,
} from "./PrimaryPageAction";
import { PrimaryDrawerPanel } from "./PrimaryDrawerPanel";
import {
  PrimarySurfaceInteractionProvider,
} from "./PrimarySurfaceInteraction";
import { PrimaryTopSwitch } from "./PrimaryTopSwitch";
import {
  PrimarySelectionBarProvider,
  usePrimarySelectionBarContent,
} from "./PrimarySelectionBar";
import { resolvePrimaryAppBarGeometry } from "./primaryAppBarGeometry";
import { useDrawerFocusContainment } from "./useDrawerFocusContainment";
import { usePrimaryDrawerBack } from "./usePrimaryDrawerBack";

export {
  PRIMARY_APP_BAR_HEIGHT,
  PRIMARY_APP_BAR_LAYOUT_MODE,
  resolvePrimaryAppBarGeometry,
} from "./primaryAppBarGeometry";

interface PrimaryDrawerShellProps {
  activePrimaryRoute: PrimaryRouteName;
  children: ReactNode;
  onSelectPrimaryRoute(route: PrimaryRouteName): void;
}

interface PrimaryAppBarProps {
  activePrimaryRoute: PrimaryRouteName;
  drawerVisible: boolean;
  menuButtonRef: RefObject<ViewInstance | null>;
  onOpenDrawer(): void;
  onOpenPressIn(): void;
  onSelectPrimaryRoute(route: PrimaryRouteName): void;
  topInset: number;
}

const PRIMARY_DRAWER_SWIPE_EDGE_WIDTH = 40;

function PrimaryAppBar({
  activePrimaryRoute,
  drawerVisible,
  menuButtonRef,
  onOpenDrawer,
  onOpenPressIn,
  onSelectPrimaryRoute,
  topInset,
}: PrimaryAppBarProps) {
  const colors = useAppColors();
  const geometry = resolvePrimaryAppBarGeometry(topInset);
  const showBrainCanvas = activePrimaryRoute === "brain";
  const selectionBar = usePrimarySelectionBarContent();
  if (selectionBar != null) {
    return (
      <View
        style={[
          styles.appBar,
          styles.appBarOverlay,
          {
            paddingTop: geometry.safeAreaTop,
            minHeight: geometry.contentInset,
            backgroundColor: colors.bgPrimary,
            borderBottomColor: colors.borderSubtle,
          },
        ]}
      >
        <View style={styles.selectionBarSlot}>{selectionBar}</View>
      </View>
    );
  }
  return (
    <View
      style={[
        styles.appBar,
        styles.appBarOverlay,
        {
          paddingTop: geometry.safeAreaTop,
          minHeight: geometry.contentInset,
          backgroundColor: showBrainCanvas ? "transparent" : colors.bgPrimary,
          borderBottomColor: showBrainCanvas
            ? "transparent"
            : colors.borderSubtle,
        },
      ]}
    >
      <Pressable
        ref={menuButtonRef}
        onPress={onOpenDrawer}
        onPressIn={onOpenPressIn}
        accessibilityRole="button"
        accessibilityLabel="Open navigation drawer"
        tabIndex={drawerVisible ? -1 : 0}
        hitSlop={6}
        style={({ pressed }) => [
          styles.menuButton,
          pressed ? styles.pressedIcon : null,
        ]}
      >
        <NavMenuIcon color={colors.textPrimary} size={22} />
      </Pressable>
      <PrimaryTopSwitch
        activeRoute={activePrimaryRoute}
        onSelectRoute={onSelectPrimaryRoute}
      />
      <PrimaryAppBarPageAction drawerVisible={drawerVisible} />
    </View>
  );
}

export function PrimaryDrawerShell({
  activePrimaryRoute,
  children,
  onSelectPrimaryRoute,
}: PrimaryDrawerShellProps) {
  const colors = useAppColors();
  const { width: windowWidth } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  const routeFocused = useIsFocused();
  const drawerWidth = Math.min(320, Math.max(240, windowWidth - 52));
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [restoreMenuFocus, setRestoreMenuFocus] = useState(false);
  const drawerWasOpenRef = useRef(false);
  const primaryRef = useRef<ViewInstance>(null);
  const drawerRef = useRef<ViewInstance>(null);
  const menuButtonRef = useRef<ViewInstance>(null);
  const closeButtonRef = useRef<ViewInstance>(null);

  const beginOpenInteraction = useCallback(() => {
    setRestoreMenuFocus(false);
  }, []);
  const openDrawer = useCallback(() => {
    drawerWasOpenRef.current = true;
    setRestoreMenuFocus(false);
    Keyboard.dismiss();
    setDrawerOpen(true);
  }, []);
  const closeDrawer = useCallback(() => {
    if (drawerWasOpenRef.current) {
      setRestoreMenuFocus(true);
    }
    drawerWasOpenRef.current = false;
    setDrawerOpen(false);
  }, []);
  const dismissDrawerForNavigation = useCallback(() => {
    drawerWasOpenRef.current = false;
    setRestoreMenuFocus(false);
    setDrawerOpen(false);
  }, []);
  const consumeMenuFocusReturn = useCallback(() => {
    setRestoreMenuFocus(false);
  }, []);
  const configureDrawerGesture = useCallback(
    (gesture: PanGesture) => {
      if (drawerOpen) {
        return gesture
          .activeOffsetX([-12, windowWidth])
          .failOffsetX(12);
      }
      return gesture
        .activeOffsetX([-windowWidth, 12])
        .failOffsetX(-12);
    },
    [drawerOpen, windowWidth],
  );

  useEffect(() => {
    if (!routeFocused && drawerOpen) {
      dismissDrawerForNavigation();
    }
  }, [dismissDrawerForNavigation, drawerOpen, routeFocused]);

  useEffect(() => {
    if (Platform.OS !== "web" || !drawerOpen) {
      return;
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      closeDrawer();
    };
    document.addEventListener("keydown", handleKeyDown, true);
    return () => document.removeEventListener("keydown", handleKeyDown, true);
  }, [closeDrawer, drawerOpen]);

  useDrawerFocusContainment({
    closeButtonRef,
    drawerRef,
    drawerVisible: drawerOpen,
    menuButtonRef,
    onMenuFocusRestored: consumeMenuFocusReturn,
    primaryRef,
    restoreMenuFocus,
    routeFocused,
  });
  usePrimaryDrawerBack({
    enabled: routeFocused && drawerOpen,
    onBack: closeDrawer,
  });

  const primaryWebProps = useMemo(
    () => (Platform.OS === "web" ? { inert: drawerOpen } : {}),
    [drawerOpen],
  );
  const drawerWebProps = useMemo(
    () =>
      Platform.OS === "web"
        ? {
            inert: !drawerOpen,
            role: "dialog" as const,
            "aria-modal": drawerOpen,
          }
        : {},
    [drawerOpen],
  );

  const renderDrawerContent = useCallback(
    () => (
      <View
        {...drawerWebProps}
        ref={drawerRef}
        accessibilityElementsHidden={!drawerOpen}
        aria-hidden={!drawerOpen}
        importantForAccessibility={
          drawerOpen ? "yes" : "no-hide-descendants"
        }
        accessibilityViewIsModal={drawerOpen}
        accessibilityLabel="Navigation drawer"
        style={styles.drawerContent}
      >
        <PrimaryDrawerPanel
          closeButtonRef={closeButtonRef}
          drawerVisible={drawerOpen}
          onClose={closeDrawer}
          onClosePressIn={() => undefined}
          onNavigateAway={dismissDrawerForNavigation}
        />
      </View>
    ),
    [
      closeDrawer,
      dismissDrawerForNavigation,
      drawerOpen,
      drawerWebProps,
    ],
  );

  return (
    <PrimaryPageActionProvider>
      <PrimarySelectionBarProvider>
      <PrimarySurfaceInteractionProvider
        drawerPhase={drawerOpen ? "open" : "closed"}
        routeFocused={routeFocused}
      >
        <Drawer
          configureGestureHandler={configureDrawerGesture}
          drawerPosition="left"
          drawerStyle={{
            width: drawerWidth,
            backgroundColor: colors.bgSurface,
            borderRightColor: colors.borderSubtle,
            borderRightWidth: StyleSheet.hairlineWidth,
          }}
          drawerType="front"
          keyboardDismissMode="on-drag"
          onClose={closeDrawer}
          onGestureStart={beginOpenInteraction}
          onOpen={openDrawer}
          open={drawerOpen}
          overlayAccessibilityLabel="Close navigation drawer"
          overlayStyle={{ backgroundColor: colors.modalBackdrop }}
          renderDrawerContent={renderDrawerContent}
          style={[styles.root, { backgroundColor: colors.bgPrimary }]}
          swipeEdgeWidth={PRIMARY_DRAWER_SWIPE_EDGE_WIDTH}
          swipeEnabled={routeFocused && activePrimaryRoute === "brain"}
        >
          <View
            {...primaryWebProps}
            ref={primaryRef}
            style={styles.primary}
            pointerEvents={drawerOpen ? "none" : "auto"}
            accessibilityElementsHidden={drawerOpen}
            aria-hidden={drawerOpen}
            importantForAccessibility={
              drawerOpen ? "no-hide-descendants" : "auto"
            }
          >
            <PrimaryAppBar
              activePrimaryRoute={activePrimaryRoute}
              drawerVisible={drawerOpen}
              menuButtonRef={menuButtonRef}
              onOpenDrawer={openDrawer}
              onOpenPressIn={beginOpenInteraction}
              onSelectPrimaryRoute={onSelectPrimaryRoute}
              topInset={insets.top}
            />
            <View style={styles.content}>{children}</View>
          </View>
        </Drawer>
      </PrimarySurfaceInteractionProvider>
      </PrimarySelectionBarProvider>
    </PrimaryPageActionProvider>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    overflow: "hidden",
  },
  primary: {
    flex: 1,
    position: "relative",
  },
  content: {
    flex: 1,
  },
  appBar: {
    width: "100%",
    flexDirection: "row",
    alignItems: "flex-end",
    justifyContent: "space-between",
    borderBottomWidth: StyleSheet.hairlineWidth,
    zIndex: 2,
  },
  appBarOverlay: {
    position: "absolute",
    top: 0,
    right: 0,
    left: 0,
    zIndex: 5,
  },
  menuButton: {
    width: 52,
    minWidth: 44,
    minHeight: 52,
    borderRadius: 10,
    alignItems: "center",
    justifyContent: "center",
  },
  pressedIcon: {
    opacity: 0.55,
  },
  selectionBarSlot: {
    flex: 1,
    alignSelf: "stretch",
    minWidth: 0,
  },
  drawerContent: {
    flex: 1,
  },
});
