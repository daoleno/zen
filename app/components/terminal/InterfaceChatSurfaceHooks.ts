import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  type FlatList,
  Keyboard,
  Platform,
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  type TextInput,
} from "react-native";
import { useReducedMotion, useSharedValue } from "react-native-reanimated";
import { agentKindFromCommand } from "../../services/chatComposerPresentation";
import {
  buildInterfaceComposerPresentation,
  type InterfaceComposerPresentationInput,
} from "./InterfaceChatSurfaceModel";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import {
  INITIAL_TIMELINE_SCROLL_STATE,
  focusTimelineOnSentMessage,
  reduceTimelineScrollPosition,
  returnTimelineToBottom,
  settleFocusedTimeline,
  timelineDragContinuesWithMomentum,
  timelineDistanceFromLatest,
  type TimelineScrollState,
} from "./timelineScrollPolicy";
import {
  COMPOSER_STALE_HIDE_GRACE_MS,
  INITIAL_COMPOSER_FOCUS_LIFECYCLE_STATE,
  reduceComposerFocusLifecycle,
  resolvePendingComposerFocusHide,
  type ComposerFocusLifecycleState,
} from "./composerFocusLifecycle";
import {
  createTurnFocusState,
  reduceTurnFocus,
  turnFocusOwnsMomentum,
  turnFocusSuppressesOrdinaryFollow,
  type TurnFocusCancelReason,
  type TurnFocusEffect,
  type TurnFocusEvent,
  type TurnFocusSpacerRequest,
} from "./turnFocusState";
const TEXT_SELECTION_ANCHOR_SETTLE_MS = 30000;
const TEXT_SELECTION_ANCHOR_MAX_MS = 60000;

type UseInterfaceComposerPresentationInput = Omit<
  InterfaceComposerPresentationInput,
  "agentKind"
> & {
  agentCommand?: string;
};

export function useInterfaceComposerPresentation({
  draft,
  slashCommands,
  agentCommand,
  connectionState,
  runningActivity,
  attachmentCount,
  interrupting,
  canSend,
  elapsedStartedAt,
  actionMenuPinned,
  safeAreaBottom,
  placeholder,
  keyboardVerticalOffset,
  composerBottomInset,
  composerLayout,
  modelControl,
}: UseInterfaceComposerPresentationInput) {
  const agentKind = agentKindFromCommand(agentCommand);
  return useMemo(
    () =>
      buildInterfaceComposerPresentation({
        draft,
        slashCommands,
        agentKind,
        connectionState,
        runningActivity,
        attachmentCount,
        interrupting,
        canSend,
        elapsedStartedAt,
        actionMenuPinned,
        safeAreaBottom,
        placeholder,
        keyboardVerticalOffset,
        composerBottomInset,
        composerLayout,
        modelControl,
      }),
    [
      attachmentCount,
      canSend,
      elapsedStartedAt,
      actionMenuPinned,
      composerLayout,
      connectionState,
      draft,
      interrupting,
      modelControl,
      runningActivity,
      safeAreaBottom,
      slashCommands,
      agentKind,
      placeholder,
      keyboardVerticalOffset,
      composerBottomInset,
    ],
  );
}

export function usePinnedTimeline(
  itemCount: number,
  resetKey: string,
  topChromeInset: number = 0,
) {
  const scrollRef = useRef<FlatList<ZenTimelineItem>>(null);
  const resetKeyRef = useRef(resetKey);
  const scrollStateRef = useRef<TimelineScrollState>(
    INITIAL_TIMELINE_SCROLL_STATE,
  );
  const userDraggingRef = useRef(false);
  const userMomentumRef = useRef(false);
  const timelineTouchActiveRef = useRef(false);
  const automaticReturnsInFlightRef = useRef(0);
  const turnFocusIntentSeqRef = useRef(0);
  const latestOffsetRef = useRef(0);
  const rawContentOffsetRef = useRef(0);
  const distanceFromLatestRef = useRef(0);
  const textSelectionActiveRef = useRef(false);
  const textSelectionTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );
  const turnFocusStateRef = useRef(createTurnFocusState(resetKey));
  const turnFocusClearanceRequest = useSharedValue(0);
  const turnFocusSpacer = useSharedValue<TurnFocusSpacerRequest>({
    height: 0,
    requestEpoch: 0,
  });
  const reducedMotion = useReducedMotion();
  const [showJumpToLatest, setShowJumpToLatest] = useState(false);
  const [nativeFollowSuspended, setNativeFollowSuspended] = useState(false);
  const textSelectable = true;
  const [turnFocusPendingMessageId, setTurnFocusPendingMessageId] =
    useState<string>();

  const implicitAnchorSuspended = useCallback(
    () =>
      textSelectionActiveRef.current ||
      userDraggingRef.current ||
      userMomentumRef.current ||
      timelineTouchActiveRef.current,
    [],
  );

  const syncNativeFollowSuspension = useCallback(() => {
    setNativeFollowSuspended(
      implicitAnchorSuspended() || scrollStateRef.current.mode === "detached",
    );
  }, [implicitAnchorSuspended]);

  const updateJumpButton = useCallback(() => {
    setShowJumpToLatest(
      scrollStateRef.current.mode !== "attached" && itemCount > 0,
    );
  }, [itemCount]);

  const scrollToLatestOffset = useCallback(
    (animated: boolean, exactLatestOffset?: number) => {
      if (!scrollRef.current) {
        return;
      }
      const latestOffset = exactLatestOffset ?? latestOffsetRef.current;
      scrollRef.current.scrollToOffset({
        offset: latestOffset,
        animated,
      });
      rawContentOffsetRef.current = latestOffset;
      distanceFromLatestRef.current = 0;
    },
    [],
  );

  const clearTextSelectionTimer = useCallback(() => {
    if (!textSelectionTimerRef.current) {
      return;
    }
    clearTimeout(textSelectionTimerRef.current);
    textSelectionTimerRef.current = null;
  }, []);

  const resumeImplicitAnchorAfterTextSelection = useCallback(() => {
    clearTextSelectionTimer();
    textSelectionActiveRef.current = false;
    syncNativeFollowSuspension();
    updateJumpButton();
  }, [
    clearTextSelectionTimer,
    syncNativeFollowSuspension,
    updateJumpButton,
  ]);

  const scheduleTextSelectionAnchorResume = useCallback(
    (delay: number) => {
      clearTextSelectionTimer();
      textSelectionTimerRef.current = setTimeout(() => {
        textSelectionTimerRef.current = null;
        textSelectionActiveRef.current = false;
        syncNativeFollowSuspension();
        updateJumpButton();
      }, delay);
    },
    [
      clearTextSelectionTimer,
      syncNativeFollowSuspension,
      updateJumpButton,
    ],
  );

  const applyTurnFocusEvent = useCallback(
    (event: TurnFocusEvent) => {
      const previous = turnFocusStateRef.current;
      const transition = reduceTurnFocus(previous, event);
      const next = transition.state;
      if (next !== previous) {
        turnFocusStateRef.current = next;
        if (next.spacerRequestEpoch !== previous.spacerRequestEpoch) {
          turnFocusSpacer.value = {
            height: next.spacerHeight,
            requestEpoch: next.spacerRequestEpoch,
          };
        }
        const previousClearanceRequest =
          previous.phase === "idle" ? 0 : (previous.intentToken ?? 0);
        const nextClearanceRequest =
          next.phase === "idle" ? 0 : (next.intentToken ?? 0);
        if (nextClearanceRequest !== previousClearanceRequest) {
          turnFocusClearanceRequest.value = nextClearanceRequest;
        }
        if (next.pendingMessageId !== previous.pendingMessageId) {
          setTurnFocusPendingMessageId(next.pendingMessageId);
        }
        if (
          event.type !== "cancel" &&
          event.type !== "reset" &&
          previous.phase !== "idle" &&
          next.phase === "idle"
        ) {
          const settled = settleFocusedTimeline(scrollStateRef.current);
          if (settled !== scrollStateRef.current) {
            scrollStateRef.current = settled;
            setNativeFollowSuspended(true);
            setShowJumpToLatest(itemCount > 0);
          }
        }
      }
      return transition;
    },
    [itemCount, turnFocusClearanceRequest, turnFocusSpacer],
  );

  const cancelTurnFocus = useCallback(
    (reason: TurnFocusCancelReason) => {
      automaticReturnsInFlightRef.current = 0;
      applyTurnFocusEvent({ type: "cancel", reason });
      if (
        reason !== "return-to-latest" &&
        scrollStateRef.current.mode === "focused"
      ) {
        scrollStateRef.current = settleFocusedTimeline(scrollStateRef.current);
        setNativeFollowSuspended(true);
        setShowJumpToLatest(itemCount > 0);
      }
    },
    [applyTurnFocusEvent, itemCount],
  );

  const handleTimelineTouchActiveChange = useCallback(
    (active: boolean) => {
      // Root touch observation is passive. A stationary press must leave the
      // mounted row and turn-focus geometry untouched so native text can own
      // long-press selection; onScrollBeginDrag is the cancellation boundary.
      timelineTouchActiveRef.current = active;
      syncNativeFollowSuspension();
    },
    [syncNativeFollowSuspension],
  );

  const attachToLatest = useCallback(() => {
    scrollStateRef.current = returnTimelineToBottom();
    setNativeFollowSuspended(implicitAnchorSuspended());
    setShowJumpToLatest(false);
  }, [implicitAnchorSuspended]);

  const detachFromLatest = useCallback(() => {
    scrollStateRef.current = { mode: "detached" };
    setNativeFollowSuspended(true);
    updateJumpButton();
  }, [updateJumpButton]);

  const handleTimelineItemsMutated = useCallback(() => {
    if (implicitAnchorSuspended()) {
      detachFromLatest();
    }
  }, [detachFromLatest, implicitAnchorSuspended]);

  const scrollToLatest = useCallback(
    (animated: boolean = true, exactLatestOffset?: number) => {
      cancelTurnFocus("return-to-latest");
      resumeImplicitAnchorAfterTextSelection();
      attachToLatest();
      scrollToLatestOffset(animated, exactLatestOffset);
    },
    [
      attachToLatest,
      cancelTurnFocus,
      resumeImplicitAnchorAfterTextSelection,
      scrollToLatestOffset,
    ],
  );

  const performTurnFocusEffect = useCallback(
    (effect?: TurnFocusEffect) => {
      if (!effect) {
        return;
      }
      if (implicitAnchorSuspended()) {
        cancelTurnFocus(textSelectionActiveRef.current ? "selection" : "touch");
        return;
      }
      if (effect.animated) {
        automaticReturnsInFlightRef.current += 1;
      }
      latestOffsetRef.current = effect.latestOffset;
      scrollToLatestOffset(effect.animated, effect.latestOffset);
    },
    [cancelTurnFocus, implicitAnchorSuspended, scrollToLatestOffset],
  );

  const transitionTurnFocus = useCallback(
    (event: TurnFocusEvent) => {
      const transition = applyTurnFocusEvent(event);
      performTurnFocusEffect(transition.effect);
      return transition;
    },
    [applyTurnFocusEvent, performTurnFocusEffect],
  );

  const requestTurnFocus = useCallback(
    (pendingMessageId: string) => {
      if (!pendingMessageId) {
        return;
      }
      turnFocusIntentSeqRef.current += 1;
      scrollStateRef.current = focusTimelineOnSentMessage();
      setNativeFollowSuspended(false);
      setShowJumpToLatest(itemCount > 0);
      transitionTurnFocus({
        type: "intent",
        generation: resetKey,
        pendingMessageId,
        intentToken: turnFocusIntentSeqRef.current,
        reducedMotion,
      });
      if (implicitAnchorSuspended()) {
        cancelTurnFocus(
          textSelectionActiveRef.current
            ? "selection"
            : userDraggingRef.current
              ? "drag"
              : userMomentumRef.current
                ? "momentum"
                : "touch",
        );
        return;
      }
    },
    [
      cancelTurnFocus,
      implicitAnchorSuspended,
      itemCount,
      reducedMotion,
      resetKey,
      transitionTurnFocus,
    ],
  );

  const handleTurnFocusRowLayout = useCallback(
    (pendingMessageId: string, height: number, newestEdgeOffset: number) => {
      transitionTurnFocus({
        type: "row_layout",
        generation: resetKey,
        pendingMessageId,
        height,
        newestEdgeOffset,
      });
    },
    [resetKey, transitionTurnFocus],
  );

  const handleTurnFocusAnchorAvailable = useCallback(
    (pendingMessageId: string) => {
      transitionTurnFocus({
        type: "anchor_available",
        generation: resetKey,
        pendingMessageId,
        latestOffset: latestOffsetRef.current,
      });
    },
    [resetKey, transitionTurnFocus],
  );

  const handleTurnFocusSpacerLayout = useCallback(
    (height: number, requestEpoch: number) => {
      transitionTurnFocus({
        type: "spacer_layout",
        height,
        requestEpoch,
      });
    },
    [transitionTurnFocus],
  );

  const updateTurnFocusGeometry = useCallback(
    (geometry: { viewportHeight?: number; topChromeInset?: number }) => {
      const current = turnFocusStateRef.current;
      return transitionTurnFocus({
        type: "geometry",
        viewportHeight: geometry.viewportHeight ?? current.viewportHeight,
        topChromeInset: geometry.topChromeInset ?? current.topChromeInset,
      });
    },
    [transitionTurnFocus],
  );

  const handleClearanceChange = useCallback(
    (intentToken: number, clearance: number, latestOffset: number) => {
      if (!Number.isFinite(latestOffset)) {
        return;
      }
      latestOffsetRef.current = latestOffset;
      distanceFromLatestRef.current = timelineDistanceFromLatest(
        rawContentOffsetRef.current,
        latestOffset,
      );
      transitionTurnFocus({
        type: "clearance_sample",
        intentToken,
        clearance,
        latestOffset,
      });
    },
    [transitionTurnFocus],
  );

  const clearTurnFocusForLifecycle = useCallback(() => {
    cancelTurnFocus("lifecycle");
  }, [cancelTurnFocus]);

  const resetForConversation = useCallback(
    (generation: string) => {
      resumeImplicitAnchorAfterTextSelection();
      scrollStateRef.current = returnTimelineToBottom();
      userDraggingRef.current = false;
      userMomentumRef.current = false;
      timelineTouchActiveRef.current = false;
      setNativeFollowSuspended(false);
      automaticReturnsInFlightRef.current = 0;
      applyTurnFocusEvent({ type: "reset", generation });
      distanceFromLatestRef.current = timelineDistanceFromLatest(
        rawContentOffsetRef.current,
        latestOffsetRef.current,
      );
      setShowJumpToLatest(false);
    },
    [applyTurnFocusEvent, resumeImplicitAnchorAfterTextSelection],
  );

  const handleTextSelectionGestureStart = useCallback(() => {
    textSelectionActiveRef.current = true;
    syncNativeFollowSuspension();
    scheduleTextSelectionAnchorResume(TEXT_SELECTION_ANCHOR_MAX_MS);
  }, [scheduleTextSelectionAnchorResume, syncNativeFollowSuspension]);

  const handleTextSelectionGestureEnd = useCallback(() => {
    if (!textSelectionActiveRef.current) {
      return;
    }
    scheduleTextSelectionAnchorResume(TEXT_SELECTION_ANCHOR_SETTLE_MS);
  }, [scheduleTextSelectionAnchorResume]);

  const updateScrollPosition = useCallback(
    (event: NativeSyntheticEvent<NativeScrollEvent>, userDriven: boolean) => {
      const { contentOffset, contentInset } = event.nativeEvent;
      latestOffsetRef.current =
        Platform.OS === "ios" ? -Math.max(0, contentInset.top) : 0;
      rawContentOffsetRef.current = contentOffset.y;
      const distanceFromLatest = timelineDistanceFromLatest(
        contentOffset.y,
        latestOffsetRef.current,
      );
      const previousDistanceFromLatest = distanceFromLatestRef.current;
      if (textSelectionActiveRef.current && !userDriven) {
        distanceFromLatestRef.current = distanceFromLatest;
        updateJumpButton();
        return;
      }
      const nextScrollState = reduceTimelineScrollPosition(
        scrollStateRef.current,
        distanceFromLatest,
        userDriven,
        previousDistanceFromLatest,
      );
      distanceFromLatestRef.current = distanceFromLatest;
      if (nextScrollState.mode === "attached") {
        attachToLatest();
        return;
      }
      if (nextScrollState.mode === "detached" && userDriven) {
        detachFromLatest();
        return;
      }
      updateJumpButton();
    },
    [attachToLatest, detachFromLatest, updateJumpButton],
  );

  const handleScroll = useCallback(
    (event: NativeSyntheticEvent<NativeScrollEvent>) => {
      updateScrollPosition(
        event,
        userDraggingRef.current || userMomentumRef.current,
      );
    },
    [updateScrollPosition],
  );

  const handleScrollBeginDrag = useCallback(() => {
    if (!textSelectionActiveRef.current) {
      resumeImplicitAnchorAfterTextSelection();
    }
    userDraggingRef.current = true;
    syncNativeFollowSuspension();
    cancelTurnFocus("drag");
  }, [
    cancelTurnFocus,
    resumeImplicitAnchorAfterTextSelection,
    syncNativeFollowSuspension,
  ]);

  const handleScrollEndDrag = useCallback(
    (event: NativeSyntheticEvent<NativeScrollEvent>) => {
      updateScrollPosition(event, true);
      userDraggingRef.current = false;
      userMomentumRef.current = timelineDragContinuesWithMomentum(
        event.nativeEvent.velocity?.y,
      );
      syncNativeFollowSuspension();
    },
    [syncNativeFollowSuspension, updateScrollPosition],
  );

  const handleMomentumScrollBegin = useCallback(() => {
    if (
      turnFocusOwnsMomentum(
        turnFocusStateRef.current.phase,
        automaticReturnsInFlightRef.current,
      )
    ) {
      return;
    }
    userMomentumRef.current = true;
    syncNativeFollowSuspension();
    cancelTurnFocus("momentum");
  }, [cancelTurnFocus, syncNativeFollowSuspension]);

  const handleMomentumScrollEnd = useCallback(
    (event: NativeSyntheticEvent<NativeScrollEvent>) => {
      if (
        turnFocusOwnsMomentum(
          turnFocusStateRef.current.phase,
          automaticReturnsInFlightRef.current,
        )
      ) {
        automaticReturnsInFlightRef.current = Math.max(
          0,
          automaticReturnsInFlightRef.current - 1,
        );
        updateScrollPosition(event, false);
        return;
      }
      updateScrollPosition(event, true);
      userMomentumRef.current = false;
      syncNativeFollowSuspension();
    },
    [syncNativeFollowSuspension, updateScrollPosition],
  );

  const handleContentSizeChange = useCallback(
    (_: number, height: number) => {
      // flexGrow can keep the content-container height constant while the live
      // response moves the focused turn boundary. Positioned cell layout is the
      // focus owner's geometry signal. For ordinary live mutations, the
      // FlatList's native visible-child owner atomically preserves history or
      // follows within its newest-edge threshold; issuing a second JS scroll
      // here races that adjustment and can move or blank the settled viewport.
      if (turnFocusSuppressesOrdinaryFollow(turnFocusStateRef.current)) {
        return;
      }
      if (itemCount === 0 || height <= 0) {
        attachToLatest();
        return;
      }
      if (implicitAnchorSuspended()) {
        detachFromLatest();
        return;
      }
      updateJumpButton();
    },
    [
      attachToLatest,
      detachFromLatest,
      implicitAnchorSuspended,
      itemCount,
      updateJumpButton,
    ],
  );

  const handleLayout = useCallback(
    (event: LayoutChangeEvent) => {
      const focusWasSuppressing = turnFocusSuppressesOrdinaryFollow(
        turnFocusStateRef.current,
      );
      const focusTransition = updateTurnFocusGeometry({
        viewportHeight: event.nativeEvent.layout.height,
        topChromeInset,
      });
      if (
        focusWasSuppressing ||
        turnFocusSuppressesOrdinaryFollow(focusTransition.state) ||
        focusTransition.effect
      ) {
        return;
      }
      if (itemCount === 0) {
        attachToLatest();
        return;
      }
      if (implicitAnchorSuspended()) {
        updateJumpButton();
        return;
      }
      if (
        scrollStateRef.current.mode === "attached" &&
        distanceFromLatestRef.current > 1
      ) {
        scrollToLatest(false);
        return;
      }
      updateJumpButton();
    },
    [
      attachToLatest,
      implicitAnchorSuspended,
      itemCount,
      scrollToLatest,
      topChromeInset,
      updateTurnFocusGeometry,
      updateJumpButton,
    ],
  );

  useEffect(
    () => () => {
      clearTextSelectionTimer();
    },
    [clearTextSelectionTimer],
  );

  useEffect(() => {
    if (resetKeyRef.current === resetKey) {
      return;
    }
    resetKeyRef.current = resetKey;
    resetForConversation(resetKey);
  }, [resetForConversation, resetKey]);

  useEffect(() => {
    updateTurnFocusGeometry({ topChromeInset });
  }, [topChromeInset, updateTurnFocusGeometry]);

  useEffect(() => {
    if (itemCount === 0) {
      attachToLatest();
      return;
    }
    if (turnFocusSuppressesOrdinaryFollow(turnFocusStateRef.current)) {
      return;
    }
    if (implicitAnchorSuspended()) {
      detachFromLatest();
      return;
    }
    // Item-count changes are live list mutations. Native visible-child
    // tracking owns both detached anchoring and newest-edge follow so this
    // effect must not race it with an imperative scroll.
    updateJumpButton();
  }, [
    attachToLatest,
    detachFromLatest,
    implicitAnchorSuspended,
    itemCount,
    resetKey,
    updateJumpButton,
  ]);

  return {
    scrollRef,
    nativeFollowSuspended,
    showJumpToLatest,
    textSelectable,
    turnFocusClearanceRequest,
    turnFocusPendingMessageId,
    turnFocusSpacer,
    scrollToLatest,
    requestTurnFocus,
    clearTurnFocusForLifecycle,
    resetForConversation,
    handleScroll,
    handleScrollBeginDrag,
    handleScrollEndDrag,
    handleMomentumScrollBegin,
    handleMomentumScrollEnd,
    handleTimelineTouchActiveChange,
    handleTimelineItemsMutated,
    handleContentSizeChange,
    handleClearanceChange,
    handleLayout,
    handleTurnFocusAnchorAvailable,
    handleTurnFocusRowLayout,
    handleTurnFocusSpacerLayout,
    handleTextSelectionGestureStart,
    handleTextSelectionGestureEnd,
  };
}

export function useRelativeTimeLabel(targetTimestamp?: string) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!targetTimestamp) {
      return;
    }
    setNow(Date.now());
    const timer = setInterval(() => {
      setNow(Date.now());
    }, 1000);
    return () => clearInterval(timer);
  }, [targetTimestamp]);

  return useMemo(() => {
    if (!targetTimestamp) {
      return "";
    }
    const timestamp = new Date(targetTimestamp).getTime();
    if (!Number.isFinite(timestamp)) {
      return "";
    }
    const elapsed = Math.max(0, Math.floor((now - timestamp) / 1000));
    if (elapsed < 60) {
      return `${Math.max(1, elapsed)}s`;
    }
    const minutes = Math.floor(elapsed / 60);
    if (minutes < 60) {
      return `${minutes}m`;
    }
    const hours = Math.floor(minutes / 60);
    if (hours < 24) {
      return `${hours}h`;
    }
    const days = Math.floor(hours / 24);
    return `${days}d`;
  }, [now, targetTimestamp]);
}

export function useInterfaceComposerInput({ enabled }: { enabled: boolean }) {
  const inputRef = useRef<TextInput>(null);
  const [focused, setFocused] = useState(false);
  const focusLifecycleRef = useRef(INITIAL_COMPOSER_FOCUS_LIFECYCLE_STATE);
  const hideRecheckTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null,
  );

  const commitFocusLifecycle = useCallback(
    (next: ComposerFocusLifecycleState) => {
      const previous = focusLifecycleRef.current;
      if (next === previous) return next;
      focusLifecycleRef.current = next;
      setFocused(next.inputFocused);
      return next;
    },
    [],
  );

  const applyFocusLifecycle = useCallback(
    (event: Parameters<typeof reduceComposerFocusLifecycle>[1]) => {
      return commitFocusLifecycle(
        reduceComposerFocusLifecycle(focusLifecycleRef.current, event),
      );
    },
    [commitFocusLifecycle],
  );

  const cancelHideRecheck = useCallback(() => {
    if (hideRecheckTimerRef.current !== null) {
      clearTimeout(hideRecheckTimerRef.current);
      hideRecheckTimerRef.current = null;
    }
  }, []);

  const scheduleHideRecheck = useCallback(() => {
    cancelHideRecheck();
    hideRecheckTimerRef.current = setTimeout(() => {
      hideRecheckTimerRef.current = null;
      const previous = focusLifecycleRef.current;
      const next = resolvePendingComposerFocusHide(previous, {
        now: Date.now(),
        keyboardVisible: Keyboard.isVisible(),
      });
      if (next === previous) return;
      focusLifecycleRef.current = next;
      setFocused(next.inputFocused);
      if (previous.inputFocused && next.inputFocused === false) {
        inputRef.current?.blur();
      }
    }, COMPOSER_STALE_HIDE_GRACE_MS);
  }, [cancelHideRecheck]);

  const clearNativeText = useCallback(() => {
    inputRef.current?.clear();
    inputRef.current?.setNativeProps?.({ text: "" });
  }, []);

  const focus = useCallback(() => {
    if (!enabled) {
      return;
    }
    inputRef.current?.focus();
  }, [enabled]);

  const blur = useCallback(() => {
    inputRef.current?.blur();
    Keyboard.dismiss();
    applyFocusLifecycle({ type: "input_blur" });
    cancelHideRecheck();
  }, [applyFocusLifecycle, cancelHideRecheck]);

  const handleFocus = useCallback(() => {
    applyFocusLifecycle({ type: "input_focus" });
    cancelHideRecheck();
  }, [applyFocusLifecycle, cancelHideRecheck]);

  const handleBlur = useCallback(() => {
    applyFocusLifecycle({ type: "input_blur" });
    cancelHideRecheck();
  }, [applyFocusLifecycle, cancelHideRecheck]);

  useEffect(() => {
    const showSubscription = Keyboard.addListener("keyboardDidShow", () => {
      applyFocusLifecycle({ type: "keyboard_show" });
      cancelHideRecheck();
    });
    const hideSubscription = Keyboard.addListener("keyboardDidHide", () => {
      const previous = focusLifecycleRef.current;
      if (!previous.inputFocused) return;
      // Defer the hide through the grace window: a stale hide from the
      // previous IME epoch must not collapse the current focus, while a real
      // dismissal collapses after the native visibility recheck. Duplicate
      // hides are inert in the reducer, so the recheck is scheduled only when
      // this hide newly opened the deferral window — never extending the
      // original deadline.
      const next = applyFocusLifecycle({
        type: "keyboard_hide",
        at: Date.now(),
      });
      if (next.pendingHide !== null && previous.pendingHide === null) {
        scheduleHideRecheck();
      }
    });
    return () => {
      showSubscription.remove();
      hideSubscription.remove();
      cancelHideRecheck();
    };
  }, [applyFocusLifecycle, cancelHideRecheck, scheduleHideRecheck]);

  useEffect(() => {
    if (!enabled) {
      blur();
    }
  }, [blur, enabled]);

  return {
    inputRef,
    focused,
    focus,
    blur,
    clearNativeText,
    handleFocus,
    handleBlur,
  };
}
