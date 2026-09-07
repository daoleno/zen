import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
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
  reduceTimelineScrollPosition,
  returnTimelineToBottom,
  timelineDragContinuesWithMomentum,
  timelineDistanceFromLatest,
  type TimelineScrollState,
} from "./timelineScrollPolicy";
import {
  captureTimelineReadingAnchor,
  recallTimelineReadingPosition,
  rememberTimelineReadingPosition,
  resolveTimelineReadingAnchor,
  timelineReadingOffset,
  type TimelineCellFrame,
  type TimelineReadingAnchor,
  type TimelineReadingPosition,
  type MeasureTimelineCell,
} from "./timelineReadingPosition";
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
  workerCommand?: string;
};

export function useInterfaceComposerPresentation({
  draft,
  slashCommands,
  workerCommand,
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
  const agentKind = agentKindFromCommand(workerCommand);
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
  const initialReading = useMemo(
    () => recallTimelineReadingPosition(resetKey),
    [resetKey],
  );
  const resetKeyRef = useRef(resetKey);
  const scrollStateRef = useRef<TimelineScrollState>({
    mode: initialReading.mode,
  });
  const userDraggingRef = useRef(false);
  const userMomentumRef = useRef(false);
  const timelineTouchActiveRef = useRef(false);
  const automaticReturnsInFlightRef = useRef(0);
  const turnFocusIntentSeqRef = useRef(0);
  const latestOffsetRef = useRef(0);
  const rawContentOffsetRef = useRef(0);
  const distanceFromLatestRef = useRef(0);
  const readingAnchorRef = useRef<TimelineReadingAnchor | undefined>(
    initialReading.anchor,
  );
  const cellFramesRef = useRef(new Map<string, TimelineCellFrame>());
  const readingIdsRef = useRef<string[]>([]);
  const viewportHeightRef = useRef(0);
  const contentHeightRef = useRef(0);
  const contentTranslationRef = useRef(0);
  const nativeInsetRef = useRef(0);
  const viewportOriginRef = useRef<number | undefined>(undefined);
  const cellMeasuresRef = useRef(new Map<string, MeasureTimelineCell>());
  const scrollGeometryRef = useRef<{ height?: number; viewport?: number }>({});
  const readingLayoutFrameRef = useRef<number | null>(null);
  const revealLatestRef = useRef<(() => boolean) | null>(null);
  const readingScopeRef = useRef(resetKey);
  const readingEpochRef = useRef(0);
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
  const [showJumpToLatest, setShowJumpToLatest] = useState(
    initialReading.mode === "detached",
  );
  const [nativeFollowSuspended, setNativeFollowSuspended] = useState(
    initialReading.mode === "detached",
  );
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

  const saveReadingPosition = useCallback(() => {
    rememberTimelineReadingPosition(readingScopeRef.current, {
      mode: scrollStateRef.current.mode,
      anchor: readingAnchorRef.current,
    });
  }, []);
  const captureReadingPosition = useCallback(() => {
    if (scrollStateRef.current.mode !== "detached") return;
    const anchor = captureTimelineReadingAnchor(
      cellFramesRef.current,
      readingIdsRef.current,
      rawContentOffsetRef.current,
      viewportHeightRef.current,
      topChromeInset,
      contentTranslationRef.current,
    );
    if (anchor) readingAnchorRef.current = anchor;
    saveReadingPosition();
    const origin = viewportOriginRef.current;
    const offset = rawContentOffsetRef.current;
    const epoch = readingEpochRef.current;
    if (anchor && origin !== undefined)
      cellMeasuresRef.current.get(anchor.id)?.((screenY) => {
        if (
          readingEpochRef.current !== epoch ||
          readingAnchorRef.current !== anchor ||
          rawContentOffsetRef.current !== offset
        )
          return;
        readingAnchorRef.current = {
          ...anchor,
          intraOffset: origin + topChromeInset - screenY,
          nativeInset: nativeInsetRef.current,
        };
        saveReadingPosition();
      });
  }, [saveReadingPosition, topChromeInset]);
  const sampleContentOrigin = useCallback((id: string) => {
    const frame = cellFramesRef.current.get(id);
    const measure = cellMeasuresRef.current.get(id);
    const offset = rawContentOffsetRef.current;
    const origin = viewportOriginRef.current;
    const epoch = readingEpochRef.current;
    if (
      !frame ||
      !measure ||
      origin === undefined ||
      viewportHeightRef.current <= 0
    )
      return;
    measure((screenY) => {
      if (
        readingEpochRef.current !== epoch ||
        rawContentOffsetRef.current !== offset ||
        cellFramesRef.current.get(id) !== frame
      )
        return;
      contentTranslationRef.current =
        origin +
        viewportHeightRef.current -
        frame.offset -
        frame.length +
        offset -
        screenY;
    });
  }, []);
  const reconcileReadingLayout = useCallback(() => {
    if (readingLayoutFrameRef.current !== null) return;
    readingLayoutFrameRef.current = requestAnimationFrame(() => {
      readingLayoutFrameRef.current = null;
      const anchor = readingAnchorRef.current;
      if (
        implicitAnchorSuspended() ||
        viewportHeightRef.current <= 0 ||
        contentHeightRef.current <= 0 ||
        turnFocusSuppressesOrdinaryFollow(turnFocusStateRef.current)
      )
        return;
      if (scrollStateRef.current.mode === "attached") {
        if (
          Math.abs(rawContentOffsetRef.current - latestOffsetRef.current) > 1
        ) {
          scrollRef.current?.scrollToOffset({
            offset: latestOffsetRef.current,
            animated: false,
          });
          rawContentOffsetRef.current = latestOffsetRef.current;
          distanceFromLatestRef.current = 0;
        }
        return;
      }
      if (!anchor) return;
      const frame = cellFramesRef.current.get(anchor.id);
      if (!frame) return;
      const applyOffset = (target: number) => {
        const maximum = Math.max(
          latestOffsetRef.current,
          contentHeightRef.current - viewportHeightRef.current + nativeInsetRef.current,
        );
        target = Math.max(latestOffsetRef.current, Math.min(maximum, target));
        if (Math.abs(target - rawContentOffsetRef.current) <= 1) return;
        scrollRef.current?.scrollToOffset({ offset: target, animated: false });
        rawContentOffsetRef.current = target;
        distanceFromLatestRef.current = timelineDistanceFromLatest(
          target,
          latestOffsetRef.current,
        );
      };
      const measure = cellMeasuresRef.current.get(anchor.id);
      const origin = viewportOriginRef.current;
      if (measure && origin !== undefined) {
        const offset = rawContentOffsetRef.current;
        const epoch = readingEpochRef.current;
        measure((screenY) => {
          if (
            readingEpochRef.current !== epoch ||
            readingAnchorRef.current !== anchor ||
            cellFramesRef.current.get(anchor.id) !== frame ||
            implicitAnchorSuspended() ||
            rawContentOffsetRef.current !== offset
          )
            return;
          // Read the native position after the keyboard decorator has applied
          // its translation; a requested inset alone is not a mounted origin.
          contentTranslationRef.current =
            origin +
            viewportHeightRef.current -
            frame.offset -
            frame.length +
            offset -
            screenY;
          const insetDelta =
            anchor.nativeInset === undefined
              ? 0
              : nativeInsetRef.current - anchor.nativeInset;
          applyOffset(
            offset +
              origin +
              topChromeInset -
              anchor.intraOffset -
              screenY +
              insetDelta,
          );
        });
      } else {
        applyOffset(
          timelineReadingOffset(
            anchor,
            frame,
            viewportHeightRef.current,
            topChromeInset,
            contentTranslationRef.current,
          ),
        );
      }
    });
  }, [implicitAnchorSuspended, topChromeInset]);
  const readingPosition = useMemo<TimelineReadingPosition>(
    () => ({
      scope: resetKey,
      initialAnchor:
        initialReading.mode === "detached" ? initialReading.anchor : undefined,
      revealLatest: revealLatestRef,
      onItems(ids, anchorAlias) {
        readingIdsRef.current = ids;
        if (readingAnchorRef.current && anchorAlias && anchorAlias !== readingAnchorRef.current.id && ids.includes(anchorAlias)) {
          readingAnchorRef.current = { ...readingAnchorRef.current, id: anchorAlias };
        }
        if (ids.length && readingAnchorRef.current)
          readingAnchorRef.current = resolveTimelineReadingAnchor(
            readingAnchorRef.current,
            ids,
          );
        reconcileReadingLayout();
      },
      onCellLayout(id, frame, measure) {
        cellFramesRef.current.set(id, frame);
        if (measure) cellMeasuresRef.current.set(id, measure);
        sampleContentOrigin(id);
        if (id === readingAnchorRef.current?.id) reconcileReadingLayout();
      },
      onCellUnmount(id) {
        cellFramesRef.current.delete(id);
        cellMeasuresRef.current.delete(id);
      },
      onViewportOrigin(origin) {
        viewportOriginRef.current = origin;
        const id =
          readingAnchorRef.current?.id ??
          cellFramesRef.current.keys().next().value;
        if (id) sampleContentOrigin(id);
        reconcileReadingLayout();
      },
      onInsetChange(inset) {
        nativeInsetRef.current = Platform.OS === "android" ? inset : 0;
        contentTranslationRef.current = nativeInsetRef.current;
        const id =
          readingAnchorRef.current?.id ??
          cellFramesRef.current.keys().next().value;
        if (id) sampleContentOrigin(id);
        reconcileReadingLayout();
      },
      userScrolling: () => userDraggingRef.current || userMomentumRef.current,
      current: () => {
        const anchor = readingAnchorRef.current;
        const frame = anchor ? cellFramesRef.current.get(anchor.id) : undefined;
        const insetDelta =
          anchor?.nativeInset === undefined
            ? 0
            : nativeInsetRef.current - anchor.nativeInset;
        return {
          mode: scrollStateRef.current.mode,
          anchor,
          contentOffset: rawContentOffsetRef.current,
          viewportHeight: viewportHeightRef.current,
          observedIntraOffset: frame
            ? frame.offset +
              frame.length +
              contentTranslationRef.current -
              rawContentOffsetRef.current -
              viewportHeightRef.current +
              topChromeInset +
              insetDelta
            : undefined,
        };
      },
    }),
    [
      initialReading,
      reconcileReadingLayout,
      resetKey,
      sampleContentOrigin,
      topChromeInset,
    ],
  );

  const scrollToLatestOffset = useCallback(
    (animated: boolean, exactLatestOffset?: number) => {
      if (!scrollRef.current) {
        return;
      }
      const latestOffset = exactLatestOffset ?? latestOffsetRef.current;
      if (revealLatestRef.current?.()) return;
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
  }, [clearTextSelectionTimer, syncNativeFollowSuspension, updateJumpButton]);

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
    [clearTextSelectionTimer, syncNativeFollowSuspension, updateJumpButton],
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
      }
      return transition;
    },
    [turnFocusClearanceRequest, turnFocusSpacer],
  );

  const cancelTurnFocus = useCallback(
    (reason: TurnFocusCancelReason) => {
      automaticReturnsInFlightRef.current = 0;
      applyTurnFocusEvent({ type: "cancel", reason });
    },
    [applyTurnFocusEvent],
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
    readingAnchorRef.current = undefined;
    saveReadingPosition();
    setNativeFollowSuspended(implicitAnchorSuspended());
    setShowJumpToLatest(false);
  }, [implicitAnchorSuspended, saveReadingPosition]);

  const detachFromLatest = useCallback(() => {
    scrollStateRef.current = { mode: "detached" };
    setNativeFollowSuspended(true);
    updateJumpButton();
  }, [updateJumpButton]);

  const handleTimelineItemsMutated = useCallback(() => {
    // Data is not a user intent. Native preservation handles live mutations.
    updateJumpButton();
  }, [updateJumpButton]);

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
      if (implicitAnchorSuspended()) {
        cancelTurnFocus(textSelectionActiveRef.current ? "selection" : "touch");
        return;
      }
      attachToLatest();
      revealLatestRef.current?.();
      readingAnchorRef.current = undefined;
      turnFocusIntentSeqRef.current += 1;
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
      attachToLatest,
      implicitAnchorSuspended,
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
      reconcileReadingLayout();
    },
    [reconcileReadingLayout, transitionTurnFocus],
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
    saveReadingPosition();
    cancelTurnFocus("lifecycle");
  }, [cancelTurnFocus, saveReadingPosition]);

  const resetForConversation = useCallback(
    (generation: string) => {
      resumeImplicitAnchorAfterTextSelection();
      const saved = recallTimelineReadingPosition(generation);
      readingScopeRef.current = generation;
      scrollStateRef.current = { mode: saved.mode };
      readingAnchorRef.current = saved.anchor;
      cellFramesRef.current.clear();
      cellMeasuresRef.current.clear();
      rawContentOffsetRef.current = 0;
      userDraggingRef.current = false;
      userMomentumRef.current = false;
      timelineTouchActiveRef.current = false;
      setNativeFollowSuspended(saved.mode === "detached");
      automaticReturnsInFlightRef.current = 0;
      applyTurnFocusEvent({ type: "reset", generation });
      distanceFromLatestRef.current = timelineDistanceFromLatest(
        rawContentOffsetRef.current,
        latestOffsetRef.current,
      );
      setShowJumpToLatest(saved.mode === "detached");
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
      const height = event.nativeEvent.contentSize?.height;
      const viewport = event.nativeEvent.layoutMeasurement?.height;
      const previousGeometry = scrollGeometryRef.current;
      const geometryChanged =
        (height !== undefined &&
          previousGeometry.height !== undefined &&
          height !== previousGeometry.height) ||
        (viewport !== undefined &&
          previousGeometry.viewport !== undefined &&
          viewport !== previousGeometry.viewport);
      scrollGeometryRef.current = { height, viewport };
      userDriven = userDriven && !geometryChanged;
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
        captureReadingPosition();
        return;
      }
      updateJumpButton();
    },
    [
      attachToLatest,
      captureReadingPosition,
      detachFromLatest,
      updateJumpButton,
    ],
  );

  const handleScroll = useCallback(
    (event: NativeSyntheticEvent<NativeScrollEvent>) => {
      updateScrollPosition(
        event,
        userDraggingRef.current || userMomentumRef.current,
      );
      if (!userDraggingRef.current && !userMomentumRef.current)
        reconcileReadingLayout();
    },
    [reconcileReadingLayout, updateScrollPosition],
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
      contentHeightRef.current = height;
      // Native preservation owns mutations during touch/inertia. At rest this
      // hook reconciles the same reading anchor or the attached newest edge.
      if (turnFocusSuppressesOrdinaryFollow(turnFocusStateRef.current)) {
        return;
      }
      if (itemCount === 0 || height <= 0 || implicitAnchorSuspended()) return;
      reconcileReadingLayout();
      updateJumpButton();
    },
    [
      reconcileReadingLayout,
      implicitAnchorSuspended,
      itemCount,
      updateJumpButton,
    ],
  );

  const handleLayout = useCallback(
    (event: LayoutChangeEvent) => {
      viewportHeightRef.current = event.nativeEvent.layout.height;
      reconcileReadingLayout();
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
        return;
      }
      if (implicitAnchorSuspended()) {
        updateJumpButton();
        return;
      }
      updateJumpButton();
    },
    [
      implicitAnchorSuspended,
      itemCount,
      topChromeInset,
      updateTurnFocusGeometry,
      updateJumpButton,
      reconcileReadingLayout,
    ],
  );

  useEffect(
    () => () => {
      clearTextSelectionTimer();
    },
    [clearTextSelectionTimer],
  );

  useLayoutEffect(
    () => () => {
      saveReadingPosition();
      readingEpochRef.current++;
      if (readingLayoutFrameRef.current !== null)
        cancelAnimationFrame(readingLayoutFrameRef.current);
      readingLayoutFrameRef.current = null;
    },
    [resetKey, saveReadingPosition],
  );

  useLayoutEffect(() => {
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
      return;
    }
    if (turnFocusSuppressesOrdinaryFollow(turnFocusStateRef.current)) {
      return;
    }
    if (implicitAnchorSuspended()) {
      return;
    }
    // Item-count changes update the list only. Do not reposition the reader
    // from a lifecycle or data subscription callback.
    updateJumpButton();
  }, [implicitAnchorSuspended, itemCount, resetKey, updateJumpButton]);

  return {
    scrollRef,
    readingPosition,
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
    setFocused(false);
  }, []);

  const handleFocus = useCallback(() => {
    setFocused(true);
  }, []);

  const handleBlur = useCallback(() => {
    setFocused(false);
  }, []);

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
