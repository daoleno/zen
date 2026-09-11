import React from "react";
import { StyleSheet, View } from "react-native";
import {
  buildTerminalChrome,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { GitRepoFileContentPayload } from "../../services/gitDiff";
import { GitDiffCodeSnapshotPanel } from "./GitDiffCodeView";
import { GitDiffStateCard } from "./GitDiffStateCard";

interface GitDiffRepoFileViewProps {
  path: string;
  payload?: GitRepoFileContentPayload;
  loading: boolean;
  error: string | null;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
  bottomInset: number;
}

/**
 * Working-tree file content only. The single sheet header owns the file title
 * and Back, so this view deliberately renders no second title bar.
 */
export function GitDiffRepoFileView({
  path,
  payload,
  loading,
  error,
  theme,
  chrome,
  bottomInset,
}: GitDiffRepoFileViewProps) {
  return (
    <View style={styles.repoFileRoot}>
      {loading ? (
        <View style={[styles.contentPad, { paddingBottom: bottomInset + 14 }]}>
          <GitDiffStateCard
            icon="sync-outline"
            title="Loading file"
            accent={theme.cursor}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
            busy
          />
        </View>
      ) : error ? (
        <View style={[styles.contentPad, { paddingBottom: bottomInset + 14 }]}>
          <GitDiffStateCard
            icon="warning-outline"
            title="Could not load file"
            detail={error}
            accent={theme.red}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
          />
        </View>
      ) : (
        <GitDiffCodeSnapshotPanel
          path={path}
          snapshot={payload?.snapshot ?? null}
          chrome={chrome}
          theme={theme}
          bottomInset={bottomInset}
        />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  contentPad: {
    flex: 1,
    paddingHorizontal: 14,
    paddingVertical: 14,
  },
  repoFileRoot: {
    flex: 1,
  },
});
