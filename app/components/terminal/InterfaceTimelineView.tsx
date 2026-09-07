import React from "react";
import type { CellRendererProps } from "@react-native/virtualized-lists";
import {
  FlatList,
  Platform,
  StyleSheet,
  View,
  type GestureResponderEvent,
  type LayoutChangeEvent,
  type ListRenderItemInfo,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
  type ScrollViewProps,
  type ViewToken,
} from "react-native";
import Reanimated, {
  measure,
  runOnJS,
  type SharedValue,
  useAnimatedRef,
  useAnimatedReaction,
  useAnimatedStyle,
  useEvent,
} from "react-native-reanimated";
import { useAppTheme } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";

import { InterfaceTimelineEmptyContent } from "./InterfaceTimelineContent";
import {
  TimelineTextSelectableContext,
  type TimelineTextSelectableContextValue,
} from "./TimelineTextSelectableContext";
import {
  ZenTimelineItemView,
  type ZenTimelineItem,
} from "./InterfaceTimelineItemView";
import { InterfaceTimelineDateDivider } from "./InterfaceTimelineDateDivider";
import {
  projectTimelineRenderItems,
  type TimelineRenderItem,
  type TimelineRenderProjectionCache,
} from "./InterfaceTimelineGrouping";
import type { PatchFileSummary } from "./InterfaceTimelineActivityTypes";
import { timelineListStabilityProps } from "./timelineScrollPolicy";
import {
  initialTimelineReadingWindow,
  type TimelineReadingPosition,
} from "./timelineReadingPosition";
import { StructuredChatInsetScrollView } from "./StructuredChatInsetScrollView";
import type { StructuredChatKeyboardLifecycleGate } from "./chatKeyboardOverlayPolicy";
import {
  resolveTurnFocusAnchorItemId,
  turnFocusRowGeometryFromCell,
  type TurnFocusSpacerRequest,
} from "./turnFocusState";
import { INTERFACE_TIMELINE_HORIZONTAL_INSET } from "./interfaceTimelineGeometry";
import {
  isTimelineProjectionPerfEnabled,
  getTimelineProjectionPerfScenarioRevision,
  recordTimelineBlankWindowSample,
  recordTimelineListDataIdentityProbe,
} from "./timelineProjectionPerf";

type TurnFocusCellMeasurement = {
  pendingMessageId?: string;
  anchorItemId?: string;
  onRowLayout?: (
    pendingMessageId: string,
    height: number,
    newestEdgeOffset: number,
  ) => void;
};

const TURN_FOCUS_SPACER_USES_NATIVE_MEASUREMENT = Platform.OS !== "web";
const TURN_FOCUS_ZERO_EPSILON = 0.5;

interface InterfaceTimelineViewProps {
  scrollRef: React.RefObject<FlatList<ZenTimelineItem> | null>;
  readingPosition?: TimelineReadingPosition;
  items: ZenTimelineItem[];
  loading: boolean;
  error?: string | null;
  emptyStateSuppressed: boolean;
  unavailable: boolean | null;
  unavailableReason?: string;
  syncing: boolean;
  textSelectable: boolean;
  extraContentPadding: SharedValue<number>;
  keyboardLifecycleGate: SharedValue<StructuredChatKeyboardLifecycleGate>;
  turnFocusClearanceRequest?: SharedValue<number>;
  turnFocusSpacer?: SharedValue<TurnFocusSpacerRequest>;
  turnFocusPendingMessageId?: string;
  topChromeInset: number;
  emptyTitle?: string;
  emptyBody?: string;
  workerCwd?: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onLayout(event: LayoutChangeEvent): void;
  onScroll(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onScrollBeginDrag(): void;
  onScrollEndDrag(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  onMomentumScrollBegin(): void;
  onMomentumScrollEnd(event: NativeSyntheticEvent<NativeScrollEvent>): void;
  /** Passively observes touch lifetime without taking the scroll responder. */
  onTouchActiveChange?(active: boolean): void;
  onItemsMutated?(): void;
  onContentSizeChange(width: number, height: number): void;
  onClearanceChange?(
    intentToken: number,
    clearance: number,
    latestOffset: number,
  ): void;
  onTurnFocusAnchorAvailable?(pendingMessageId: string): void;
  onTurnFocusRowLayout?(
    pendingMessageId: string,
    height: number,
    newestEdgeOffset: number,
  ): void;
  onTurnFocusSpacerLayout?(height: number, requestEpoch: number): void;
  onTextSelectionGestureStart: TimelineTextSelectableContextValue["onTextSelectionGestureStart"];
  onTextSelectionGestureEnd: TimelineTextSelectableContextValue["onTextSelectionGestureEnd"];
  onUnavailableAction?: () => void;
  showUnavailableAction?: boolean;
  loadAssetPreview(path: string): Promise<string | null>;
  formatPatchPath(file: PatchFileSummary): string;
  truncateBody(value: string, limit: number): string;
}

export function InterfaceTimelineView({
  scrollRef,
  readingPosition,
  items,
  loading,
  error,
  emptyStateSuppressed,
  unavailable,
  unavailableReason,
  syncing,
  textSelectable,
  extraContentPadding,
  keyboardLifecycleGate,
  turnFocusClearanceRequest,
  turnFocusSpacer,
  turnFocusPendingMessageId,
  topChromeInset,
  emptyTitle,
  emptyBody,
  workerCwd,
  chrome,
  theme,
  onLayout,
  onScroll,
  onScrollBeginDrag,
  onScrollEndDrag,
  onMomentumScrollBegin,
  onMomentumScrollEnd,
  onTouchActiveChange,
  onItemsMutated,
  onContentSizeChange,
  onClearanceChange,
  onTurnFocusAnchorAvailable,
  onTurnFocusRowLayout,
  onTurnFocusSpacerLayout,
  onTextSelectionGestureStart,
  onTextSelectionGestureEnd,
  onUnavailableAction,
  showUnavailableAction,
  loadAssetPreview,
  formatPatchPath,
  truncateBody,
}: InterfaceTimelineViewProps) {
  const { theme: zenTheme } = useAppTheme();
  const listStabilityProps = React.useMemo(
    () => timelineListStabilityProps(),
    [],
  );
  const turnFocusCellMeasurementRef = React.useRef<TurnFocusCellMeasurement>(
    {},
  );
  const readingPositionRef = React.useRef(readingPosition);
  const viewportRef = React.useRef<View>(null);
  readingPositionRef.current = readingPosition;
  const renderProjectionCacheRef =
    React.useRef<TimelineRenderProjectionCache | null>(null);
  const previousItemsRef = React.useRef(items);
  const previousPerfItemsRef = React.useRef<ZenTimelineItem[] | null>(null);
  const perfItemCountRef = React.useRef(items.length);
  const perfSawVisibleRowsRef = React.useRef(false);
  const perfBlankStartedAtRef = React.useRef<number | null>(null);
  perfItemCountRef.current = items.length;
  React.useEffect(() => {
    const previousPerfItems = previousPerfItemsRef.current;
    if (isTimelineProjectionPerfEnabled() && previousPerfItems !== items) {
      recordTimelineListDataIdentityProbe({
        previousItems: previousPerfItems,
        nextItems: items,
      });
    }
    previousPerfItemsRef.current = items;

    const previous = previousItemsRef.current;
    if (previous === items) {
      return;
    }
    previousItemsRef.current = items;
    onItemsMutated?.();
  }, [items, onItemsMutated]);
  const renderItems = React.useMemo(() => {
    const projected = projectTimelineRenderItems(
      items,
      {
        showDateDividers: zenTheme.chat.showDateDividers,
      },
      renderProjectionCacheRef.current,
    );
    renderProjectionCacheRef.current = projected.cache;
    return projected.items;
  }, [items, zenTheme.chat.showDateDividers]);
  // A remounted variable-height list measures a small prefix around the saved
  // message, rather than guessing an offset through thousands of unmounted rows.
  const readingIds = React.useMemo(
    () => renderItems.map((item) => item.id),
    [renderItems],
  );
  const resolveReadingAlias = (anchor: TimelineReadingPosition["initialAnchor"]) => {
    const id = resolveTurnFocusAnchorItemId(anchor?.id, renderItems);
    return anchor && id && id !== anchor.id ? { ...anchor, id } : anchor;
  };
  const [readingWindow, setReadingWindow] = React.useState(() =>
    initialTimelineReadingWindow(readingIds, resolveReadingAlias(readingPosition?.initialAnchor)),
  );
  const resolvedWindow =
    readingWindow ??
    initialTimelineReadingWindow(
      readingIds,
      readingPosition?.current().mode === "detached"
        ? resolveReadingAlias(readingPosition.current().anchor)
        : undefined,
    );
  const newestBoundary = resolvedWindow?.start;
  const boundaryIndex = newestBoundary
    ? Math.max(
        0,
        renderItems.findIndex((item) => item.id === newestBoundary),
      )
    : 0;
  const windowItems = React.useMemo(
    () => (boundaryIndex ? renderItems.slice(boundaryIndex) : renderItems),
    [boundaryIndex, renderItems],
  );
  React.useLayoutEffect(() => {
    if (readingWindow === null && resolvedWindow !== null)
      setReadingWindow(resolvedWindow);
  }, [readingWindow, resolvedWindow]);
  React.useLayoutEffect(() => {
    readingPosition?.onItems(readingIds, resolveTurnFocusAnchorItemId(readingPosition.current().anchor?.id, renderItems));
  }, [readingPosition, readingIds, renderItems]);
  React.useLayoutEffect(() => {
    if (!readingPosition) return;
    readingPosition.revealLatest.current = () => {
      if (resolvedWindow !== null && !newestBoundary) return false;
      setReadingWindow({ initialCount: 8 });
      return true;
    };
    return () => {
      readingPosition.revealLatest.current = null;
    };
  }, [newestBoundary, readingPosition, resolvedWindow]);
  const revealNewerRows = React.useCallback(() => {
    if (!boundaryIndex || !readingPosition?.userScrolling()) return;
    const next = Math.max(0, boundaryIndex - 40);
    setReadingWindow({
      start: next ? renderItems[next].id : undefined,
      initialCount: resolvedWindow?.initialCount ?? 8,
    });
  }, [boundaryIndex, readingPosition, renderItems, resolvedWindow]);
  const turnFocusAnchorItemId = resolveTurnFocusAnchorItemId(
    turnFocusPendingMessageId,
    renderItems,
  );
  turnFocusCellMeasurementRef.current.pendingMessageId =
    turnFocusPendingMessageId;
  turnFocusCellMeasurementRef.current.anchorItemId = turnFocusAnchorItemId;
  turnFocusCellMeasurementRef.current.onRowLayout = onTurnFocusRowLayout;
  React.useEffect(() => {
    if (
      !turnFocusPendingMessageId ||
      !turnFocusAnchorItemId ||
      !onTurnFocusAnchorAvailable
    ) {
      return;
    }
    // This passive effect runs only after FlatList has committed the anchor to
    // its data. The scroll owner can now reveal index zero so virtualization
    // mounts the exact cell that supplies native row geometry.
    onTurnFocusAnchorAvailable(turnFocusPendingMessageId);
  }, [
    onTurnFocusAnchorAvailable,
    turnFocusAnchorItemId,
    turnFocusPendingMessageId,
  ]);
  const textSelectionContext = React.useMemo(
    () => ({
      selectable: textSelectable,
      onTextSelectionGestureStart,
      onTextSelectionGestureEnd,
    }),
    [onTextSelectionGestureEnd, onTextSelectionGestureStart, textSelectable],
  );
  const renderItem = React.useCallback(
    ({ item }: ListRenderItemInfo<TimelineRenderItem>) => {
      if (item.type === "date-divider") {
        return (
          <InterfaceTimelineDateDivider label={item.label} chrome={chrome} />
        );
      }
      return (
        <ZenTimelineItemView
          item={item}
          presentation={item.type === "message" ? item.presentation : undefined}
          chrome={chrome}
          theme={theme}
          loadAssetPreview={loadAssetPreview}
          formatPatchPath={formatPatchPath}
          truncateBody={truncateBody}
        />
      );
    },
    [chrome, formatPatchPath, loadAssetPreview, theme, truncateBody],
  );
  const renderTimelineCell = React.useCallback(
    (props: CellRendererProps<TimelineRenderItem>) => (
      <TurnFocusTimelineCell
        {...props}
        measurementRef={turnFocusCellMeasurementRef}
        readingPositionRef={readingPositionRef}
      />
    ),
    [],
  );
  const renderScrollComponent = React.useCallback(
    (props: ScrollViewProps) => (
      <StructuredChatInsetScrollView
        {...props}
        clearance={extraContentPadding}
        keyboardLifecycleGate={keyboardLifecycleGate}
        clearanceObservationRequest={turnFocusClearanceRequest}
        inverted
        onClearanceChange={onClearanceChange ?? ignoreClearanceChange}
        onReadingInsetChange={readingPosition?.onInsetChange}
      />
    ),
    [
      extraContentPadding,
      keyboardLifecycleGate,
      onClearanceChange,
      readingPosition,
      turnFocusClearanceRequest,
    ],
  );
  const handleTouchStart = React.useCallback(() => {
    onTouchActiveChange?.(true);
  }, [onTouchActiveChange]);
  const handleTouchEnd = React.useCallback(
    (event: GestureResponderEvent) => {
      onTouchActiveChange?.(event.nativeEvent.touches.length > 0);
    },
    [onTouchActiveChange],
  );
  const handleTouchCancel = React.useCallback(() => {
    onTouchActiveChange?.(false);
  }, [onTouchActiveChange]);
  const handleViewableItemsChanged = React.useCallback(
    ({ viewableItems }: { viewableItems: ViewToken[] }) => {
      if (!isTimelineProjectionPerfEnabled()) {
        perfSawVisibleRowsRef.current = false;
        perfBlankStartedAtRef.current = null;
        return;
      }
      const now = timelineViewNowMs();
      if (viewableItems.length > 0) {
        perfSawVisibleRowsRef.current = true;
        const blankStartedAt = perfBlankStartedAtRef.current;
        if (blankStartedAt !== null) {
          recordTimelineBlankWindowSample({
            durationMs: Math.max(0, now - blankStartedAt),
            scenarioRevision: getTimelineProjectionPerfScenarioRevision(),
            itemCount: perfItemCountRef.current,
          });
          perfBlankStartedAtRef.current = null;
        }
        return;
      }
      if (
        perfItemCountRef.current > 0 &&
        perfSawVisibleRowsRef.current &&
        perfBlankStartedAtRef.current === null
      ) {
        perfBlankStartedAtRef.current = now;
      }
    },
    [],
  );

  const emptyContent = React.useMemo(
    () => (
      <InterfaceTimelineEmptyContent
        items={items}
        loading={loading}
        error={error}
        suppressed={emptyStateSuppressed}
        unavailable={unavailable}
        unavailableReason={unavailableReason}
        syncing={syncing}
        chrome={chrome}
        onUnavailableAction={onUnavailableAction}
        showUnavailableAction={showUnavailableAction}
        workerCwd={workerCwd}
        emptyTitle={emptyTitle}
        emptyBody={emptyBody}
      />
    ),
    [
      workerCwd,
      chrome,
      error,
      emptyStateSuppressed,
      items,
      loading,
      onUnavailableAction,
      showUnavailableAction,
      emptyTitle,
      emptyBody,
      unavailable,
      unavailableReason,
      syncing,
    ],
  );

  return (
    <TimelineTextSelectableContext.Provider value={textSelectionContext}>
      <View
        ref={viewportRef}
        collapsable={false}
        style={styles.timelineStage}
        onLayout={() =>
          viewportRef.current?.measureInWindow((_x, y) =>
            readingPositionRef.current?.onViewportOrigin(y),
          )
        }
      >
        {resolvedWindow ? (
          <FlatList<TimelineRenderItem>
            accessibilityLabel="Conversation timeline"
            testID="structured-chat-timeline"
            ref={
              scrollRef as React.RefObject<FlatList<TimelineRenderItem> | null>
            }
            data={windowItems}
            ListHeaderComponent={
              turnFocusSpacer ? (
                <TurnFocusSpacer
                  request={turnFocusSpacer}
                  onLayout={onTurnFocusSpacerLayout}
                />
              ) : null
            }
            CellRendererComponent={renderTimelineCell}
            keyExtractor={(item) => item.id}
            renderItem={renderItem}
            style={styles.timeline}
            contentContainerStyle={[
              styles.timelineContent,
              { paddingBottom: Math.max(12, topChromeInset) },
            ]}
            inverted
            renderScrollComponent={renderScrollComponent}
            {...listStabilityProps}
            initialNumToRender={resolvedWindow.initialCount}
            onStartReached={revealNewerRows}
            onStartReachedThreshold={1}
            keyboardDismissMode={
              Platform.OS === "ios" ? "interactive" : "on-drag"
            }
            keyboardShouldPersistTaps="handled"
            showsVerticalScrollIndicator={false}
            scrollEventThrottle={32}
            onLayout={onLayout}
            onScroll={onScroll}
            onScrollBeginDrag={() => {
              onScrollBeginDrag();
              revealNewerRows();
            }}
            onScrollEndDrag={onScrollEndDrag}
            onMomentumScrollBegin={onMomentumScrollBegin}
            onMomentumScrollEnd={onMomentumScrollEnd}
            onTouchStart={handleTouchStart}
            onTouchEnd={handleTouchEnd}
            onTouchCancel={handleTouchCancel}
            onContentSizeChange={onContentSizeChange}
            onViewableItemsChanged={handleViewableItemsChanged}
          />
        ) : null}

        {items.length === 0 ? (
          <View style={styles.emptyOverlay}>{emptyContent}</View>
        ) : null}
      </View>
    </TimelineTextSelectableContext.Provider>
  );
}

function timelineViewNowMs() {
  const perf = (globalThis as { performance?: { now(): number } }).performance;
  return perf?.now?.() ?? Date.now();
}

function ignoreClearanceChange() {}

function TurnFocusTimelineCell({
  children,
  item,
  measurementRef,
  readingPositionRef,
  onLayout,
  style,
}: CellRendererProps<TimelineRenderItem> & {
  measurementRef: React.RefObject<TurnFocusCellMeasurement>;
  readingPositionRef: React.RefObject<TimelineReadingPosition | undefined>;
}) {
  const cellRef = React.useRef<View>(null);
  React.useLayoutEffect(
    () => () => readingPositionRef.current?.onCellUnmount(item.id),
    [item.id, readingPositionRef],
  );
  const handleLayout = React.useCallback(
    (event: LayoutChangeEvent) => {
      // This positioned content cell includes newer Activity/divider siblings;
      // a wrapper inside renderItem would only report its local y (normally 0).
      onLayout?.(event);
      readingPositionRef.current?.onCellLayout(
        item.id,
        {
          offset: event.nativeEvent.layout.y,
          length: event.nativeEvent.layout.height,
        },
        (callback) =>
          cellRef.current?.measureInWindow((_x, y, _width, height) =>
            callback(y, height),
          ),
      );
      const measurement = measurementRef.current;
      const geometry = turnFocusRowGeometryFromCell(
        measurement.pendingMessageId,
        item.id,
        event.nativeEvent.layout,
        measurement.anchorItemId,
      );
      if (!geometry || !measurement.onRowLayout) {
        return;
      }
      measurement.onRowLayout(
        geometry.pendingMessageId,
        geometry.height,
        geometry.newestEdgeOffset,
      );
    },
    [item.id, measurementRef, onLayout, readingPositionRef],
  );

  return (
    <View
      ref={cellRef}
      collapsable={false}
      onLayout={handleLayout}
      style={style}
    >
      {children}
    </View>
  );
}

function TurnFocusSpacer({
  request,
  onLayout,
}: {
  request: SharedValue<TurnFocusSpacerRequest>;
  onLayout?(height: number, requestEpoch: number): void;
}) {
  const spacerRef = useAnimatedRef();
  const animatedStyle = useAnimatedStyle(
    () => ({
      height: Math.max(0, request.value.height),
    }),
    [request],
  );
  useAnimatedReaction(
    () => {
      const observation = request.value;
      return observation.height <= TURN_FOCUS_ZERO_EPSILON
        ? observation.requestEpoch
        : -1;
    },
    (requestEpoch, previousRequestEpoch) => {
      if (requestEpoch < 0 || requestEpoch === previousRequestEpoch) {
        return;
      }
      const observation = request.value;
      if (
        observation.requestEpoch !== requestEpoch ||
        observation.height > TURN_FOCUS_ZERO_EPSILON
      ) {
        return;
      }
      const measuredHeight = TURN_FOCUS_SPACER_USES_NATIVE_MEASUREMENT
        ? measure(spacerRef)?.height
        : observation.height;
      if (measuredHeight == null) {
        return;
      }
      if (onLayout) {
        runOnJS(onLayout)(measuredHeight, requestEpoch);
      }
    },
    [onLayout, request],
  );
  const handleLayout = useEvent<LayoutChangeEvent>(
    (event) => {
      "worklet";
      // A JS onLayout payload can arrive after a newer SharedValue request. Read
      // the mounted native view instead, then correlate that physical height
      // with the request epoch observed by the same UI-thread worklet.
      const measuredHeight = TURN_FOCUS_SPACER_USES_NATIVE_MEASUREMENT
        ? measure(spacerRef)?.height
        : event.layout.height;
      if (measuredHeight == null) {
        return;
      }
      const observation = request.value;
      if (onLayout) {
        runOnJS(onLayout)(measuredHeight, observation.requestEpoch);
      }
    },
    ["onLayout"],
  );
  return (
    <Reanimated.View
      accessibilityElementsHidden
      collapsable={false}
      importantForAccessibility="no-hide-descendants"
      onLayout={handleLayout}
      pointerEvents="none"
      ref={spacerRef}
      style={animatedStyle}
    />
  );
}

const styles = StyleSheet.create({
  timelineStage: {
    flex: 1,
    minHeight: 0,
    position: "relative",
  },
  timeline: {
    flex: 1,
    minHeight: 0,
  },
  timelineContent: {
    alignItems: "stretch",
    paddingHorizontal: INTERFACE_TIMELINE_HORIZONTAL_INSET,
    paddingBottom: 12,
    flexGrow: 1,
  },
  emptyOverlay: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    paddingHorizontal: INTERFACE_TIMELINE_HORIZONTAL_INSET,
    paddingTop: 14,
    justifyContent: "center",
    pointerEvents: "box-none",
  },
});
