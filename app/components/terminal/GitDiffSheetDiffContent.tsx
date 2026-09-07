import React, {
  useDeferredValue,
  useLayoutEffect,
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
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import {
  describeGitDiffScope,
  filterGitDiffFiles,
  gitDiffCounts,
  type GitDiffFileInfo,
  type GitDiffPage,
  type GitDiffPageRequest,
  type GitDiffScope,
} from "../../services/gitDiff";
import { GitDiffReader, type GitDiffPosition } from "./GitDiffReader";
import { DiffIconButton } from "./GitDiffReviewControls";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import { Ionicons } from "@expo/vector-icons";

interface GitDiffSheetDiffContentProps {
  files: GitDiffFileInfo[];
  clean: boolean;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  loadPage(request: GitDiffPageRequest): Promise<GitDiffPage>;
  refreshKey: number;
  onOpenFile(path: string): void;
  onReviewChange?(active: boolean): void;
}

export function GitDiffSheetDiffContent({
  files,
  clean,
  theme,
  chrome,
  loadPage,
  refreshKey,
  onOpenFile,
  onReviewChange,
}: GitDiffSheetDiffContentProps) {
  const [scope, setScope] = useState<GitDiffScope>("all");
  const [query, setQuery] = useState("");
  const deferredQuery = useDeferredValue(query);
  const [selected, setSelected] = useState<string | null>(null);
  const [wrap, setWrap] = useState(true);
  const [fontSize, setFontSize] = useState(12);
  const [showSearch, setShowSearch] = useState(false);
  const [optionsOpen, setOptionsOpen] = useState(false);
  const [showHeaders, setShowHeaders] = useState(false);
  const positions = useRef(new Map<string, GitDiffPosition>());
  const overviewOffset = useRef(0);
  const filtered = useMemo(
    () => filterGitDiffFiles(files, scope, deferredQuery),
    [files, scope, deferredQuery],
  );
  const index = filtered.findIndex((file) => file.path === selected);
  const file = index >= 0 ? filtered[index] : null;
  const reviewing = Boolean(file);
  useLayoutEffect(() => {
    onReviewChange?.(reviewing);
  }, [reviewing, onReviewChange]);
  const positionKey = JSON.stringify([selected, scope]);
  const renderFile = ({ item }: { item: GitDiffFileInfo }) => (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${item.path}, ${item.status}, ${describeGitDiffScope(item)}, plus ${item.additions ?? 0}, minus ${item.deletions ?? 0}`}
      onPress={() => setSelected(item.path)}
      style={({ pressed }) => [
        styles.file,
        {
          borderColor: chrome.border,
          backgroundColor: pressed ? chrome.surfaceMuted : "transparent",
        },
      ]}
    >
      <View style={styles.fileCopy}>
        <Text style={[styles.path, { color: chrome.text }]} numberOfLines={2}>
          {item.path}
        </Text>
        <Text
          style={[styles.meta, { color: chrome.textMuted }]}
          numberOfLines={2}
        >
          {item.status} · {describeGitDiffScope(item)}
          {item.old_path ? ` · from ${item.old_path}` : ""}
        </Text>
      </View>
      <View style={styles.stats}>
        {item.binary ? (
          <Text style={[styles.meta, { color: chrome.textMuted }]}>Binary</Text>
        ) : (
          <>
            <Text style={[styles.count, { color: theme.green }]}>
              +{gitDiffCounts(item, scope)[0]}
            </Text>
            <Text style={[styles.count, { color: theme.red }]}>
              -{gitDiffCounts(item, scope)[1]}
            </Text>
          </>
        )}
      </View>
    </Pressable>
  );

  return (
    <View style={styles.root}>
      <View
        accessibilityRole="tablist"
        style={[styles.scopes, { borderColor: chrome.border }]}
      >
        {(
          [
            ["all", "All"],
            ["working", "Working"],
            ["staged", "Staged"],
          ] as const
        ).map(([value, label]) => (
          <Pressable
            key={value}
            accessibilityRole="tab"
            accessibilityState={{ selected: scope === value }}
            onPress={() => {
              setScope(value);
              overviewOffset.current = 0;
            }}
            style={[
              styles.scope,
              {
                borderBottomColor:
                  scope === value ? chrome.accent : "transparent",
              },
            ]}
          >
            <Text
              style={{
                color: scope === value ? chrome.text : chrome.textMuted,
                fontSize: 13,
              }}
            >
              {label}
            </Text>
          </Pressable>
        ))}
      </View>
      {file ? (
        <>
          <View style={[styles.fileHeader, { borderColor: chrome.border }]}>
            <DiffIconButton
              icon="arrow-back"
              label="Changed files"
              chrome={chrome}
              onPress={() => setSelected(null)}
            />
            <View style={styles.fileCopy}>
              <Text selectable style={[styles.path, { color: chrome.text }]}>
                {file.path}
              </Text>
              <Text style={[styles.meta, { color: chrome.textMuted }]}>
                {file.status}
                {file.old_path ? ` · from ${file.old_path}` : ""}
              </Text>
            </View>
            <DiffIconButton
              icon="search"
              label="Search diff"
              chrome={chrome}
              selected={showSearch}
              onPress={() => setShowSearch((value) => !value)}
            />
            <DiffIconButton icon="ellipsis-horizontal" label="Diff options" chrome={chrome} onPress={() => setOptionsOpen(true)} />
          </View>
          <GitDiffReader
            key={`${positionKey}:${refreshKey}`}
            path={file.path}
            scope={scope}
            theme={theme}
            chrome={chrome}
            loadPage={loadPage}
            refreshKey={refreshKey}
            position={
              positions.current.get(positionKey) ?? { offset: 0 }
            }
            onPosition={(position) =>
              positions.current.set(positionKey, position)
            }
            wrap={wrap}
            fontSize={fontSize}
            showSearch={showSearch}
            showHeaders={showHeaders}
          />
          <BottomSheetFrame visible={optionsOpen} onClose={() => setOptionsOpen(false)} maxHeight="75%" cardStyle={{ backgroundColor: chrome.surface }}>
            <View style={styles.option}>
              <Text style={{ color: chrome.text }}>Wrap lines</Text>
              <Switch accessibilityLabel="Wrap lines" value={wrap} onValueChange={setWrap} />
            </View>
            <View style={styles.option}>
              <Text style={{ color: chrome.text }}>Text size</Text>
              <View style={styles.stepper}>
                <DiffIconButton icon="remove" label="Smaller code text" chrome={chrome} disabled={fontSize <= 10} onPress={() => setFontSize(value => value - 2)} />
                <Text style={{ color: chrome.text }}>{fontSize}</Text>
                <DiffIconButton icon="add" label="Larger code text" chrome={chrome} disabled={fontSize >= 20} onPress={() => setFontSize(value => value + 2)} />
              </View>
            </View>
            <View style={styles.option}>
              <Text style={{ color: chrome.text }}>Patch headers</Text>
              <Switch accessibilityLabel="Patch headers" value={showHeaders} onValueChange={setShowHeaders} />
            </View>
            <Pressable accessibilityRole="button" accessibilityLabel="Open working file" disabled={file.status === "deleted"} onPress={() => { setOptionsOpen(false); onOpenFile(file.path); }} style={styles.option}>
              <Text style={{ color: file.status === "deleted" ? chrome.textSubtle : chrome.text }}>Open working file</Text>
              <Ionicons name="document-text-outline" size={20} color={file.status === "deleted" ? chrome.textSubtle : chrome.text} />
            </Pressable>
          </BottomSheetFrame>
        </>
      ) : (
        <>
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
            value={query}
            onChangeText={(value) => {
              setQuery(value);
              overviewOffset.current = 0;
            }}
            clearButtonMode="while-editing"
          />
          <Text style={[styles.overviewMeta, { color: chrome.textMuted }]}>
            {filtered.length} / {files.length} files
          </Text>
          <FlatList
            data={filtered}
            keyExtractor={(item) => item.path}
            renderItem={renderFile}
            style={styles.root}
            initialNumToRender={12}
            maxToRenderPerBatch={12}
            windowSize={5}
            keyboardShouldPersistTaps="handled"
            contentOffset={{ x: 0, y: overviewOffset.current }}
            onScroll={(event) => {
              overviewOffset.current = event.nativeEvent.contentOffset.y;
            }}
            scrollEventThrottle={100}
            ListEmptyComponent={
              <Text style={[styles.empty, { color: chrome.textMuted }]}>
                {clean ? "Working tree is clean" : "No matching changes"}
              </Text>
            }
          />
        </>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  scopes: { flexDirection: "row", borderBottomWidth: StyleSheet.hairlineWidth },
  scope: {
    flex: 1,
    minHeight: 44,
    alignItems: "center",
    justifyContent: "center",
    borderBottomWidth: 2,
  },
  search: {
    minHeight: 48,
    paddingHorizontal: 12,
    fontSize: 14,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  file: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    padding: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  fileCopy: { flex: 1, minWidth: 0 },
  path: { fontSize: 13, fontFamily: Typography.terminalFont },
  meta: { fontSize: 11, marginTop: 3 },
  stats: { alignItems: "flex-end", minWidth: 48 },
  count: { fontSize: 12, fontFamily: Typography.terminalFont },
  fileHeader: {
    flexDirection: "row",
    alignItems: "center",
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  option: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", minHeight: 52, gap: 12 },
  stepper: { flexDirection: "row", alignItems: "center" },
  overviewMeta: { paddingHorizontal: 12, paddingVertical: 6, fontSize: 11 },
  empty: { padding: 24, textAlign: "center", fontSize: 14 },
});
