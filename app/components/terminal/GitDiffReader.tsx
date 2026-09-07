import React, { useEffect, useRef, useState } from "react";
import {
  ActivityIndicator,
  FlatList,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
  useWindowDimensions,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type {
  GitDiffPage,
  GitDiffPageRequest,
  GitDiffRow,
  GitDiffScope,
} from "../../services/gitDiff";
import { DiffIconButton } from "./GitDiffReviewControls";
import { withAlpha } from "./colorWithAlpha";

export interface GitDiffPosition {
  row: number;
  offset: number;
  version?: string;
}

export function GitDiffReader({
  path,
  scope,
  loadPage,
  chrome,
  theme,
  position,
  onPosition,
  wrap,
  fontSize,
  refreshKey,
  showSearch,
}: {
  path: string;
  scope: GitDiffScope;
  loadPage(request: GitDiffPageRequest): Promise<GitDiffPage>;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  position: GitDiffPosition;
  onPosition(position: GitDiffPosition): void;
  wrap: boolean;
  fontSize: number;
  refreshKey: number;
  showSearch: boolean;
}) {
  const [location, setLocation] = useState(position);
  const [page, setPage] = useState<GitDiffPage | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  useEffect(() => {
    if (!showSearch) setSearch("");
  }, [showSearch]);
  const [retry, setRetry] = useState(0);
  const [showHeaders, setShowHeaders] = useState(false);
  const { width, fontScale } = useWindowDimensions();
  const list = useRef<FlatList<GitDiffRow>>(null);
  const currentPosition = useRef(position);
  const onPositionRef = useRef(onPosition);
  onPositionRef.current = onPosition;
  useEffect(() => () => onPositionRef.current(currentPosition.current), []);
  const previousRefresh = useRef(refreshKey);
  useEffect(() => {
    if (previousRefresh.current === refreshKey) return;
    previousRefresh.current = refreshKey;
    currentPosition.current = { row: 0, offset: 0 };
    setLocation(currentPosition.current);
  }, [refreshKey]);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError(null);
    setPage(null);
    loadPage({
      path,
      scope,
      row: location.row,
      version: location.version,
      query: search,
    })
      .then((result) => {
        if (!active) return;
        setPage(result);
        if (!result.stale)
          currentPosition.current = { ...location, version: result.version };
      })
      .catch((reason: Error) => {
        if (active) setError(reason.message);
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [path, scope, loadPage, location, search, retry]);

  const move = (row: number) => {
    if (row < 0 || !page) return;
    currentPosition.current = { row, offset: 0, version: page.version };
    setLocation(currentPosition.current);
  };
  const refresh = () => {
    currentPosition.current = { row: 0, offset: 0 };
    setLocation(currentPosition.current);
  };
  const unavailable = loading || !page || page.stale;
  const renderRow = ({ item }: { item: GitDiffRow }) =>
    !showHeaders &&
    !search &&
    item.kind === "meta" &&
    /^(diff --git |index |--- |\+\+\+ )/.test(item.text) ? null : (
      <DiffRow
        row={item}
        theme={theme}
        chrome={chrome}
        wrap={wrap}
        fontSize={fontSize}
        query={search}
      />
    );
  const rows =
    page && !page.stale ? (
      <FlatList
        key={`${page.start}:${wrap}:${fontSize}`}
        ref={list}
        data={page.rows}
        renderItem={renderRow}
        keyExtractor={(_, index) => String(page.start + index)}
        style={styles.root}
        initialNumToRender={12}
        maxToRenderPerBatch={8}
        windowSize={3}
        contentOffset={{ x: 0, y: currentPosition.current.offset }}
        onScroll={(event) => {
          if (currentPosition.current.row === page.start)
            currentPosition.current.offset = event.nativeEvent.contentOffset.y;
        }}
        scrollEventThrottle={100}
      />
    ) : null;
  const horizontalWidth = Math.max(
    width,
    ...(page?.rows ?? []).map(
      (row) => row.text.length * fontSize * fontScale + 120 * fontScale,
    ),
  );
  return (
    <View style={styles.root}>
      {showSearch ? (
        <View style={[styles.search, { borderColor: chrome.border }]}>
          <TextInput
            accessibilityLabel="Search this diff"
            placeholder="Find in diff"
            placeholderTextColor={chrome.textSubtle}
            value={query}
            onChangeText={setQuery}
            onSubmitEditing={() => setSearch(query)}
            returnKeyType="search"
            autoCapitalize="none"
            autoCorrect={false}
            style={[styles.input, { color: chrome.text }]}
          />
          <DiffIconButton
            icon="search"
            label="Find in diff"
            chrome={chrome}
            onPress={() => setSearch(query)}
          />
          <DiffIconButton
            icon="code-slash-outline"
            label="Patch headers"
            chrome={chrome}
            selected={showHeaders}
            onPress={() => setShowHeaders((value) => !value)}
          />
          <DiffIconButton
            icon="chevron-up"
            label="Previous matching line"
            chrome={chrome}
            disabled={unavailable || page.previous_match < 0}
            onPress={() => move(page!.previous_match)}
          />
          <DiffIconButton
            icon="chevron-down"
            label="Next matching line"
            chrome={chrome}
            disabled={unavailable || page.next_match < 0}
            onPress={() => move(page!.next_match)}
          />
        </View>
      ) : null}
      {search && page && !loading ? (
        <Text style={[styles.meta, { color: chrome.textMuted }]}>
          {page.matches} matching lines
        </Text>
      ) : null}
      {loading ? (
        <View style={styles.state}>
          <ActivityIndicator color={chrome.accent} />
          <Text style={{ color: chrome.textMuted }}>Loading diff</Text>
        </View>
      ) : error ? (
        <View style={styles.state}>
          <Text style={{ color: theme.red }}>{error}</Text>
          <DiffIconButton
            icon="refresh"
            label="Retry diff"
            chrome={chrome}
            onPress={() => setRetry((value) => value + 1)}
          />
        </View>
      ) : page?.stale ? (
        <View style={styles.state}>
          <Text style={{ color: theme.yellow }}>
            This file changed. Refresh to review the current diff.
          </Text>
          <DiffIconButton
            icon="refresh"
            label="Refresh changed diff"
            chrome={chrome}
            onPress={refresh}
          />
        </View>
      ) : !page?.rows.length ? (
        <View style={styles.state}>
          <Text style={{ color: chrome.textMuted }}>
            No changes in this comparison.
          </Text>
        </View>
      ) : wrap ? (
        rows
      ) : (
        <ScrollView
          horizontal
          style={styles.root}
          contentContainerStyle={{ height: "100%" }}
        >
          <View style={{ width: horizontalWidth }}>{rows}</View>
        </ScrollView>
      )}
      <View style={[styles.footer, { borderColor: chrome.border }]}>
        <DiffIconButton
          icon="play-back-outline"
          label="Previous hunk"
          chrome={chrome}
          disabled={unavailable || page.previous_hunk < 0}
          onPress={() => move(page!.previous_hunk)}
        />
        <DiffIconButton
          icon="chevron-back"
          label="Previous page"
          chrome={chrome}
          disabled={unavailable || page.start === 0}
          onPress={() => move(Math.max(0, page!.start - 120))}
        />
        <Text style={[styles.page, { color: chrome.textMuted }]}>
          {page && !page.stale
            ? `${page.total ? page.start + 1 : 0}-${Math.min(page.start + page.rows.length, page.total)} / ${page.total}`
            : ""}
        </Text>
        <DiffIconButton
          icon="chevron-forward"
          label="Next page"
          chrome={chrome}
          disabled={unavailable || page.start + page.rows.length >= page.total}
          onPress={() => move(page!.start + page!.rows.length)}
        />
        <DiffIconButton
          icon="play-forward-outline"
          label="Next hunk"
          chrome={chrome}
          disabled={unavailable || page.next_hunk < 0}
          onPress={() => move(page!.next_hunk)}
        />
      </View>
    </View>
  );
}

const DiffRow = React.memo(function DiffRow({
  row,
  chrome,
  theme,
  wrap,
  fontSize,
  query,
}: {
  row: GitDiffRow;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  wrap: boolean;
  fontSize: number;
  query: string;
}) {
  const { fontScale } = useWindowDimensions();
  const color =
    row.kind === "add"
      ? theme.green
      : row.kind === "delete"
        ? theme.red
        : row.kind === "hunk"
          ? theme.cyan
          : chrome.text;
  const backgroundColor =
    row.kind === "add" || row.kind === "delete" || row.kind === "hunk"
      ? withAlpha(color, 0.09)
      : "transparent";
  const code = ["add", "delete", "context", "marker"].includes(row.kind);
  const text = (
    <Text
      selectable
      style={{
        color,
        fontFamily: Typography.terminalFont,
        fontSize,
        lineHeight: fontSize * 1.5,
        ...(wrap ? { flex: 1 } : {}),
      }}
    >
      {row.kind === "scope"
        ? `${row.text === "staged" ? "Staged" : row.text === "untracked" ? "Untracked" : "Working tree"} snapshot`
        : highlightMatch(row.text, query, theme, chrome)}
    </Text>
  );
  return (
    <View style={[styles.row, { backgroundColor }]}>
      {code ? (
        <Text
          accessibilityLabel={
            row.continuation
              ? "Line continuation"
              : `Old line ${row.old ?? ""}, new line ${row.new ?? ""}`
          }
          style={[
            styles.gutter,
            {
              width: (Math.max(10, fontSize - 2) * 6.8 + 12) * fontScale,
              color: chrome.textSubtle,
              fontSize: Math.max(10, fontSize - 2),
              lineHeight: fontSize * 1.5,
            },
          ]}
        >
          {row.continuation
            ? "..."
            : `${String(row.old ?? "").padStart(5)} ${String(row.new ?? "").padStart(5)}`}
        </Text>
      ) : null}
      {text}
    </View>
  );
});

function highlightMatch(
  text: string,
  query: string,
  theme: TerminalThemePalette,
  chrome: TerminalThemeChrome,
) {
  if (!query) return text || " ";
  const index = text.toLowerCase().indexOf(query.toLowerCase());
  if (index < 0) return text || " ";
  return (
    <>
      {text.slice(0, index)}
      <Text
        style={{ backgroundColor: theme.yellow, color: chrome.appBackground }}
      >
        {text.slice(index, index + query.length)}
      </Text>
      {text.slice(index + query.length)}
    </>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  search: {
    flexDirection: "row",
    alignItems: "center",
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  input: {
    flex: 1,
    minWidth: 0,
    minHeight: 44,
    paddingHorizontal: 12,
    fontSize: 14,
  },
  state: {
    flex: 1,
    padding: 20,
    gap: 12,
    alignItems: "center",
    justifyContent: "center",
  },
  meta: { fontSize: 12, paddingHorizontal: 12, paddingVertical: 4 },
  footer: {
    flexDirection: "row",
    alignItems: "center",
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  page: { flex: 1, textAlign: "center", fontSize: 11 },
  row: {
    flexDirection: "row",
    alignItems: "flex-start",
    paddingHorizontal: 8,
    paddingVertical: 2,
  },
  gutter: {
    width: 82,
    textAlign: "right",
    paddingRight: 8,
    fontFamily: Typography.terminalFont,
  },
});
