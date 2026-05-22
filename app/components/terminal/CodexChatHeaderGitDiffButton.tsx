import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ChatHeaderIconButton } from "./ChatHeaderIconButton";

export interface CodexChatHeaderGitDiff {
  tone: "clean" | "dirty" | "error" | "loading";
  onPress(): void;
}

interface CodexChatHeaderGitDiffButtonProps {
  gitDiff: CodexChatHeaderGitDiff;
  chrome: TerminalThemeChrome;
}

export function CodexChatHeaderGitDiffButton({
  gitDiff,
  chrome,
}: CodexChatHeaderGitDiffButtonProps) {
  return (
    <ChatHeaderIconButton
      icon={gitDiff.tone === "loading" ? "sync-outline" : "git-branch-outline"}
      accessibilityLabel="Git diff"
      chrome={chrome}
      color={gitDiff.tone === "dirty" ? chrome.accent : chrome.textMuted}
      onPress={gitDiff.onPress}
    />
  );
}
