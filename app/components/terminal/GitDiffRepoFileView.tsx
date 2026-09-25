import React from "react";
import { StyleSheet, View } from "react-native";
import {
  buildTerminalChrome,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { GitRepoFileContentPayload } from "../../services/gitDiff";
import { EmptyState } from "../ui/EmptyState";
import { InlineNotice } from "../ui/InlineNotice";
import { GitDiffCodeSnapshotPanel } from "./GitDiffCodeView";

interface GitDiffRepoFileViewProps {
  path: string;
  payload?: GitRepoFileContentPayload;
  loading: boolean;
  error: string | null;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
  bottomInset: number;
  onRetry?(): void;
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
  onRetry,
}: GitDiffRepoFileViewProps) {
  return (
    <View style={styles.repoFileRoot}>
      {loading ? (
        <View style={[styles.contentPad, { paddingBottom: bottomInset + 14 }]}>
          <EmptyState size="inline" busy title="Loading file" />
        </View>
      ) : error ? (
        <View style={[styles.contentPad, { paddingBottom: bottomInset + 14 }]}>
          <InlineNotice
            tone="danger"
            title="Couldn't load file"
            detail={error}
            action={onRetry ? { label: "Retry", onPress: onRetry } : undefined}
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
    paddingHorizontal: 16,
    paddingVertical: 16,
  },
  repoFileRoot: {
    flex: 1,
  },
});
