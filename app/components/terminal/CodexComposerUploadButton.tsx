import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ComposerIconButton } from "./ComposerIconButton";

interface CodexComposerUploadButtonProps {
  canAttach: boolean;
  uploading: boolean;
  chrome: TerminalThemeChrome;
  onPress(): void;
}

export function CodexComposerUploadButton({
  canAttach,
  uploading,
  chrome,
  onPress,
}: CodexComposerUploadButtonProps) {
  return (
    <ComposerIconButton
      accessibilityLabel="Upload file"
      icon="add"
      chrome={chrome}
      loading={uploading}
      disabled={!canAttach}
      iconColor={canAttach ? chrome.text : chrome.textSubtle}
      onPress={onPress}
    />
  );
}
