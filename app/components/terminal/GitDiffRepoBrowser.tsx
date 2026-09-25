import React from "react";
import {
  FlatList,
  Pressable,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import {
  ContinuousCorners,
  Radii,
  TouchTarget,
  TypeScale,
  UiTextMetrics,
} from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type {
  GitRepoBrowserEntry,
  GitRepoFileContentPayload,
} from "../../services/gitDiff";
import { EmptyState } from "../ui/EmptyState";
import { InlineNotice } from "../ui/InlineNotice";
import { GitDiffRepoFileView } from "./GitDiffRepoFileView";
import { GitDiffStateCard } from "./GitDiffStateCard";
import { withAlpha } from "./colorWithAlpha";

interface GitDiffRepoBrowserProps {
  repoTitle: string;
  repoBrowserPath: string;
  repoBrowserEntries: GitRepoBrowserEntry[];
  repoBrowserLoading: boolean;
  repoBrowserError: string | null;
  repoFilePath: string | null;
  repoFileContent?: GitRepoFileContentPayload;
  repoFileLoading: boolean;
  repoFileError: string | null;
  changedPathSet: Set<string>;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  bottomInset: number;
  onOpenRepoPath(path: string): void;
  onOpenRepoFile(path: string): void;
  onLongPressEntry?(entry: GitRepoBrowserEntry): void;
}

/**
 * Working-tree browser as one inset grouped list. The sheet header is the
 * only Back: it climbs one folder at a time, so this view draws no folder-up
 * control of its own.
 */
export function GitDiffRepoBrowser({
  repoBrowserPath,
  repoBrowserEntries,
  repoBrowserLoading,
  repoBrowserError,
  repoFilePath,
  repoFileContent,
  repoFileLoading,
  repoFileError,
  changedPathSet,
  theme,
  chrome,
  bottomInset,
  onOpenRepoPath,
  onOpenRepoFile,
  onLongPressEntry,
}: GitDiffRepoBrowserProps) {
  const count = repoBrowserEntries.length;
  const renderRepoEntry = React.useCallback(
    ({ item, index }: { item: GitRepoBrowserEntry; index: number }) => (
      <RepoEntryRow
        entry={item}
        first={index === 0}
        last={index === count - 1}
        changed={changedPathSet.has(item.path)}
        theme={theme}
        chrome={chrome}
        onPress={() => {
          if (item.kind === "directory") {
            onOpenRepoPath(item.path);
            return;
          }
          onOpenRepoFile(item.path);
        }}
        onLongPress={onLongPressEntry ? () => onLongPressEntry(item) : undefined}
      />
    ),
    [changedPathSet, chrome, count, onLongPressEntry, onOpenRepoFile, onOpenRepoPath, theme],
  );

  if (repoFilePath) {
    return (
      <GitDiffRepoFileView
        key={`repo-file:${repoFilePath}`}
        path={repoFilePath}
        payload={repoFileContent}
        loading={repoFileLoading}
        error={repoFileError}
        theme={theme}
        chrome={chrome}
        bottomInset={bottomInset}
        onRetry={() => onOpenRepoFile(repoFilePath)}
      />
    );
  }

  return (
    <FlatList
      key={`repo-browser-list:${repoBrowserPath || "root"}`}
      data={repoBrowserEntries}
      keyExtractor={(item) => `${item.kind}:${item.path}`}
      renderItem={renderRepoEntry}
      style={styles.fullList}
      contentContainerStyle={[
        styles.browserContent,
        { paddingBottom: bottomInset + 20 },
        count === 0 ? styles.fullListEmpty : null,
      ]}
      ListHeaderComponent={
        <View style={styles.header}>
          {repoBrowserError ? (
            <InlineNotice
              tone="danger"
              title="Couldn't load folder"
              detail={repoBrowserError}
              action={{
                label: "Retry",
                onPress: () => onOpenRepoPath(repoBrowserPath),
                disabled: repoBrowserLoading,
              }}
              style={styles.notice}
            />
          ) : null}
        </View>
      }
      ListEmptyComponent={
        repoBrowserLoading ? (
          <EmptyState size="inline" busy title="Loading folder" />
        ) : repoBrowserError ? null : (
          <GitDiffStateCard
            icon="folder-open-outline"
            title="Empty folder"
            accent={chrome.textSubtle}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
          />
        )
      }
      keyboardShouldPersistTaps="handled"
      showsVerticalScrollIndicator={false}
      nestedScrollEnabled={false}
    />
  );
}

function RepoEntryRow({
  entry,
  first,
  last,
  changed,
  theme,
  chrome,
  onPress,
  onLongPress,
}: {
  entry: GitRepoBrowserEntry;
  first: boolean;
  last: boolean;
  changed: boolean;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  onPress(): void;
  onLongPress?(): void;
}) {
  const isDirectory = entry.kind === "directory";
  const tint = isDirectory ? chrome.accent : chrome.textMuted;

  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${entry.name}, ${isDirectory ? "folder" : "file"}${changed ? ", changed" : ""}`}
      accessibilityHint={isDirectory ? "Opens folder" : "Opens file"}
      onPress={() => {
        void Haptics.selectionAsync();
        onPress();
      }}
      onLongPress={onLongPress}
      style={({ pressed }) => [
        styles.row,
        {
          backgroundColor: pressed ? chrome.surfaceActive : chrome.surface,
          borderTopLeftRadius: first ? Radii.card : 0,
          borderTopRightRadius: first ? Radii.card : 0,
          borderBottomLeftRadius: last ? Radii.card : 0,
          borderBottomRightRadius: last ? Radii.card : 0,
        },
      ]}
    >
      <View style={[styles.tile, { backgroundColor: withAlpha(tint, 0.12) }]}>
        <Ionicons
          name={isDirectory ? "folder" : "document-text-outline"}
          size={16}
          color={tint}
        />
      </View>
      <View style={[styles.rowBody, !last && { borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: chrome.border }]}>
        <Text
          style={[styles.name, { color: chrome.text }]}
          numberOfLines={1}
          ellipsizeMode="middle"
        >
          {entry.name}
        </Text>
        {changed ? (
          <View
            style={[styles.changedDot, { backgroundColor: theme.yellow }]}
            accessible={false}
          />
        ) : null}
        {isDirectory ? (
          <Ionicons name="chevron-forward" size={16} color={chrome.textSubtle} />
        ) : null}
      </View>
    </Pressable>
  );
}

const TILE = 28;

const styles = StyleSheet.create({
  fullList: {
    flex: 1,
  },
  fullListEmpty: {
    flexGrow: 1,
  },
  browserContent: {
    paddingHorizontal: 16,
    paddingTop: 8,
  },
  header: {
    gap: 8,
    paddingBottom: 8,
  },
  notice: {
    marginTop: 4,
  },
  row: {
    ...ContinuousCorners,
    minHeight: Math.max(TouchTarget, 48),
    flexDirection: "row",
    alignItems: "center",
    paddingLeft: 14,
    gap: 12,
    overflow: "hidden",
  },
  tile: {
    width: TILE,
    height: TILE,
    borderRadius: 8,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
  },
  rowBody: {
    flex: 1,
    minWidth: 0,
    alignSelf: "stretch",
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingRight: 14,
  },
  name: {
    ...TypeScale.compact,
    ...UiTextMetrics,
    flex: 1,
    minWidth: 0,
  },
  changedDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
});
