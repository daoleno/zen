import React, {
  useCallback,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  FlatList,
  Pressable,
  StyleSheet,
  Switch,
  Text,
  TextInput,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  ContinuousCorners,
  Radii,
  TouchTarget,
  TypeScale,
  Typography,
  UiTextMetrics,
  useAppTheme,
} from "../../constants/tokens";
import {
  filterGitDiffFiles,
  type GitDiffFileInfo,
  type GitDiffPage,
  type GitDiffPageRequest,
  type GitDiffScope,
} from "../../services/gitDiff";
import { EmptyState } from "../ui/EmptyState";
import { GitDiffReader, type GitDiffPosition } from "./GitDiffReader";
import { DiffIconButton } from "./GitDiffReviewControls";
import { GitDiffDetailHeader, StatusTile } from "./GitDiffSheetTopChrome";
import {
  describeGitDiffFile,
  gitDiffRowScopeNote,
  summarizeGitDiffFiles,
} from "./gitDiffPresentation";
import { withAlpha } from "./colorWithAlpha";

interface GitDiffSheetDiffContentProps {
  files: GitDiffFileInfo[];
  clean: boolean;
  branch?: string;
  repoTitle: string;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  loadPage(request: GitDiffPageRequest): Promise<GitDiffPage>;
  refreshKey: number;
  scope: GitDiffScope;
  scopeCounts: Record<GitDiffScope, number>;
  selectedFile: GitDiffFileInfo | null;
  wide: boolean;
  bottomInset: number;
  fileFilterOpen: boolean;
  diffSearchOpen: boolean;
  diffOptionsOpen: boolean;
  loading: boolean;
  onSelectFile(path: string): void;
  onScopeChange(scope: GitDiffScope): void;
  onClearSelection(): void;
  onCloseFileFilter(): void;
  onToggleDiffSearch(): void;
  onToggleDiffOptions(): void;
  onBrowseFiles(): void;
  /** Opens the shared action menu for one file (row long-press, wide header). */
  onOpenFileActions(file: GitDiffFileInfo, reading: boolean): void;
}

const FONT_SIZES = { min: 10, max: 20, step: 2 };

export function GitDiffSheetDiffContent({
  files,
  clean,
  branch,
  repoTitle,
  theme,
  chrome,
  loadPage,
  refreshKey,
  scope,
  scopeCounts,
  selectedFile,
  wide,
  bottomInset,
  fileFilterOpen,
  diffSearchOpen,
  diffOptionsOpen,
  loading,
  onSelectFile,
  onScopeChange,
  onClearSelection,
  onCloseFileFilter,
  onToggleDiffSearch,
  onToggleDiffOptions,
  onBrowseFiles,
  onOpenFileActions,
}: GitDiffSheetDiffContentProps) {
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query);
  const [wrap, setWrap] = useState(true);
  const [fontSize, setFontSize] = useState(12);
  const [showHeaders, setShowHeaders] = useState(false);
  const positions = useRef(new Map<string, GitDiffPosition>());
  const listRef = useRef<FlatList<GitDiffFileInfo>>(null);
  const filtered = useMemo(
    () => filterGitDiffFiles(files, scope, deferredQuery),
    [files, scope, deferredQuery],
  );
  const scopedFiles = useMemo(
    () => filterGitDiffFiles(files, scope, ""),
    [files, scope],
  );
  const positionKey = JSON.stringify([selectedFile?.path ?? null, scope]);

  // Hiding the filter never leaves the list silently filtered.
  useEffect(() => {
    if (!fileFilterOpen) setQuery("");
  }, [fileFilterOpen]);

  // Scope and filter changes reset the overview to the top exactly once.
  // The list itself is never controlled through `contentOffset`: Android
  // re-applies that prop on every render, which cancels momentum mid-fling.
  useEffect(() => {
    listRef.current?.scrollToOffset({ offset: 0, animated: false });
  }, [scope, deferredQuery]);

  const listBottomPadding = bottomInset + 16;
  const scopeTotal = scopeCounts[scope];
  const summary = summarizeGitDiffFiles(scopedFiles, scope);
  const metaLabel = deferredQuery
    ? `${filtered.length} of ${scopeTotal} ${scopeTotal === 1 ? "file" : "files"}`
    : summary.label;
  const noChanges = clean && files.length === 0;
  const count = filtered.length;

  const renderFile = useCallback(
    ({ item, index }: { item: GitDiffFileInfo; index: number }) => (
      <GitDiffFileRow
        file={item}
        scope={scope}
        theme={theme}
        chrome={chrome}
        first={index === 0}
        last={index === count - 1}
        selected={selectedFile?.path === item.path}
        onPress={() => onSelectFile(item.path)}
        onLongPress={() => onOpenFileActions(item, false)}
      />
    ),
    [chrome, count, onOpenFileActions, onSelectFile, scope, selectedFile?.path, theme],
  );

  const detailHeader = selectedFile ? (
    <GitDiffDetailHeader
      chrome={chrome}
      theme={theme}
      file={selectedFile}
      scope={scope}
      repoTitle={repoTitle}
      loading={loading}
      onClear={onClearSelection}
      onOpenActions={() => onOpenFileActions(selectedFile, true)}
    />
  ) : null;

  const reader = selectedFile ? (
    <>
      {diffOptionsOpen ? (
        <GitDiffOptionsStrip
          chrome={chrome}
          wrap={wrap}
          fontSize={fontSize}
          showHeaders={showHeaders}
          onWrapChange={setWrap}
          onFontSizeChange={setFontSize}
          onShowHeadersChange={setShowHeaders}
          onDone={onToggleDiffOptions}
        />
      ) : null}
      <GitDiffReader
        key={`${positionKey}:${refreshKey}`}
        path={selectedFile.path}
        scope={scope}
        theme={theme}
        chrome={chrome}
        loadPage={loadPage}
        refreshKey={refreshKey}
        position={positions.current.get(positionKey) ?? { offset: 0 }}
        onPosition={(position) => positions.current.set(positionKey, position)}
        wrap={wrap}
        fontSize={fontSize}
        showSearch={diffSearchOpen}
        showHeaders={showHeaders}
        bottomInset={bottomInset}
        onCloseSearch={onToggleDiffSearch}
      />
    </>
  ) : (
    <EmptyState
      icon="git-compare-outline"
      title="No file selected"
      style={styles.emptyDetail}
    />
  );

  const emptyList = noChanges ? (
    <EmptyState
      icon="checkmark-circle-outline"
      title="Working tree clean"
      detail={branch ?? null}
      action={{ label: "Browse files", icon: "folder-open-outline", onPress: onBrowseFiles }}
    />
  ) : deferredQuery ? (
    <EmptyState
      size="inline"
      title="No matching files"
      detail={`Nothing matches “${deferredQuery}”.`}
      action={{ label: "Clear filter", onPress: () => setQuery("") }}
    />
  ) : (
    <EmptyState
      size="inline"
      title={scope === "staged" ? "Nothing staged" : scope === "working" ? "No working changes" : "No changes"}
    />
  );

  return (
    <View style={[styles.root, wide ? styles.rootWide : styles.rootStack]}>
      <View
        style={[
          styles.listPane,
          wide ? [styles.listPaneWide, { borderRightColor: chrome.border }] : styles.listPaneStack,
          { backgroundColor: chrome.appBackground },
        ]}
        importantForAccessibility={
          selectedFile && !wide ? "no-hide-descendants" : "auto"
        }
        accessibilityElementsHidden={Boolean(selectedFile && !wide)}
      >
        {noChanges ? null : (
          <View style={styles.listTop}>
            <GitDiffScopeControl
              chrome={chrome}
              scope={scope}
              counts={scopeCounts}
              onChange={onScopeChange}
            />
            {fileFilterOpen ? (
              <View style={styles.filterRow}>
                <View style={[styles.filterField, { backgroundColor: chrome.surfaceMuted }]}>
                  <Ionicons name="search" size={16} color={chrome.textSubtle} />
                  <TextInput
                    accessibilityLabel="Filter changed paths"
                    placeholder="Filter paths"
                    placeholderTextColor={chrome.textSubtle}
                    style={[styles.filterInput, { color: chrome.text }]}
                    autoCorrect={false}
                    autoCapitalize="none"
                    autoFocus
                    value={query}
                    onChangeText={setQuery}
                    returnKeyType="search"
                  />
                  {query ? (
                    <Pressable
                      accessibilityRole="button"
                      accessibilityLabel="Clear path filter"
                      onPress={() => setQuery("")}
                      hitSlop={Math.ceil((TouchTarget - 17) / 2)}
                    >
                      <Ionicons name="close-circle" size={17} color={chrome.textSubtle} />
                    </Pressable>
                  ) : null}
                </View>
                <Pressable
                  accessibilityRole="button"
                  accessibilityLabel="Done filtering"
                  onPress={onCloseFileFilter}
                  style={({ pressed }) => [styles.textButton, { opacity: pressed ? 0.55 : 1 }]}
                >
                  <Text style={[styles.textButtonLabel, { color: chrome.link }]}>Done</Text>
                </Pressable>
              </View>
            ) : null}
            {scopeTotal > 0 ? (
              <Text
                style={[styles.listMetaText, { color: chrome.textSubtle }]}
                numberOfLines={1}
                accessibilityLiveRegion="polite"
              >
                {metaLabel}
              </Text>
            ) : null}
          </View>
        )}
        <FlatList
          ref={listRef}
          data={filtered}
          keyExtractor={(item) => item.path}
          renderItem={renderFile}
          style={styles.list}
          contentContainerStyle={[
            styles.listContent,
            { paddingBottom: listBottomPadding },
            count === 0 ? styles.listContentEmpty : null,
          ]}
          initialNumToRender={12}
          maxToRenderPerBatch={12}
          windowSize={5}
          keyboardShouldPersistTaps="handled"
          keyboardDismissMode="on-drag"
          removeClippedSubviews={false}
          onScrollToIndexFailed={() => {}}
          ListEmptyComponent={emptyList}
        />
      </View>

      {selectedFile && !wide ? (
        <View style={[styles.overlay, { backgroundColor: chrome.surface }]}>
          {reader}
        </View>
      ) : null}
      {wide ? (
        <View style={[styles.detailPane, { backgroundColor: chrome.surface }]}>
          {detailHeader}
          {reader}
        </View>
      ) : null}
    </View>
  );
}

const GitDiffFileRow = React.memo(function GitDiffFileRow({
  file,
  scope,
  theme,
  chrome,
  first,
  last,
  selected,
  onPress,
  onLongPress,
}: {
  file: GitDiffFileInfo;
  scope: GitDiffScope;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  first: boolean;
  last: boolean;
  selected: boolean;
  onPress(): void;
  onLongPress(): void;
}) {
  const presentation = describeGitDiffFile(file, scope);
  const oldLabel = presentation.oldName
    ? `${presentation.oldDirectory ?? ""}${presentation.oldName}`
    : null;
  const note = gitDiffRowScopeNote(file, scope);
  const detail = oldLabel
    ? `from ${oldLabel}`
    : presentation.directory.replace(/\/$/, "");

  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ selected }}
      accessibilityLabel={`${file.path}, ${presentation.statusLabel}, ${presentation.scopeLabel}, ${
        presentation.binary
          ? "binary"
          : `plus ${presentation.additions}, minus ${presentation.deletions}`
      }`}
      accessibilityHint="More actions available"
      accessibilityActions={[{ name: "longpress", label: "More actions" }]}
      onAccessibilityAction={(event) => {
        if (event.nativeEvent.actionName === "longpress") onLongPress();
      }}
      onPress={() => {
        void Haptics.selectionAsync();
        onPress();
      }}
      onLongPress={() => {
        void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
        onLongPress();
      }}
      delayLongPress={350}
      style={({ pressed }) => [
        styles.file,
        {
          backgroundColor: pressed
            ? chrome.surfaceActive
            : selected
              ? chrome.accentSoft
              : chrome.surface,
          borderTopLeftRadius: first ? Radii.card : 0,
          borderTopRightRadius: first ? Radii.card : 0,
          borderBottomLeftRadius: last ? Radii.card : 0,
          borderBottomRightRadius: last ? Radii.card : 0,
        },
      ]}
    >
      <StatusTile presentation={presentation} theme={theme} chrome={chrome} size={28} />
      <View
        style={[
          styles.fileBody,
          !last && { borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: chrome.border },
        ]}
      >
        <View style={styles.fileCopy}>
          <Text
            style={[styles.fileName, { color: chrome.text }]}
            numberOfLines={1}
            ellipsizeMode="middle"
          >
            {presentation.name}
          </Text>
          {detail || note ? (
            <View style={styles.fileDetailRow}>
              {detail ? (
                <Text
                  style={[styles.fileDetail, { color: chrome.textSubtle }]}
                  numberOfLines={1}
                  ellipsizeMode="head"
                >
                  {detail}
                </Text>
              ) : null}
              {note ? (
                <Text
                  style={[styles.fileNote, { color: chrome.textMuted }]}
                  numberOfLines={1}
                >
                  {detail ? `· ${note}` : note}
                </Text>
              ) : null}
            </View>
          ) : null}
        </View>
        {presentation.binary ? (
          <Text style={[styles.count, { color: chrome.textSubtle }]}>Binary</Text>
        ) : (
          <Text style={styles.count} numberOfLines={1}>
            <Text style={{ color: presentation.additions ? theme.green : chrome.textSubtle }}>
              +{presentation.additions}
            </Text>
            <Text style={{ color: presentation.deletions ? theme.red : chrome.textSubtle }}>
              {" −"}
              {presentation.deletions}
            </Text>
          </Text>
        )}
      </View>
    </Pressable>
  );
});

function GitDiffScopeControl({
  chrome,
  scope,
  counts,
  onChange,
}: {
  chrome: TerminalThemeChrome;
  scope: GitDiffScope;
  counts: Record<GitDiffScope, number>;
  onChange(scope: GitDiffScope): void;
}) {
  const { colors, isLight } = useAppTheme();
  const options: { value: GitDiffScope; label: string }[] = [
    { value: "all", label: "All" },
    { value: "working", label: "Working" },
    { value: "staged", label: "Staged" },
  ];
  return (
    <View
      accessibilityRole="tablist"
      style={[styles.scopeTrack, { backgroundColor: withAlpha(chrome.text, isLight ? 0.06 : 0.08) }]}
    >
      {options.map((option) => {
        const active = scope === option.value;
        return (
          <Pressable
            key={option.value}
            accessibilityRole="tab"
            accessibilityState={{ selected: active }}
            accessibilityLabel={`${option.label}, ${counts[option.value]} files`}
            onPress={() => {
              if (active) return;
              void Haptics.selectionAsync();
              onChange(option.value);
            }}
            style={styles.scopeHit}
          >
            <View
              style={[
                styles.scope,
                active && [
                  styles.scopeActive,
                  { backgroundColor: isLight ? colors.bgSurface : colors.bgElevated, shadowColor: chrome.shadowColor },
                ],
              ]}
            >
              <Text
                style={[
                  styles.scopeLabel,
                  { color: active ? chrome.text : chrome.textMuted },
                ]}
                numberOfLines={1}
              >
                {option.label}
              </Text>
              <Text
                style={[styles.scopeCount, { color: active ? chrome.textMuted : chrome.textSubtle }]}
                numberOfLines={1}
              >
                {counts[option.value]}
              </Text>
            </View>
          </Pressable>
        );
      })}
    </View>
  );
}

function GitDiffOptionsStrip({
  chrome,
  wrap,
  fontSize,
  showHeaders,
  onWrapChange,
  onFontSizeChange,
  onShowHeadersChange,
  onDone,
}: {
  chrome: TerminalThemeChrome;
  wrap: boolean;
  fontSize: number;
  showHeaders: boolean;
  onWrapChange(value: boolean): void;
  onFontSizeChange(value: number): void;
  onShowHeadersChange(value: boolean): void;
  onDone(): void;
}) {
  return (
    <View style={[styles.options, { backgroundColor: chrome.appBackground, borderBottomColor: chrome.border }]}>
      <View style={styles.optionsHeader}>
        <Text accessibilityRole="header" style={[styles.optionsTitle, { color: chrome.textSubtle }]}>
          Display
        </Text>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Done with display options"
          onPress={onDone}
          style={({ pressed }) => [styles.textButton, { opacity: pressed ? 0.55 : 1 }]}
        >
          <Text style={[styles.textButtonLabel, { color: chrome.link }]}>Done</Text>
        </Pressable>
      </View>
      <View style={[styles.optionsCard, { backgroundColor: chrome.surface }]}>
        <View style={[styles.option, { borderBottomColor: chrome.border }]}>
          <Text style={[styles.optionLabel, { color: chrome.text }]}>Wrap lines</Text>
          <Switch accessibilityLabel="Wrap lines" value={wrap} onValueChange={onWrapChange} />
        </View>
        <View style={[styles.option, { borderBottomColor: chrome.border }]}>
          <Text style={[styles.optionLabel, { color: chrome.text }]}>Text size</Text>
          <View style={styles.stepper}>
            <DiffIconButton
              icon="remove"
              label="Smaller code text"
              chrome={chrome}
              disabled={fontSize <= FONT_SIZES.min}
              onPress={() => onFontSizeChange(fontSize - FONT_SIZES.step)}
            />
            <Text style={[styles.stepperValue, { color: chrome.text }]}>{fontSize}</Text>
            <DiffIconButton
              icon="add"
              label="Larger code text"
              chrome={chrome}
              disabled={fontSize >= FONT_SIZES.max}
              onPress={() => onFontSizeChange(fontSize + FONT_SIZES.step)}
            />
          </View>
        </View>
        <View style={[styles.option, styles.optionLast]}>
          <Text style={[styles.optionLabel, { color: chrome.text }]}>Patch headers</Text>
          <Switch
            accessibilityLabel="Patch headers"
            value={showHeaders}
            onValueChange={onShowHeadersChange}
          />
        </View>
      </View>
    </View>
  );
}

const TILE = 28;

const styles = StyleSheet.create({
  root: { flex: 1 },
  rootStack: { flexDirection: "column" },
  rootWide: { flexDirection: "row" },
  listPane: { flex: 1, minWidth: 0 },
  listPaneStack: { flex: 1 },
  listPaneWide: { flex: 0, width: 340, borderRightWidth: StyleSheet.hairlineWidth },
  detailPane: { flex: 1, minWidth: 0 },
  overlay: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
  },
  list: { flex: 1 },
  listTop: {
    paddingHorizontal: 16,
    paddingTop: 10,
    gap: 10,
  },
  listContent: { paddingHorizontal: 16, paddingTop: 2 },
  listContentEmpty: { flexGrow: 1, justifyContent: "center" },
  scopeTrack: {
    flexDirection: "row",
    padding: 2,
    borderRadius: Radii.sm,
    ...ContinuousCorners,
  },
  scopeHit: {
    flex: 1,
    minHeight: TouchTarget - 4,
  },
  scope: {
    flex: 1,
    borderRadius: 10,
    ...ContinuousCorners,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
  },
  scopeActive: {
    shadowOpacity: 0.08,
    shadowRadius: 3,
    shadowOffset: { width: 0, height: 1 },
    elevation: 1,
  },
  scopeLabel: {
    ...TypeScale.label,
    ...UiTextMetrics,
  },
  scopeCount: {
    ...TypeScale.caption,
    ...UiTextMetrics,
    fontVariant: ["tabular-nums"],
  },
  filterRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
  filterField: {
    flex: 1,
    minHeight: 40,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 12,
    borderRadius: Radii.sm,
    ...ContinuousCorners,
  },
  filterInput: {
    ...TypeScale.compact,
    flex: 1,
    minWidth: 0,
    minHeight: 40,
    paddingVertical: 0,
  },
  textButton: {
    minHeight: TouchTarget,
    minWidth: TouchTarget,
    paddingHorizontal: 8,
    alignItems: "center",
    justifyContent: "center",
  },
  textButtonLabel: {
    ...TypeScale.label,
    ...UiTextMetrics,
  },
  listMetaText: {
    ...TypeScale.caption,
    ...UiTextMetrics,
    paddingHorizontal: 16,
    paddingBottom: 6,
    fontVariant: ["tabular-nums"],
  },
  file: {
    ...ContinuousCorners,
    minHeight: 56,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingLeft: 14,
    overflow: "hidden",
  },
  fileBody: {
    flex: 1,
    minWidth: 0,
    alignSelf: "stretch",
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingVertical: 9,
    paddingRight: 14,
  },
  fileCopy: { flex: 1, minWidth: 0 },
  fileName: {
    ...TypeScale.compact,
    ...UiTextMetrics,
    fontFamily: Typography.uiFontMedium,
  },
  fileDetailRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
    minWidth: 0,
  },
  fileDetail: {
    ...TypeScale.caption,
    ...UiTextMetrics,
    flexShrink: 1,
    minWidth: 0,
  },
  fileNote: {
    ...TypeScale.caption,
    ...UiTextMetrics,
    flexShrink: 0,
  },
  count: {
    ...TypeScale.caption,
    ...UiTextMetrics,
    fontFamily: Typography.terminalFont,
    fontVariant: ["tabular-nums"],
    textAlign: "right",
    minWidth: TILE + 16,
  },
  emptyDetail: { flex: 1, justifyContent: "center" },
  options: {
    paddingHorizontal: 16,
    paddingBottom: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  optionsHeader: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingLeft: 16,
  },
  optionsTitle: {
    ...TypeScale.caption,
    ...UiTextMetrics,
  },
  optionsCard: {
    borderRadius: Radii.card,
    ...ContinuousCorners,
    paddingLeft: 16,
    overflow: "hidden",
  },
  option: {
    minHeight: TouchTarget + 4,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
    paddingRight: 8,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  optionLast: { borderBottomWidth: 0 },
  optionLabel: { ...TypeScale.compact, ...UiTextMetrics },
  stepper: { flexDirection: "row", alignItems: "center" },
  stepperValue: {
    ...TypeScale.label,
    minWidth: 26,
    textAlign: "center",
    fontVariant: ["tabular-nums"],
  },
});
