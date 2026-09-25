import React from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ContinuousCorners, TypeScale, Typography } from "../../constants/tokens";
import type { ActiveAttachmentUpload } from "../../services/uploads";
import { buildAttachmentUploadPresentation } from "./attachmentUploadPresentation";
import { ComposerLoadingDots } from "./ComposerLoadingDots";
import {
  COMPOSER_ATTACHMENT_TILE_HEIGHT,
  COMPOSER_ATTACHMENT_TILE_RADIUS,
} from "./InterfaceComposerAttachmentChip";

interface InterfaceComposerUploadingChipProps {
  upload: ActiveAttachmentUpload;
  chrome: TerminalThemeChrome;
  onCancel(): void;
}

/**
 * In-flight upload tile: same geometry as a staged attachment, with a
 * determinate bar when the transport reports a fraction and the loading
 * mark otherwise. Cancel is a full-height 44 pt trailing action.
 */
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
        { backgroundColor: chrome.composerInput, borderColor: chrome.border },
      ]}
    >
      <View
        accessible
        accessibilityLabel={presentation.accessibilityLabel}
        accessibilityRole="progressbar"
        accessibilityValue={presentation.accessibilityValue}
        style={styles.status}
      >
        <View style={[styles.iconTile, { backgroundColor: chrome.accentSoft }]}>
          <ComposerLoadingDots color={chrome.accent} size={7} />
        </View>
        <View style={styles.copy}>
          <Text numberOfLines={1} style={[styles.name, { color: chrome.text }]}>
            {upload.name}
          </Text>
          {presentation.progressPercent === null ? null : (
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
        onPress={onCancel}
        style={({ pressed }) => [
          styles.cancel,
          { borderColor: chrome.border, opacity: pressed ? 0.6 : 1 },
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
    width: 284,
    minHeight: COMPOSER_ATTACHMENT_TILE_HEIGHT,
    borderRadius: COMPOSER_ATTACHMENT_TILE_RADIUS,
    ...ContinuousCorners,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 8,
    flexDirection: "row",
    alignItems: "stretch",
  },
  status: {
    flex: 1,
    minWidth: 0,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingVertical: 7,
  },
  iconTile: {
    width: 36,
    height: 36,
    borderRadius: 10,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
  },
  copy: {
    flex: 1,
    minWidth: 0,
    gap: 3,
  },
  name: {
    ...TypeScale.label,
  },
  progress: {
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFont,
  },
  track: {
    alignSelf: "stretch",
    height: 3,
    borderRadius: 1.5,
    overflow: "hidden",
  },
  fill: {
    height: 3,
    borderRadius: 1.5,
  },
  cancel: {
    minWidth: 64,
    minHeight: 44,
    borderLeftWidth: StyleSheet.hairlineWidth,
    marginLeft: 8,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 10,
  },
  cancelText: {
    ...TypeScale.label,
  },
});
