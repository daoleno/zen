import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Keyboard,
  Platform,
  type LayoutChangeEvent,
} from "react-native";
import { Gesture } from "react-native-gesture-handler";
import {
  cancelAnimation,
  runOnJS,
  useAnimatedStyle,
  useReducedMotion,
  useSharedValue,
  withSpring,
} from "react-native-reanimated";
import {
  beginInteraction,
  recordCompletedInteraction,
  ZEN_INTERACTION_TRACE_ENABLED,
  type CompletedInteraction,
  type DrawerTraceSource,
  type ZenInteractionToken,
} from "../../services/interactionTrace";
import {
  clampDrawerOffset,
  clampDrawerVelocity,
  getDrawerProgress,
  getProjectedDrawerTarget,
  PrimaryDrawerMotion,
  PrimaryDrawerSpring,
} from "./primaryDrawerMotion";
import {
  INITIAL_DRAWER_STATE,
  isDrawerVisible,
  transitionDrawerState,
  type DrawerFocusReturn,
  type DrawerState,
  type DrawerStateEvent,
} from "./primaryDrawerState";
import { usePrimaryDrawerBack } from "./usePrimaryDrawerBack";

type DrawerDirection = "close" | "open";
type DrawerEndpoint = "closed" | "open";
type DrawerTraceToken =
  | ZenInteractionToken<"drawer.close">
  | ZenInteractionToken<"drawer.open">;
type UiDrawerTrace =
  | CompletedInteraction<"drawer.close">
  | CompletedInteraction<"drawer.open">;

interface PendingDrawerTrace {
  direction: DrawerDirection;
  token: DrawerTraceToken;
}

interface UsePrimaryDrawerControllerOptions {
  drawerWidth: number;
  gestureEnabled: boolean;
  routeFocused: boolean;
}

export interface PrimaryDrawerController {
  beginCloseInteraction(): void;
  beginOpenInteraction(): void;
  beginWebOverlayInteraction(): boolean;
  cancelWebOverlayInteraction(): void;
  closeDrawer(): void;
  closeDrawerFromWebOverlay(): void;
  consumeMenuFocusReturn(): void;
  dismissForNavigation(): void;
  drawerStyle: ReturnType<typeof useAnimatedStyle>;
  drawerVisible: boolean;
  gesture: ReturnType<typeof Gesture.Race>;
  onDrawerLayout(event: LayoutChangeEvent): void;
  openDrawer(): void;
  overlayStyle: ReturnType<typeof useAnimatedStyle>;
  state: DrawerState;
}

export function usePrimaryDrawerController({
  drawerWidth,
  gestureEnabled,
  routeFocused,
}: UsePrimaryDrawerControllerOptions): PrimaryDrawerController {
  const reducedMotion = useReducedMotion();
  const [state, setState] = useState<DrawerState>(INITIAL_DRAWER_STATE);
  const stateRef = useRef<DrawerState>(INITIAL_DRAWER_STATE);
  const pendingTraceRef = useRef<PendingDrawerTrace | null>(null);
  const measuredWidthRef = useRef(drawerWidth);

  const drawerOffsetPx = useSharedValue(-drawerWidth);
  const drawerWidthPx = useSharedValue(drawerWidth);
  const gestureStartOffsetPx = useSharedValue(-drawerWidth);
  const gestureStartX = useSharedValue(0);
  const gestureStartY = useSharedValue(0);
  const gestureAnchorOpen = useSharedValue(0);
  const gestureActivated = useSharedValue(0);
  const gestureReleased = useSharedValue(0);
  const settling = useSharedValue(0);
  const uiTraceDirection = useSharedValue(0);
  const uiTraceSource = useSharedValue(0);
  const uiTraceStartAt = useSharedValue(0);
  const uiTraceActivationAt = useSharedValue(0);
  const uiTraceReleaseAt = useSharedValue(0);
  const overlayTapTracking = useSharedValue(0);

  const transition = useCallback((event: DrawerStateEvent) => {
    const current = stateRef.current;
    const next = transitionDrawerState(current, event);
    if (next === current) {
      return false;
    }
    stateRef.current = next;
    setState(next);
    return true;
  }, []);

  const clearUiTrace = useCallback(() => {
    "worklet";
    uiTraceDirection.value = 0;
    uiTraceSource.value = 0;
    uiTraceStartAt.value = 0;
    uiTraceActivationAt.value = 0;
    uiTraceReleaseAt.value = 0;
  }, [
    uiTraceActivationAt,
    uiTraceDirection,
    uiTraceReleaseAt,
    uiTraceSource,
    uiTraceStartAt,
  ]);

  const collectUiTrace = useCallback(
    (target: DrawerEndpoint, cancelled: boolean): UiDrawerTrace | null => {
      "worklet";
      if (
        !ZEN_INTERACTION_TRACE_ENABLED ||
        uiTraceDirection.value === 0 ||
        uiTraceStartAt.value === 0
      ) {
        clearUiTrace();
        return null;
      }
      const name =
        uiTraceDirection.value === 1 ? "drawer.open" : "drawer.close";
      const source = uiTraceSource.value === 2 ? "overlay" : "gesture";
      const completed = {
        name,
        metadata: { source, target },
        startAt: uiTraceStartAt.value,
        activationAt:
          uiTraceActivationAt.value > 0
            ? uiTraceActivationAt.value
            : undefined,
        releaseAt:
          uiTraceReleaseAt.value > 0 ? uiTraceReleaseAt.value : undefined,
        endAt: performance.now(),
        cancelled,
      } as UiDrawerTrace;
      clearUiTrace();
      return completed;
    },
    [
      clearUiTrace,
      uiTraceActivationAt,
      uiTraceDirection,
      uiTraceReleaseAt,
      uiTraceSource,
      uiTraceStartAt,
    ],
  );

  const cancelPendingTrace = useCallback(() => {
    pendingTraceRef.current?.token.cancel();
    pendingTraceRef.current = null;
  }, []);

  const beginProgrammaticTrace = useCallback(
    (direction: DrawerDirection, source: DrawerTraceSource) => {
      cancelPendingTrace();
      const target = direction === "open" ? "open" : "closed";
      const token =
        direction === "open"
          ? beginInteraction("drawer.open", { source, target })
          : beginInteraction("drawer.close", { source, target });
      const pending: PendingDrawerTrace = { direction, token };
      pendingTraceRef.current = pending;
      return pending;
    },
    [cancelPendingTrace],
  );

  const activateProgrammaticTrace = useCallback(
    (direction: DrawerDirection, source: DrawerTraceSource) => {
      const pending =
        pendingTraceRef.current?.direction === direction
          ? pendingTraceRef.current
          : beginProgrammaticTrace(direction, source);
      pending.token.markActivation();
      pending.token.markRelease();
    },
    [beginProgrammaticTrace],
  );

  const handleSettled = useCallback(
    (
      target: DrawerEndpoint,
      focusReturn: DrawerFocusReturn,
      completedTrace: UiDrawerTrace | null,
    ) => {
      transition({ type: "settled", target, focusReturn });
      if (completedTrace != null) {
        cancelPendingTrace();
        if (completedTrace.name === "drawer.open") {
          recordCompletedInteraction(completedTrace);
        } else {
          recordCompletedInteraction(completedTrace);
        }
        return;
      }
      pendingTraceRef.current?.token.end();
      pendingTraceRef.current = null;
    },
    [cancelPendingTrace, transition],
  );

  const handleGestureActivation = useCallback(
    (anchor: "closed" | "open") => {
      cancelPendingTrace();
      transition({ type: "gesture-activated", anchor });
      Keyboard.dismiss();
    },
    [cancelPendingTrace, transition],
  );

  const handleSettleStart = useCallback(
    (target: DrawerEndpoint, focusReturn: DrawerFocusReturn) => {
      transition({ type: "settle-started", target, focusReturn });
    },
    [transition],
  );

  const animateTo = useCallback(
    (
      target: DrawerEndpoint,
      velocity: number,
      focusReturn: DrawerFocusReturn,
      usesUiTrace: boolean,
    ) => {
      cancelAnimation(drawerOffsetPx);
      settling.value = 1;
      handleSettleStart(target, focusReturn);
      const targetOffset = target === "open" ? 0 : -drawerWidthPx.value;
      const clampedVelocity = clampDrawerVelocity(velocity);

      if (reducedMotion) {
        drawerOffsetPx.value = targetOffset;
        settling.value = 0;
        handleSettled(
          target,
          focusReturn,
          usesUiTrace ? collectUiTrace(target, false) : null,
        );
        return;
      }

      drawerOffsetPx.value = withSpring(
        targetOffset,
        { ...PrimaryDrawerSpring, velocity: clampedVelocity },
        (finished) => {
          if (!finished) {
            return;
          }
          settling.value = 0;
          const completedTrace = usesUiTrace
            ? collectUiTrace(target, false)
            : null;
          runOnJS(handleSettled)(target, focusReturn, completedTrace);
        },
      );
    },
    [
      collectUiTrace,
      drawerOffsetPx,
      drawerWidthPx,
      handleSettleStart,
      handleSettled,
      reducedMotion,
      settling,
    ],
  );

  const beginOpenInteraction = useCallback(() => {
    if (stateRef.current.phase !== "closed") {
      return;
    }
    beginProgrammaticTrace("open", "menu");
  }, [beginProgrammaticTrace]);

  const openDrawer = useCallback(() => {
    if (stateRef.current.phase !== "closed") {
      cancelPendingTrace();
      return;
    }
    activateProgrammaticTrace("open", "menu");
    Keyboard.dismiss();
    animateTo("open", 0, "none", false);
  }, [
    activateProgrammaticTrace,
    animateTo,
    cancelPendingTrace,
  ]);

  const beginCloseInteraction = useCallback(() => {
    if (
      stateRef.current.phase === "closed" ||
      stateRef.current.phase === "settling-closed"
    ) {
      return;
    }
    beginProgrammaticTrace("close", "close-button");
  }, [beginProgrammaticTrace]);

  const closeDrawerFrom = useCallback(
    (source: "back" | "close-button" | "escape") => {
      if (stateRef.current.phase === "settling-closed") {
        return;
      }
      if (stateRef.current.phase === "closed") {
        cancelPendingTrace();
        return;
      }
      activateProgrammaticTrace("close", source);
      clearUiTrace();
      animateTo("closed", 0, "menu", false);
    },
    [
      activateProgrammaticTrace,
      animateTo,
      cancelPendingTrace,
      clearUiTrace,
    ],
  );

  const closeDrawer = useCallback(() => {
    closeDrawerFrom("close-button");
  }, [closeDrawerFrom]);

  const closeDrawerFromBack = useCallback(() => {
    closeDrawerFrom("back");
  }, [closeDrawerFrom]);

  const closeDrawerFromOverlay = useCallback(() => {
    if (
      stateRef.current.phase === "settling-open" ||
      stateRef.current.phase === "settling-closed" ||
      stateRef.current.phase === "closed"
    ) {
      return;
    }
    cancelPendingTrace();
    animateTo("closed", 0, "menu", true);
  }, [animateTo, cancelPendingTrace, clearUiTrace]);

  const beginWebOverlayInteraction = useCallback(() => {
    if (stateRef.current.phase !== "open") {
      return false;
    }
    beginProgrammaticTrace("close", "overlay");
    return true;
  }, [beginProgrammaticTrace]);

  const cancelWebOverlayInteraction = useCallback(() => {
    if (pendingTraceRef.current?.direction !== "close") {
      return;
    }
    cancelPendingTrace();
  }, [cancelPendingTrace]);

  const closeDrawerFromWebOverlay = useCallback(() => {
    if (stateRef.current.phase !== "open") {
      cancelWebOverlayInteraction();
      return;
    }
    activateProgrammaticTrace("close", "overlay");
    clearUiTrace();
    animateTo("closed", 0, "menu", false);
  }, [
    activateProgrammaticTrace,
    animateTo,
    cancelWebOverlayInteraction,
    clearUiTrace,
  ]);

  const resetWithoutFocus = useCallback(() => {
    cancelAnimation(drawerOffsetPx);
    cancelPendingTrace();
    clearUiTrace();
    settling.value = 0;
    gestureActivated.value = 0;
    gestureReleased.value = 0;
    drawerOffsetPx.value = -drawerWidthPx.value;
    gestureStartOffsetPx.value = -drawerWidthPx.value;
    transition({ type: "reset" });
  }, [
    cancelPendingTrace,
    clearUiTrace,
    drawerOffsetPx,
    drawerWidthPx,
    gestureActivated,
    gestureReleased,
    gestureStartOffsetPx,
    settling,
    transition,
  ]);

  const dismissForNavigation = useCallback(() => {
    if (stateRef.current.phase === "closed") {
      return;
    }
    cancelPendingTrace();
    clearUiTrace();
    const token = beginInteraction("drawer.close", {
      source: "navigation",
      target: "closed",
    });
    token.markActivation();
    token.markRelease();
    cancelAnimation(drawerOffsetPx);
    settling.value = 0;
    gestureActivated.value = 0;
    gestureReleased.value = 0;
    drawerOffsetPx.value = -drawerWidthPx.value;
    gestureStartOffsetPx.value = -drawerWidthPx.value;
    transition({ type: "reset" });
    token.end();
  }, [
    cancelPendingTrace,
    clearUiTrace,
    drawerOffsetPx,
    drawerWidthPx,
    gestureActivated,
    gestureReleased,
    gestureStartOffsetPx,
    settling,
    transition,
  ]);

  const consumeMenuFocusReturn = useCallback(() => {
    transition({ type: "focus-returned" });
  }, [transition]);

  useEffect(() => {
    if (!routeFocused) {
      resetWithoutFocus();
    }
  }, [resetWithoutFocus, routeFocused]);

  useEffect(
    () => () => {
      pendingTraceRef.current?.token.cancel();
      pendingTraceRef.current = null;
    },
    [],
  );

  useEffect(() => {
    if (
      state.phase === "closed" &&
      drawerOffsetPx.value !== -drawerWidthPx.value
    ) {
      drawerOffsetPx.value = -drawerWidthPx.value;
    } else if (state.phase === "open" && drawerOffsetPx.value !== 0) {
      drawerOffsetPx.value = 0;
    }
  }, [drawerOffsetPx, drawerWidthPx, state.phase]);

  usePrimaryDrawerBack({
    enabled: routeFocused && isDrawerVisible(state),
    onBack: closeDrawerFromBack,
  });

  useEffect(() => {
    if (Platform.OS !== "web" || !routeFocused || !isDrawerVisible(state)) {
      return;
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape") {
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      closeDrawerFrom("escape");
    };
    document.addEventListener("keydown", handleKeyDown, true);
    return () => document.removeEventListener("keydown", handleKeyDown, true);
  }, [closeDrawerFrom, routeFocused, state]);

  const panGesture = useMemo(() => {
    const settleFromWorklet = (
      target: DrawerEndpoint,
      velocity: number,
      cancelled: boolean,
    ) => {
      "worklet";
      settling.value = 1;
      const focusReturn: DrawerFocusReturn =
        target === "closed" ? "menu" : "none";
      runOnJS(handleSettleStart)(target, focusReturn);
      const targetOffset = target === "open" ? 0 : -drawerWidthPx.value;

      if (reducedMotion) {
        drawerOffsetPx.value = targetOffset;
        settling.value = 0;
        const completedTrace = collectUiTrace(target, cancelled);
        runOnJS(handleSettled)(target, focusReturn, completedTrace);
        return;
      }

      drawerOffsetPx.value = withSpring(
        targetOffset,
        {
          ...PrimaryDrawerSpring,
          velocity: clampDrawerVelocity(velocity),
        },
        (finished) => {
          if (!finished) {
            return;
          }
          settling.value = 0;
          const completedTrace = collectUiTrace(target, cancelled);
          runOnJS(handleSettled)(target, focusReturn, completedTrace);
        },
      );
    };

    return Gesture.Pan()
      .enabled(gestureEnabled)
      .manualActivation(true)
      .maxPointers(1)
      .enableTrackpadTwoFingerGesture(false)
      .shouldCancelWhenOutside(false)
      .onTouchesDown((event, stateManager) => {
        gestureActivated.value = 0;
        gestureReleased.value = 0;
        if (
          settling.value === 1 ||
          event.numberOfTouches !== 1 ||
          !event.allTouches[0]
        ) {
          stateManager.fail();
          return;
        }

        const touch = event.allTouches[0];
        const width = drawerWidthPx.value;
        const fullyOpen = drawerOffsetPx.value >= -0.5;
        const fullyClosed = drawerOffsetPx.value <= -width + 0.5;
        if (!fullyOpen && !fullyClosed) {
          stateManager.fail();
          return;
        }
        if (
          fullyClosed &&
          touch.absoluteX > PrimaryDrawerMotion.edgeWidth
        ) {
          stateManager.fail();
          return;
        }

        gestureAnchorOpen.value = fullyOpen ? 1 : 0;
        gestureStartX.value = touch.absoluteX;
        gestureStartY.value = touch.absoluteY;
        if (ZEN_INTERACTION_TRACE_ENABLED) {
          uiTraceDirection.value = fullyOpen ? 2 : 1;
          uiTraceSource.value = 1;
          uiTraceStartAt.value = performance.now();
          uiTraceActivationAt.value = 0;
          uiTraceReleaseAt.value = 0;
        }
      })
      .onTouchesMove((event, stateManager) => {
        const touch = event.allTouches[0];
        if (!touch || gestureActivated.value === 1) {
          return;
        }

        const dx = touch.absoluteX - gestureStartX.value;
        const dy = touch.absoluteY - gestureStartY.value;
        const absoluteDx = Math.abs(dx);
        const absoluteDy = Math.abs(dy);

        if (gestureAnchorOpen.value === 0) {
          if (
            dx >= PrimaryDrawerMotion.activationDistance &&
            absoluteDx >=
              PrimaryDrawerMotion.horizontalDominance * absoluteDy
          ) {
            stateManager.activate();
            return;
          }
          if (
            absoluteDy >= PrimaryDrawerMotion.verticalFailDistance &&
            absoluteDy > absoluteDx
          ) {
            stateManager.fail();
            return;
          }
          if (dx <= -PrimaryDrawerMotion.wrongDirectionDistance) {
            stateManager.fail();
          }
          return;
        }

        if (
          dx <= -PrimaryDrawerMotion.activationDistance &&
          absoluteDx >= PrimaryDrawerMotion.horizontalDominance * absoluteDy
        ) {
          stateManager.activate();
          return;
        }
        if (
          absoluteDy >= PrimaryDrawerMotion.verticalFailDistance &&
          absoluteDy > absoluteDx
        ) {
          stateManager.fail();
          return;
        }
        if (dx >= PrimaryDrawerMotion.wrongDirectionDistance) {
          stateManager.fail();
        }
      })
      .onStart(() => {
        cancelAnimation(drawerOffsetPx);
        gestureActivated.value = 1;
        gestureStartOffsetPx.value = drawerOffsetPx.value;
        uiTraceSource.value = 1;
        if (ZEN_INTERACTION_TRACE_ENABLED) {
          uiTraceActivationAt.value = performance.now();
        }
        runOnJS(handleGestureActivation)(
          gestureAnchorOpen.value === 1 ? "open" : "closed",
        );
      })
      .onUpdate((event) => {
        if (settling.value === 1) {
          return;
        }
        drawerOffsetPx.value = clampDrawerOffset(
          gestureStartOffsetPx.value + event.translationX,
          drawerWidthPx.value,
        );
      })
      .onEnd((event) => {
        if (settling.value === 1) {
          return;
        }
        gestureReleased.value = 1;
        if (ZEN_INTERACTION_TRACE_ENABLED) {
          uiTraceReleaseAt.value = performance.now();
        }
        const velocity = clampDrawerVelocity(event.velocityX);
        const target = getProjectedDrawerTarget(
          drawerOffsetPx.value,
          drawerWidthPx.value,
          velocity,
        );
        settleFromWorklet(target, velocity, false);
      })
      .onFinalize((_event, success) => {
        if (gestureActivated.value === 0) {
          if (uiTraceSource.value === 1) {
            clearUiTrace();
          }
          return;
        }
        if (success || gestureReleased.value === 1 || settling.value === 1) {
          return;
        }
        const target = gestureAnchorOpen.value === 1 ? "open" : "closed";
        settleFromWorklet(target, 0, true);
      });
  }, [
    clearUiTrace,
    collectUiTrace,
    drawerOffsetPx,
    drawerWidthPx,
    gestureActivated,
    gestureAnchorOpen,
    gestureEnabled,
    gestureReleased,
    gestureStartOffsetPx,
    gestureStartX,
    gestureStartY,
    handleGestureActivation,
    handleSettleStart,
    handleSettled,
    reducedMotion,
    settling,
    uiTraceActivationAt,
    uiTraceDirection,
    uiTraceReleaseAt,
    uiTraceSource,
    uiTraceStartAt,
  ]);

  const overlayTap = useMemo(
    () =>
      Gesture.Tap()
        .enabled(gestureEnabled && Platform.OS !== "web")
        .maxDistance(8)
        .onTouchesDown((event, stateManager) => {
          overlayTapTracking.value = 0;
          const touch = event.allTouches[0];
          const fullyOpen = drawerOffsetPx.value >= -0.5;
          if (
            settling.value === 1 ||
            event.numberOfTouches !== 1 ||
            !touch ||
            !fullyOpen ||
            touch.absoluteX <= drawerWidthPx.value
          ) {
            stateManager.fail();
            return;
          }
          if (ZEN_INTERACTION_TRACE_ENABLED) {
            uiTraceDirection.value = 2;
            uiTraceSource.value = 2;
            uiTraceStartAt.value = performance.now();
            uiTraceActivationAt.value = 0;
            uiTraceReleaseAt.value = 0;
          }
          overlayTapTracking.value = 1;
        })
        .onEnd((_event, success) => {
          if (!success || settling.value === 1) {
            return;
          }
          if (ZEN_INTERACTION_TRACE_ENABLED) {
            const timestamp = performance.now();
            uiTraceActivationAt.value = timestamp;
            uiTraceReleaseAt.value = timestamp;
          }
          overlayTapTracking.value = 0;
          runOnJS(closeDrawerFromOverlay)();
        })
        .onFinalize((_event, success) => {
          if (
            !success &&
            overlayTapTracking.value === 1 &&
            uiTraceSource.value === 2
          ) {
            overlayTapTracking.value = 0;
            clearUiTrace();
          }
        }),
    [
      clearUiTrace,
      closeDrawerFromOverlay,
      drawerOffsetPx,
      drawerWidthPx,
      gestureEnabled,
      overlayTapTracking,
      settling,
      uiTraceActivationAt,
      uiTraceDirection,
      uiTraceReleaseAt,
      uiTraceSource,
      uiTraceStartAt,
    ],
  );

  const gesture = useMemo(
    () => Gesture.Race(panGesture, overlayTap),
    [overlayTap, panGesture],
  );

  const drawerStyle = useAnimatedStyle(() => ({
    transform: [{ translateX: drawerOffsetPx.value }],
  }));

  const overlayStyle = useAnimatedStyle(() => ({
    opacity:
      getDrawerProgress(drawerOffsetPx.value, drawerWidthPx.value) *
      PrimaryDrawerMotion.overlayMaxOpacity,
  }));

  const onDrawerLayout = useCallback(
    (event: LayoutChangeEvent) => {
      const measuredWidth = event.nativeEvent.layout.width;
      if (
        measuredWidth <= 0 ||
        Math.abs(measuredWidthRef.current - measuredWidth) < 0.5
      ) {
        return;
      }
      const previousWidth = drawerWidthPx.value;
      const progress = getDrawerProgress(drawerOffsetPx.value, previousWidth);
      measuredWidthRef.current = measuredWidth;
      drawerWidthPx.value = measuredWidth;
      if (stateRef.current.phase === "closed") {
        drawerOffsetPx.value = -measuredWidth;
      } else if (stateRef.current.phase === "open") {
        drawerOffsetPx.value = 0;
      } else {
        drawerOffsetPx.value = -measuredWidth + progress * measuredWidth;
      }
    },
    [drawerOffsetPx, drawerWidthPx],
  );

  return {
    beginCloseInteraction,
    beginOpenInteraction,
    beginWebOverlayInteraction,
    cancelWebOverlayInteraction,
    closeDrawer,
    closeDrawerFromWebOverlay,
    consumeMenuFocusReturn,
    dismissForNavigation,
    drawerStyle,
    drawerVisible: isDrawerVisible(state),
    gesture,
    onDrawerLayout,
    openDrawer,
    overlayStyle,
    state,
  };
}
