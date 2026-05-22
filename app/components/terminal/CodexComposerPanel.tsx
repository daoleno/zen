import React from "react";
import {
  Platform,
  StyleSheet,
  TextInput,
  View,
  type TextInput as TextInputInstance,
} from "react-native";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  ComposerIconButton,
  ComposerSendButton,
} from "./CodexChatControls";

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
    <View
      collapsable={false}
      style={[
        styles.panel,
        floating ? styles.floating : null,
        {
          backgroundColor: focused ? chrome.surfaceActive : chrome.surface,
          borderColor: focused ? chrome.borderStrong : chrome.border,
        },
      ]}
    >
      <ComposerIconButton
        accessibilityLabel="Upload file"
        icon="add"
        chrome={chrome}
        loading={uploading}
        disabled={!canAttach}
        iconColor={canAttach ? chrome.text : chrome.textSubtle}
        onPress={onUploadPress}
      />

      <View
        collapsable={false}
        onStartShouldSetResponderCapture={onInputStart}
        style={styles.inputWrap}
      >
        <TextInput
          ref={inputRef}
          style={[styles.input, { color: chrome.text }]}
          value={draft}
          onChangeText={onDraftChange}
          placeholder={placeholder}
          placeholderTextColor={chrome.textSubtle}
          selectionColor={chrome.accent}
          multiline
          editable={editable}
          textAlignVertical="top"
          autoCorrect={false}
          autoCapitalize="none"
          autoComplete="off"
          spellCheck={false}
          keyboardType={Platform.OS === "android" ? "visible-password" : "default"}
          disableFullscreenUI
          importantForAutofill="no"
          selectTextOnFocus={false}
          underlineColorAndroid="transparent"
          showSoftInputOnFocus
          returnKeyType="send"
          enterKeyHint="send"
          submitBehavior="submit"
          blurOnSubmit={false}
          onPressIn={onInputPress}
          onSubmitEditing={onSubmit}
          onFocus={onInputFocus}
          onBlur={onInputBlur}
        />
      </View>

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
    </View>
  );
}

const styles = StyleSheet.create({
  panel: {
    minHeight: 50,
    borderRadius: 25,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 4,
    paddingRight: 6,
    paddingVertical: 4,
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
  floating: {
    shadowColor: "#000000",
    shadowOffset: { width: 0, height: 8 },
    shadowOpacity: 0.16,
    shadowRadius: 18,
    elevation: 10,
  },
  inputWrap: {
    flex: 1,
    minHeight: 40,
    maxHeight: 110,
    justifyContent: "center",
  },
  input: {
    width: "100%",
    minHeight: 40,
    maxHeight: 110,
    paddingHorizontal: 4,
    paddingTop: 9,
    paddingBottom: 7,
    fontSize: 15,
    lineHeight: 21,
    fontFamily: Typography.uiFont,
    includeFontPadding: false,
  },
});
