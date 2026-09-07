import React from "react";
import { Modal, StatusBar, StyleSheet, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import {
  buildTerminalChrome,
  isLightTerminalTheme,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import type {
  GitDiffPage,
  GitDiffPageRequest,
  GitDiffStatusSnapshot,
  GitRepoBrowserEntry,
  GitRepoFileContentPayload,
} from "../../services/gitDiff";
import { GitDiffRepoBrowser } from "./GitDiffRepoBrowser";
import { GitDiffSheetDiffContent } from "./GitDiffSheetDiffContent";
import { GitDiffStateCard } from "./GitDiffStateCard";
import {
  GitDiffSheetTopChrome,
  type GitDiffSheetTab,
} from "./GitDiffSheetTopChrome";

interface GitDiffSheetProps {
  visible: boolean;
  theme: TerminalThemePalette;
  snapshot: GitDiffStatusSnapshot | null;
  loading: boolean;
  error: string | null;
  loadPage(request: GitDiffPageRequest): Promise<GitDiffPage>;
  refreshKey: number;
  ownerKey: string;
  repoBrowserPath: string;
  repoBrowserEntries: GitRepoBrowserEntry[];
  repoBrowserLoading: boolean;
  repoBrowserError: string | null;
  repoFilePath: string | null;
  repoFileLoadingPath: string | null;
  repoFileError: string | null;
  repoFileByPath: Record<string, GitRepoFileContentPayload | undefined>;
  onClose(): void;
  onRefresh(): void;
  onOpenRepoPath(path: string): void;
  onOpenRepoFile(path: string): void;
  onCloseRepoFile(): void;
  onBackRepoPath(): void;
}

export function GitDiffSheet({
  visible,
  theme,
  snapshot,
  loading,
  error,
  loadPage,
  refreshKey,
  ownerKey,
  repoBrowserPath,
  repoBrowserEntries,
  repoBrowserLoading,
  repoBrowserError,
  repoFilePath,
  repoFileLoadingPath,
  repoFileError,
  repoFileByPath,
  onClose,
  onRefresh,
  onOpenRepoPath,
  onOpenRepoFile,
  onCloseRepoFile,
  onBackRepoPath,
}: GitDiffSheetProps) {
  const chrome = React.useMemo(() => buildTerminalChrome(theme), [theme]);
  const [activeTab, setActiveTab] = React.useState<GitDiffSheetTab>("diff");
  const [reviewing, setReviewing] = React.useState(false);

  const files = snapshot?.files ?? [];
  const changedPathSet = React.useMemo(
    () => new Set(files.map((file) => file.path)),
    [files],
  );
  const repoFileContent = repoFilePath
    ? repoFileByPath[repoFilePath]
    : undefined;
  const repoFileLoading = Boolean(
    repoFilePath && repoFileLoadingPath === repoFilePath && !repoFileContent,
  );

  React.useEffect(() => {
    if (visible) {
      return;
    }
    setActiveTab("diff");
  }, [visible]);

  const repoTitle =
    snapshot?.repo_name || repoBaseName(snapshot?.repo_root || "") || "repo";

  return (
    <Modal visible={visible} animationType="slide" onRequestClose={onClose}>
      <SafeAreaView
        style={[styles.root, { backgroundColor: chrome.appBackground }]}
        edges={["top", "bottom"]}
      >
        <StatusBar
          barStyle={
            isLightTerminalTheme(theme) ? "dark-content" : "light-content"
          }
        />
        <View
          style={[
            styles.sheet,
            {
              backgroundColor: chrome.surface,
              borderColor: chrome.border,
            },
          ]}
        >
          <GitDiffSheetTopChrome
            chrome={chrome}
            snapshot={snapshot}
            loading={loading}
            compact={reviewing && activeTab === "diff"}
            activeTab={activeTab}
            fileCount={files.length}
            accentColor={theme.cursor}
            onClose={onClose}
            onRefresh={() => {
              onRefresh();
              if (activeTab === "browser") {
                if (repoFilePath) onOpenRepoFile(repoFilePath);
                else onOpenRepoPath(repoBrowserPath);
              }
            }}
            onTabChange={(tab) => {
              setActiveTab(tab);
              if (tab === "browser") onOpenRepoPath(repoBrowserPath);
            }}
          />

          {error ? (
            <View style={styles.contentPad}>
              <GitDiffStateCard
                icon="warning-outline"
                title="Could not load git data"
                detail={error}
                accent={theme.red}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
              />
            </View>
          ) : loading && !snapshot ? (
            <View style={styles.contentPad}>
              <GitDiffStateCard
                icon="sync-outline"
                title="Inspecting repository"
                accent={theme.cursor}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
                busy
              />
            </View>
          ) : !snapshot?.available ? (
            <View style={styles.contentPad}>
              <GitDiffStateCard
                icon="git-branch-outline"
                title={
                  snapshot?.reason === "no_cwd"
                    ? "No working directory yet"
                    : "Not a git repository"
                }
                detail={
                  snapshot?.reason === "no_cwd"
                    ? "This terminal has not reported a cwd yet."
                    : "Move this terminal into a git repository and refresh."
                }
                accent={chrome.textSubtle}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
              />
            </View>
          ) : (
            <>
              {activeTab === "browser" ? (
                <GitDiffRepoBrowser
                  repoTitle={repoTitle}
                  repoBrowserPath={repoBrowserPath}
                  repoBrowserEntries={repoBrowserEntries}
                  repoBrowserLoading={repoBrowserLoading}
                  repoBrowserError={repoBrowserError}
                  repoFilePath={repoFilePath}
                  repoFileContent={repoFileContent}
                  repoFileLoading={repoFileLoading}
                  repoFileError={repoFileError}
                  changedPathSet={changedPathSet}
                  theme={theme}
                  chrome={chrome}
                  onOpenRepoPath={onOpenRepoPath}
                  onOpenRepoFile={onOpenRepoFile}
                  onCloseRepoFile={onCloseRepoFile}
                  onBackRepoPath={onBackRepoPath}
                />
              ) : null}
              <View
                style={{
                  flex: 1,
                  display: activeTab === "diff" ? "flex" : "none",
                }}
              >
                <GitDiffSheetDiffContent
                  key={ownerKey}
                  files={files}
                  clean={snapshot.clean}
                  loadPage={loadPage}
                  refreshKey={refreshKey}
                  onReviewChange={setReviewing}
                  theme={theme}
                  chrome={chrome}
                  onOpenFile={(path) => {
                    setActiveTab("browser");
                    onOpenRepoFile(path);
                  }}
                />
              </View>
            </>
          )}
        </View>
      </SafeAreaView>
    </Modal>
  );
}

function pathBaseName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? path : path.slice(index + 1);
}

function repoBaseName(path: string): string {
  const trimmed = path.replace(/\/+$/, "");
  if (!trimmed) return "";
  return pathBaseName(trimmed);
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
  },
  sheet: {
    flex: 1,
    borderWidth: 0,
  },
  contentPad: {
    flex: 1,
    paddingHorizontal: 14,
    paddingVertical: 14,
  },
});
