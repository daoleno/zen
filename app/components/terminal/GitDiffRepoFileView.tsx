import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import {
  buildTerminalChrome,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { GitRepoFileContentPayload } from "../../services/gitDiff";
import { GitDiffCodeSnapshotPanel } from "./GitDiffCodeView";
import { GitDiffStateCard } from "./GitDiffStateCard";
import { withAlpha } from "./gitDiffColor";

interface GitDiffRepoFileViewProps {
  repoTitle: string;
  path: string;
  payload?: GitRepoFileContentPayload;
  loading: boolean;
  error: string | null;
  changed: boolean;
  theme: TerminalThemePalette;
  chrome: ReturnType<typeof buildTerminalChrome>;
  onBack(): void;
}

export function GitDiffRepoFileView({
  repoTitle,
  path,
  payload,
  loading,
  error,
  changed,
  theme,
  chrome,
  onBack,
}: GitDiffRepoFileViewProps) {
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
          <Text style={[styles.repoFileTitle, { color: chrome.text }]} numberOfLines={2}>
            {pathBaseName(path)}
          </Text>
          <Text style={[styles.repoFilePath, { color: chrome.textMuted }]} numberOfLines={1}>
            {[repoTitle, pathDirectoryName(path)].filter(Boolean).join(" / ")}
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
          <GitDiffStateCard
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
        />
      )}
    </View>
  );
}

function pathBaseName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? path : path.slice(index + 1);
}

function pathDirectoryName(path: string): string {
  const index = path.lastIndexOf("/");
  return index === -1 ? "" : path.slice(0, index);
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
  repoFileHeader: {
    minHeight: 48,
    paddingHorizontal: 10,
    paddingVertical: 6,
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 9,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  repoFileBack: {
    width: 32,
    height: 32,
    marginTop: 1,
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
    marginTop: 5,
  },
  changedPillText: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFontMedium,
  },
});
