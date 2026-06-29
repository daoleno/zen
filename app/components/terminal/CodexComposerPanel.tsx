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
  sendElapsedLabel?: string;
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
  onAttachPress?(): void;
  onEmojiToggle?(): void;
  emojiStripOpen?: boolean;
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
  uploading,
  sendEnabled,
  sending,
  sendIcon,
  sendLabel,
  sendElapsedLabel,
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
  onAttachPress,
  onEmojiToggle,
  emojiStripOpen = false,
  onInputPress,
  onInputFocus,
  onInputBlur,
  onInputStart,
  onSubmit,
  onSendPress,
}: CodexComposerPanelProps) {
  if (composerLayout === "chatgpt") {
    return (
      <View style={styles.chatgptRow}>
        <CodexComposerPanelFrame focused={focused} chrome={chrome} layout="chatgpt">
          {showActionMenuButton ? (
            <ComposerIconButton
              accessibilityLabel="Add attachment"
              icon="add"
              chrome={chrome}
              disabled={!actionMenuButtonEnabled}
              iconColor={
                actionMenuButtonEnabled ? chrome.textMuted : chrome.textSubtle
              }
              onPress={onAttachPress ?? onActionMenuPress}
            />
          ) : null}
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
        </CodexComposerPanelFrame>

        <ComposerSendButton
          accessibilityLabel={sendLabel}
          icon={sendIcon}
          chrome={chrome}
          theme={theme}
          enabled={sendEnabled}
          loading={sending}
          running={running}
          elapsedLabel={sendElapsedLabel}
          onPress={onSendPress}
          variant="chatgpt"
        />
      </View>
    );
  }

  if (composerLayout === "telegram") {
    return (
      <View style={styles.telegramRow}>
        {showActionMenuButton ? (
          <ComposerIconButton
            accessibilityLabel={emojiStripOpen ? "Hide emoji" : "Insert emoji"}
            icon={emojiStripOpen ? "close" : actionMenuIcon}
            chrome={chrome}
            disabled={!actionMenuButtonEnabled}
            iconColor={
              emojiStripOpen
                ? chrome.accent
                : actionMenuButtonEnabled
                  ? chrome.textMuted
                  : chrome.textSubtle
            }
            onPress={onEmojiToggle ?? onActionMenuPress}
          />
        ) : null}

        <CodexComposerPanelFrame focused={focused} chrome={chrome} layout="telegram">
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
          {showActionMenuButton ? (
            <ComposerIconButton
              accessibilityLabel="Attach files"
              icon="attach-outline"
              chrome={chrome}
              iconSize={22}
              loading={uploading}
              disabled={!actionMenuButtonEnabled}
              iconColor={
                actionMenuButtonEnabled ? chrome.textMuted : chrome.textSubtle
              }
              style={styles.attachButton}
              onPress={onAttachPress ?? onActionMenuPress}
            />
          ) : null}
        </CodexComposerPanelFrame>

        <ComposerSendButton
          accessibilityLabel={sendLabel}
          icon={sendIcon}
          chrome={chrome}
          theme={theme}
          enabled={sendEnabled}
          loading={sending}
          running={running}
          elapsedLabel={sendElapsedLabel}
          onPress={onSendPress}
        />
      </View>
    );
  }

  return (
    <CodexComposerPanelFrame focused={focused} chrome={chrome} layout="classic">
      {showActionMenuButton ? (
        <ComposerIconButton
          accessibilityLabel={
            actionMenuExpanded ? "Hide composer actions" : "Show composer actions"
          }
          icon={actionMenuExpanded ? "close" : actionMenuIcon}
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
      ) : null}

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
        running={running}
        elapsedLabel={sendElapsedLabel}
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
  telegramRow: {
    flexDirection: "row",
    alignItems: "flex-end",
    gap: 6,
  },
  attachButton: {
    width: 34,
    height: 34,
  },
});