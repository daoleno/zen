import React from "react";
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { Ionicons, MaterialCommunityIcons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { ActiveAttachmentUpload } from "../../services/uploads";
import { buildAttachmentUploadPresentation } from "./attachmentUploadPresentation";
import { TerminalAccessoryIconButton } from "./TerminalAccessoryIconButton";
import { TerminalAccessoryShortcutList } from "./TerminalAccessoryShortcutList";

interface TerminalAccessoryControlsProps {
  uploadEnabled: boolean;
  activeUpload: ActiveAttachmentUpload | null;
  keyboardVisible: boolean;
  ctrlArmed: boolean;
  chrome: TerminalThemeChrome;
  onUploadPress(): void;
  onCancelUpload(): void;
  onKeyboardToggle(): void;
  onCtrlToggle(): void;
  onHoldPressIn(sequence: string): void;
  onHoldPressOut(): void;
  onTapSequence(sequence: string): void;
}

export function TerminalAccessoryControls({
  uploadEnabled,
  activeUpload,
  keyboardVisible,
  ctrlArmed,
  chrome,
  onUploadPress,
  onCancelUpload,
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
      {activeUpload ? (
        <TerminalUploadStatus
          upload={activeUpload}
          chrome={chrome}
          onCancel={onCancelUpload}
        />
      ) : (
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
      )}

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

function TerminalUploadStatus({
  upload,
  chrome,
  onCancel,
}: {
  upload: ActiveAttachmentUpload;
  chrome: TerminalThemeChrome;
  onCancel(): void;
}) {
  const presentation = buildAttachmentUploadPresentation(
    upload.name,
    upload.progress,
  );
  return (
    <View
      style={[
        styles.upload,
        { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
      ]}
    >
      <View
        accessible
        accessibilityLabel={presentation.accessibilityLabel}
        accessibilityRole="progressbar"
        accessibilityValue={presentation.accessibilityValue}
        style={styles.uploadStatus}
      >
        <Text
          numberOfLines={1}
          style={[styles.uploadName, { color: chrome.text }]}
        >
          {upload.name}
        </Text>
        <View style={styles.uploadProgressRow}>
          {presentation.progressPercent === null ? null : (
            <View
              style={[
                styles.uploadTrack,
                { backgroundColor: chrome.disabledSurface },
              ]}
            >
              <View
                style={[
                  styles.uploadFill,
                  {
                    backgroundColor: chrome.accent,
                    width: `${presentation.progressPercent}%`,
                  },
                ]}
              />
            </View>
          )}
          <Text
            numberOfLines={1}
            style={[styles.uploadProgress, { color: chrome.textMuted }]}
          >
            {presentation.progressLabel}
          </Text>
        </View>
      </View>
      <Pressable
        accessibilityLabel={presentation.cancelAccessibilityLabel}
        accessibilityRole="button"
        hitSlop={6}
        onPress={onCancel}
        style={({ pressed }) => [
          styles.uploadCancel,
          { borderColor: chrome.border, opacity: pressed ? 0.68 : 1 },
        ]}
      >
        <Text style={[styles.uploadCancelText, { color: chrome.accent }]}>
          {presentation.cancelLabel}
        </Text>
      </Pressable>
    </View>
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
  upload: {
    width: 286,
    height: 44,
    marginRight: 4,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 8,
    paddingLeft: 10,
    paddingRight: 4,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  uploadStatus: {
    flex: 1,
    minWidth: 0,
    gap: 1,
  },
  uploadName: {
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFontMedium,
  },
  uploadProgressRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  uploadProgress: {
    flex: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
  },
  uploadTrack: {
    width: 36,
    height: 3,
    borderRadius: 2,
    overflow: "hidden",
  },
  uploadFill: {
    height: 3,
    borderRadius: 2,
  },
  uploadCancel: {
    minWidth: 54,
    minHeight: 36,
    borderLeftWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 6,
  },
  uploadCancelText: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
});
