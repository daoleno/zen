import React from "react";
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { ActiveAttachmentUpload } from "../../services/uploads";
import {
  InterfaceComposerAttachmentChip,
  type InterfaceComposerAttachment,
} from "./InterfaceComposerAttachmentChip";
import { InterfaceComposerUploadingChip } from "./InterfaceComposerUploadingChip";

export type { InterfaceComposerAttachment } from "./InterfaceComposerAttachmentChip";

interface InterfaceComposerAttachmentRailProps {
  attachments: InterfaceComposerAttachment[];
  activeUpload: ActiveAttachmentUpload | null;
  chrome: TerminalThemeChrome;
  onRemoveAttachment(id: string): void;
  onCancelUpload(): void;
}

export function InterfaceComposerAttachmentRail({
  attachments,
  activeUpload,
  chrome,
  onRemoveAttachment,
  onCancelUpload,
}: InterfaceComposerAttachmentRailProps) {
  if (attachments.length === 0 && !activeUpload) {
    return null;
  }

  return (
    <View style={styles.rail}>
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={styles.list}
      >
        {attachments.map((attachment) => (
          <View key={attachment.id} style={{ gap: 4 }}>
          <InterfaceComposerAttachmentChip
            attachment={attachment}
            chrome={chrome}
            onRemove={onRemoveAttachment}
          />
          {attachment.uploadStatus ? <Text numberOfLines={1} style={{ color: chrome.textMuted, maxWidth: 220 }}>
            {attachment.uploadStatus === "uploading" && attachment.uploadProgress?.fraction != null ? `${Math.round(attachment.uploadProgress.fraction * 100)}%` : attachment.uploadStatus}
          </Text> : null}
          {attachment.uploadStatus === "failed" ? <Pressable accessibilityRole="button" accessibilityLabel={`Retry ${attachment.name}`} onPress={attachment.retryUpload}>
            <Text numberOfLines={2} style={{ color: chrome.textMuted, maxWidth: 220 }}>{attachment.uploadError}</Text>
            <Text style={{ color: chrome.accent }}>Retry</Text>
          </Pressable> : null}
          </View>
        ))}
        {activeUpload ? (
          <InterfaceComposerUploadingChip
            upload={activeUpload}
            chrome={chrome}
            onCancel={onCancelUpload}
          />
        ) : null}
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  rail: {
    marginBottom: 8,
  },
  list: {
    minHeight: 56,
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 2,
  },
});
