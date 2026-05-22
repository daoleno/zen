import React from "react";
import {
  FlatList,
  StyleSheet,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type {
  GitDiffFileInfo,
  GitDiffPatchPayload,
} from "../../services/gitDiff";
import { GitDiffFileCard } from "./GitDiffFileCard";
import { GitDiffStateCard } from "./GitDiffStateCard";

interface GitDiffSheetDiffContentProps {
  files: GitDiffFileInfo[];
  clean: boolean;
  collapsedDiffPaths: ReadonlySet<string>;
  patchByPath: Record<string, GitDiffPatchPayload | undefined>;
  patchLoadingByPath: Record<string, boolean>;
  patchErrorByPath: Record<string, string | undefined>;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  onLoadDiffPatch(path: string): void;
  onToggleDiffFile(path: string): void;
  onOpenFile(path: string): void;
}

export function GitDiffSheetDiffContent({
  files,
  clean,
  collapsedDiffPaths,
  patchByPath,
  patchLoadingByPath,
  patchErrorByPath,
  theme,
  chrome,
  onLoadDiffPatch,
  onToggleDiffFile,
  onOpenFile,
}: GitDiffSheetDiffContentProps) {
  const renderDiffFile = React.useCallback(
    ({ item }: { item: GitDiffFileInfo }) => (
      <GitDiffFileCard
        file={item}
        patch={patchByPath[item.path]}
        loading={Boolean(patchLoadingByPath[item.path])}
        error={patchErrorByPath[item.path] ?? null}
        expanded={!collapsedDiffPaths.has(item.path)}
        theme={theme}
        chrome={chrome}
        onLoadPatch={() => onLoadDiffPatch(item.path)}
        onToggle={() => onToggleDiffFile(item.path)}
        onOpenFile={() => onOpenFile(item.path)}
      />
    ),
    [
      chrome,
      collapsedDiffPaths,
      onLoadDiffPatch,
      onOpenFile,
      onToggleDiffFile,
      patchByPath,
      patchErrorByPath,
      patchLoadingByPath,
      theme,
    ],
  );

  if (clean) {
    return (
      <View style={styles.contentPad}>
        <GitDiffStateCard
          icon="checkmark-done-outline"
          title="Working tree is clean"
          detail="No staged, unstaged, or untracked changes were found."
          accent={theme.green}
          chromeText={chrome.text}
          chromeMuted={chrome.textMuted}
        />
      </View>
    );
  }

  return (
    <FlatList
      key="git-diff-list"
      data={files}
      keyExtractor={(item) => item.path}
      renderItem={renderDiffFile}
      style={styles.fullList}
      contentContainerStyle={styles.diffContent}
      showsVerticalScrollIndicator={false}
      removeClippedSubviews={false}
      initialNumToRender={4}
      maxToRenderPerBatch={4}
      windowSize={5}
    />
  );
}

const styles = StyleSheet.create({
  contentPad: {
    flex: 1,
    paddingHorizontal: 14,
    paddingVertical: 14,
  },
  fullList: {
    flex: 1,
  },
  diffContent: {
    paddingHorizontal: 8,
    paddingTop: 8,
    paddingBottom: 18,
    gap: 8,
  },
});
