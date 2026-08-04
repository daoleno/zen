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
  buildTimelineRenderItems,
  stabilizeTimelineRenderItems,
  type TimelineRenderItem,
} from "./InterfaceTimelineGrouping";
import type { PatchFileSummary } from "./InterfaceTimelineActivityTypes";
import { timelineListStabilityProps } from "./timelineScrollPolicy";
import { StructuredChatInsetScrollView } from "./StructuredChatInsetScrollView";
import type { StructuredChatKeyboardLifecycleGate } from "./chatKeyboardOverlayPolicy";
import {
  resolveTurnFocusAnchorItemId,
  turnFocusRowGeometryFromCell,
  type TurnFocusSpacerRequest,
} from "./turnFocusState";

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

const TimelineCellView = View as React.ComponentType<
  React.ComponentProps<typeof View> & {
    onFocusCapture?: CellRendererProps<TimelineRenderItem>["onFocusCapture"];
  }
>;

interface InterfaceTimelineViewProps {
  scrollRef: React.RefObject<FlatList<ZenTimelineItem> | null>;
  nativeFollowSuspended: boolean;
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
  agentCwd?: string;
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
  nativeFollowSuspended,
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
  agentCwd,
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
    () => timelineListStabilityProps(nativeFollowSuspended),
    [nativeFollowSuspended],
  );
  const turnFocusCellMeasurementRef = React.useRef<TurnFocusCellMeasurement>(
    {},
  );
  const previousRenderItemsRef = React.useRef<TimelineRenderItem[]>([]);
  const previousItemsRef = React.useRef(items);
  React.useEffect(() => {
    if (previousItemsRef.current === items) {
      return;
    }
    previousItemsRef.current = items;
    onItemsMutated?.();
  }, [items, onItemsMutated]);
  const renderItems = React.useMemo(() => {
    const next = buildTimelineRenderItems([...items].reverse(), {
      showDateDividers: zenTheme.chat.showDateDividers,
    });
    const stable = stabilizeTimelineRenderItems(
      previousRenderItemsRef.current,
      next,
    );
    previousRenderItemsRef.current = stable;
    return stable;
  }, [items, zenTheme.chat.showDateDividers]);
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
      />
    ),
    [
      extraContentPadding,
      keyboardLifecycleGate,
      onClearanceChange,
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
        agentCwd={agentCwd}
        emptyTitle={emptyTitle}
        emptyBody={emptyBody}
      />
    ),
    [
      agentCwd,
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
      <View style={styles.timelineStage}>
        <FlatList<TimelineRenderItem>
          accessibilityLabel="Conversation timeline"
          testID="structured-chat-timeline"
          ref={
            scrollRef as React.RefObject<FlatList<TimelineRenderItem> | null>
          }
          data={renderItems}
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
          keyboardDismissMode={
            Platform.OS === "ios" ? "interactive" : "on-drag"
          }
          keyboardShouldPersistTaps="handled"
          showsVerticalScrollIndicator={false}
          scrollEventThrottle={32}
          onLayout={onLayout}
          onScroll={onScroll}
          onScrollBeginDrag={onScrollBeginDrag}
          onScrollEndDrag={onScrollEndDrag}
          onMomentumScrollBegin={onMomentumScrollBegin}
          onMomentumScrollEnd={onMomentumScrollEnd}
          onTouchStart={handleTouchStart}
          onTouchEnd={handleTouchEnd}
          onTouchCancel={handleTouchCancel}
          onContentSizeChange={onContentSizeChange}
          initialNumToRender={8}
          maxToRenderPerBatch={6}
          updateCellsBatchingPeriod={48}
          windowSize={5}
        />

        {items.length === 0 ? (
          <View style={styles.emptyOverlay}>{emptyContent}</View>
        ) : null}
      </View>
    </TimelineTextSelectableContext.Provider>
  );
}

function ignoreClearanceChange() {}

function TurnFocusTimelineCell({
  children,
  item,
  measurementRef,
  onFocusCapture,
  onLayout,
  style,
}: CellRendererProps<TimelineRenderItem> & {
  measurementRef: React.RefObject<TurnFocusCellMeasurement>;
}) {
  const handleLayout = React.useCallback(
    (event: LayoutChangeEvent) => {
      // This positioned content cell includes newer Activity/divider siblings;
      // a wrapper inside renderItem would only report its local y (normally 0).
      onLayout?.(event);
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
    [item.id, measurementRef, onLayout],
  );

  return (
    <TimelineCellView
      collapsable={false}
      onFocusCapture={onFocusCapture}
      onLayout={handleLayout}
      style={style}
    >
      {children}
    </TimelineCellView>
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
    paddingHorizontal: 14,
    paddingBottom: 12,
    flexGrow: 1,
  },
  emptyOverlay: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    paddingHorizontal: 14,
    paddingTop: 14,
    justifyContent: "center",
    pointerEvents: "box-none",
  },
});
