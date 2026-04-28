import React from "react";
import {
  ActivityIndicator,
  FlatList,
  Modal,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import { Typography } from "../../constants/tokens";
import {
  buildTerminalChrome,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import type {
  GitDiffContentSnapshot,
  GitDiffFileInfo,
  GitDiffPatchPayload,
  GitDiffPatchSection,
  GitDiffStatusSnapshot,
  GitRepoBrowserEntry,
  GitRepoFileContentPayload,
} from "../../services/gitDiff";
import { describeGitDiffScope } from "../../services/gitDiff";

type SheetTab = "diff" | "browser";

interface GitDiffSheetProps {
  visible: boolean;
  theme: TerminalThemePalette;
  snapshot: GitDiffStatusSnapshot | null;
  loading: boolean;
  error: string | null;
  patchByPath: Record<string, GitDiffPatchPayload | undefined>;
  patchLoadingByPath: Record<string, boolean>;
  patchErrorByPath: Record<string, string | undefined>;
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
  patchByPath,
  patchLoadingByPath,
  patchErrorByPath,
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
  const [activeTab, setActiveTab] = React.useState<SheetTab>("diff");
  const [collapsedDiffPaths, setCollapsedDiffPaths] = React.useState<Set<string>>(
    () => new Set(),
  );

  const files = snapshot?.files ?? [];
  const changedPathSet = React.useMemo(
    () => new Set(files.map((file) => file.path)),
    [files],
  );
  const repoFileContent = repoFilePath ? repoFileByPath[repoFilePath] : undefined;
  const repoFileLoading = Boolean(
    repoFilePath && repoFileLoadingPath === repoFilePath && !repoFileContent,
  );

  React.useEffect(() => {
    if (visible) {
      return;
    }
    setActiveTab("diff");
    setCollapsedDiffPaths(new Set());
  }, [visible]);

  const allDiffFilesCollapsed = files.length > 0
    && files.every((file) => collapsedDiffPaths.has(file.path));

  const toggleDiffFile = React.useCallback((path: string) => {
    setCollapsedDiffPaths((previous) => {
      const next = new Set(previous);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  }, []);

  const collapseAllDiffFiles = React.useCallback(() => {
    setCollapsedDiffPaths(new Set(files.map((file) => file.path)));
  }, [files]);

  const expandAllDiffFiles = React.useCallback(() => {
    setCollapsedDiffPaths(new Set());
  }, []);

  const renderDiffFile = React.useCallback(
    ({ item }: { item: GitDiffFileInfo }) => (
      <DiffFileCard
        file={item}
        patch={patchByPath[item.path]}
        loading={Boolean(patchLoadingByPath[item.path])}
        error={patchErrorByPath[item.path] ?? null}
        expanded={!collapsedDiffPaths.has(item.path)}
        theme={theme}
        chrome={chrome}
        onToggle={() => toggleDiffFile(item.path)}
        onOpenFile={() => {
          setActiveTab("browser");
          onOpenRepoFile(item.path);
        }}
      />
    ),
    [
      chrome,
      collapsedDiffPaths,
      onOpenRepoFile,
      patchByPath,
      patchErrorByPath,
      patchLoadingByPath,
      theme,
      toggleDiffFile,
    ],
  );

  const renderRepoEntry = React.useCallback(
    ({ item }: { item: GitRepoBrowserEntry }) => (
      <RepoEntryRow
        entry={item}
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
      />
    ),
    [changedPathSet, chrome, onOpenRepoFile, onOpenRepoPath, theme],
  );

  const repoTitle = snapshot?.repo_name || repoBaseName(snapshot?.repo_root || "") || "repo";

  return (
    <Modal
      visible={visible}
      animationType="slide"
      onRequestClose={onClose}
      statusBarTranslucent
    >
      <SafeAreaView
        style={[styles.root, { backgroundColor: chrome.appBackground }]}
        edges={["top", "bottom"]}
      >
        <View
          style={[
            styles.sheet,
            {
              backgroundColor: chrome.surface,
              borderColor: chrome.border,
            },
          ]}
        >
          <View style={[styles.header, { borderBottomColor: chrome.border }]}>
            <TouchableOpacity
              style={[
                styles.iconButton,
                {
                  backgroundColor: chrome.surfaceMuted,
                  borderColor: chrome.border,
                },
              ]}
              onPress={onClose}
              activeOpacity={0.82}
            >
              <Ionicons name="close" size={18} color={chrome.textMuted} />
            </TouchableOpacity>

            <View style={styles.headerCopy}>
              <Text style={[styles.title, { color: chrome.text }]}>Git</Text>
              <Text style={[styles.subtitle, { color: chrome.textMuted }]} numberOfLines={1}>
                {buildSubtitle(snapshot)}
              </Text>
            </View>

            <TouchableOpacity
              style={[
                styles.iconButton,
                {
                  backgroundColor: chrome.surfaceMuted,
                  borderColor: chrome.border,
                },
              ]}
              onPress={onRefresh}
              activeOpacity={0.82}
            >
              {loading ? (
                <ActivityIndicator size="small" color={chrome.accent} />
              ) : (
                <Ionicons name="refresh" size={16} color={chrome.textMuted} />
              )}
            </TouchableOpacity>
          </View>

          {snapshot?.available ? (
            <View style={[styles.modeBar, { borderBottomColor: chrome.border }]}>
              <SegmentedControl
                options={[
                  { value: "diff", label: `Diff ${files.length}` },
                  { value: "browser", label: "Files" },
                ]}
                selectedValue={activeTab}
                onSelect={(value) => setActiveTab(value as SheetTab)}
                chrome={chrome}
                theme={theme}
              />
              <View style={styles.modeMetaRow}>
                <View style={styles.modeSummaryWrap}>
                  <Text style={[styles.modeSummary, { color: chrome.textMuted }]} numberOfLines={1}>
                    {buildCompactSummary(snapshot)}
                  </Text>
                </View>
                {activeTab === "diff" && !snapshot.clean && files.length > 0 ? (
                  <TouchableOpacity
                    style={[
                      styles.collapseAllButton,
                      {
                        backgroundColor: chrome.surfaceMuted,
                        borderColor: chrome.border,
                      },
                    ]}
                    onPress={allDiffFilesCollapsed ? expandAllDiffFiles : collapseAllDiffFiles}
                    activeOpacity={0.82}
                    hitSlop={{ top: 6, right: 6, bottom: 6, left: 6 }}
                  >
                    <Text style={[styles.collapseAllText, { color: chrome.textMuted }]}>
                      {allDiffFilesCollapsed ? "Expand all" : "Collapse all"}
                    </Text>
                  </TouchableOpacity>
                ) : null}
              </View>
            </View>
          ) : null}

          {error && !snapshot?.available ? (
            <View style={styles.contentPad}>
              <StateCard
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
              <StateCard
                icon="sync-outline"
                title="Inspecting repository"
                detail="zen is checking the current working tree."
                accent={theme.cursor}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
                busy
              />
            </View>
          ) : !snapshot?.available ? (
            <View style={styles.contentPad}>
              <StateCard
                icon="git-branch-outline"
                title={snapshot?.reason === "no_cwd" ? "No working directory yet" : "Not a git repository"}
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
          ) : activeTab === "browser" ? (
            repoFilePath ? (
              <RepoFileView
                key={`repo-file:${repoFilePath}`}
                repoTitle={repoTitle}
                path={repoFilePath}
                payload={repoFileContent}
                loading={repoFileLoading}
                error={repoFileError}
                changed={changedPathSet.has(repoFilePath)}
                theme={theme}
                chrome={chrome}
                onBack={onCloseRepoFile}
              />
            ) : (
              <FlatList
                key={`repo-browser-list:${repoBrowserPath || "root"}`}
                data={repoBrowserEntries}
                keyExtractor={(item) => `${item.kind}:${item.path}`}
                renderItem={renderRepoEntry}
                style={styles.fullList}
                contentContainerStyle={[
                  styles.browserContent,
                  repoBrowserEntries.length === 0 ? styles.fullListEmpty : null,
                ]}
                ListHeaderComponent={
                  <RepoBrowserHeader
                    repoTitle={repoTitle}
                    path={repoBrowserPath}
                    loading={repoBrowserLoading}
                    error={repoBrowserError}
                    theme={theme}
                    chrome={chrome}
                    onBack={onBackRepoPath}
                    canGoBack={repoBrowserPath !== ""}
                  />
                }
                ListEmptyComponent={
                  repoBrowserLoading ? null : (
                    <StateCard
                      icon="folder-open-outline"
                      title="No files here"
                      detail="This folder does not contain visible repository entries."
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
            )
          ) : snapshot.clean ? (
            <View style={styles.contentPad}>
              <StateCard
                icon="checkmark-done-outline"
                title="Working tree is clean"
                detail="No staged, unstaged, or untracked changes were found."
                accent={theme.green}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
              />
            </View>
          ) : (
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
          )}
        </View>
      </SafeAreaView>
    </Modal>
  );
}

function DiffFileCard({
  file,
  patch,
  loading,
  error,
  expanded,
  theme,
  chrome,
  onToggle,
  onOpenFile,
}: {
  file: GitDiffFileInfo;
  patch?: GitDiffPatchPayload;
  loading: boolean;
  error: string | null;
  expanded: boolean;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
  onToggle(): void;
  onOpenFile(): void;
}) {
  const sections = patch?.sections ?? [];

  return (
    <View
      style={[
        styles.diffCard,
        {
          backgroundColor: chrome.surfaceMuted,
          borderColor: chrome.border,
        },
      ]}
    >
      <TouchableOpacity
        style={[
          styles.diffCardHeader,
          { borderBottomColor: expanded ? chrome.border : "transparent" },
        ]}
        onPress={onToggle}
        activeOpacity={0.82}
      >
        <Ionicons
          name={expanded ? "chevron-down" : "chevron-forward"}
          size={16}
          color={chrome.textSubtle}
        />
        <View style={styles.diffCardTitleWrap}>
          <Text style={[styles.diffFileName, { color: chrome.text }]} numberOfLines={1}>
            {pathBaseName(file.path)}
          </Text>
          <Text style={[styles.diffFilePath, { color: chrome.textMuted }]} numberOfLines={1}>
            {buildFilePathMeta(file)}
          </Text>
        </View>
        <View style={styles.diffHeaderBadges}>
          <StatusPill file={file} theme={theme} compact />
          <TouchableOpacity
            style={[styles.diffOpenButton, { borderColor: chrome.border }]}
            onPress={onOpenFile}
            activeOpacity={0.82}
          >
            <Ionicons name="document-text-outline" size={14} color={chrome.textSubtle} />
          </TouchableOpacity>
        </View>
      </TouchableOpacity>

      {!expanded ? null : loading && sections.length === 0 ? (
        <View style={styles.inlineStateWrap}>
          <StateCard
            icon="sync-outline"
            title="Loading patch"
            detail="Fetching this file's staged and unstaged hunks."
            accent={theme.cursor}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
            busy
          />
        </View>
      ) : error ? (
        <View style={styles.inlineStateWrap}>
          <StateCard
            icon="warning-outline"
            title="Patch unavailable"
            detail={error}
            accent={theme.red}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
          />
        </View>
      ) : patch && sections.length === 0 ? (
        <View style={styles.inlineStateWrap}>
          <StateCard
            icon="information-circle-outline"
            title="No patch content"
            detail="Git reports this file as changed, but there are no hunks to display."
            accent={chrome.textSubtle}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
          />
        </View>
      ) : sections.length === 0 ? (
        <View style={styles.inlineStateWrap}>
          <StateCard
            icon="time-outline"
            title="Queued"
            detail="This patch will load shortly."
            accent={chrome.textSubtle}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
          />
        </View>
      ) : (
        <View style={styles.patchList}>
          {sections.map((section, index) => (
            <PatchSection
              key={`${file.path}:${section.scope}:${index}`}
              section={section}
              theme={theme}
              chrome={chrome}
            />
          ))}
        </View>
      )}
    </View>
  );
}

function PatchSection({
  section,
  theme,
  chrome,
}: {
  section: GitDiffPatchSection;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
}) {
  return (
    <View style={[styles.patchSection, { backgroundColor: chrome.surface, borderColor: chrome.border }]}>
      <View style={styles.patchHeader}>
        <Text style={[styles.patchTitle, { color: chrome.text }]}>
          {section.title}
        </Text>
        <Text style={[styles.patchScope, { color: chrome.textSubtle }]}>
          {section.scope}
        </Text>
      </View>
      <DiffBlock patch={section.patch} theme={theme} />
    </View>
  );
}

function RepoBrowserHeader({
  repoTitle,
  path,
  loading,
  error,
  theme,
  chrome,
  onBack,
  canGoBack,
}: {
  repoTitle: string;
  path: string;
  loading: boolean;
  error: string | null;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
  onBack(): void;
  canGoBack: boolean;
}) {
  return (
    <View style={styles.browserHeaderWrap}>
      <View
        style={[
          styles.browserPathBar,
          {
            backgroundColor: chrome.surfaceMuted,
            borderColor: chrome.border,
          },
        ]}
      >
        <TouchableOpacity
          style={[
            styles.browserBackButton,
            {
              backgroundColor: canGoBack ? chrome.surface : "transparent",
              borderColor: canGoBack ? chrome.border : "transparent",
              opacity: canGoBack ? 1 : 0.35,
            },
          ]}
          onPress={onBack}
          disabled={!canGoBack}
          activeOpacity={0.82}
        >
          <Ionicons name="arrow-up" size={16} color={chrome.textMuted} />
        </TouchableOpacity>
        <View style={styles.browserPathCopy}>
          <Text style={[styles.browserRepoTitle, { color: chrome.text }]} numberOfLines={1}>
            {repoTitle}
          </Text>
          <Text style={[styles.browserPathText, { color: chrome.textMuted }]} numberOfLines={1}>
            {path ? `/${path}` : "/"}
          </Text>
        </View>
        {loading ? <ActivityIndicator size="small" color={theme.cursor} /> : null}
      </View>

      {error ? (
        <StateCard
          icon="warning-outline"
          title="Could not load folder"
          detail={error}
          accent={theme.red}
          chromeText={chrome.text}
          chromeMuted={chrome.textMuted}
        />
      ) : null}
    </View>
  );
}

function RepoEntryRow({
  entry,
  changed,
  theme,
  chrome,
  onPress,
}: {
  entry: GitRepoBrowserEntry;
  changed: boolean;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
  onPress(): void;
}) {
  const isDirectory = entry.kind === "directory";

  return (
    <TouchableOpacity
      style={[
        styles.repoEntryRow,
        {
          backgroundColor: chrome.surfaceMuted,
          borderColor: chrome.border,
        },
      ]}
      onPress={onPress}
      activeOpacity={0.82}
    >
      <Ionicons
        name={isDirectory ? "folder-outline" : "document-text-outline"}
        size={16}
        color={isDirectory ? theme.yellow : chrome.textSubtle}
      />
      <View style={styles.repoEntryCopy}>
        <Text style={[styles.repoEntryName, { color: chrome.text }]} numberOfLines={1}>
          {entry.name}
        </Text>
      </View>
      {changed ? (
        <View style={[styles.changedPill, { backgroundColor: withAlpha(theme.cursor, 0.12) }]}>
          <Text style={[styles.changedPillText, { color: theme.cursor }]}>Changed</Text>
        </View>
      ) : null}
      <Ionicons
        name={isDirectory ? "chevron-forward" : "open-outline"}
        size={15}
        color={chrome.textSubtle}
      />
    </TouchableOpacity>
  );
}

function RepoFileView({
  repoTitle,
  path,
  payload,
  loading,
  error,
  changed,
  theme,
  chrome,
  onBack,
}: {
  repoTitle: string;
  path: string;
  payload?: GitRepoFileContentPayload;
  loading: boolean;
  error: string | null;
  changed: boolean;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
  onBack(): void;
}) {
  return (
    <View style={styles.repoFileRoot}>
      <View style={[styles.repoFileHeader, { borderBottomColor: chrome.border }]}>
        <TouchableOpacity
          style={[
            styles.repoFileBack,
            {
              backgroundColor: chrome.surfaceMuted,
              borderColor: chrome.border,
            },
          ]}
          onPress={onBack}
          activeOpacity={0.82}
        >
          <Ionicons name="chevron-back" size={17} color={chrome.textMuted} />
        </TouchableOpacity>
        <View style={styles.repoFileCopy}>
          <Text style={[styles.repoFileTitle, { color: chrome.text }]} numberOfLines={1}>
            {pathBaseName(path)}
          </Text>
          <Text style={[styles.repoFilePath, { color: chrome.textMuted }]} numberOfLines={1}>
            {repoTitle}/{pathDirectoryName(path)}
          </Text>
        </View>
        {changed ? (
          <View style={[styles.changedPill, { backgroundColor: withAlpha(theme.cursor, 0.12) }]}>
            <Text style={[styles.changedPillText, { color: theme.cursor }]}>Changed</Text>
          </View>
        ) : null}
      </View>

      {loading ? (
        <View style={styles.contentPad}>
          <StateCard
            icon="sync-outline"
            title="Loading file"
            detail="Fetching the current working tree snapshot."
            accent={theme.cursor}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
            busy
          />
        </View>
      ) : error ? (
        <View style={styles.contentPad}>
          <StateCard
            icon="warning-outline"
            title="Could not load file"
            detail={error}
            accent={theme.red}
            chromeText={chrome.text}
            chromeMuted={chrome.textMuted}
          />
        </View>
      ) : (
        <CodeSnapshotPanel
          snapshot={payload?.snapshot ?? null}
          chrome={chrome}
          theme={theme}
        />
      )}
    </View>
  );
}

function StatusPill({
  file,
  theme,
  compact = false,
}: {
  file: GitDiffFileInfo;
  theme: TerminalThemePalette;
  compact?: boolean;
}) {
  const color = statusTone(file, theme);
  return (
    <View
      style={[
        styles.statusPill,
        compact ? styles.statusPillCompact : null,
        { backgroundColor: withAlpha(color, 0.14) },
      ]}
    >
      <Text
        style={[
          styles.statusPillText,
          compact ? styles.statusPillTextCompact : null,
          { color },
        ]}
      >
        {statusLabel(file)}
      </Text>
    </View>
  );
}

function SegmentedControl({
  options,
  selectedValue,
  onSelect,
  chrome,
  theme,
}: {
  options: Array<{ value: string; label: string }>;
  selectedValue: string;
  onSelect(value: string): void;
  chrome: ReturnType<typeof buildTerminalChrome>;
  theme: TerminalThemePalette;
}) {
  return (
    <View
      style={[
        styles.segmented,
        {
          backgroundColor: chrome.surface,
          borderColor: chrome.border,
        },
      ]}
    >
      {options.map((option) => {
        const active = option.value === selectedValue;
        return (
          <TouchableOpacity
            key={option.value}
            style={[
              styles.segmentButton,
              active
                ? {
                    backgroundColor: withAlpha(theme.cursor, 0.16),
                  }
                : null,
            ]}
            onPress={() => onSelect(option.value)}
            activeOpacity={0.82}
          >
            <Text
              style={[
                styles.segmentButtonText,
                { color: active ? chrome.text : chrome.textMuted },
              ]}
            >
              {option.label}
            </Text>
          </TouchableOpacity>
        );
      })}
    </View>
  );
}

function CodeSnapshotPanel({
  snapshot,
  chrome,
  theme,
}: {
  snapshot: GitDiffContentSnapshot | null;
  chrome: ReturnType<typeof buildTerminalChrome>;
  theme: TerminalThemePalette;
}) {
  if (!snapshot?.exists || !snapshot.content) {
    return (
      <View style={styles.contentPad}>
        <StateCard
          icon="document-text-outline"
          title="File snapshot unavailable"
          detail={snapshot?.reason || "This file could not be read from the working tree."}
          accent={theme.cursor}
          chromeText={chrome.text}
          chromeMuted={chrome.textMuted}
        />
      </View>
    );
  }

  if (snapshot.binary) {
    return (
      <View style={styles.contentPad}>
        <StateCard
          icon="cube-outline"
          title="Binary file"
          detail="zen does not render binary file content."
          accent={theme.cursor}
          chromeText={chrome.text}
          chromeMuted={chrome.textMuted}
        />
      </View>
    );
  }

  const lines = snapshot.content.split("\n");
  return (
    <ScrollView
      style={styles.codeScroll}
      contentContainerStyle={styles.codeScrollContent}
      showsVerticalScrollIndicator={false}
      nestedScrollEnabled={false}
    >
      {snapshot.truncated ? (
        <View
          style={[
            styles.truncationBanner,
            {
              backgroundColor: withAlpha(theme.yellow, 0.1),
              borderColor: withAlpha(theme.yellow, 0.2),
            },
          ]}
        >
          <Text style={[styles.truncationText, { color: theme.yellow }]}>
            Showing the first {formatByteCount(snapshot.content.length)} of {formatByteCount(snapshot.byte_count)}.
          </Text>
        </View>
      ) : null}
      <ScrollView horizontal showsHorizontalScrollIndicator nestedScrollEnabled={false}>
        <View
          style={[
            styles.codeFrame,
            {
              backgroundColor: chrome.surfaceMuted,
              borderColor: chrome.border,
            },
          ]}
        >
          {lines.map((line, index) => (
            <View key={index} style={styles.codeRow}>
              <Text style={[styles.codeLineNumber, { color: chrome.textSubtle }]}>
                {index + 1}
              </Text>
              <Text style={[styles.codeLine, { color: chrome.text || theme.foreground }]}>
                {line || " "}
              </Text>
            </View>
          ))}
        </View>
      </ScrollView>
    </ScrollView>
  );
}

function DiffBlock({
  patch,
  theme,
}: {
  patch: string;
  theme: TerminalThemePalette;
}) {
  const chrome = React.useMemo(() => buildTerminalChrome(theme), [theme]);
  const lines = React.useMemo(() => patch.split("\n"), [patch]);

  return (
    <ScrollView horizontal showsHorizontalScrollIndicator nestedScrollEnabled={false}>
      <View style={styles.diffBlock}>
        {lines.map((line, index) => {
          const presentation = linePresentation(line, theme, chrome);
          return (
            <View
              key={index}
              style={[
                styles.diffLineWrap,
                presentation.backgroundColor
                  ? { backgroundColor: presentation.backgroundColor }
                  : null,
              ]}
            >
              <Text style={[styles.diffLineNumber, { color: chrome.textSubtle }]}>
                {index + 1}
              </Text>
              <Text style={[styles.diffLine, { color: presentation.color }]}>
                {line || " "}
              </Text>
            </View>
          );
        })}
      </View>
    </ScrollView>
  );
}

function StateCard({
  icon,
  title,
  detail,
  accent,
  chromeText,
  chromeMuted,
  busy = false,
}: {
  icon: keyof typeof Ionicons.glyphMap;
  title: string;
  detail: string;
  accent: string;
  chromeText: string;
  chromeMuted: string;
  busy?: boolean;
}) {
  return (
    <View
      style={[
        styles.stateCard,
        { borderColor: withAlpha(accent, 0.24), backgroundColor: withAlpha(accent, 0.08) },
      ]}
    >
      {busy ? (
        <ActivityIndicator size="small" color={accent} />
      ) : (
        <Ionicons name={icon} size={18} color={accent} />
      )}
      <Text style={[styles.stateTitle, { color: chromeText }]}>{title}</Text>
      <Text style={[styles.stateDetail, { color: chromeMuted }]}>{detail}</Text>
    </View>
  );
}

function buildSubtitle(snapshot: GitDiffStatusSnapshot | null): string {
  if (!snapshot?.available) {
    return "Diff and files";
  }
  if (snapshot.repo_name && snapshot.branch) {
    return `${snapshot.repo_name} · ${snapshot.branch}`;
  }
  return snapshot.repo_name || "Repository";
}

function buildCompactSummary(snapshot: GitDiffStatusSnapshot): string {
  if (snapshot.clean) {
    return "working tree clean";
  }

  const parts = [
    `${snapshot.file_count} changed`,
    `${snapshot.staged_file_count} staged`,
    `${snapshot.unstaged_file_count} unstaged`,
  ];
  if (snapshot.untracked_file_count > 0) {
    parts.push(`${snapshot.untracked_file_count} untracked`);
  }
  if (snapshot.additions > 0 || snapshot.deletions > 0) {
    parts.push(`+${snapshot.additions} -${snapshot.deletions}`);
  }
  return parts.join(" · ");
}

function buildFilePathMeta(file: GitDiffFileInfo): string {
  if (file.old_path) {
    return `${describeGitDiffScope(file)} · ${file.old_path} -> ${file.path}`;
  }
  const directory = pathDirectoryName(file.path);
  return [describeGitDiffScope(file), directory].filter(Boolean).join(" · ");
}

function pathBaseName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? path : path.slice(index + 1);
}

function pathDirectoryName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? "" : path.slice(0, index);
}

function repoBaseName(path: string): string {
  const trimmed = path.replace(/\/+$/, "");
  if (!trimmed) return "";
  return pathBaseName(trimmed);
}

function statusLabel(file: GitDiffFileInfo): string {
  switch (file.status) {
    case "added":
      return "Added";
    case "deleted":
      return "Deleted";
    case "renamed":
      return "Renamed";
    case "copied":
      return "Copied";
    case "conflict":
      return "Conflict";
    case "untracked":
      return "Untracked";
    case "modified":
      return "Modified";
    default:
      return "Changed";
  }
}

function statusTone(file: GitDiffFileInfo, theme: TerminalThemePalette): string {
  switch (file.status) {
    case "added":
    case "untracked":
      return theme.green;
    case "deleted":
      return theme.red;
    case "renamed":
    case "copied":
      return theme.blue;
    case "conflict":
      return theme.magenta;
    case "modified":
      return theme.yellow;
    default:
      return theme.cursor;
  }
}

function linePresentation(
  line: string,
  theme: TerminalThemePalette,
  chrome: ReturnType<typeof buildTerminalChrome>,
): { color: string; backgroundColor?: string } {
  if (line.startsWith("@@")) {
    return {
      color: theme.yellow,
      backgroundColor: withAlpha(theme.yellow, 0.1),
    };
  }
  if (line.startsWith("+") && !line.startsWith("+++")) {
    return {
      color: theme.green,
      backgroundColor: withAlpha(theme.green, 0.08),
    };
  }
  if (line.startsWith("-") && !line.startsWith("---")) {
    return {
      color: theme.red,
      backgroundColor: withAlpha(theme.red, 0.08),
    };
  }
  if (line.startsWith("diff --git") || line.startsWith("index ")) {
    return {
      color: chrome.textMuted,
      backgroundColor: withAlpha(chrome.textMuted, 0.06),
    };
  }
  if (
    line.startsWith("rename from ")
    || line.startsWith("rename to ")
    || line.startsWith("new file mode")
    || line.startsWith("deleted file mode")
  ) {
    return {
      color: theme.blue,
      backgroundColor: withAlpha(theme.blue, 0.08),
    };
  }
  if (line.startsWith("+++ ") || line.startsWith("--- ")) {
    return {
      color: theme.cursor,
      backgroundColor: withAlpha(theme.cursor, 0.08),
    };
  }
  return { color: chrome.text };
}

function formatByteCount(bytes: number): string {
  if (bytes >= 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }
  if (bytes >= 1024) {
    return `${Math.round(bytes / 1024)} KB`;
  }
  return `${bytes} B`;
}

function withAlpha(hex: string, alpha: number): string {
  const normalized = hex.replace("#", "").trim();
  if (!/^[0-9a-fA-F]{6}$/.test(normalized)) {
    return hex;
  }

  const red = Number.parseInt(normalized.slice(0, 2), 16);
  const green = Number.parseInt(normalized.slice(2, 4), 16);
  const blue = Number.parseInt(normalized.slice(4, 6), 16);
  return `rgba(${red}, ${green}, ${blue}, ${Math.min(Math.max(alpha, 0), 1)})`;
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
  },
  sheet: {
    flex: 1,
    borderWidth: 0,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 10,
    paddingTop: 5,
    paddingBottom: 6,
    gap: 8,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  headerCopy: {
    flex: 1,
    minWidth: 0,
  },
  title: {
    fontSize: 19,
    lineHeight: 24,
    fontFamily: Typography.uiFontMedium,
  },
  subtitle: {
    marginTop: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
  },
  iconButton: {
    width: 31,
    height: 31,
    borderRadius: 10,
    alignItems: "center",
    justifyContent: "center",
    borderWidth: StyleSheet.hairlineWidth,
  },
  modeBar: {
    paddingHorizontal: 10,
    paddingVertical: 6,
    gap: 4,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  modeSummary: {
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
    flexShrink: 1,
  },
  modeSummaryWrap: {
    flex: 1,
    minWidth: 0,
    paddingRight: 4,
  },
  modeMetaRow: {
    minHeight: 24,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  collapseAllButton: {
    minHeight: 28,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
    paddingVertical: 4,
    alignItems: "center",
    justifyContent: "center",
    flexShrink: 0,
  },
  collapseAllText: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFontMedium,
  },
  segmented: {
    minHeight: 34,
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    padding: 3,
    flexDirection: "row",
    gap: 3,
  },
  segmentButton: {
    flex: 1,
    minHeight: 26,
    borderRadius: 9,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 8,
  },
  segmentButtonText: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  contentPad: {
    flex: 1,
    paddingHorizontal: 14,
    paddingVertical: 14,
  },
  fullList: {
    flex: 1,
  },
  fullListEmpty: {
    flexGrow: 1,
  },
  diffContent: {
    paddingHorizontal: 8,
    paddingTop: 8,
    paddingBottom: 18,
    gap: 8,
  },
  diffCard: {
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  diffCardHeader: {
    minHeight: 46,
    paddingHorizontal: 9,
    paddingVertical: 6,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
  },
  diffCardTitleWrap: {
    flex: 1,
    minWidth: 0,
  },
  diffOpenButton: {
    width: 28,
    height: 28,
    borderRadius: 9,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  diffFileName: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.terminalFontBold,
  },
  diffFilePath: {
    marginTop: 2,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
  },
  diffHeaderBadges: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  patchList: {
    padding: 5,
    gap: 5,
  },
  patchSection: {
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  patchHeader: {
    paddingHorizontal: 8,
    paddingVertical: 5,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 10,
  },
  patchTitle: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  patchScope: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFont,
    textTransform: "uppercase",
    letterSpacing: 0.5,
  },
  browserContent: {
    paddingHorizontal: 8,
    paddingTop: 8,
    paddingBottom: 20,
    gap: 3,
  },
  browserHeaderWrap: {
    gap: 8,
    marginBottom: 2,
  },
  browserPathBar: {
    minHeight: 44,
    borderRadius: 11,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 9,
    paddingVertical: 6,
    flexDirection: "row",
    alignItems: "center",
    gap: 9,
  },
  browserBackButton: {
    width: 30,
    height: 30,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  browserPathCopy: {
    flex: 1,
    minWidth: 0,
  },
  browserRepoTitle: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
  browserPathText: {
    marginTop: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.terminalFont,
  },
  repoEntryRow: {
    minHeight: 34,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 8,
    paddingVertical: 4,
    flexDirection: "row",
    alignItems: "center",
    gap: 9,
  },
  repoEntryCopy: {
    flex: 1,
    minWidth: 0,
  },
  repoEntryName: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  repoFileRoot: {
    flex: 1,
  },
  repoFileHeader: {
    minHeight: 48,
    paddingHorizontal: 10,
    paddingVertical: 6,
    flexDirection: "row",
    alignItems: "center",
    gap: 9,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  repoFileBack: {
    width: 32,
    height: 32,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  repoFileCopy: {
    flex: 1,
    minWidth: 0,
  },
  repoFileTitle: {
    fontSize: 14,
    lineHeight: 18,
    fontFamily: Typography.terminalFontBold,
  },
  repoFilePath: {
    marginTop: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.terminalFont,
  },
  changedPill: {
    borderRadius: 999,
    paddingHorizontal: 7,
    paddingVertical: 3,
  },
  changedPillText: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFontMedium,
  },
  statusPill: {
    borderRadius: 999,
    paddingHorizontal: 8,
    paddingVertical: 5,
  },
  statusPillCompact: {
    paddingHorizontal: 7,
    paddingVertical: 3,
  },
  statusPillText: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFontMedium,
  },
  statusPillTextCompact: {
    fontSize: 9,
    lineHeight: 11,
  },
  inlineStateWrap: {
    padding: 8,
  },
  stateCard: {
    borderRadius: 14,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingVertical: 12,
    alignItems: "flex-start",
    gap: 6,
  },
  stateTitle: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
  },
  stateDetail: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
  codeScroll: {
    flex: 1,
  },
  codeScrollContent: {
    paddingHorizontal: 8,
    paddingTop: 8,
    paddingBottom: 20,
  },
  truncationBanner: {
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingVertical: 9,
    marginBottom: 10,
  },
  truncationText: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
  codeFrame: {
    minWidth: "100%",
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  codeRow: {
    minHeight: 18,
    flexDirection: "row",
    alignItems: "center",
    paddingRight: 12,
  },
  codeLineNumber: {
    width: 36,
    textAlign: "right",
    paddingRight: 8,
    fontSize: 10,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  codeLine: {
    fontSize: 10,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  diffBlock: {
    minWidth: "100%",
  },
  diffLineWrap: {
    minHeight: 18,
    flexDirection: "row",
    alignItems: "center",
    paddingRight: 12,
  },
  diffLineNumber: {
    width: 36,
    textAlign: "right",
    paddingRight: 8,
    fontSize: 10,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  diffLine: {
    fontSize: 10,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
});
