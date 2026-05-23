import React from "react";
import {
  ScrollView,
  StyleSheet,
} from "react-native";
import { Ionicons, MaterialCommunityIcons } from "@expo/vector-icons";
import type {
  TerminalThemeChrome,
} from "../../constants/terminalThemes";
import { TerminalAccessoryIconButton } from "./TerminalAccessoryIconButton";
import { TerminalAccessoryShortcutList } from "./TerminalAccessoryShortcutList";

interface TerminalAccessoryControlsProps {
  uploadEnabled: boolean;
  keyboardVisible: boolean;
  ctrlArmed: boolean;
  chrome: TerminalThemeChrome;
  onUploadPress(): void;
  onKeyboardToggle(): void;
  onCtrlToggle(): void;
  onHoldPressIn(sequence: string): void;
  onHoldPressOut(): void;
  onTapSequence(sequence: string): void;
}

export function TerminalAccessoryControls({
  uploadEnabled,
  keyboardVisible,
  ctrlArmed,
  chrome,
  onUploadPress,
  onKeyboardToggle,
  onCtrlToggle,
  onHoldPressIn,
  onHoldPressOut,
  onTapSequence,
}: TerminalAccessoryControlsProps) {
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      keyboardShouldPersistTaps="handled"
      style={styles.shortcutRow}
      contentContainerStyle={styles.shortcutRowContent}
    >
      <TerminalAccessoryIconButton
        accessibilityLabel="Attach"
        onPress={onUploadPress}
        disabled={!uploadEnabled}
      >
        <Ionicons
          name="attach-outline"
          size={16}
          color={uploadEnabled ? chrome.textMuted : chrome.textSubtle}
        />
      </TerminalAccessoryIconButton>

      <TerminalAccessoryIconButton
        accessibilityLabel={keyboardVisible ? "Hide keyboard" : "Show keyboard"}
        accessibilityState={{ selected: keyboardVisible }}
        onPress={onKeyboardToggle}
      >
        <MaterialCommunityIcons
          name="keyboard-outline"
          size={18}
          color={keyboardVisible ? chrome.accent : chrome.textMuted}
        />
      </TerminalAccessoryIconButton>

      <TerminalAccessoryShortcutList
        chrome={chrome}
        ctrlArmed={ctrlArmed}
        onCtrlToggle={onCtrlToggle}
        onHoldPressIn={onHoldPressIn}
        onHoldPressOut={onHoldPressOut}
        onTapSequence={onTapSequence}
      />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  shortcutRow: {
    paddingTop: 3,
    paddingBottom: 3,
  },
  shortcutRowContent: {
    paddingLeft: 12,
    paddingRight: 12,
  },
});
