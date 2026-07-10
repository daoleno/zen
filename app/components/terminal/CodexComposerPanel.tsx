import React from "react";
import {
  StyleSheet,
  View,
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
  uploading: boolean;
  sendEnabled: boolean;
  sending: boolean;
  sendIcon: React.ComponentProps<typeof ComposerSendButton>["icon"];
  sendLabel: string;
  sendElapsedStartedAt?: string;
  running: boolean;
  actionMenuExpanded: boolean;
  actionMenuButtonEnabled: boolean;
  showActionMenuButton: boolean;
  actionMenuIcon: "add" | "happy-outline";
  composerLayout: "chatgpt" | "telegram" | "classic";
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onDraftChange(value: string): void;
  onActionMenuPress(): void;
  onInputFocus(): void;
  onInputBlur(): void;
  onSendPress(): void;
}

export function CodexComposerPanel({
  inputRef,
  draft,
  placeholder,
  editable,
  focused,
  uploading,
  sendEnabled,
  sending,
  sendIcon,
  sendLabel,
  sendElapsedStartedAt,
  running,
  actionMenuExpanded,
  actionMenuButtonEnabled,
  showActionMenuButton,
  actionMenuIcon,
  composerLayout,
  chrome,
  theme,
  onDraftChange,
  onActionMenuPress,
  onInputFocus,
  onInputBlur,
  onSendPress,
}: CodexComposerPanelProps) {
  if (composerLayout === "chatgpt") {
    return (
      <View style={styles.chatgptRow}>
        <CodexComposerPanelFrame focused={focused} chrome={chrome} layout="chatgpt">
          {showActionMenuButton ? (
            <ComposerIconButton
              accessibilityLabel={
                actionMenuExpanded
                  ? "Hide composer actions"
                  : "Show composer actions"
              }
              icon={actionMenuExpanded ? "close" : "add"}
              chrome={chrome}
              disabled={!actionMenuButtonEnabled}
              iconColor={
                actionMenuExpanded
                  ? chrome.accent
                  : actionMenuButtonEnabled
                    ? chrome.textMuted
                    : chrome.textSubtle
              }
              onPress={onActionMenuPress}
            />
          ) : null}
          <CodexComposerInput
            inputRef={inputRef}
            draft={draft}
            placeholder={placeholder}
            editable={editable}
            chrome={chrome}
            onDraftChange={onDraftChange}
            onInputFocus={onInputFocus}
            onInputBlur={onInputBlur}
          />
        </CodexComposerPanelFrame>

        <ComposerSendButton
          accessibilityLabel={sendLabel}
          icon={sendIcon}
          chrome={chrome}
          theme={theme}
          enabled={sendEnabled}
          loading={sending}
          running={running}
          elapsedStartedAt={sendElapsedStartedAt}
          onPress={onSendPress}
          variant="chatgpt"
        />
      </View>
    );
  }

  // telegram + classic: one continuous dock (plus | input | send)
  return (
    <CodexComposerPanelFrame
      focused={focused}
      chrome={chrome}
      layout={composerLayout === "telegram" ? "telegram" : "classic"}
    >
      {showActionMenuButton ? (
        <ComposerIconButton
          accessibilityLabel={
            actionMenuExpanded
              ? "Hide composer actions"
              : "Show composer actions"
          }
          icon={actionMenuExpanded ? "close" : actionMenuIcon}
          chrome={chrome}
          loading={uploading}
          disabled={!actionMenuButtonEnabled}
          iconColor={
            actionMenuExpanded
              ? chrome.accent
              : actionMenuButtonEnabled
                ? chrome.textMuted
                : chrome.textSubtle
          }
          onPress={onActionMenuPress}
        />
      ) : null}

      <CodexComposerInput
        inputRef={inputRef}
        draft={draft}
        placeholder={placeholder}
        editable={editable}
        chrome={chrome}
        onDraftChange={onDraftChange}
        onInputFocus={onInputFocus}
        onInputBlur={onInputBlur}
      />

      <ComposerSendButton
        accessibilityLabel={sendLabel}
        icon={sendIcon}
        chrome={chrome}
        theme={theme}
        enabled={sendEnabled}
        loading={sending}
        running={running}
        elapsedStartedAt={sendElapsedStartedAt}
        onPress={onSendPress}
      />
    </CodexComposerPanelFrame>
  );
}

const styles = StyleSheet.create({
  chatgptRow: {
    flexDirection: "row",
    alignItems: "flex-end",
    gap: 8,
  },
});
