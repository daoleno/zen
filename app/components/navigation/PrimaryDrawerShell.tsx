import React, { useMemo, useRef, type ReactNode, type RefObject } from "react";
import {
  Platform,
  Pressable,
  StyleSheet,
  View,
  useWindowDimensions,
  type View as ViewInstance,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useIsFocused, useRootNavigationState } from "expo-router";
import { GestureDetector } from "react-native-gesture-handler";
import Animated from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useAppColors } from "../../constants/tokens";
import { PrimaryDrawerPanel } from "./PrimaryDrawerPanel";
import {
  PrimarySurfaceInteractionProvider,
} from "./PrimarySurfaceInteraction";
import { PrimaryTopSwitch } from "./PrimaryTopSwitch";
import { useDrawerFocusContainment } from "./useDrawerFocusContainment";
import { usePrimaryDrawerController } from "./usePrimaryDrawerController";
import { useWebOverlayTap } from "./useWebOverlayTap";

interface PrimaryDrawerShellProps {
  children: ReactNode;
}

interface PrimaryAppBarProps {
  drawerVisible: boolean;
  menuButtonRef: RefObject<ViewInstance | null>;
  onOpenDrawer(): void;
  onOpenPressIn(): void;
  topInset: number;
}

function PrimaryAppBar({
  drawerVisible,
  menuButtonRef,
  onOpenDrawer,
  onOpenPressIn,
  topInset,
}: PrimaryAppBarProps) {
  const colors = useAppColors();
  return (
    <View
      style={[
        styles.appBar,
        {
          paddingTop: topInset,
          minHeight: topInset + 52,
          backgroundColor: colors.bgPrimary,
          borderBottomColor: colors.borderSubtle,
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
        style={styles.menuButton}
      >
        <Ionicons name="menu-outline" size={23} color={colors.textPrimary} />
      </Pressable>
      <PrimaryTopSwitch />
      <View style={styles.appBarSpacer} />
    </View>
  );
}

export function PrimaryDrawerShell({ children }: PrimaryDrawerShellProps) {
  const colors = useAppColors();
  const { width: windowWidth } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  const routeFocused = useIsFocused();
  const rootNavigationState = useRootNavigationState();
  const drawerWidth = Math.min(320, Math.max(240, windowWidth - 52));
  const gestureEnabled =
    routeFocused &&
    (Platform.OS !== "ios" || rootNavigationState.index === 0);
  const controller = usePrimaryDrawerController({
    drawerWidth,
    gestureEnabled,
    routeFocused,
  });
  const primaryRef = useRef<ViewInstance>(null);
  const drawerRef = useRef<ViewInstance>(null);
  const overlayRef = useRef<ViewInstance>(null);
  const menuButtonRef = useRef<ViewInstance>(null);
  const closeButtonRef = useRef<ViewInstance>(null);
  const restoreMenuFocus =
    controller.state.phase === "closed" &&
    controller.state.focusReturn === "menu";
  useWebOverlayTap({
    enabled: controller.state.phase === "open",
    onActivate: controller.closeDrawerFromWebOverlay,
    onBegin: controller.beginWebOverlayInteraction,
    onCancel: controller.cancelWebOverlayInteraction,
    overlayRef,
  });

  useDrawerFocusContainment({
    closeButtonRef,
    drawerRef,
    drawerVisible: controller.drawerVisible,
    menuButtonRef,
    onMenuFocusRestored: controller.consumeMenuFocusReturn,
    primaryRef,
    restoreMenuFocus,
    routeFocused,
  });

  const primaryWebProps = useMemo(
    () => (Platform.OS === "web" ? { inert: controller.drawerVisible } : {}),
    [controller.drawerVisible],
  );
  const drawerWebProps = useMemo(
    () =>
      Platform.OS === "web"
        ? {
            inert: !controller.drawerVisible,
            role: "dialog" as const,
            "aria-modal": controller.drawerVisible,
          }
        : {},
    [controller.drawerVisible],
  );

  return (
    <PrimarySurfaceInteractionProvider
      drawerPhase={controller.state.phase}
      routeFocused={routeFocused}
    >
      <GestureDetector gesture={controller.gesture}>
        <View style={[styles.root, { backgroundColor: colors.bgPrimary }]}>
          <Animated.View
            {...primaryWebProps}
            ref={primaryRef}
            style={styles.primary}
            pointerEvents={controller.drawerVisible ? "none" : "auto"}
            accessibilityElementsHidden={controller.drawerVisible}
            aria-hidden={controller.drawerVisible}
            importantForAccessibility={
              controller.drawerVisible ? "no-hide-descendants" : "auto"
            }
          >
            <PrimaryAppBar
              drawerVisible={controller.drawerVisible}
              menuButtonRef={menuButtonRef}
              onOpenDrawer={controller.openDrawer}
              onOpenPressIn={controller.beginOpenInteraction}
              topInset={insets.top}
            />
            <View style={styles.content}>{children}</View>
          </Animated.View>

          <Animated.View
            ref={overlayRef}
            pointerEvents={controller.drawerVisible ? "auto" : "none"}
            accessible={false}
            accessibilityElementsHidden
            aria-hidden
            importantForAccessibility="no-hide-descendants"
            style={[
              styles.overlay,
              { backgroundColor: colors.modalBackdrop },
              controller.overlayStyle,
            ]}
          />

          <Animated.View
            {...drawerWebProps}
            ref={drawerRef}
            onLayout={controller.onDrawerLayout}
            pointerEvents={controller.drawerVisible ? "auto" : "none"}
            accessibilityElementsHidden={!controller.drawerVisible}
            aria-hidden={!controller.drawerVisible}
            importantForAccessibility={
              controller.drawerVisible ? "yes" : "no-hide-descendants"
            }
            accessibilityViewIsModal={controller.drawerVisible}
            accessibilityLabel="Navigation drawer"
            style={[
              styles.drawer,
              {
                width: drawerWidth,
                backgroundColor: colors.bgSurface,
                borderRightColor: colors.borderSubtle,
              },
              controller.drawerStyle,
            ]}
          >
            <PrimaryDrawerPanel
              closeButtonRef={closeButtonRef}
              drawerVisible={controller.drawerVisible}
              onClose={controller.closeDrawer}
              onClosePressIn={controller.beginCloseInteraction}
              onNavigateAway={controller.dismissForNavigation}
            />
          </Animated.View>
        </View>
      </GestureDetector>
    </PrimarySurfaceInteractionProvider>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    overflow: "hidden",
  },
  primary: {
    flex: 1,
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
  menuButton: {
    width: 52,
    minHeight: 52,
    alignItems: "center",
    justifyContent: "center",
  },
  appBarSpacer: {
    width: 52,
    minHeight: 52,
  },
  overlay: {
    ...StyleSheet.absoluteFill,
    zIndex: 3,
  },
  drawer: {
    position: "absolute",
    left: 0,
    top: 0,
    bottom: 0,
    zIndex: 4,
    borderRightWidth: StyleSheet.hairlineWidth,
  },
});
