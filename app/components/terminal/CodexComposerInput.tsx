import React from "react";
import {
  Platform,
  StyleSheet,
  TextInput,
  View,
  type TextInput as TextInputInstance,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";

interface CodexComposerInputProps {
  inputRef: React.RefObject<TextInputInstance | null>;
  draft: string;
  placeholder: string;
  editable: boolean;
  chrome: TerminalThemeChrome;
  onDraftChange(value: string): void;
  onInputPress(): void;
  onInputFocus(): void;
  onInputBlur(): void;
  onInputStart(): boolean;
  onSubmit(): void;
}

export function CodexComposerInput({
  inputRef,
  draft,
  placeholder,
  editable,
  chrome,
  onDraftChange,
  onInputPress,
  onInputFocus,
  onInputBlur,
  onInputStart,
  onSubmit,
}: CodexComposerInputProps) {
  return (
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
  );
}

const styles = StyleSheet.create({
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
