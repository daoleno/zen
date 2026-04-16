import React from "react";
import {
  ActivityIndicator,
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
  GitDiffFileInfo,
  GitDiffPatchPayload,
  GitDiffStatusSnapshot,
} from "../../services/gitDiff";
import { describeGitDiffScope } from "../../services/gitDiff";

interface GitDiffSheetProps {
  visible: boolean;
  theme: TerminalThemePalette;
  snapshot: GitDiffStatusSnapshot | null;
  loading: boolean;
  error: string | null;
  expandedPath: string | null;
  patchLoadingPath: string | null;
  patchByPath: Record<string, GitDiffPatchPayload | undefined>;
  onClose(): void;
  onRefresh(): void;
  onSelectPath(path: string): void;
}

export function GitDiffSheet({
  visible,
  theme,
  snapshot,
  loading,
  error,
  expandedPath,
  patchLoadingPath,
  patchByPath,
  onClose,
  onRefresh,
  onSelectPath,
}: GitDiffSheetProps) {
  const chrome = React.useMemo(() => buildTerminalChrome(theme), [theme]);

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
              <Text style={[styles.title, { color: chrome.text }]}>Git Diff</Text>
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

          <ScrollView
            style={styles.content}
            contentContainerStyle={styles.contentContainer}
            showsVerticalScrollIndicator={false}
          >
            {error && !snapshot?.available ? (
              <StateCard
                icon="warning-outline"
                title="Could not load git diff"
                detail={error}
                accent={theme.red}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
              />
            ) : loading && !snapshot ? (
              <StateCard
                icon="sync-outline"
                title="Inspecting repository"
                detail="zen is checking the current working tree for local changes."
                accent={theme.cursor}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
                busy
              />
            ) : !snapshot?.available ? (
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
            ) : snapshot.clean ? (
              <StateCard
                icon="checkmark-done-outline"
                title="Working tree is clean"
                detail="No staged, unstaged, or untracked changes were found for this repository."
                accent={theme.green}
                chromeText={chrome.text}
                chromeMuted={chrome.textMuted}
              />
            ) : (
              <>
                <View
                  style={[
                    styles.countStrip,
                    {
                      backgroundColor: chrome.surfaceMuted,
                      borderColor: chrome.border,
                    },
                  ]}
                >
                  <Text style={[styles.countStripLabel, { color: chrome.textMuted }]}>
                    Local change volume
                  </Text>
                  <View style={styles.countStripValues}>
                    <Text style={[styles.countStripValue, { color: theme.green }]}>
                      +{snapshot.additions}
                    </Text>
                    <Text style={[styles.countStripValue, { color: theme.red }]}>
                      -{snapshot.deletions}
                    </Text>
                  </View>
                </View>

                {(snapshot.files ?? []).map((file) => {
                  const expanded = expandedPath === file.path;
                  const patch = patchByPath[file.path];
                  const patchLoading = patchLoadingPath === file.path;

                  return (
                    <View
                      key={file.path}
                      style={[
                        styles.fileCard,
                        {
                          backgroundColor: chrome.surfaceMuted,
                          borderColor: expanded ? chrome.borderStrong : chrome.border,
                        },
                      ]}
                    >
                      <TouchableOpacity
                        style={styles.fileButton}
                        onPress={() => onSelectPath(file.path)}
                        activeOpacity={0.84}
                      >
                        <View style={styles.fileLead}>
                          <StatusPill file={file} theme={theme} />
                          <View style={styles.fileCopy}>
                            <Text style={[styles.filePath, { color: chrome.text }]} numberOfLines={1}>
                              {file.path}
                            </Text>
                            <Text
                              style={[styles.fileMeta, { color: chrome.textMuted }]}
                              numberOfLines={1}
                            >
                              {buildFileMeta(file)}
                            </Text>
                          </View>
                        </View>

                        <Ionicons
                          name={expanded ? "chevron-up" : "chevron-down"}
                          size={16}
                          color={chrome.textMuted}
                        />
                      </TouchableOpacity>

                      {expanded ? (
                        <View style={styles.patchWrap}>
                          {patchLoading ? (
                            <View style={styles.patchLoading}>
                              <ActivityIndicator size="small" color={chrome.accent} />
                              <Text style={[styles.patchLoadingText, { color: chrome.textMuted }]}>
                                Loading patch…
                              </Text>
                            </View>
                          ) : patch ? (
                            patch.sections.map((section) => (
                              <View
                                key={`${file.path}:${section.scope}`}
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
                                  <Text style={[styles.sectionScope, { color: chrome.textSubtle }]}>
                                    {section.scope}
                                  </Text>
                                </View>
                                <DiffBlock patch={section.patch} theme={theme} />
                              </View>
                            ))
                          ) : (
                            <Text style={[styles.patchEmpty, { color: chrome.textMuted }]}>
                              No patch content available for this file.
                            </Text>
                          )}
                        </View>
                      ) : null}
                    </View>
                  );
                })}
              </>
            )}
          </ScrollView>
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

function StatusPill({
  file,
  theme,
}: {
  file: GitDiffFileInfo;
  theme: TerminalThemePalette;
}) {
  const tone = statusTone(file, theme);
  return (
    <View
      style={[
        styles.statusPill,
        { backgroundColor: withAlpha(tone, 0.16) },
      ]}
    >
      <Text style={[styles.statusPillText, { color: tone }]}>
        {statusLabel(file)}
      </Text>
    </View>
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
    <View style={styles.diffBlock}>
      {lines.map((line, index) => {
        const presentation = linePresentation(line, theme, chrome);

        return (
          <View
            key={`${index}:${line}`}
            style={[
              styles.diffLineWrap,
              presentation.backgroundColor ? { backgroundColor: presentation.backgroundColor } : null,
            ]}
          >
            <Text style={[styles.diffLine, { color: presentation.color }]}>
              {line || " "}
            </Text>
          </View>
        );
      })}
    </View>
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
    return "Open local changes without disrupting the active shell.";
  }
  if (snapshot.repo_name && snapshot.branch) {
    return `${snapshot.repo_name} · ${snapshot.branch}`;
  }
  return "Open local changes without disrupting the active shell.";
}

function buildFileMeta(file: GitDiffFileInfo): string {
  if (file.old_path) {
    return `${describeGitDiffScope(file)} · ${file.old_path} -> ${file.path}`;
  }
  return `${describeGitDiffScope(file)} · ${statusLabel(file)}`;
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
  content: {
    flex: 1,
  },
  contentContainer: {
    paddingHorizontal: 14,
    paddingTop: 14,
    paddingBottom: 32,
    gap: 12,
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
  countStrip: {
    borderRadius: 18,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 14,
    paddingVertical: 12,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
  },
  countStripLabel: {
    fontSize: 12,
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
  fileCard: {
    borderRadius: 18,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  fileButton: {
    paddingHorizontal: 14,
    paddingVertical: 14,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
  },
  fileLead: {
    flex: 1,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    minWidth: 0,
  },
  statusPill: {
    borderRadius: 12,
    paddingHorizontal: 10,
    paddingVertical: 5,
  },
  statusPillText: {
    fontSize: 11,
    lineHeight: 13,
    fontFamily: Typography.uiFontMedium,
  },
  fileCopy: {
    flex: 1,
    minWidth: 0,
  },
  filePath: {
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.terminalFontBold,
  },
  fileMeta: {
    marginTop: 4,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
  patchWrap: {
    paddingHorizontal: 12,
    paddingBottom: 12,
    gap: 10,
  },
  patchLoading: {
    height: 64,
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
  },
  patchLoadingText: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFont,
  },
  patchEmpty: {
    paddingHorizontal: 6,
    paddingBottom: 6,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  section: {
    borderRadius: 16,
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
    paddingHorizontal: 10,
    paddingBottom: 10,
  },
  diffLineWrap: {
    borderRadius: 8,
    paddingHorizontal: 8,
    paddingVertical: 2,
    marginBottom: 2,
  },
  diffLine: {
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.terminalFont,
  },
});
