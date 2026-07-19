import React from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { ActiveAttachmentUpload } from "../../services/uploads";
import { buildAttachmentUploadPresentation } from "./attachmentUploadPresentation";
import { ComposerLoadingDots } from "./ComposerLoadingDots";

interface InterfaceComposerUploadingChipProps {
  upload: ActiveAttachmentUpload;
  chrome: TerminalThemeChrome;
  onCancel(): void;
}

export function InterfaceComposerUploadingChip({
  upload,
  chrome,
  onCancel,
}: InterfaceComposerUploadingChipProps) {
  const presentation = buildAttachmentUploadPresentation(
    upload.name,
    upload.progress,
  );
  return (
    <View
      style={[
        styles.chip,
        { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
      ]}
    >
      <View
        accessible
        accessibilityLabel={presentation.accessibilityLabel}
        accessibilityRole="progressbar"
        accessibilityValue={presentation.accessibilityValue}
        style={styles.status}
      >
        <Text numberOfLines={1} style={[styles.name, { color: chrome.text }]}>
          {upload.name}
        </Text>
        <View style={styles.progressRow}>
          {presentation.progressPercent === null ? (
            <View style={styles.loader}>
              <ComposerLoadingDots color={chrome.accent} size={6} />
            </View>
          ) : (
            <View
              style={[
                styles.track,
                { backgroundColor: chrome.disabledSurface },
              ]}
            >
              <View
                style={[
                  styles.fill,
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
            style={[styles.progress, { color: chrome.textMuted }]}
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
          styles.cancel,
          { borderColor: chrome.border, opacity: pressed ? 0.68 : 1 },
        ]}
      >
        <Text style={[styles.cancelText, { color: chrome.accent }]}>
          {presentation.cancelLabel}
        </Text>
      </Pressable>
    </View>
  );
}

const styles = StyleSheet.create({
  chip: {
    width: 296,
    minHeight: 48,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 10,
    paddingRight: 6,
    paddingVertical: 5,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  status: {
    flex: 1,
    minWidth: 0,
    gap: 2,
  },
  progressRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  loader: {
    width: 30,
    alignItems: "center",
  },
  name: {
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFontMedium,
  },
  progress: {
    flex: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
  },
  track: {
    width: 38,
    height: 3,
    borderRadius: 2,
    overflow: "hidden",
  },
  fill: {
    height: 3,
    borderRadius: 2,
  },
  cancel: {
    minWidth: 54,
    minHeight: 36,
    borderLeftWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 6,
  },
  cancelText: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
});
