import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { AgentKind } from "../../services/agentPresentation";
import type { StoredCodexRenderMode } from "../../services/storage";
import { AgentKindIcon } from "./AgentKindIcon";
import { TerminalTopBarChromeButton } from "./TerminalTopBarChromeButton";

export interface TerminalTopBarGitDiffPresentation {
  accessibilityLabel: string;
  backgroundColor: string;
  iconColor: string;
  additionsText?: string;
  additionsColor?: string;
  deletionsText?: string;
  deletionsColor?: string;
}

export interface TerminalTopBarProps {
  title: string;
  kind: AgentKind;
  backgroundColor: string;
  chrome: TerminalThemeChrome;
  menuAnchorRef: React.RefObject<View | null>;
  codexRenderMode: StoredCodexRenderMode;
  gitDiffDisabled: boolean;
  gitDiffPresentation: TerminalTopBarGitDiffPresentation;
  isCodexAgent: boolean;
  onBack(): void;
  onOpenPicker(): void;
  onOpenGitDiff(): void;
  onOpenMenu(): void;
  onToggleCodexRenderMode(): void;
}

export function TerminalTopBar({
  title,
  kind,
  backgroundColor,
  chrome,
  menuAnchorRef,
  codexRenderMode,
  gitDiffDisabled,
  gitDiffPresentation,
  isCodexAgent,
  onBack,
  onOpenPicker,
  onOpenGitDiff,
  onOpenMenu,
  onToggleCodexRenderMode,
}: TerminalTopBarProps) {
  return (
    <View
      style={[
        styles.topBar,
        { backgroundColor },
      ]}
    >
      <TerminalTopBarChromeButton
        accessibilityLabel="Back"
        chrome={chrome}
        icon="chevron-back"
        onPress={onBack}
      />

      <TouchableOpacity
        accessibilityLabel="Open session switcher"
        accessibilityRole="button"
        style={styles.titleButton}
        onPress={onOpenPicker}
        activeOpacity={0.78}
      >
        <View style={styles.titleIconWrap}>
          <AgentKindIcon kind={kind} size={15} />
        </View>
        <Text style={[styles.title, { color: chrome.text }]} numberOfLines={1}>
          {title}
        </Text>
      </TouchableOpacity>

      <TouchableOpacity
        accessibilityLabel={gitDiffPresentation.accessibilityLabel}
        accessibilityRole="button"
        accessibilityState={{ disabled: gitDiffDisabled }}
        disabled={gitDiffDisabled}
        style={[
          styles.gitDiffButton,
          { backgroundColor: gitDiffPresentation.backgroundColor },
          gitDiffDisabled ? styles.disabled : null,
        ]}
        activeOpacity={0.75}
        onPress={onOpenGitDiff}
      >
        <Ionicons
          name="git-branch-outline"
          size={13}
          color={gitDiffPresentation.iconColor}
        />
        {gitDiffPresentation.additionsText || gitDiffPresentation.deletionsText ? (
          <View style={styles.gitDiffStats}>
            {gitDiffPresentation.additionsText ? (
              <Text
                style={[
                  styles.gitDiffStat,
                  { color: gitDiffPresentation.additionsColor || chrome.textMuted },
                ]}
                numberOfLines={1}
              >
                {gitDiffPresentation.additionsText}
              </Text>
            ) : null}
            {gitDiffPresentation.deletionsText ? (
              <Text
                style={[
                  styles.gitDiffStat,
                  { color: gitDiffPresentation.deletionsColor || chrome.textMuted },
                ]}
                numberOfLines={1}
              >
                {gitDiffPresentation.deletionsText}
              </Text>
            ) : null}
          </View>
        ) : null}
      </TouchableOpacity>

      {isCodexAgent ? (
        <TerminalTopBarChromeButton
          accessibilityLabel={
            codexRenderMode === "chat"
              ? "Open terminal renderer"
              : "Open Codex chat renderer"
          }
          chrome={chrome}
          icon={codexRenderMode === "chat" ? "terminal-outline" : "chatbubble-outline"}
          onPress={onToggleCodexRenderMode}
        />
      ) : null}

      <View ref={menuAnchorRef} collapsable={false}>
        <TerminalTopBarChromeButton
          accessibilityLabel="Terminal actions"
          chrome={chrome}
          icon="ellipsis-vertical"
          onPress={onOpenMenu}
        />
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  topBar: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 8,
    paddingTop: 3,
    paddingBottom: 5,
  },
  titleButton: {
    flex: 1,
    minWidth: 0,
    height: 32,
    marginHorizontal: 6,
    paddingHorizontal: 6,
    borderRadius: 8,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  titleIconWrap: {
    width: 16,
    height: 18,
    alignItems: "center",
    justifyContent: "center",
  },
  title: {
    flex: 1,
    minWidth: 0,
    flexShrink: 1,
    fontSize: 14,
    lineHeight: 17,
    fontFamily: Typography.uiFontMedium,
    includeFontPadding: false,
    textAlignVertical: "center",
    transform: [{ translateY: -0.5 }],
  },
  gitDiffButton: {
    minWidth: 32,
    maxWidth: 94,
    height: 32,
    paddingHorizontal: 6,
    borderRadius: 8,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 4,
  },
  gitDiffStats: {
    minWidth: 0,
    flexDirection: "row",
    alignItems: "center",
    gap: 3,
  },
  gitDiffStat: {
    flexShrink: 1,
    fontSize: 11,
    lineHeight: 13,
    fontFamily: Typography.uiFontMedium,
    includeFontPadding: false,
  },
  disabled: {
    opacity: 0.48,
  },
});
