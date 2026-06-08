import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
import {
  Colors,
  Typography,
  useAppColors,
} from "../../constants/tokens";
import { wsClient, type BrainWorkspaceEntry, type BrainWorkspaceFile, type BrainWorkspaceTree } from "../../services/websocket";
import { AppText, BottomSheetFrame, IconButton } from "../ui";
import { CodexNativeMarkdownBody } from "../terminal/CodexNativeMarkdownBody";

interface BrainWorkspaceViewerProps {
  visible: boolean;
  serverId?: string;
  workspace?: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onClose(): void;
}

type BrainWorkspaceRow = {
  entry: BrainWorkspaceEntry;
  depth: number;
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
  const [tree, setTree] = useState<BrainWorkspaceTree | null>(null);
  const [treeLoading, setTreeLoading] = useState(false);
  const [treeError, setTreeError] = useState<string | null>(null);
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [selectedFile, setSelectedFile] = useState<BrainWorkspaceFile | null>(null);
  const [fileLoading, setFileLoading] = useState(false);
  const [fileError, setFileError] = useState<string | null>(null);
  const fileRequestRef = useRef(0);

  useEffect(() => {
    if (!visible || !serverId) {
      return;
    }
    let cancelled = false;
    setTreeLoading(true);
    setTreeError(null);
    setTree(null);
    setSelectedPath(null);
    setSelectedFile(null);
    setFileError(null);
    void wsClient
      .getBrainWorkspaceTree(serverId)
      .then((nextTree) => {
        if (!cancelled) {
          setTree(nextTree);
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
  }, [serverId, visible]);

  const rows = useMemo(
    () => flattenWorkspaceEntries(tree?.entries ?? []),
    [tree?.entries],
  );

  const loadFile = useCallback(
    async (entry: BrainWorkspaceEntry) => {
      if (!serverId || entry.kind === "directory") {
        return;
      }
      const request = fileRequestRef.current + 1;
      fileRequestRef.current = request;
      setSelectedPath(entry.path);
      setSelectedFile(null);
      setFileError(null);
      setFileLoading(true);
      try {
        const file = await wsClient.getBrainWorkspaceFile(serverId, entry.path);
        if (fileRequestRef.current === request) {
          setSelectedFile(file);
        }
      } catch (error: any) {
        if (fileRequestRef.current === request) {
          setFileError(error?.message || "Failed to load Brain workspace file.");
        }
      } finally {
        if (fileRequestRef.current === request) {
          setFileLoading(false);
        }
      }
    },
    [serverId],
  );

  return (
    <BottomSheetFrame
      visible={visible}
      onClose={onClose}
      maxHeight="90%"
      cardStyle={styles.sheetCard}
      contentStyle={styles.sheetContent}
    >
      <View style={styles.header}>
        <View style={styles.headerText}>
          <AppText variant="title" tone="primary" numberOfLines={1}>
            Brain workspace
          </AppText>
          <AppText variant="caption" tone="secondary" numberOfLines={1}>
            {compactWorkspaceLabel(tree?.workspace || workspace)}
          </AppText>
        </View>
        <IconButton
          icon="close-outline"
          size={34}
          iconSize={18}
          tone="ghost"
          accessibilityRole="button"
          accessibilityLabel="Close Brain workspace viewer"
          onPress={onClose}
        />
      </View>

      <View style={styles.body}>
        <View style={styles.treePanel}>
          {treeLoading ? (
            <View style={styles.stateRow}>
              <ActivityIndicator size="small" color={colors.accent} />
              <AppText variant="caption" tone="secondary">
                Loading workspace
              </AppText>
            </View>
          ) : treeError ? (
            <View style={styles.stateRow}>
              <Ionicons name="warning-outline" size={15} color={colors.dangerText} />
              <AppText variant="caption" tone="danger" style={styles.stateText}>
                {treeError}
              </AppText>
            </View>
          ) : rows.length === 0 ? (
            <View style={styles.stateRow}>
              <Ionicons name="folder-open-outline" size={15} color={colors.textSecondary} />
              <AppText variant="caption" tone="secondary">
                Workspace is empty.
              </AppText>
            </View>
          ) : (
            <ScrollView
              style={styles.treeScroll}
              contentContainerStyle={styles.treeScrollContent}
              nestedScrollEnabled
              showsVerticalScrollIndicator={false}
            >
              {rows.map(({ entry, depth }) => {
                const directory = entry.kind === "directory";
                const selected = !directory && entry.path === selectedPath;
                return (
                  <Pressable
                    key={entry.path}
                    accessibilityRole={directory ? undefined : "button"}
                    accessibilityLabel={directory ? entry.name : `Open ${entry.name}`}
                    disabled={directory || fileLoading}
                    onPress={() => void loadFile(entry)}
                    style={({ pressed }) => [
                      styles.treeRow,
                      { paddingLeft: 8 + depth * 16 },
                      selected ? styles.treeRowSelected : null,
                      pressed && !directory ? styles.treeRowPressed : null,
                    ]}
                  >
                    <Ionicons
                      name={directory ? "folder-outline" : markdownFile(entry.path) ? "document-text-outline" : "document-outline"}
                      size={15}
                      color={selected ? colors.accent : colors.textSecondary}
                    />
                    <Text
                      style={[
                        styles.treeRowText,
                        {
                          color: selected ? colors.textPrimary : colors.textSecondary,
                        },
                      ]}
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

        <View style={styles.previewPanel}>
          {fileLoading ? (
            <View style={styles.previewState}>
              <ActivityIndicator size="small" color={colors.accent} />
            </View>
          ) : fileError ? (
            <View style={styles.previewState}>
              <Ionicons name="warning-outline" size={18} color={colors.dangerText} />
              <AppText variant="caption" tone="danger" style={styles.previewStateText}>
                {fileError}
              </AppText>
            </View>
          ) : selectedFile ? (
            <BrainWorkspaceFilePreview
              file={selectedFile}
              chrome={chrome}
              theme={theme}
              styles={styles}
            />
          ) : (
            <View style={styles.previewState}>
              <Ionicons name="document-text-outline" size={18} color={colors.textSecondary} />
              <AppText variant="caption" tone="secondary">
                No file selected.
              </AppText>
            </View>
          )}
        </View>
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
  const markdown = file.language === "markdown" || markdownFile(file.path);
  const content = file.content;
  return (
    <View style={styles.previewContent}>
      <View style={styles.previewHeader}>
        <View style={styles.previewTitleBlock}>
          <AppText variant="label" tone="primary" numberOfLines={1} ellipsizeMode="middle">
            {file.path}
          </AppText>
          <AppText variant="caption" tone="secondary" numberOfLines={1}>
            {markdown ? "Markdown" : "Text"}
          </AppText>
        </View>
      </View>
      <ScrollView
        style={styles.fileScroll}
        contentContainerStyle={styles.fileScrollContent}
        nestedScrollEnabled
        showsVerticalScrollIndicator
      >
        {content.trim() ? (
          markdown ? (
            <CodexNativeMarkdownBody
              value={content}
              chrome={chrome}
              theme={theme}
              renderFallback={(value) => (
                <Text selectable style={[styles.plainText, { color: chrome.text }]}>
                  {value}
                </Text>
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

function flattenWorkspaceEntries(entries: BrainWorkspaceEntry[]) {
  const rows: BrainWorkspaceRow[] = [];
  const visit = (items: BrainWorkspaceEntry[], depth: number) => {
    for (const entry of items) {
      rows.push({ entry, depth });
      if (entry.kind === "directory" && entry.children.length > 0) {
        visit(entry.children, depth + 1);
      }
    }
  };
  visit(entries, 0);
  return rows;
}

function markdownFile(path: string) {
  return /\.(md|markdown)$/i.test(path);
}

function compactWorkspaceLabel(path?: string) {
  const value = path?.trim();
  if (!value) {
    return "Workspace";
  }
  const parts = value.split(/[\\/]+/).filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : value;
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
    },
    treePanel: {
      maxHeight: 176,
      minHeight: 56,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      borderRadius: 8,
      backgroundColor: colors.surfaceSubtle,
      overflow: "hidden",
    },
    treeScroll: {
      maxHeight: 176,
    },
    treeScrollContent: {
      paddingVertical: 6,
    },
    treeRow: {
      minHeight: 34,
      paddingRight: 10,
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
    },
    treeRowSelected: {
      backgroundColor: colors.surfaceActive,
    },
    treeRowPressed: {
      opacity: 0.72,
    },
    treeRowText: {
      flex: 1,
      minWidth: 0,
      fontFamily: Typography.uiFont,
      fontSize: 13,
      lineHeight: 18,
      letterSpacing: 0,
    },
    stateRow: {
      minHeight: 54,
      paddingHorizontal: 12,
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
    },
    stateText: {
      flex: 1,
      minWidth: 0,
    },
    previewPanel: {
      flex: 1,
      minHeight: 0,
      marginTop: 12,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      borderRadius: 8,
      backgroundColor: colors.bgPrimary,
      overflow: "hidden",
    },
    previewContent: {
      flex: 1,
      minHeight: 0,
    },
    previewHeader: {
      minHeight: 48,
      paddingHorizontal: 12,
      paddingVertical: 8,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
      justifyContent: "center",
    },
    previewTitleBlock: {
      minWidth: 0,
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
