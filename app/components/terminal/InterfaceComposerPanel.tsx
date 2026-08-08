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
import type { ComposerModelControlPresentation } from "../../services/providers/sessionModelHelpers";
import { InterfaceComposerInput } from "./InterfaceComposerInput";
import { InterfaceComposerPanelFrame } from "./InterfaceComposerPanelFrame";
import { ComposerIconButton } from "./ComposerIconButton";
import { ComposerSendButton } from "./ComposerSendButton";
import { InterfaceComposerExpandingDock } from "./InterfaceComposerExpandingDock";
import {
  COMPOSER_ACTION_SLOT_WIDTH,
  COMPOSER_CHATGPT_DETACHED_ACTION_GAP,
} from "./composerActionSlot";

interface InterfaceComposerPanelProps {
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
  modelControl?: ComposerModelControlPresentation | null;
  composerLayout: "chatgpt" | "telegram" | "classic";
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onDraftChange(value: string): void;
  onActionMenuPress(): void;
  onModelControlPress?(): void;
  onInputFocus(): void;
  onInputBlur(): void;
  onSendPress(): void;
  onStopPress(): void;
}

export function InterfaceComposerPanel({
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
  modelControl,
  composerLayout,
  chrome,
  theme,
  onDraftChange,
  onActionMenuPress,
  onModelControlPress,
  onInputFocus,
  onInputBlur,
  onSendPress,
  onStopPress,
}: InterfaceComposerPanelProps) {
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
        <InterfaceComposerPanelFrame
          focused={focused}
          chrome={chrome}
          layout="chatgpt"
        >
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
          <InterfaceComposerInput
            inputRef={inputRef}
            draft={draft}
            placeholder={placeholder}
            editable={editable}
            chrome={chrome}
            onDraftChange={onDraftChange}
            onInputFocus={onInputFocus}
            onInputBlur={onInputBlur}
          />
        </InterfaceComposerPanelFrame>

        {actionButton}
      </View>
    );
  }

  if (composerLayout === "telegram") {
    return (
      <InterfaceComposerExpandingDock
        inputRef={inputRef}
        draft={draft}
        placeholder={placeholder}
        editable={editable}
        focused={focused}
        uploading={uploading}
        sendEnabled={sendEnabled}
        sending={sending}
        sendLabel={sendLabel}
        showStopButton={showStopButton}
        stopEnabled={stopEnabled}
        stopLabel={stopLabel}
        stopLoading={stopLoading}
        providerActivityStartedAt={providerActivityStartedAt}
        actionMenuExpanded={actionMenuExpanded}
        actionMenuButtonEnabled={actionMenuButtonEnabled}
        showActionMenuButton={showActionMenuButton}
        actionMenuIcon={actionMenuIcon}
        modelControl={modelControl}
        chrome={chrome}
        theme={theme}
        onDraftChange={onDraftChange}
        onActionMenuPress={onActionMenuPress}
        onModelControlPress={onModelControlPress}
        onInputFocus={onInputFocus}
        onInputBlur={onInputBlur}
        onSendPress={onSendPress}
        onStopPress={onStopPress}
      />
    );
  }

  // classic: one continuous dock (plus | input | send)
  return (
    <InterfaceComposerPanelFrame
      focused={focused}
      chrome={chrome}
      layout="classic"
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

      <InterfaceComposerInput
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
    </InterfaceComposerPanelFrame>
  );
}

const styles = StyleSheet.create({
  chatgptRow: {
    flexDirection: "row",
    alignItems: "flex-end",
    gap: COMPOSER_CHATGPT_DETACHED_ACTION_GAP,
  },
});
