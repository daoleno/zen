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
import {
  COMPOSER_ACTION_SLOT_WIDTH,
  COMPOSER_CHATGPT_DETACHED_ACTION_GAP,
} from "./composerActionSlot";

interface CodexComposerPanelProps {
  inputRef: React.RefObject<TextInputInstance | null>;
  draft: string;
  placeholder: string;
  editable: boolean;
  focused: boolean;
  uploading: boolean;
  sendEnabled: boolean;
  sending: boolean;
  sendLabel: string;
  showStopButton: boolean;
  stopEnabled: boolean;
  stopLabel: string;
  stopLoading: boolean;
  providerActivityStartedAt?: string;
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
  onStopPress(): void;
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
  sendLabel,
  showStopButton,
  stopEnabled,
  stopLabel,
  stopLoading,
  providerActivityStartedAt,
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
  onStopPress,
}: CodexComposerPanelProps) {
  const actionButton = (
    <ComposerSendButton
      accessibilityLabel={showStopButton ? stopLabel : sendLabel}
      icon={showStopButton ? "square" : "arrow-up"}
      chrome={chrome}
      theme={theme}
      enabled={showStopButton ? stopEnabled : sendEnabled}
      loading={showStopButton ? stopLoading : sending}
      running={showStopButton}
      elapsedStartedAt={providerActivityStartedAt}
      fixedWidth={COMPOSER_ACTION_SLOT_WIDTH}
      onPress={showStopButton ? onStopPress : onSendPress}
      variant={composerLayout === "chatgpt" ? "chatgpt" : "default"}
    />
  );
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

        {actionButton}
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

      {actionButton}
    </CodexComposerPanelFrame>
  );
}

const styles = StyleSheet.create({
  chatgptRow: {
    flexDirection: "row",
    alignItems: "flex-end",
    gap: COMPOSER_CHATGPT_DETACHED_ACTION_GAP,
  },
});
