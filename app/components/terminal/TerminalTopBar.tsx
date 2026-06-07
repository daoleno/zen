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
  delegated?: boolean;
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
  delegated,
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
        {delegated ? (
          <View style={[styles.brainBadge, { borderColor: chrome.border }]}>
            <Text style={[styles.brainBadgeText, { color: chrome.textMuted }]}>
              Brain
            </Text>
          </View>
        ) : null}
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
          size={20}
          color={gitDiffPresentation.iconColor}
        />
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
  brainBadge: {
    height: 16,
    paddingHorizontal: 5,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 4,
    alignItems: "center",
    justifyContent: "center",
  },
  brainBadgeText: {
    fontSize: 9,
    lineHeight: 11,
    fontFamily: Typography.uiFontMedium,
    includeFontPadding: false,
  },
  gitDiffButton: {
    width: 32,
    height: 32,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
  },
  disabled: {
    opacity: 0.48,
  },
});
