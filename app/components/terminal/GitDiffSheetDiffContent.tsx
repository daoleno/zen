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
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import {
  filterGitDiffFiles,
  type GitDiffFileInfo,
  type GitDiffPage,
  type GitDiffPageRequest,
  type GitDiffScope,
} from "../../services/gitDiff";
import { GitDiffReader, type GitDiffPosition } from "./GitDiffReader";
import { DiffIconButton } from "./GitDiffReviewControls";
import { GitDiffDetailHeader } from "./GitDiffSheetTopChrome";
import {
  describeGitDiffFile,
  type GitDiffStatusTone,
} from "./gitDiffPresentation";
import { withAlpha } from "./colorWithAlpha";

interface GitDiffSheetDiffContentProps {
  files: GitDiffFileInfo[];
  clean: boolean;
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
  onOpenFile(path: string): void;
  onScopeChange(scope: GitDiffScope): void;
  onClearSelection(): void;
  onToggleDiffSearch(): void;
  onToggleDiffOptions(): void;
  onRefresh(): void;
}

export function GitDiffSheetDiffContent({
  files,
  clean,
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
  onOpenFile,
  onScopeChange,
  onClearSelection,
  onToggleDiffSearch,
  onToggleDiffOptions,
  onRefresh,
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
  const positionKey = JSON.stringify([selectedFile?.path ?? null, scope]);

  // Scope and filter changes reset the overview to the top exactly once.
  // The list itself is never controlled through `contentOffset`: Android
  // re-applies that prop on every render, which cancels momentum mid-fling.
  useEffect(() => {
    listRef.current?.scrollToOffset({ offset: 0, animated: false });
  }, [scope, deferredQuery]);

  const listBottomPadding = bottomInset + 12;
  const scopeTotal = scopeCounts[scope];
  const metaLabel = deferredQuery
    ? `${filtered.length} / ${scopeTotal} files`
    : `${filtered.length} ${filtered.length === 1 ? "file" : "files"}`;

  const renderFile = useCallback(
    ({ item }: { item: GitDiffFileInfo }) => (
      <GitDiffFileRow
        file={item}
        scope={scope}
        theme={theme}
        chrome={chrome}
        selected={selectedFile?.path === item.path}
        onPress={() => onSelectFile(item.path)}
      />
    ),
    [chrome, onSelectFile, scope, selectedFile?.path, theme],
  );

  const detailHeader = selectedFile ? (
    <GitDiffDetailHeader
      chrome={chrome}
      file={selectedFile}
      loading={loading}
      diffSearchOpen={diffSearchOpen}
      diffOptionsOpen={diffOptionsOpen}
      onClear={onClearSelection}
      onRefresh={onRefresh}
      onToggleSearch={onToggleDiffSearch}
      onToggleOptions={onToggleDiffOptions}
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
          deleted={selectedFile.status === "deleted"}
          onWrapChange={setWrap}
          onFontSizeChange={setFontSize}
          onShowHeadersChange={setShowHeaders}
          onOpenFile={() => onOpenFile(selectedFile.path)}
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
      />
    </>
  ) : (
    <View style={styles.emptyDetail}>
      <Ionicons name="reader-outline" size={22} color={chrome.textSubtle} />
      <Text style={[styles.emptyDetailText, { color: chrome.textMuted }]}>
        Select a changed file to review
      </Text>
    </View>
  );

  return (
    <View style={[styles.root, wide ? styles.rootWide : styles.rootStack]}>
      <View
        style={[
          styles.listPane,
          wide ? styles.listPaneWide : styles.listPaneStack,
          { borderColor: chrome.border },
        ]}
        importantForAccessibility={
          selectedFile && !wide ? "no-hide-descendants" : "auto"
        }
        accessibilityElementsHidden={Boolean(selectedFile && !wide)}
      >
        <GitDiffScopeControl
          chrome={chrome}
          scope={scope}
          counts={scopeCounts}
          onChange={onScopeChange}
        />
        {fileFilterOpen ? (
          <TextInput
            accessibilityLabel="Filter changed paths"
            placeholder="Filter paths"
            placeholderTextColor={chrome.textSubtle}
            style={[
              styles.search,
              { color: chrome.text, borderColor: chrome.border },
            ]}
            autoCorrect={false}
            autoCapitalize="none"
            autoFocus
            value={query}
            onChangeText={setQuery}
            returnKeyType="search"
            clearButtonMode="while-editing"
          />
        ) : null}
        <View
          style={[styles.listMeta, { borderBottomColor: chrome.border }]}
        >
          <Text
            style={[styles.listMetaText, { color: chrome.textMuted }]}
            numberOfLines={1}
          >
            {metaLabel}
          </Text>
          {deferredQuery ? (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel="Clear path filter"
              onPress={() => setQuery("")}
              hitSlop={8}
            >
              <Ionicons name="close-circle" size={16} color={chrome.textSubtle} />
            </Pressable>
          ) : null}
        </View>
        <FlatList
          ref={listRef}
          data={filtered}
          keyExtractor={(item) => item.path}
          renderItem={renderFile}
          style={styles.list}
          contentContainerStyle={{ paddingBottom: listBottomPadding }}
          initialNumToRender={12}
          maxToRenderPerBatch={12}
          windowSize={5}
          keyboardShouldPersistTaps="handled"
          removeClippedSubviews={false}
          onScrollToIndexFailed={() => {}}
          ListEmptyComponent={
            <Text style={[styles.empty, { color: chrome.textMuted }]}>
              {clean ? "Working tree is clean" : "No matching changes"}
            </Text>
          }
        />
      </View>

      {selectedFile && !wide ? (
        <View
          style={[
            styles.overlay,
            { backgroundColor: chrome.surface, borderColor: chrome.border },
          ]}
        >
          {reader}
        </View>
      ) : null}
      {wide ? (
        <View
          style={[
            styles.detailPane,
            { borderColor: chrome.border, backgroundColor: chrome.surface },
          ]}
        >
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
  selected,
  onPress,
}: {
  file: GitDiffFileInfo;
  scope: GitDiffScope;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  selected: boolean;
  onPress(): void;
}) {
  const presentation = describeGitDiffFile(file, scope);
  const accent = toneColor(presentation.tone, theme, chrome);
  const oldLabel = presentation.oldName
    ? `${presentation.oldDirectory ?? ""}${presentation.oldName}`
    : null;
  const detail = [
    presentation.directory || null,
    presentation.scopeLabel,
    oldLabel ? `from ${oldLabel}` : null,
  ]
    .filter(Boolean)
    .join("  ·  ");

  return (
    <Pressable
      accessibilityRole="button"
      accessibilityState={{ selected }}
      accessibilityLabel={`${file.path}, ${presentation.statusLabel}, ${presentation.scopeLabel}, ${
        presentation.binary
          ? "binary"
          : `plus ${presentation.additions}, minus ${presentation.deletions}`
      }`}
      onPress={onPress}
      style={({ pressed }) => [
        styles.file,
        {
          borderColor: chrome.border,
          backgroundColor: pressed
            ? chrome.surfaceMuted
            : selected
              ? chrome.surfaceActive
              : "transparent",
        },
      ]}
    >
      <Ionicons
        name={presentation.icon as React.ComponentProps<typeof Ionicons>["name"]}
        size={18}
        color={accent}
      />
      <View style={styles.fileCopy}>
        <Text
          style={[styles.fileName, { color: chrome.text }]}
          numberOfLines={1}
        >
          {presentation.name}
        </Text>
        <Text
          style={[styles.fileDetail, { color: chrome.textMuted }]}
          numberOfLines={1}
          ellipsizeMode="head"
        >
          {detail}
        </Text>
      </View>
      <View style={styles.stats}>
        {presentation.binary ? (
          <Text style={[styles.binary, { color: chrome.textSubtle }]}>
            Binary
          </Text>
        ) : (
          <>
            <Text style={[styles.count, { color: theme.green }]}>
              +{presentation.additions}
            </Text>
            <Text style={[styles.count, { color: theme.red }]}>
              -{presentation.deletions}
            </Text>
          </>
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
  const options: { value: GitDiffScope; label: string }[] = [
    { value: "all", label: "All" },
    { value: "working", label: "Working" },
    { value: "staged", label: "Staged" },
  ];
  return (
    <View
      accessibilityRole="tablist"
      style={[styles.scopeBar, { borderBottomColor: chrome.border }]}
    >
      {options.map((option) => {
        const active = scope === option.value;
        return (
          <Pressable
            key={option.value}
            accessibilityRole="tab"
            accessibilityState={{ selected: active }}
            accessibilityLabel={`${option.label}, ${counts[option.value]} files`}
            onPress={() => onChange(option.value)}
            style={[
              styles.scope,
              {
                backgroundColor: active
                  ? withAlpha(chrome.accent, 0.14)
                  : "transparent",
                borderColor: active
                  ? withAlpha(chrome.accent, 0.32)
                  : "transparent",
              },
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
              style={[
                styles.scopeCount,
                { color: active ? chrome.accent : chrome.textSubtle },
              ]}
              numberOfLines={1}
            >
              {counts[option.value]}
            </Text>
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
  deleted,
  onWrapChange,
  onFontSizeChange,
  onShowHeadersChange,
  onOpenFile,
}: {
  chrome: TerminalThemeChrome;
  wrap: boolean;
  fontSize: number;
  showHeaders: boolean;
  deleted: boolean;
  onWrapChange(value: boolean): void;
  onFontSizeChange(value: number): void;
  onShowHeadersChange(value: boolean): void;
  onOpenFile(): void;
}) {
  return (
    <View style={[styles.options, { borderColor: chrome.border }]}>
      <View style={styles.option}>
        <Text style={[styles.optionLabel, { color: chrome.text }]}>
          Wrap lines
        </Text>
        <Switch
          accessibilityLabel="Wrap lines"
          value={wrap}
          onValueChange={onWrapChange}
        />
      </View>
      <View style={styles.option}>
        <Text style={[styles.optionLabel, { color: chrome.text }]}>
          Text size
        </Text>
        <View style={styles.stepper}>
          <DiffIconButton
            icon="remove"
            label="Smaller code text"
            chrome={chrome}
            disabled={fontSize <= 10}
            onPress={() => onFontSizeChange(fontSize - 2)}
          />
          <Text style={[styles.stepperValue, { color: chrome.text }]}>
            {fontSize}
          </Text>
          <DiffIconButton
            icon="add"
            label="Larger code text"
            chrome={chrome}
            disabled={fontSize >= 20}
            onPress={() => onFontSizeChange(fontSize + 2)}
          />
        </View>
      </View>
      <View style={styles.option}>
        <Text style={[styles.optionLabel, { color: chrome.text }]}>
          Patch headers
        </Text>
        <Switch
          accessibilityLabel="Patch headers"
          value={showHeaders}
          onValueChange={onShowHeadersChange}
        />
      </View>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Open working file"
        accessibilityState={{ disabled: deleted }}
        disabled={deleted}
        onPress={onOpenFile}
        style={styles.option}
      >
        <Text
          style={[
            styles.optionLabel,
            { color: deleted ? chrome.textSubtle : chrome.text },
          ]}
        >
          Open working file
        </Text>
        <Ionicons
          name="document-text-outline"
          size={20}
          color={deleted ? chrome.textSubtle : chrome.text}
        />
      </Pressable>
    </View>
  );
}

function toneColor(
  tone: GitDiffStatusTone,
  theme: TerminalThemePalette,
  chrome: TerminalThemeChrome,
): string {
  switch (tone) {
    case "added":
      return theme.green;
    case "deleted":
    case "conflict":
      return theme.red;
    case "renamed":
      return theme.blue;
    case "modified":
      return theme.yellow;
    case "untracked":
      return chrome.textMuted;
    case "binary":
      return chrome.textSubtle;
  }
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  rootStack: { flexDirection: "column" },
  rootWide: { flexDirection: "row" },
  listPane: { flex: 1, minWidth: 0 },
  listPaneStack: { flex: 1 },
  listPaneWide: { flex: 0, width: 320, borderRightWidth: StyleSheet.hairlineWidth },
  detailPane: {
    flex: 1,
    minWidth: 0,
    borderLeftWidth: StyleSheet.hairlineWidth,
  },
  overlay: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
    borderWidth: 0,
  },
  list: { flex: 1 },
  scopeBar: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    paddingHorizontal: 8,
    paddingVertical: 6,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  scope: {
    flex: 1,
    minHeight: 36,
    borderRadius: 9,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
  },
  scopeLabel: {
    fontSize: 12,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
  scopeCount: {
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.terminalFont,
  },
  search: {
    minHeight: 48,
    paddingHorizontal: 12,
    fontSize: 14,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  listMeta: {
    minHeight: 28,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 8,
    paddingHorizontal: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  listMetaText: { fontSize: 11 },
  file: {
    minHeight: 56,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 12,
    paddingVertical: 9,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  fileCopy: { flex: 1, minWidth: 0 },
  fileName: { fontSize: 13, lineHeight: 18, fontFamily: Typography.uiFontMedium },
  fileDetail: {
    marginTop: 2,
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.terminalFont,
  },
  stats: { alignItems: "flex-end", minWidth: 46 },
  count: { fontSize: 12, lineHeight: 16, fontFamily: Typography.terminalFont },
  binary: { fontSize: 11, lineHeight: 16, fontFamily: Typography.uiFont },
  empty: { padding: 24, textAlign: "center", fontSize: 14 },
  emptyDetail: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
    padding: 24,
  },
  emptyDetailText: { fontSize: 13, textAlign: "center" },
  options: { borderBottomWidth: StyleSheet.hairlineWidth, paddingHorizontal: 12 },
  option: {
    minHeight: 48,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderColor: "transparent",
  },
  optionLabel: { fontSize: 13, fontFamily: Typography.uiFont },
  stepper: { flexDirection: "row", alignItems: "center" },
  stepperValue: { minWidth: 26, textAlign: "center", fontSize: 13 },
});
