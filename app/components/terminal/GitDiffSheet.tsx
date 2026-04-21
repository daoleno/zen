import React from "react";
import {
  ActivityIndicator,
  Modal,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
  useWindowDimensions,
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
  GitDiffFileContentPayload,
  GitDiffFileInfo,
  GitDiffPatchPayload,
  GitDiffStatusSnapshot,
} from "../../services/gitDiff";
import { describeGitDiffScope } from "../../services/gitDiff";

type PreviewMode = "diff" | "current" | "base";
type MobilePane = "files" | "preview";

interface GitDiffSheetProps {
  visible: boolean;
  theme: TerminalThemePalette;
  snapshot: GitDiffStatusSnapshot | null;
  loading: boolean;
  error: string | null;
  selectedPath: string | null;
  patchLoadingPath: string | null;
  patchByPath: Record<string, GitDiffPatchPayload | undefined>;
  contentLoadingPath: string | null;
  contentByPath: Record<string, GitDiffFileContentPayload | undefined>;
  onClose(): void;
  onRefresh(): void;
  onSelectPath(path: string): void;
}

interface GitDiffFileGroup {
  directory: string;
  files: GitDiffFileInfo[];
}

export function GitDiffSheet({
  visible,
  theme,
  snapshot,
  loading,
  error,
  selectedPath,
  patchLoadingPath,
  patchByPath,
  contentLoadingPath,
  contentByPath,
  onClose,
  onRefresh,
  onSelectPath,
}: GitDiffSheetProps) {
  const chrome = React.useMemo(() => buildTerminalChrome(theme), [theme]);
  const { width: windowWidth, height: windowHeight } = useWindowDimensions();
  const isWideLayout = windowWidth >= 920;
  const isTabletLike = windowWidth >= 720;
  const isCompactHeight = windowHeight < 760;
  const [previewMode, setPreviewMode] = React.useState<PreviewMode>("diff");
  const [mobilePane, setMobilePane] = React.useState<MobilePane>("files");

  const files = snapshot?.files ?? [];
  const selectedFile = React.useMemo(
    () => files.find((file) => file.path === selectedPath) ?? null,
    [files, selectedPath],
  );
  const groupedFiles = React.useMemo(
    () => buildFileGroups(files),
    [files],
  );
  const selectedPatch = selectedPath ? patchByPath[selectedPath] : undefined;
  const selectedContent = selectedPath ? contentByPath[selectedPath] : undefined;
  const contentSnapshot =
    previewMode === "current"
      ? (selectedContent?.current ?? null)
      : previewMode === "base"
        ? (selectedContent?.base ?? null)
        : null;
  const previewLoading =
    (selectedPath && patchLoadingPath === selectedPath && previewMode === "diff")
    || (selectedPath && contentLoadingPath === selectedPath && previewMode !== "diff");

  React.useEffect(() => {
    if (!visible) {
      return;
    }
    if (isWideLayout) {
      setMobilePane("preview");
      return;
    }
    setMobilePane(selectedPath ? "preview" : "files");
  }, [isWideLayout, selectedPath, visible]);

  return (
    <Modal
      visible={visible}
      animationType="slide"
      onRequestClose={onClose}
      statusBarTranslucent
    >
      <SafeAreaView
        style={[
          styles.root,
          { backgroundColor: chrome.appBackground },
        ]}
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
          <View
            style={[
              styles.header,
              { borderBottomColor: chrome.border },
            ]}
          >
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
              <Text style={[styles.title, { color: chrome.text }]}>Git Diff Browser</Text>
              <Text style={[styles.subtitle, { color: chrome.textMuted }]}>
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
            <View
              style={[
                styles.summaryRow,
                { borderBottomColor: chrome.border },
              ]}
            >
              <SummaryChip
                label="repo"
                value={snapshot.repo_name || "repo"}
                backgroundColor={chrome.accentSoft}
                textColor={chrome.accent}
              />
              <SummaryChip
                label="files"
                value={`${snapshot.file_count}`}
                backgroundColor={withAlpha(theme.cursor, 0.14)}
                textColor={theme.cursor}
              />
              <SummaryChip
                label="branch"
                value={snapshot.branch || "detached"}
                backgroundColor={withAlpha(theme.blue, 0.14)}
                textColor={theme.blue}
              />
              <SummaryChip
                label="staged"
                value={`${snapshot.staged_file_count}`}
                backgroundColor={withAlpha(theme.green, 0.12)}
                textColor={theme.green}
              />
              <SummaryChip
                label="unstaged"
                value={`${snapshot.unstaged_file_count}`}
                backgroundColor={withAlpha(theme.yellow, 0.12)}
                textColor={theme.yellow}
              />
              <SummaryChip
                label="untracked"
                value={`${snapshot.untracked_file_count}`}
                backgroundColor={withAlpha(theme.magenta, 0.12)}
                textColor={theme.magenta}
              />
            </View>
          ) : null}

          {error && !snapshot?.available ? (
            <View style={styles.contentPad}>
              <StateCard
                icon="warning-outline"
                title="Could not load git diff"
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
                detail="zen is checking the current working tree for local changes."
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
                    ? "This terminal session has not reported a cwd yet, so zen cannot resolve a repository."
                    : "The current terminal cwd is outside a git repository. Move into a repo and refresh."
                }
                accent={chrome.textSubtle}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
              />
            </View>
          ) : snapshot.clean ? (
            <View style={styles.contentPad}>
              <StateCard
                icon="checkmark-done-outline"
                title="Working tree is clean"
                detail="No staged, unstaged, or untracked changes were found for this repository."
                accent={theme.green}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
              />
            </View>
          ) : (
            <View style={styles.workspaceRoot}>
              <View
                style={[
                  styles.countStrip,
                  {
                    backgroundColor: chrome.surfaceMuted,
                    borderColor: chrome.border,
                  },
                ]}
              >
                <View style={styles.countStripCopy}>
                  <Text style={[styles.countStripLabel, { color: chrome.textMuted }]}>
                    Local change volume
                  </Text>
                  <Text style={[styles.countStripMeta, { color: chrome.textSubtle }]}>
                    Browse files, patch context, and live code without leaving the shell.
                  </Text>
                </View>
                <View style={styles.countStripValues}>
                  <Text style={[styles.countStripValue, { color: theme.green }]}>
                    +{snapshot.additions}
                  </Text>
                  <Text style={[styles.countStripValue, { color: theme.red }]}>
                    -{snapshot.deletions}
                  </Text>
                </View>
              </View>

              {!isWideLayout ? (
                <View style={styles.mobileToolbar}>
                  <SegmentedControl
                    options={[
                      { value: "files", label: `Files ${files.length}` },
                      { value: "preview", label: selectedFile ? "Preview" : "Preview" },
                    ]}
                    selectedValue={mobilePane}
                    onSelect={(value) => setMobilePane(value as MobilePane)}
                    chrome={chrome}
                    theme={theme}
                    compact={isCompactHeight}
                  />
                </View>
              ) : null}

              <View
                style={[
                  styles.workspace,
                  isWideLayout ? styles.workspaceWide : styles.workspaceStacked,
                ]}
              >
                {(isWideLayout || mobilePane === "files") ? (
                  <View
                    style={[
                      styles.browserPane,
                      isWideLayout ? styles.browserPaneWide : styles.browserPaneStacked,
                      {
                        backgroundColor: chrome.surfaceMuted,
                        borderColor: chrome.border,
                      },
                    ]}
                  >
                    <View
                      style={[
                        styles.browserPaneHeader,
                        { borderBottomColor: chrome.border },
                      ]}
                    >
                      <View style={styles.browserPaneTitleWrap}>
                        <Text style={[styles.panelEyebrow, { color: chrome.textSubtle }]}>
                          Changed Files
                        </Text>
                        <Text style={[styles.panelTitle, { color: chrome.text }]}>
                          File browser
                        </Text>
                      </View>
                      <Text style={[styles.browserPaneMeta, { color: chrome.textMuted }]}>
                        {files.length} files
                      </Text>
                    </View>

                    <ScrollView
                      style={styles.browserScroll}
                      contentContainerStyle={[
                        styles.browserScrollContent,
                        isTabletLike ? styles.browserScrollContentTablet : null,
                      ]}
                      showsVerticalScrollIndicator={false}
                    >
                      {groupedFiles.map((group) => (
                        <View
                          key={group.directory}
                          style={[
                            styles.groupCard,
                            {
                              backgroundColor: chrome.surface,
                              borderColor: chrome.border,
                            },
                          ]}
                        >
                          <View style={styles.groupHeader}>
                            <View style={styles.groupHeaderLead}>
                              <Ionicons
                                name="folder-open-outline"
                                size={15}
                                color={chrome.textSubtle}
                              />
                              <Text style={[styles.groupHeaderText, { color: chrome.text }]}>
                                {group.directory}
                              </Text>
                            </View>
                            <Text style={[styles.groupHeaderCount, { color: chrome.textMuted }]}>
                              {group.files.length}
                            </Text>
                          </View>

                          {group.files.map((file) => {
                            const active = selectedPath === file.path;
                            return (
                              <TouchableOpacity
                                key={file.path}
                                style={[
                                  styles.fileRow,
                                  {
                                    backgroundColor: active
                                      ? withAlpha(theme.cursor, 0.16)
                                      : "transparent",
                                    borderColor: active
                                      ? withAlpha(theme.cursor, 0.26)
                                      : chrome.border,
                                  },
                                ]}
                                onPress={() => onSelectPath(file.path)}
                                activeOpacity={0.84}
                              >
                                <View style={styles.fileRowLead}>
                                  <View style={styles.fileRowIconWrap}>
                                    <Ionicons
                                      name="document-text-outline"
                                      size={16}
                                      color={active ? theme.cursor : chrome.textSubtle}
                                    />
                                  </View>
                                  <View style={styles.fileRowCopy}>
                                    <Text
                                      style={[
                                        styles.fileRowTitle,
                                        { color: active ? chrome.text : chrome.text },
                                      ]}
                                      numberOfLines={1}
                                    >
                                      {pathBaseName(file.path)}
                                    </Text>
                                    <Text
                                      style={[styles.fileRowMeta, { color: chrome.textMuted }]}
                                      numberOfLines={1}
                                    >
                                      {buildFileRowMeta(file)}
                                    </Text>
                                  </View>
                                </View>

                                <StatusPill file={file} theme={theme} compact />
                              </TouchableOpacity>
                            );
                          })}
                        </View>
                      ))}
                    </ScrollView>
                  </View>
                ) : null}

                {(isWideLayout || mobilePane === "preview") ? (
                  <View
                    style={[
                      styles.previewPane,
                      {
                        backgroundColor: chrome.surfaceMuted,
                        borderColor: chrome.border,
                      },
                    ]}
                  >
                    {selectedFile ? (
                      <>
                        <View
                          style={[
                            styles.previewHeader,
                            { borderBottomColor: chrome.border },
                          ]}
                        >
                          <View style={styles.previewHeaderCopy}>
                            <Text style={[styles.panelEyebrow, { color: chrome.textSubtle }]}>
                              Inspector
                            </Text>
                            <Text
                              style={[styles.previewPath, { color: chrome.text }]}
                              numberOfLines={isWideLayout ? 1 : 2}
                            >
                              {selectedFile.path}
                            </Text>
                            <Text style={[styles.previewMeta, { color: chrome.textMuted }]}>
                              {buildFileMeta(selectedFile)}
                            </Text>
                          </View>

                          <View style={styles.previewHeaderActions}>
                            <StatusPill file={selectedFile} theme={theme} />
                            <ScopeBadge
                              label={describeGitDiffScope(selectedFile)}
                              color={theme.cursor}
                            />
                          </View>
                        </View>

                        <View style={styles.previewToolbar}>
                          <SegmentedControl
                            options={[
                              { value: "diff", label: "Diff" },
                              { value: "current", label: "Working Tree" },
                              { value: "base", label: "Base" },
                            ]}
                            selectedValue={previewMode}
                            onSelect={(value) => setPreviewMode(value as PreviewMode)}
                            chrome={chrome}
                            theme={theme}
                            compact={!isWideLayout && isCompactHeight}
                          />

                          <View style={styles.previewStats}>
                            {previewMode === "diff" ? (
                              <MetaPill
                                label={selectedPatch?.sections.length ? `${selectedPatch.sections.length} sections` : "Patch"}
                                value={patchLoadingPath === selectedPath ? "Loading" : "Ready"}
                                chrome={chrome}
                                theme={theme}
                              />
                            ) : (
                              <ContentSnapshotMeta
                                snapshot={contentSnapshot}
                                chrome={chrome}
                                theme={theme}
                              />
                            )}
                          </View>
                        </View>

                        <View style={styles.previewBody}>
                          {previewLoading ? (
                            <View style={styles.inlineStateWrap}>
                              <StateCard
                                icon="sync-outline"
                                title="Loading file preview"
                                detail="zen is fetching the selected snapshot."
                                accent={theme.cursor}
                                chromeText={chrome.text}
                                chromeMuted={chrome.textMuted}
                                busy
                              />
                            </View>
                          ) : previewMode === "diff" ? (
                            selectedPatch?.sections.length ? (
                              <ScrollView
                                style={styles.previewScroll}
                                contentContainerStyle={styles.previewScrollContent}
                                showsVerticalScrollIndicator={false}
                              >
                                {selectedPatch.sections.map((section) => (
                                  <View
                                    key={`${selectedFile.path}:${section.scope}`}
                                    style={[
                                      styles.section,
                                      {
                                        backgroundColor: chrome.surface,
                                        borderColor: chrome.border,
                                      },
                                    ]}
                                  >
                                    <View style={styles.sectionHeader}>
                                      <Text style={[styles.sectionTitle, { color: chrome.text }]}>
                                        {section.title}
                                      </Text>
                                      <Text
                                        style={[styles.sectionScope, { color: chrome.textSubtle }]}
                                      >
                                        {section.scope}
                                      </Text>
                                    </View>
                                    <DiffBlock patch={section.patch} theme={theme} />
                                  </View>
                                ))}
                              </ScrollView>
                            ) : (
                              <View style={styles.inlineStateWrap}>
                                <StateCard
                                  icon="document-text-outline"
                                  title="No patch content"
                                  detail="This file does not expose a patch preview right now."
                                  accent={theme.cursor}
                                  chromeText={chrome.text}
                                  chromeMuted={chrome.textMuted}
                                />
                              </View>
                            )
                          ) : (
                            <CodeSnapshotPanel
                              snapshot={contentSnapshot}
                              chrome={chrome}
                              theme={theme}
                              mode={previewMode}
                            />
                          )}
                        </View>
                      </>
                    ) : (
                      <View style={styles.inlineStateWrap}>
                        <StateCard
                          icon="documents-outline"
                          title="Select a file"
                          detail={
                            isWideLayout
                              ? "Choose a changed file from the browser to inspect its diff or source snapshot."
                              : "Choose a file from the Files tab to inspect its diff or source snapshot."
                          }
                          accent={theme.cursor}
                          chromeText={chrome.text}
                          chromeMuted={chrome.textMuted}
                        />
                      </View>
                    )}
                  </View>
                ) : null}
              </View>
            </View>
          )}
        </View>
      </SafeAreaView>
    </Modal>
  );
}

function SummaryChip({
  label,
  value,
  backgroundColor,
  textColor,
}: {
  label: string;
  value: string;
  backgroundColor: string;
  textColor: string;
}) {
  return (
    <View style={[styles.summaryChip, { backgroundColor }]}>
      <Text style={[styles.summaryChipLabel, { color: textColor }]}>
        {label}
      </Text>
      <Text style={[styles.summaryChipValue, { color: textColor }]}>
        {value}
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
  compact = false,
}: {
  options: Array<{ value: string; label: string }>;
  selectedValue: string;
  onSelect(value: string): void;
  chrome: ReturnType<typeof buildTerminalChrome>;
  theme: TerminalThemePalette;
  compact?: boolean;
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
              compact ? styles.segmentButtonCompact : null,
              active
                ? {
                    backgroundColor: withAlpha(theme.cursor, 0.16),
                    borderColor: withAlpha(theme.cursor, 0.24),
                  }
                : {
                    borderColor: "transparent",
                  },
            ]}
            onPress={() => onSelect(option.value)}
            activeOpacity={0.84}
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

function StatusPill({
  file,
  theme,
  compact = false,
}: {
  file: GitDiffFileInfo;
  theme: TerminalThemePalette;
  compact?: boolean;
}) {
  const tone = statusTone(file, theme);
  return (
    <View
      style={[
        styles.statusPill,
        compact ? styles.statusPillCompact : null,
        { backgroundColor: withAlpha(tone, 0.16) },
      ]}
    >
      <Text style={[styles.statusPillText, { color: tone }]}>
        {statusLabel(file)}
      </Text>
    </View>
  );
}

function ScopeBadge({
  label,
  color,
}: {
  label: string;
  color: string;
}) {
  return (
    <View style={[styles.scopeBadge, { backgroundColor: withAlpha(color, 0.12) }]}>
      <Text style={[styles.scopeBadgeText, { color }]}>{label}</Text>
    </View>
  );
}

function MetaPill({
  label,
  value,
  chrome,
  theme,
}: {
  label: string;
  value: string;
  chrome: ReturnType<typeof buildTerminalChrome>;
  theme: TerminalThemePalette;
}) {
  return (
    <View
      style={[
        styles.metaPill,
        {
          backgroundColor: chrome.surface,
          borderColor: chrome.border,
        },
      ]}
    >
      <Text style={[styles.metaPillLabel, { color: chrome.textSubtle }]}>{label}</Text>
      <Text style={[styles.metaPillValue, { color: theme.cursor }]}>{value}</Text>
    </View>
  );
}

function ContentSnapshotMeta({
  snapshot,
  chrome,
  theme,
}: {
  snapshot: GitDiffContentSnapshot | null;
  chrome: ReturnType<typeof buildTerminalChrome>;
  theme: TerminalThemePalette;
}) {
  if (!snapshot) {
    return (
      <MetaPill
        label="Snapshot"
        value="Pending"
        chrome={chrome}
        theme={theme}
      />
    );
  }

  if (!snapshot.exists) {
    return (
      <MetaPill
        label={snapshot.label}
        value={snapshot.reason === "untracked" ? "No base" : "Unavailable"}
        chrome={chrome}
        theme={theme}
      />
    );
  }

  if (snapshot.binary) {
    return (
      <MetaPill
        label={snapshot.label}
        value="Binary"
        chrome={chrome}
        theme={theme}
      />
    );
  }

  const value = snapshot.line_count > 0
    ? `${snapshot.line_count} lines`
    : formatByteCount(snapshot.byte_count);

  return (
    <MetaPill
      label={snapshot.label}
      value={snapshot.truncated ? `${value} · cut` : value}
      chrome={chrome}
      theme={theme}
    />
  );
}

function CodeSnapshotPanel({
  snapshot,
  chrome,
  theme,
  mode,
}: {
  snapshot: GitDiffContentSnapshot | null;
  chrome: ReturnType<typeof buildTerminalChrome>;
  theme: TerminalThemePalette;
  mode: Exclude<PreviewMode, "diff">;
}) {
  if (!snapshot) {
    return (
      <View style={styles.inlineStateWrap}>
        <StateCard
          icon="document-outline"
          title="Preparing snapshot"
          detail="zen is waiting for the file snapshot to load."
          accent={theme.cursor}
          chromeText={chrome.text}
          chromeMuted={chrome.textMuted}
          busy
        />
      </View>
    );
  }

  if (!snapshot.exists) {
    return (
      <View style={styles.inlineStateWrap}>
        <StateCard
          icon={mode === "current" ? "document-outline" : "layers-outline"}
          title={mode === "current" ? "Working tree snapshot unavailable" : "Base snapshot unavailable"}
          detail={describeSnapshotMissing(snapshot, mode)}
          accent={theme.cursor}
          chromeText={chrome.text}
          chromeMuted={chrome.textMuted}
        />
      </View>
    );
  }

  if (snapshot.binary) {
    return (
      <View style={styles.inlineStateWrap}>
        <StateCard
          icon="cube-outline"
          title="Binary file"
          detail="This snapshot is binary, so zen keeps the inspector in metadata mode instead of rendering source text."
          accent={theme.cursor}
          chromeText={chrome.text}
          chromeMuted={chrome.textMuted}
        />
      </View>
    );
  }

  return (
    <View style={styles.codePanel}>
      <View style={styles.codePanelMeta}>
        <MetaPill
          label="Bytes"
          value={formatByteCount(snapshot.byte_count)}
          chrome={chrome}
          theme={theme}
        />
        <MetaPill
          label="Lines"
          value={`${snapshot.line_count || 0}`}
          chrome={chrome}
          theme={theme}
        />
        {snapshot.truncated ? (
          <MetaPill
            label="Preview"
            value="Truncated"
            chrome={chrome}
            theme={theme}
          />
        ) : null}
      </View>

      <CodeBlock
        content={snapshot.content || ""}
        chrome={chrome}
        theme={theme}
      />
    </View>
  );
}

function CodeBlock({
  content,
  chrome,
  theme,
}: {
  content: string;
  chrome: ReturnType<typeof buildTerminalChrome>;
  theme: TerminalThemePalette;
}) {
  const lines = React.useMemo(() => content.split("\n"), [content]);

  return (
    <ScrollView
      style={styles.previewScroll}
      contentContainerStyle={styles.previewScrollContent}
      showsVerticalScrollIndicator={false}
    >
      <ScrollView horizontal showsHorizontalScrollIndicator>
        <View
          style={[
            styles.codeFrame,
            {
              backgroundColor: chrome.surface,
              borderColor: chrome.border,
            },
          ]}
        >
          {lines.map((line, index) => (
            <View key={`${index}:${line}`} style={styles.codeRow}>
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
    <ScrollView horizontal showsHorizontalScrollIndicator>
      <View style={styles.diffBlock}>
        {lines.map((line, index) => {
          const presentation = linePresentation(line, theme, chrome);

          return (
            <View
              key={`${index}:${line}`}
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
    return "Browse changed files, patch context, and live code without disrupting the active shell.";
  }
  if (snapshot.repo_name && snapshot.branch) {
    return `${snapshot.repo_name} · ${snapshot.branch}`;
  }
  return "Browse changed files, patch context, and live code without disrupting the active shell.";
}

function buildFileGroups(files: GitDiffFileInfo[]): GitDiffFileGroup[] {
  const grouped = new Map<string, GitDiffFileInfo[]>();

  for (const file of files) {
    const directory = pathDirectory(file.path);
    if (!grouped.has(directory)) {
      grouped.set(directory, []);
    }
    grouped.get(directory)?.push(file);
  }

  return Array.from(grouped.entries())
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([directory, groupFiles]) => ({
      directory,
      files: [...groupFiles].sort((left, right) => left.path.localeCompare(right.path)),
    }));
}

function buildFileMeta(file: GitDiffFileInfo): string {
  if (file.old_path) {
    return `${describeGitDiffScope(file)} · ${file.old_path} -> ${file.path}`;
  }
  return `${describeGitDiffScope(file)} · ${statusLabel(file)}`;
}

function buildFileRowMeta(file: GitDiffFileInfo): string {
  if (file.old_path) {
    return `${describeGitDiffScope(file)} · ${pathBaseName(file.old_path)} -> ${pathBaseName(file.path)}`;
  }
  return `${describeGitDiffScope(file)} · ${statusLabel(file)}`;
}

function pathDirectory(path: string): string {
  const index = path.lastIndexOf("/");
  if (index === -1) {
    return "repo root";
  }
  return path.slice(0, index);
}

function pathBaseName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? path : path.slice(index + 1);
}

function describeSnapshotMissing(
  snapshot: GitDiffContentSnapshot,
  mode: Exclude<PreviewMode, "diff">,
): string {
  if (mode === "base" && snapshot.reason === "untracked") {
    return "Untracked files do not have a base revision yet.";
  }
  if (mode === "base") {
    return "This file does not have content in the current HEAD base snapshot.";
  }
  return "This file is not present in the current working tree snapshot.";
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
    paddingHorizontal: 14,
    paddingTop: 8,
    paddingBottom: 12,
    gap: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  headerCopy: {
    flex: 1,
    minWidth: 0,
  },
  title: {
    fontSize: 24,
    lineHeight: 30,
    fontFamily: Typography.uiFontMedium,
  },
  subtitle: {
    marginTop: 4,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  iconButton: {
    width: 36,
    height: 36,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
    borderWidth: StyleSheet.hairlineWidth,
  },
  summaryRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 8,
    paddingHorizontal: 14,
    paddingTop: 12,
    paddingBottom: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  summaryChip: {
    minWidth: 84,
    borderRadius: 14,
    paddingHorizontal: 12,
    paddingVertical: 9,
  },
  summaryChipLabel: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFont,
    textTransform: "uppercase",
    letterSpacing: 0.4,
    opacity: 0.9,
  },
  summaryChipValue: {
    marginTop: 4,
    fontSize: 13,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  contentPad: {
    flex: 1,
    paddingHorizontal: 14,
    paddingVertical: 14,
  },
  workspaceRoot: {
    flex: 1,
    paddingHorizontal: 14,
    paddingTop: 14,
    paddingBottom: 18,
    gap: 12,
  },
  countStrip: {
    borderRadius: 18,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 14,
    paddingVertical: 12,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 14,
  },
  countStripCopy: {
    flex: 1,
    minWidth: 0,
  },
  countStripLabel: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  countStripMeta: {
    marginTop: 4,
    fontSize: 11,
    lineHeight: 16,
    fontFamily: Typography.uiFont,
  },
  countStripValues: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  countStripValue: {
    fontSize: 15,
    lineHeight: 18,
    fontFamily: Typography.terminalFontBold,
  },
  mobileToolbar: {
    gap: 10,
  },
  workspace: {
    flex: 1,
    gap: 12,
  },
  workspaceWide: {
    flexDirection: "row",
  },
  workspaceStacked: {
    flexDirection: "column",
  },
  browserPane: {
    borderRadius: 22,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
    minHeight: 0,
  },
  browserPaneWide: {
    width: 336,
    flexShrink: 0,
  },
  browserPaneStacked: {
    flex: 1,
  },
  previewPane: {
    flex: 1,
    borderRadius: 22,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
    minHeight: 0,
  },
  browserPaneHeader: {
    paddingHorizontal: 14,
    paddingTop: 14,
    paddingBottom: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "flex-end",
    justifyContent: "space-between",
    gap: 12,
  },
  browserPaneTitleWrap: {
    minWidth: 0,
    flex: 1,
  },
  browserPaneMeta: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
  panelEyebrow: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFont,
    textTransform: "uppercase",
    letterSpacing: 0.5,
  },
  panelTitle: {
    marginTop: 5,
    fontSize: 18,
    lineHeight: 22,
    fontFamily: Typography.uiFontMedium,
  },
  browserScroll: {
    flex: 1,
  },
  browserScrollContent: {
    paddingHorizontal: 12,
    paddingTop: 12,
    paddingBottom: 20,
    gap: 10,
  },
  browserScrollContentTablet: {
    paddingBottom: 28,
  },
  groupCard: {
    borderRadius: 18,
    borderWidth: StyleSheet.hairlineWidth,
    padding: 10,
    gap: 8,
  },
  groupHeader: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 10,
    paddingHorizontal: 4,
  },
  groupHeaderLead: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    flex: 1,
    minWidth: 0,
  },
  groupHeaderText: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  groupHeaderCount: {
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.terminalFont,
  },
  fileRow: {
    borderRadius: 16,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
    paddingVertical: 10,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 10,
  },
  fileRowLead: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    flex: 1,
    minWidth: 0,
  },
  fileRowIconWrap: {
    width: 28,
    alignItems: "center",
    justifyContent: "center",
  },
  fileRowCopy: {
    flex: 1,
    minWidth: 0,
  },
  fileRowTitle: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.terminalFontBold,
  },
  fileRowMeta: {
    marginTop: 3,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
  previewHeader: {
    paddingHorizontal: 14,
    paddingTop: 14,
    paddingBottom: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
    gap: 12,
  },
  previewHeaderCopy: {
    minWidth: 0,
  },
  previewHeaderActions: {
    flexDirection: "row",
    alignItems: "center",
    flexWrap: "wrap",
    gap: 8,
  },
  previewPath: {
    marginTop: 4,
    fontSize: 16,
    lineHeight: 22,
    fontFamily: Typography.terminalFontBold,
  },
  previewMeta: {
    marginTop: 4,
    fontSize: 11,
    lineHeight: 16,
    fontFamily: Typography.uiFont,
  },
  previewToolbar: {
    paddingHorizontal: 14,
    paddingTop: 12,
    gap: 10,
  },
  previewStats: {
    flexDirection: "row",
    alignItems: "center",
    flexWrap: "wrap",
    gap: 8,
  },
  previewBody: {
    flex: 1,
    minHeight: 0,
    paddingHorizontal: 14,
    paddingTop: 12,
    paddingBottom: 14,
  },
  previewScroll: {
    flex: 1,
    minHeight: 0,
  },
  previewScrollContent: {
    paddingBottom: 24,
    gap: 10,
  },
  segmented: {
    flexDirection: "row",
    alignItems: "center",
    borderRadius: 16,
    borderWidth: StyleSheet.hairlineWidth,
    padding: 4,
    gap: 4,
  },
  segmentButton: {
    flex: 1,
    minHeight: 38,
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 12,
  },
  segmentButtonCompact: {
    minHeight: 34,
    paddingHorizontal: 10,
  },
  segmentButtonText: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
    textAlign: "center",
  },
  statusPill: {
    borderRadius: 12,
    paddingHorizontal: 10,
    paddingVertical: 5,
  },
  statusPillCompact: {
    paddingHorizontal: 8,
    paddingVertical: 4,
  },
  statusPillText: {
    fontSize: 11,
    lineHeight: 13,
    fontFamily: Typography.uiFontMedium,
  },
  scopeBadge: {
    borderRadius: 12,
    paddingHorizontal: 10,
    paddingVertical: 5,
  },
  scopeBadgeText: {
    fontSize: 11,
    lineHeight: 13,
    fontFamily: Typography.uiFontMedium,
  },
  metaPill: {
    borderRadius: 14,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
    paddingVertical: 8,
  },
  metaPillLabel: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFont,
    textTransform: "uppercase",
    letterSpacing: 0.4,
  },
  metaPillValue: {
    marginTop: 4,
    fontSize: 12,
    lineHeight: 15,
    fontFamily: Typography.terminalFontBold,
  },
  section: {
    borderRadius: 18,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  sectionHeader: {
    paddingHorizontal: 12,
    paddingTop: 12,
    paddingBottom: 8,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
  },
  sectionTitle: {
    fontSize: 13,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  sectionScope: {
    fontSize: 11,
    lineHeight: 13,
    fontFamily: Typography.terminalFont,
    textTransform: "uppercase",
  },
  diffBlock: {
    minWidth: "100%",
    paddingHorizontal: 10,
    paddingBottom: 10,
  },
  diffLineWrap: {
    borderRadius: 8,
    paddingHorizontal: 8,
    paddingVertical: 4,
    marginBottom: 2,
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 10,
  },
  diffLineNumber: {
    minWidth: 32,
    fontSize: 11,
    lineHeight: 18,
    fontFamily: Typography.terminalFont,
    textAlign: "right",
  },
  diffLine: {
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.terminalFont,
  },
  codePanel: {
    flex: 1,
    minHeight: 0,
    gap: 12,
  },
  codePanelMeta: {
    flexDirection: "row",
    alignItems: "center",
    flexWrap: "wrap",
    gap: 8,
  },
  codeFrame: {
    minWidth: "100%",
    borderRadius: 18,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
    paddingVertical: 8,
  },
  codeRow: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 12,
    paddingHorizontal: 12,
    paddingVertical: 2,
  },
  codeLineNumber: {
    minWidth: 34,
    fontSize: 11,
    lineHeight: 20,
    fontFamily: Typography.terminalFont,
    textAlign: "right",
  },
  codeLine: {
    fontSize: 12,
    lineHeight: 20,
    fontFamily: Typography.terminalFont,
  },
  inlineStateWrap: {
    flex: 1,
    justifyContent: "center",
  },
  stateCard: {
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 18,
    paddingVertical: 24,
    borderRadius: 20,
    borderWidth: 1,
    minHeight: 180,
  },
  stateTitle: {
    marginTop: 12,
    fontSize: 18,
    lineHeight: 22,
    fontFamily: Typography.uiFontMedium,
    textAlign: "center",
  },
  stateDetail: {
    marginTop: 8,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
    textAlign: "center",
  },
});
