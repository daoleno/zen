import React from "react";
import {
  type TextInput as TextInputInstance,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { CodexComposerInput } from "./CodexComposerInput";
import { CodexComposerPanelFrame } from "./CodexComposerPanelFrame";
import { CodexComposerUploadButton } from "./CodexComposerUploadButton";
import { ComposerSendButton } from "./ComposerSendButton";

interface CodexComposerPanelProps {
  inputRef: React.RefObject<TextInputInstance | null>;
  draft: string;
  placeholder: string;
  editable: boolean;
  focused: boolean;
  floating: boolean;
  canAttach: boolean;
  uploading: boolean;
  sendEnabled: boolean;
  sending: boolean;
  sendIcon: React.ComponentProps<typeof ComposerSendButton>["icon"];
  sendLabel: string;
  compactSendIcon: boolean;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onDraftChange(value: string): void;
  onUploadPress(): void;
  onInputPress(): void;
  onInputFocus(): void;
  onInputBlur(): void;
  onInputStart(): boolean;
  onSubmit(): void;
  onSendPress(): void;
}

export function CodexComposerPanel({
  inputRef,
  draft,
  placeholder,
  editable,
  focused,
  floating,
  canAttach,
  uploading,
  sendEnabled,
  sending,
  sendIcon,
  sendLabel,
  compactSendIcon,
  chrome,
  theme,
  onDraftChange,
  onUploadPress,
  onInputPress,
  onInputFocus,
  onInputBlur,
  onInputStart,
  onSubmit,
  onSendPress,
}: CodexComposerPanelProps) {
  return (
    <CodexComposerPanelFrame
      focused={focused}
      floating={floating}
      chrome={chrome}
    >
      <CodexComposerUploadButton
        canAttach={canAttach}
        uploading={uploading}
        chrome={chrome}
        onPress={onUploadPress}
      />

      <CodexComposerInput
        inputRef={inputRef}
        draft={draft}
        placeholder={placeholder}
        editable={editable}
        chrome={chrome}
        onDraftChange={onDraftChange}
        onInputPress={onInputPress}
        onInputFocus={onInputFocus}
        onInputBlur={onInputBlur}
        onInputStart={onInputStart}
        onSubmit={onSubmit}
      />

      <ComposerSendButton
        accessibilityLabel={sendLabel}
        icon={sendIcon}
        chrome={chrome}
        theme={theme}
        enabled={sendEnabled}
        loading={sending}
        compact={compactSendIcon}
        onPress={onSendPress}
      />
    </CodexComposerPanelFrame>
  );
}
