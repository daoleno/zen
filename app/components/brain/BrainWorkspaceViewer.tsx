import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Colors, Typography, useAppColors } from "../../constants/tokens";
import { compactPathLabel } from "../../services/pathDisplay";
import {
  wsClient,
  type BrainWorkspaceEntry,
  type BrainWorkspaceFile,
  type BrainWorkspaceTree,
} from "../../services/websocket";
import { AppText, BottomSheetFrame, IconButton } from "../ui";
import { InterfaceNativeMarkdownBody } from "../terminal/InterfaceNativeMarkdownBody";
import { MarkdownFallbackText } from "../markdown/MarkdownFallbackText";
import {
  brainWorkspaceEntryAccessibilityLabel,
  brainWorkspaceEntryIconName,
  brainWorkspaceMarkdownPath,
} from "./brainPresentation";

interface BrainWorkspaceViewerProps {
  visible: boolean;
  serverId?: string;
  workspace?: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onClose(): void;
}

type BrainWorkspaceCache = {
  cacheKey: string;
  directories: Map<string, BrainWorkspaceTree>;
  files: Map<string, BrainWorkspaceFile>;
};

export function BrainWorkspaceViewer({
  visible,
  serverId,
  workspace,
  chrome,
  theme,
  onClose,
}: BrainWorkspaceViewerProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const workspaceCacheKey = useMemo(
    () => (serverId ? `${serverId}:${workspace || ""}` : ""),
    [serverId, workspace],
  );
  const [tree, setTree] = useState<BrainWorkspaceTree | null>(null);
  const [treeCacheKey, setTreeCacheKey] = useState("");
  const [treeLoading, setTreeLoading] = useState(false);
  const [treeError, setTreeError] = useState<string | null>(null);
  const [directoryStack, setDirectoryStack] = useState<BrainWorkspaceEntry[]>(
    [],
  );
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [selectedFile, setSelectedFile] = useState<BrainWorkspaceFile | null>(
    null,
  );
  const [selectedFileCacheKey, setSelectedFileCacheKey] = useState("");
  const [fileLoading, setFileLoading] = useState(false);
  const [fileError, setFileError] = useState<string | null>(null);
  const cacheRef = useRef<BrainWorkspaceCache>({
    cacheKey: "",
    directories: new Map(),
    files: new Map(),
  });
  const treeRequestRef = useRef(0);
  const fileRequestRef = useRef(0);
  const currentTree = treeCacheKey === workspaceCacheKey ? tree : null;
  const currentSelectedFile =
    selectedFileCacheKey === workspaceCacheKey ? selectedFile : null;

  useEffect(() => {
    if (!visible || !serverId) {
      return;
    }
    if (cacheRef.current.cacheKey !== workspaceCacheKey) {
      cacheRef.current = {
        cacheKey: workspaceCacheKey,
        directories: new Map(),
        files: new Map(),
      };
    }

    treeRequestRef.current += 1;
    fileRequestRef.current += 1;
    setDirectoryStack([]);
    setSelectedPath(null);
    setSelectedFile(null);
    setSelectedFileCacheKey("");
    setFileLoading(false);
    setFileError(null);

    const cache = cacheRef.current;
    const cachedRoot = cache.directories.get("");
    if (cachedRoot) {
      setTree(cachedRoot);
      setTreeCacheKey(workspaceCacheKey);
      setTreeLoading(false);
      setTreeError(null);
      return;
    }

    let cancelled = false;
    setTreeLoading(true);
    setTreeError(null);
    setTree(null);
    setTreeCacheKey(workspaceCacheKey);
    void wsClient
      .getBrainWorkspaceTree(serverId, "")
      .then((nextTree) => {
        if (!cancelled) {
          cacheRef.current.directories.set("", nextTree);
          setTree(nextTree);
          setTreeCacheKey(workspaceCacheKey);
        }
      })
      .catch((error: any) => {
        if (!cancelled) {
          setTreeError(error?.message || "Failed to load Brain workspace.");
        }
      })
      .finally(() => {
        if (!cancelled) {
          setTreeLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [serverId, visible, workspaceCacheKey]);

  const currentDirectory = directoryStack.at(-1) ?? null;
  const currentEntries = useMemo(
    () =>
      [...(currentTree?.entries ?? [])].sort((left, right) => {
        if (left.kind !== right.kind) {
          return left.kind === "directory" ? -1 : 1;
        }
        return left.name.localeCompare(right.name);
      }),
    [currentTree?.entries],
  );

  const loadFile = useCallback(
    async (entry: BrainWorkspaceEntry) => {
      if (!serverId || entry.kind === "directory") {
        return;
      }
      const cache =
        cacheRef.current.cacheKey === workspaceCacheKey
          ? cacheRef.current
          : null;
      const cachedFile = cache?.files.get(entry.path);
      const request = fileRequestRef.current + 1;
      fileRequestRef.current = request;
      setSelectedPath(entry.path);
      setFileError(null);
      if (cachedFile) {
        setSelectedFile(cachedFile);
        setSelectedFileCacheKey(workspaceCacheKey);
        setFileLoading(false);
        return;
      }
      setSelectedFile(null);
      setSelectedFileCacheKey(workspaceCacheKey);
      setFileLoading(true);
      try {
        const file = await wsClient.getBrainWorkspaceFile(serverId, entry.path);
        if (fileRequestRef.current === request) {
          if (cacheRef.current.cacheKey === workspaceCacheKey) {
            cacheRef.current.files.set(entry.path, file);
          }
          setSelectedFile(file);
          setSelectedFileCacheKey(workspaceCacheKey);
        }
      } catch (error: any) {
        if (fileRequestRef.current === request) {
          setFileError(
            error?.message || "Failed to load Brain workspace file.",
          );
        }
      } finally {
        if (fileRequestRef.current === request) {
          setFileLoading(false);
        }
      }
    },
    [serverId, workspaceCacheKey],
  );

  const loadDirectory = useCallback(
    async (entry: BrainWorkspaceEntry) => {
      if (!serverId || entry.kind !== "directory") {
        return;
      }
      const cachedTree =
        cacheRef.current.cacheKey === workspaceCacheKey
          ? cacheRef.current.directories.get(entry.path)
          : null;
      setDirectoryStack((previous) => [...previous, entry]);
      setSelectedPath(null);
      setSelectedFile(null);
      setFileError(null);
      setTreeError(null);
      if (cachedTree) {
        setTree(cachedTree);
        setTreeCacheKey(workspaceCacheKey);
        setTreeLoading(false);
        return;
      }

      const request = treeRequestRef.current + 1;
      treeRequestRef.current = request;
      setTree(null);
      setTreeCacheKey(workspaceCacheKey);
      setTreeLoading(true);
      try {
        const nextTree = await wsClient.getBrainWorkspaceTree(
          serverId,
          entry.path,
        );
        if (treeRequestRef.current === request) {
          if (cacheRef.current.cacheKey === workspaceCacheKey) {
            cacheRef.current.directories.set(entry.path, nextTree);
          }
          setTree(nextTree);
          setTreeCacheKey(workspaceCacheKey);
        }
      } catch (error: any) {
        if (treeRequestRef.current === request) {
          setTreeError(
            error?.message || "Failed to load Brain workspace folder.",
          );
        }
      } finally {
        if (treeRequestRef.current === request) {
          setTreeLoading(false);
        }
      }
    },
    [serverId, workspaceCacheKey],
  );

  const openEntry = useCallback(
    (entry: BrainWorkspaceEntry) => {
      if (entry.kind === "directory") {
        void loadDirectory(entry);
        return;
      }
      void loadFile(entry);
    },
    [loadDirectory, loadFile],
  );

  const goBack = useCallback(() => {
    if (currentSelectedFile || fileLoading || fileError) {
      fileRequestRef.current += 1;
      setSelectedPath(null);
      setSelectedFile(null);
      setFileLoading(false);
      setFileError(null);
      return;
    }
    treeRequestRef.current += 1;
    const parentStack = directoryStack.slice(0, -1);
    const parentPath = parentStack.at(-1)?.path ?? "";
    const parentTree =
      cacheRef.current.cacheKey === workspaceCacheKey
        ? (cacheRef.current.directories.get(parentPath) ?? null)
        : null;
    setDirectoryStack(parentStack);
    setTree(parentTree);
    setTreeCacheKey(workspaceCacheKey);
    setTreeLoading(false);
    setTreeError(null);
  }, [
    currentSelectedFile,
    directoryStack,
    fileError,
    fileLoading,
    workspaceCacheKey,
  ]);
  const showingFile = Boolean(
    selectedPath || currentSelectedFile || fileLoading || fileError,
  );
  const canGoBack = showingFile || directoryStack.length > 0;
  const locationLabel =
    selectedPath ||
    currentDirectory?.path ||
    currentTree?.workspace ||
    workspace;

  return (
    <BottomSheetFrame
      visible={visible}
      onClose={onClose}
      maxHeight="90%"
      cardStyle={styles.sheetCard}
      contentStyle={styles.sheetContent}
    >
      <View style={styles.header}>
        {canGoBack ? (
          <IconButton
            icon="arrow-back-outline"
            size={44}
            iconSize={19}
            tone="ghost"
            accessibilityRole="button"
            accessibilityLabel={
              showingFile ? "Back to folder" : "Back to parent folder"
            }
            onPress={goBack}
          />
        ) : null}
        <View style={styles.headerText}>
          <AppText variant="title" tone="primary" numberOfLines={1}>
            Workspace
          </AppText>
          <AppText
            variant="caption"
            tone="secondary"
            numberOfLines={1}
            ellipsizeMode="head"
          >
            {compactWorkspaceLabel(locationLabel)}
          </AppText>
        </View>
        <IconButton
          icon="close-outline"
          size={44}
          iconSize={18}
          tone="ghost"
          accessibilityRole="button"
          accessibilityLabel="Close Brain workspace viewer"
          onPress={onClose}
        />
      </View>

      <View style={styles.body}>
        {treeLoading ? (
          <View style={styles.previewState}>
            <ActivityIndicator size="small" color={colors.accent} />
            <AppText variant="caption" tone="secondary">
              Loading workspace
            </AppText>
          </View>
        ) : treeError ? (
          <View style={styles.previewState}>
            <Ionicons
              name="warning-outline"
              size={18}
              color={colors.dangerText}
            />
            <AppText
              variant="caption"
              tone="danger"
              style={styles.previewStateText}
            >
              {treeError}
            </AppText>
          </View>
        ) : fileLoading ? (
          <View style={styles.previewState}>
            <ActivityIndicator size="small" color={colors.accent} />
            <AppText variant="caption" tone="secondary">
              Loading file
            </AppText>
          </View>
        ) : fileError ? (
          <View style={styles.previewState}>
            <Ionicons
              name="warning-outline"
              size={18}
              color={colors.dangerText}
            />
            <AppText
              variant="caption"
              tone="danger"
              style={styles.previewStateText}
            >
              {fileError}
            </AppText>
          </View>
        ) : currentSelectedFile ? (
          <BrainWorkspaceFilePreview
            file={currentSelectedFile}
            chrome={chrome}
            theme={theme}
            styles={styles}
          />
        ) : currentEntries.length === 0 ? (
          <View style={styles.previewState}>
            <Ionicons
              name="folder-open-outline"
              size={20}
              color={colors.textSecondary}
            />
            <AppText variant="caption" tone="secondary">
              This folder is empty.
            </AppText>
          </View>
        ) : (
          <ScrollView
            style={styles.browserScroll}
            contentContainerStyle={styles.browserContent}
            nestedScrollEnabled
            showsVerticalScrollIndicator
          >
            {currentEntries.map((entry) => {
              const directory = entry.kind === "directory";
              return (
                <Pressable
                  key={entry.path}
                  accessibilityRole="button"
                  accessibilityLabel={brainWorkspaceEntryAccessibilityLabel(
                    entry.kind,
                    entry.name,
                  )}
                  onPress={() => openEntry(entry)}
                  style={({ pressed }) => [
                    styles.browserRow,
                    pressed ? styles.browserRowPressed : null,
                  ]}
                >
                  <Ionicons
                    name={brainWorkspaceEntryIconName(entry.kind, entry.path)}
                    size={18}
                    color={directory ? colors.accent : colors.textSecondary}
                  />
                  <Text
                    style={styles.browserName}
                    numberOfLines={1}
                    ellipsizeMode="middle"
                  >
                    {entry.name}
                  </Text>
                </Pressable>
              );
            })}
          </ScrollView>
        )}
      </View>
    </BottomSheetFrame>
  );
}

function BrainWorkspaceFilePreview({
  file,
  chrome,
  theme,
  styles,
}: {
  file: BrainWorkspaceFile;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  styles: ReturnType<typeof createStyles>;
}) {
  const markdown =
    file.language === "markdown" || brainWorkspaceMarkdownPath(file.path);
  const content = file.content;
  return (
    <View
      style={styles.previewContent}
      accessibilityLabel={`File ${file.name || file.path}`}
    >
      <ScrollView
        style={styles.fileScroll}
        contentContainerStyle={styles.fileScrollContent}
        nestedScrollEnabled
        showsVerticalScrollIndicator
      >
        {content.trim() ? (
          markdown ? (
            <InterfaceNativeMarkdownBody
              value={content}
              chrome={chrome}
              theme={theme}
              renderFallback={(value) => (
                <MarkdownFallbackText
                  value={value}
                  style={[styles.plainText, { color: chrome.text }]}
                  linkStyle={{
                    color: chrome.link,
                    textDecorationLine: "underline",
                  }}
                />
              )}
            />
          ) : (
            <Text selectable style={[styles.plainText, { color: chrome.text }]}>
              {content}
            </Text>
          )
        ) : (
          <AppText variant="caption" tone="secondary">
            Empty file.
          </AppText>
        )}
      </ScrollView>
    </View>
  );
}

function compactWorkspaceLabel(path?: string) {
  const value = compactPathLabel(path, { tailSegments: 2, showFullUpTo: 2 });
  return value || "Workspace";
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    sheetCard: {
      height: "90%",
    },
    sheetContent: {
      flex: 1,
      minHeight: 0,
    },
    header: {
      minHeight: 42,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 12,
      marginBottom: 10,
    },
    headerText: {
      flex: 1,
      minWidth: 0,
    },
    body: {
      flex: 1,
      minHeight: 0,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      borderRadius: 12,
      backgroundColor: colors.bgPrimary,
      overflow: "hidden",
    },
    browserScroll: {
      flex: 1,
      minHeight: 0,
    },
    browserContent: {
      paddingVertical: 2,
    },
    browserRow: {
      minHeight: 44,
      paddingHorizontal: 12,
      paddingVertical: 10,
      flexDirection: "row",
      alignItems: "center",
      gap: 10,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    browserRowPressed: {
      backgroundColor: colors.surfacePressed,
    },
    browserName: {
      flex: 1,
      minWidth: 0,
      fontFamily: Typography.uiFont,
      fontSize: 15,
      lineHeight: 20,
      color: colors.textPrimary,
      letterSpacing: 0,
    },
    previewContent: {
      flex: 1,
      minHeight: 0,
    },
    fileScroll: {
      flex: 1,
      minHeight: 0,
    },
    fileScrollContent: {
      paddingHorizontal: 14,
      paddingVertical: 12,
    },
    plainText: {
      fontFamily: Typography.chatMonoFont,
      fontSize: 13,
      lineHeight: 20,
      letterSpacing: 0,
    },
    previewState: {
      flex: 1,
      alignItems: "center",
      justifyContent: "center",
      paddingHorizontal: 22,
      gap: 8,
    },
    previewStateText: {
      textAlign: "center",
    },
  });
}
