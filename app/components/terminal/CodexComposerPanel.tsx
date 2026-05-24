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
import { ComposerIconButton } from "./ComposerIconButton";
import { ComposerSendButton } from "./ComposerSendButton";

interface CodexComposerPanelProps {
  inputRef: React.RefObject<TextInputInstance | null>;
  draft: string;
  placeholder: string;
  editable: boolean;
  focused: boolean;
  floating: boolean;
  uploading: boolean;
  sendEnabled: boolean;
  sending: boolean;
  sendIcon: React.ComponentProps<typeof ComposerSendButton>["icon"];
  sendLabel: string;
  running: boolean;
  actionMenuExpanded: boolean;
  actionMenuButtonEnabled: boolean;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onDraftChange(value: string): void;
  onActionMenuPress(): void;
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
  uploading,
  sendEnabled,
  sending,
  sendIcon,
  sendLabel,
  running,
  actionMenuExpanded,
  actionMenuButtonEnabled,
  chrome,
  theme,
  onDraftChange,
  onActionMenuPress,
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
      <ComposerIconButton
        accessibilityLabel={
          actionMenuExpanded ? "Hide composer actions" : "Show composer actions"
        }
        icon={actionMenuExpanded ? "close" : "add"}
        chrome={chrome}
        loading={uploading}
        disabled={!actionMenuButtonEnabled}
        iconColor={
          actionMenuExpanded
            ? chrome.accent
            : actionMenuButtonEnabled
              ? chrome.text
              : chrome.textSubtle
        }
        onPress={onActionMenuPress}
      />

      <CodexComposerInput
        inputRef={inputRef}
        draft={draft}
        placeholder={placeholder}
        editable={editable}
        busy={sending}
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
        running={running}
        onPress={onSendPress}
      />
    </CodexComposerPanelFrame>
  );
}
