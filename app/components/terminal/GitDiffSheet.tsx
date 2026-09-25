import React from "react";
import {
  Modal,
  StatusBar,
  StyleSheet,
  useWindowDimensions,
  View,
} from "react-native";
import {
  SafeAreaView,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
import {
  isLightTerminalTheme,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  filterGitDiffFiles,
  type GitDiffFileInfo,
  type GitDiffPage,
  type GitDiffPageRequest,
  type GitDiffScope,
  type GitDiffStatusSnapshot,
  type GitRepoBrowserEntry,
  type GitRepoFileContentPayload,
} from "../../services/gitDiff";
import { ActionMenu, type ActionMenuItem } from "../ui/ActionMenu";
import { EmptyState } from "../ui/EmptyState";
import { InlineNotice } from "../ui/InlineNotice";
import { ToastProvider } from "../ui/Toast";
import { GitDiffRepoBrowser } from "./GitDiffRepoBrowser";
import { GitDiffSheetDiffContent } from "./GitDiffSheetDiffContent";
import {
  GitDiffSheetTopChrome,
  type GitDiffSheetView,
} from "./GitDiffSheetTopChrome";
import {
  buildGitDiffFileActions,
  type GitDiffMenuRequest,
} from "./gitDiffActions";
import { useGitDiffClipboard } from "./gitDiffClipboard";
import { resolveGitDiffBack, type GitDiffViewMode } from "./gitDiffNavigation";
import { summarizeGitDiffFiles } from "./gitDiffPresentation";
import { useGitDiffChrome } from "./gitDiffSurface";

const WIDE_BREAKPOINT = 720;

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

export function GitDiffSheet(props: GitDiffSheetProps) {
  // Android hardware back resolves through the body's back stack. The toast
  // host lives inside the Modal because the app-level host renders beneath it.
  const backRef = React.useRef<() => void>(props.onClose);
  return (
    <Modal
      visible={props.visible}
      animationType="slide"
      onRequestClose={() => backRef.current()}
      statusBarTranslucent
    >
      <ToastProvider>
        <GitDiffSheetBody {...props} backRef={backRef} />
      </ToastProvider>
    </Modal>
  );
}

function GitDiffSheetBody({
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
  backRef,
}: GitDiffSheetProps & { backRef: React.MutableRefObject<() => void> }) {
  const chrome = useGitDiffChrome(theme);
  const { width } = useWindowDimensions();
  const insets = useSafeAreaInsets();
  const wide = width >= WIDE_BREAKPOINT;
  const bottomInset = Math.max(insets.bottom, 0);
  const { copyPath, copyPatch, copyText } = useGitDiffClipboard(loadPage);

  const [view, setView] = React.useState<GitDiffViewMode>("changes");
  const [selectedPath, setSelectedPath] = React.useState<string | null>(null);
  const [scope, setScope] = React.useState<GitDiffScope>("all");
  const [fileFilterOpen, setFileFilterOpen] = React.useState(false);
  const [diffSearchOpen, setDiffSearchOpen] = React.useState(false);
  const [diffOptionsOpen, setDiffOptionsOpen] = React.useState(false);
  const [fileOrigin, setFileOrigin] = React.useState<GitDiffViewMode>("files");
  // One menu host at the sheet root: ActionMenu runs the chosen action after
  // its own dismissal, so it must outlive whichever control opened it.
  const [menu, setMenu] = React.useState<GitDiffMenuRequest | null>(null);
  const [menuVisible, setMenuVisible] = React.useState(false);

  const files = React.useMemo(() => snapshot?.files ?? [], [snapshot?.files]);
  const changedPathSet = React.useMemo(
    () => new Set(files.map((file) => file.path)),
    [files],
  );
  const selectedFile =
    files.find((file) => file.path === selectedPath) ?? null;
  const repoFileContent = repoFilePath
    ? repoFileByPath[repoFilePath]
    : undefined;
  const repoFileLoading = Boolean(
    repoFilePath && repoFileLoadingPath === repoFilePath && !repoFileContent,
  );

  const resetTransientState = React.useCallback(() => {
    setView("changes");
    setSelectedPath(null);
    setScope("all");
    setFileFilterOpen(false);
    setDiffSearchOpen(false);
    setDiffOptionsOpen(false);
    setMenuVisible(false);
  }, []);

  React.useEffect(() => {
    if (!visible) resetTransientState();
  }, [resetTransientState, visible]);

  React.useEffect(() => {
    resetTransientState();
  }, [ownerKey, resetTransientState]);

  React.useEffect(() => {
    if (selectedPath && !selectedFile && snapshot?.available && !loading) {
      setSelectedPath(null);
    }
  }, [selectedPath, selectedFile, snapshot?.available, loading]);

  const scopeCounts = React.useMemo<Record<GitDiffScope, number>>(
    () => ({
      all: files.length,
      working: filterGitDiffFiles(files, "working", "").length,
      staged: filterGitDiffFiles(files, "staged", "").length,
    }),
    [files],
  );
  const summary = React.useMemo(
    () => summarizeGitDiffFiles(filterGitDiffFiles(files, scope, ""), scope).label,
    [files, scope],
  );

  const repoTitle =
    snapshot?.repo_name || repoBaseName(snapshot?.repo_root || "") || "repo";

  const closeReaderTools = React.useCallback(() => {
    setDiffSearchOpen(false);
    setDiffOptionsOpen(false);
  }, []);

  const handleBack = React.useCallback(() => {
    const action = resolveGitDiffBack({
      view,
      hasSelectedFile: Boolean(selectedFile),
      hasBrowserFile: Boolean(repoFilePath),
      hasBrowserParent: repoBrowserPath !== "",
      fileOrigin,
    });
    switch (action) {
      case "close":
        onClose();
        break;
      case "deselect-file":
        setSelectedPath(null);
        closeReaderTools();
        break;
      case "close-browser-file-to-reader":
        onCloseRepoFile();
        setView("changes");
        break;
      case "close-browser-file-to-browser":
        onCloseRepoFile();
        break;
      case "browser-to-parent":
        onBackRepoPath();
        break;
      case "browser-to-changes":
        setView("changes");
        break;
    }
  }, [closeReaderTools, fileOrigin, onBackRepoPath, onClose, onCloseRepoFile, repoBrowserPath, repoFilePath, selectedFile, view]);
  backRef.current = handleBack;

  const handleBrowseFiles = React.useCallback(() => {
    setView("files");
    onOpenRepoPath(repoBrowserPath);
  }, [onOpenRepoPath, repoBrowserPath]);

  const handleOpenWorkingFile = React.useCallback(
    (path: string) => {
      setFileOrigin("changes");
      setView("files");
      onOpenRepoFile(path);
    },
    [onOpenRepoFile],
  );

  const handleOpenBrowserFile = React.useCallback(
    (path: string) => {
      setFileOrigin("files");
      onOpenRepoFile(path);
    },
    [onOpenRepoFile],
  );

  const handleRefresh = React.useCallback(() => {
    onRefresh();
    if (view === "files") {
      if (repoFilePath) onOpenRepoFile(repoFilePath);
      else onOpenRepoPath(repoBrowserPath);
    }
  }, [onOpenRepoFile, onOpenRepoPath, onRefresh, repoBrowserPath, repoFilePath, view]);

  const openMenu = React.useCallback((request: GitDiffMenuRequest) => {
    setMenu(request);
    setMenuVisible(true);
  }, []);

  const refreshItem: ActionMenuItem = {
    key: "refresh",
    label: "Refresh",
    icon: "refresh",
    disabled: loading,
    onPress: handleRefresh,
  };

  const fileActions = React.useCallback(
    (file: GitDiffFileInfo, reading: boolean): ActionMenuItem[] =>
      buildGitDiffFileActions({
        path: file.path,
        deleted: file.status === "deleted",
        reading,
        searchOpen: diffSearchOpen,
        optionsOpen: diffOptionsOpen,
        onToggleSearch: () => setDiffSearchOpen((value) => !value),
        onToggleOptions: () => setDiffOptionsOpen((value) => !value),
        onViewDiff: () => {
          setSelectedPath(file.path);
          closeReaderTools();
        },
        onOpenFile: handleOpenWorkingFile,
        onCopyPath: copyPath,
        onCopyPatch: (path) => void copyPatch(path, scope),
      }),
    [closeReaderTools, copyPatch, copyPath, diffOptionsOpen, diffSearchOpen, handleOpenWorkingFile, scope],
  );

  const chromeView: GitDiffSheetView = !snapshot?.available
    ? "overview"
    : view === "files"
      ? repoFilePath
        ? "file"
        : "files"
      : // Wide mode keeps the repo/list header anchored to the master pane; the
        // detail pane owns its own file header.
        wide
        ? "overview"
        : selectedFile
          ? "reader"
          : "overview";

  const headerActions = (): GitDiffMenuRequest => {
    switch (chromeView) {
      case "reader":
        return selectedFile
          ? { title: selectedFile.path, items: [...fileActions(selectedFile, true), refreshItem] }
          : { items: [refreshItem] };
      case "files":
        return {
          title: repoBrowserPath || repoTitle,
          items: [
            ...(repoBrowserPath
              ? [{ key: "copy-folder", label: "Copy folder path", icon: "link-outline" as const, onPress: () => void copyPath(repoBrowserPath) }]
              : []),
            refreshItem,
          ],
        };
      case "file":
        return {
          title: repoFilePath ?? undefined,
          items: [
            ...(repoFilePath
              ? [{ key: "copy-path", label: "Copy path", icon: "link-outline" as const, onPress: () => void copyPath(repoFilePath) }]
              : []),
            refreshItem,
          ],
        };
      case "overview": {
        if (!snapshot?.available) return { items: [refreshItem] };
        const items: ActionMenuItem[] = [];
        if (files.length) {
          items.push({
            key: "filter",
            label: fileFilterOpen ? "Hide filter" : "Filter paths",
            icon: "search",
            onPress: () => setFileFilterOpen((value) => !value),
          });
        }
        items.push({
          key: "browse",
          label: "Browse files",
          icon: "folder-open-outline",
          onPress: handleBrowseFiles,
        });
        if (snapshot.repo_root) {
          const root = snapshot.repo_root;
          items.push({
            key: "copy-root",
            label: "Copy repository path",
            icon: "link-outline",
            onPress: () => void copyText(root, "Path copied", root),
          });
        }
        if (snapshot.branch) {
          const branch = snapshot.branch;
          items.push({
            key: "copy-branch",
            label: "Copy branch name",
            icon: "git-branch-outline",
            onPress: () => void copyText(branch, "Branch copied", branch),
          });
        }
        items.push(refreshItem);
        return { title: snapshot.branch ? `${repoTitle} · ${snapshot.branch}` : repoTitle, items };
      }
    }
  };

  const statePad = [styles.contentPad, { paddingBottom: bottomInset + 16 }];

  return (
    <SafeAreaView
      style={[styles.root, { backgroundColor: chrome.appBackground }]}
      edges={["top"]}
    >
      <StatusBar
        barStyle={
          isLightTerminalTheme(theme) ? "dark-content" : "light-content"
        }
      />
      <GitDiffSheetTopChrome
        chrome={chrome}
        theme={theme}
        snapshot={snapshot}
        loading={loading}
        view={chromeView}
        file={selectedFile}
        scope={scope}
        repoTitle={repoTitle}
        summary={summary}
        browserPath={repoBrowserPath}
        browserFilePath={repoFilePath}
        workingFileOrigin={fileOrigin}
        hasActions={Boolean(snapshot || error)}
        onClose={onClose}
        onBack={handleBack}
        onOpenActions={() => openMenu(headerActions())}
      />

      {error && snapshot?.available ? (
        <InlineNotice
          tone="danger"
          title="Couldn't refresh Git status"
          detail={error}
          action={{ label: "Retry", onPress: handleRefresh, disabled: loading }}
          style={styles.notice}
        />
      ) : null}

      {error && !snapshot?.available ? (
        <View style={statePad}>
          <EmptyState
            tone="danger"
            icon="warning-outline"
            title="Couldn't load Git status"
            detail={error}
            action={{ label: "Retry", icon: "refresh", onPress: handleRefresh, loading }}
          />
        </View>
      ) : loading && !snapshot ? (
        <View style={statePad}>
          <EmptyState busy title="Inspecting repository" />
        </View>
      ) : !snapshot?.available ? (
        <View style={statePad}>
          <EmptyState
            icon="git-branch-outline"
            title={
              snapshot?.reason === "no_cwd"
                ? "No working directory yet"
                : "Not a Git repository"
            }
            detail={
              snapshot?.reason === "no_cwd"
                ? "This terminal hasn't reported a folder yet."
                : "Move this terminal into a Git repository, then refresh."
            }
            action={{ label: "Refresh", icon: "refresh", onPress: handleRefresh, loading }}
          />
        </View>
      ) : (
        <View style={styles.body}>
          <View
            style={styles.changesLayer}
            pointerEvents={view === "changes" ? "auto" : "none"}
            importantForAccessibility={
              view === "changes" ? "auto" : "no-hide-descendants"
            }
            accessibilityElementsHidden={view !== "changes"}
          >
            <GitDiffSheetDiffContent
              key={ownerKey}
              files={files}
              clean={snapshot.clean}
              branch={snapshot.branch}
              repoTitle={repoTitle}
              loadPage={loadPage}
              refreshKey={refreshKey}
              scope={scope}
              scopeCounts={scopeCounts}
              selectedFile={selectedFile}
              wide={wide}
              bottomInset={bottomInset}
              fileFilterOpen={fileFilterOpen}
              diffSearchOpen={diffSearchOpen}
              diffOptionsOpen={diffOptionsOpen}
              loading={loading}
              theme={theme}
              chrome={chrome}
              onSelectFile={(path) => {
                setSelectedPath(path);
                closeReaderTools();
              }}
              onScopeChange={(next) => {
                setScope(next);
                setSelectedPath(null);
              }}
              onClearSelection={() => {
                setSelectedPath(null);
                closeReaderTools();
              }}
              onCloseFileFilter={() => setFileFilterOpen(false)}
              onToggleDiffSearch={() => setDiffSearchOpen((value) => !value)}
              onToggleDiffOptions={() => setDiffOptionsOpen((value) => !value)}
              onBrowseFiles={handleBrowseFiles}
              onOpenFileActions={(file, reading) =>
                openMenu({
                  title: file.path,
                  items: reading ? [...fileActions(file, true), refreshItem] : fileActions(file, false),
                })
              }
            />
          </View>
          {view === "files" ? (
            <View
              style={[
                styles.browserLayer,
                { backgroundColor: repoFilePath ? chrome.surface : chrome.appBackground },
              ]}
            >
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
                bottomInset={bottomInset}
                onOpenRepoPath={onOpenRepoPath}
                onOpenRepoFile={handleOpenBrowserFile}
                onLongPressEntry={(entry) =>
                  openMenu({
                    title: entry.path,
                    items: [
                      {
                        key: "open",
                        label: entry.kind === "directory" ? "Open folder" : "Open file",
                        icon: entry.kind === "directory" ? "folder-open-outline" : "document-text-outline",
                        onPress: () =>
                          entry.kind === "directory"
                            ? onOpenRepoPath(entry.path)
                            : handleOpenBrowserFile(entry.path),
                      },
                      {
                        key: "copy-path",
                        label: "Copy path",
                        icon: "link-outline",
                        onPress: () => void copyPath(entry.path),
                      },
                    ],
                  })
                }
              />
            </View>
          ) : null}
        </View>
      )}

      <ActionMenu
        visible={menuVisible}
        title={menu?.title}
        items={menu?.items ?? []}
        onClose={() => setMenuVisible(false)}
      />
    </SafeAreaView>
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
  body: {
    flex: 1,
  },
  changesLayer: {
    flex: 1,
  },
  browserLayer: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
  },
  notice: {
    marginHorizontal: 16,
    marginTop: 8,
  },
  contentPad: {
    flex: 1,
    justifyContent: "center",
    paddingHorizontal: 16,
    paddingVertical: 16,
  },
});
