import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { ActivityIndicator, ScrollView, StyleSheet, Text, View, VirtualizedList, useWindowDimensions, type ViewToken, type CellRendererProps } from "react-native";
import type { TerminalThemeChrome, TerminalThemePalette } from "../../constants/terminalThemes";
import type { GitDiffPage, GitDiffPageRequest, GitDiffScope } from "../../services/gitDiff";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";
import { DiffIconButton } from "./GitDiffReviewControls";
import { GitDiffRow } from "./GitDiffRow";
import { GitDiffStream, diffStreamRow, restoreDiffStream, diffBookmark, type DiffCellFrame, type GitDiffStreamState, type IndexedDiffRow } from "./gitDiffStream";

export interface GitDiffPosition {
  row?: number;
  offset: number;
  horizontalOffset?: number;
  snapshot?: GitDiffStreamState;
  refreshKey?: number;
  layout?: string;
}

export function GitDiffReader({ path, scope, loadPage, chrome, theme, position, onPosition, wrap, fontSize, refreshKey, showSearch, showHeaders, bottomInset }: {
  path: string; scope: GitDiffScope;
  loadPage(request: GitDiffPageRequest): Promise<GitDiffPage>;
  chrome: TerminalThemeChrome; theme: TerminalThemePalette;
  position: GitDiffPosition; onPosition(position: GitDiffPosition): void;
  wrap: boolean; fontSize: number; refreshKey: number; showSearch: boolean; showHeaders: boolean;
  bottomInset: number;
}) {
  const restored = position.refreshKey === refreshKey ? position : { offset: 0 };
  const stream = useMemo(() => new GitDiffStream(loadPage, path, scope, restoreDiffStream(restored.snapshot, restored.row)), [loadPage, path, scope, refreshKey]);
  const state = useSyncExternalStore(stream.subscribe, stream.getSnapshot);
  const [draft, setDraft] = useState(state.query);
  const { width, fontScale } = useWindowDimensions();
  const layout = `${wrap}:${fontSize}:${fontScale}`;
  const layoutRef = useRef(layout);
  layoutRef.current = layout;
  const offset = useRef(0);
  const frames = useRef(new Map<number, DiffCellFrame>());
  const withinRow = useRef(restored.layout === layout ? restored.offset : 0);
  const horizontalOffset = useRef(restored.horizontalOffset ?? 0);
  // Captured once: a live horizontal contentOffset would be re-applied by
  // Android Fabric on every reader re-render and fight native panning.
  const initialHorizontalOffset = useRef(horizontalOffset.current).current;
  const visibleRow = useRef(state.targetRow ?? state.start);
  const interacted = useRef(false);
  const list = useRef<VirtualizedList<IndexedDiffRow>>(null);
  const targetRow = useRef(state.targetRow);
  const initialCount = useRef(state.targetRow == null ? 12 : Math.max(12, state.targetRow - state.start + 1));
  const positionFrame = useRef<number | null>(null);
  const onTargetLayout = useRef<() => void>(() => {});
  const mounted = useRef(true);
  const readingStyle = useRef({ wrap, fontSize });
  const onPositionRef = useRef(onPosition);
  onPositionRef.current = onPosition;
  const viewability = useRef(({ viewableItems }: { viewableItems: ViewToken<IndexedDiffRow>[] }) => {
    const first = viewableItems.find(item => item.isViewable);
    if (first) visibleRow.current = first.item.index;
  });
  const anchor = useRef(state.anchor);
  if (anchor.current !== state.anchor) {
    anchor.current = state.anchor;
    offset.current = 0;
    visibleRow.current = state.targetRow ?? state.start;
    targetRow.current = state.targetRow;
    initialCount.current = state.targetRow == null ? 12 : Math.max(12, state.targetRow - state.start + 1);
    interacted.current = false;
    withinRow.current = 0;
    frames.current.clear();
  }
  const MeasuredCell = useMemo(() => function MeasuredCell({ item, children, style, onLayout }: CellRendererProps<IndexedDiffRow>) {
    const epoch = useRef(anchor.current);
    useEffect(() => () => { if (epoch.current === anchor.current) frames.current.delete(item.index); }, [item.index]);
    return <View style={style} onLayout={event => {
      if (epoch.current === anchor.current) frames.current.set(item.index, { offset: event.nativeEvent.layout.y, length: event.nativeEvent.layout.height });
      onLayout?.(event);
      if (item.index === targetRow.current) onTargetLayout.current();
    }}>{children}</View>;
  }, []);
  // Save geometry before native unmount can change the scroll offset.
  useLayoutEffect(() => {
    stream.resume();
    mounted.current = true;
    if (!stream.getSnapshot().chunks.length) void stream.loadNext();
    return () => {
      onPositionRef.current({ ...diffBookmark(frames.current, offset.current, visibleRow.current), layout: layoutRef.current, horizontalOffset: horizontalOffset.current, snapshot: stream.getSnapshot(), refreshKey });
      stream.dispose();
      mounted.current = false;
      if (positionFrame.current !== null) cancelAnimationFrame(positionFrame.current);
    };
  }, [stream, refreshKey]);
  useEffect(() => {
    if (!showSearch) { setDraft(""); void stream.search(""); }
  }, [showSearch, stream]);
  useEffect(() => {
    if (readingStyle.current.wrap === wrap && readingStyle.current.fontSize === fontSize) return;
    readingStyle.current = { wrap, fontSize };
    stream.reanchor(visibleRow.current);
  }, [stream, wrap, fontSize]);

  const renderRow = useCallback(({ item }: { item: IndexedDiffRow }) => (
    <GitDiffRow row={item.row} chrome={chrome} theme={theme} wrap={wrap} fontSize={fontSize} query={state.query} scope={scope} showHeaders={showHeaders} />
  ), [chrome, theme, wrap, fontSize, state.query, scope, showHeaders]);
  const hasRows = state.end > state.start;
  const positionTarget = () => {
    if (!mounted.current || targetRow.current == null || positionFrame.current !== null) return;
    positionFrame.current = requestAnimationFrame(() => {
      positionFrame.current = null;
      if (!mounted.current || !list.current || targetRow.current == null) return;
      const frame = frames.current.get(targetRow.current);
      if (frame) list.current.scrollToOffset({ offset: frame.offset + withinRow.current, animated: false });
    });
  };
  onTargetLayout.current = positionTarget;
  const edgeIndicator = (edge: "next" | "previous") => state.loading === edge ? (
    <View style={styles.pending}><ActivityIndicator size="small" accessibilityLabel="Loading diff" color={chrome.accent} /></View>
  ) : null;
  const rows = (
    <VirtualizedList<IndexedDiffRow>
      key={state.anchor}
      ref={list}
      data={state}
      getItem={diffStreamRow}
      getItemCount={(value: GitDiffStreamState) => value.end - value.start}
      keyExtractor={item => String(item.index)}
      renderItem={renderRow}
      CellRendererComponent={MeasuredCell}
      style={styles.root}
      contentContainerStyle={{ paddingBottom: bottomInset + 12 }}
      initialNumToRender={initialCount.current}
      maxToRenderPerBatch={8}
      windowSize={5}
      maintainVisibleContentPosition={{ minIndexForVisible: 0 }}
      onScroll={event => { offset.current = event.nativeEvent.contentOffset.y; }}
      onScrollBeginDrag={() => {
        interacted.current = true;
        targetRow.current = null;
        if (offset.current < 300) void stream.loadPrevious();
      }}
      onViewableItemsChanged={viewability.current}
      scrollEventThrottle={100}
      onContentSizeChange={positionTarget}
      onLayout={positionTarget}
      onEndReached={() => { void stream.loadNext(); }}
      onEndReachedThreshold={1.5}
      onStartReached={() => { if (interacted.current) void stream.loadPrevious(); }}
      onStartReachedThreshold={0.5}
      ListHeaderComponent={edgeIndicator("previous")}
      ListFooterComponent={edgeIndicator("next")}
      keyboardShouldPersistTaps="handled"
      accessibilityActions={[{ name: "increment", label: "Next hunk" }, { name: "decrement", label: "Previous hunk" }]}
      onAccessibilityAction={event => { void stream.seek(event.nativeEvent.actionName === "increment" ? "next" : "previous", visibleRow.current, "hunk"); }}
    />
  );
  return (
    <View style={styles.root}>
      {showSearch ? <View style={[styles.search, { borderColor: chrome.border }]}>
        <MobileSingleLineInput
          accessibilityLabel="Search this diff" placeholder="Find in diff" value={draft}
          onChangeText={setDraft} onSubmitEditing={() => void stream.search(draft)}
          returnKeyType="search" autoCapitalize="none" autoCorrect={false}
          containerStyle={styles.input} inputStyle={{ color: chrome.text }} placeholderTextColor={chrome.textSubtle}
        />
        <DiffIconButton icon="search" label="Find in diff" chrome={chrome} onPress={() => void stream.search(draft)} />
        <DiffIconButton icon="chevron-up" label="Previous matching line" chrome={chrome} disabled={!state.query || state.matches === 0 || Boolean(state.loading) || state.stale} onPress={() => void stream.seek("previous", visibleRow.current)} />
        <DiffIconButton icon="chevron-down" label="Next matching line" chrome={chrome} disabled={!state.query || state.matches === 0 || Boolean(state.loading) || state.stale} onPress={() => void stream.seek("next", visibleRow.current)} />
      </View> : null}
      {state.query ? <Text style={[styles.meta, { color: chrome.textMuted }]}>{state.matches} {state.matches === 1 ? "match" : "matches"}</Text> : null}
      {state.loading === "search" ? <ActivityIndicator size="small" color={chrome.accent} accessibilityLabel="Searching diff" /> : null}
      {state.error || state.stale ? <View style={[styles.notice, { borderColor: chrome.border }]}>
        <Text style={[styles.noticeText, { color: state.stale ? theme.yellow : theme.red }]}>{state.stale ? "Diff changed" : state.error}</Text>
        <DiffIconButton icon="refresh" label={state.stale ? "Refresh changed diff" : "Retry diff"} chrome={chrome} onPress={() => void (state.stale ? stream.refresh() : stream.retry())} />
      </View> : null}
      {hasRows ? wrap ? rows : <ScrollView
        horizontal style={styles.root} contentContainerStyle={styles.horizontal}
        contentOffset={{ x: initialHorizontalOffset, y: 0 }}
        onScroll={event => { horizontalOffset.current = event.nativeEvent.contentOffset.x; }} scrollEventThrottle={100}
      >
        <View style={{ width: Math.max(width, state.maxCharacters * fontSize * fontScale + 120 * fontScale) }}>{rows}</View>
      </ScrollView> : <View style={styles.state}>
        {state.loading || (state.total === null && !state.error && !state.stale) ? <ActivityIndicator color={chrome.accent} accessibilityLabel="Loading diff" /> : !state.error && !state.stale ? <Text style={{ color: chrome.textMuted }}>No changes in this comparison</Text> : null}
      </View>}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  horizontal: { height: "100%" },
  search: { flexDirection: "row", alignItems: "center", borderBottomWidth: StyleSheet.hairlineWidth },
  input: { flex: 1, minWidth: 0 },
  pending: { height: 36, alignItems: "center", justifyContent: "center" },
  state: { flex: 1, padding: 20, alignItems: "center", justifyContent: "center" },
  meta: { fontSize: 12, paddingHorizontal: 12, paddingVertical: 4 },
  notice: { flexDirection: "row", alignItems: "center", paddingLeft: 12, borderBottomWidth: StyleSheet.hairlineWidth },
  noticeText: { flex: 1, fontSize: 13 },
});
