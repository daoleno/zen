import React from "react";
import {
  Platform,
  StyleSheet,
  Text,
  TextInput,
  View,
  type TextInput as TextInputInstance,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import {
  COMPOSER_SUBMIT_BEHAVIOR,
  composerReturnKeyType,
} from "./composerInputBehavior";

interface CodexComposerInputProps {
  inputRef: React.RefObject<TextInputInstance | null>;
  draft: string;
  placeholder: string;
  editable: boolean;
  chrome: TerminalThemeChrome;
  onDraftChange(value: string): void;
  onInputFocus(): void;
  onInputBlur(): void;
}

export function CodexComposerInput({
  inputRef,
  draft,
  placeholder,
  editable,
  chrome,
  onDraftChange,
  onInputFocus,
  onInputBlur,
}: CodexComposerInputProps) {
  const draftEmpty = draft.length === 0;
  const multilineDraft = draft.includes("\n");
  const centerInputText = draftEmpty || !multilineDraft;

  return (
    <View collapsable={false} style={styles.inputWrap}>
      <TextInput
        ref={inputRef}
        style={[
          styles.input,
          centerInputText ? styles.inputCentered : null,
          { color: chrome.text },
        ]}
        value={draft}
        onChangeText={onDraftChange}
        placeholder=""
        accessibilityLabel={placeholder}
        selectionColor={chrome.accent}
        multiline
        editable={editable}
        textAlignVertical={centerInputText ? "center" : "top"}
        autoCorrect={false}
        autoCapitalize="none"
        autoComplete="off"
        spellCheck={false}
        keyboardType="default"
        disableFullscreenUI
        importantForAutofill="no"
        selectTextOnFocus={false}
        underlineColorAndroid="transparent"
        showSoftInputOnFocus
        returnKeyType={composerReturnKeyType(Platform.OS)}
        enterKeyHint="enter"
        submitBehavior={COMPOSER_SUBMIT_BEHAVIOR}
        blurOnSubmit={false}
        onFocus={onInputFocus}
        onBlur={onInputBlur}
      />
      {draftEmpty && placeholder ? (
        <View pointerEvents="none" style={styles.placeholderOverlay}>
          <Text
            numberOfLines={1}
            style={[styles.placeholderText, { color: chrome.textMuted }]}
          >
            {placeholder}
          </Text>
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  inputWrap: {
    flex: 1,
    minWidth: 0,
    minHeight: 42,
    maxHeight: 124,
    justifyContent: "center",
    position: "relative",
  },
  input: {
    width: "100%",
    minHeight: 44,
    maxHeight: 124,
    paddingLeft: 6,
    paddingRight: 8,
    paddingTop: Platform.OS === "android" ? 10 : 9,
    paddingBottom: Platform.OS === "android" ? 7 : 8,
    fontSize: 15,
    lineHeight: 23,
    fontFamily: Typography.chatFont,
    includeFontPadding: false,
  },
  inputCentered: {
    paddingTop: Platform.OS === "android" ? 0 : 8,
    paddingBottom: Platform.OS === "android" ? 0 : 8,
  },
  placeholderOverlay: {
    position: "absolute",
    top: 0,
    right: 8,
    bottom: 0,
    left: 6,
    justifyContent: "center",
  },
  placeholderText: {
    fontSize: 15,
    lineHeight: 23,
    fontFamily: Typography.chatFont,
    includeFontPadding: false,
  },
});
