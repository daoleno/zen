import React from "react";
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { ActiveAttachmentUpload } from "../../services/uploads";
import {
  InterfaceComposerAttachmentChip,
  type InterfaceComposerAttachment,
} from "./InterfaceComposerAttachmentChip";
import { imageReference, isImageAttachment, type ZenImageSource } from "../../services/imageSource";
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
  const gallery: ZenImageSource[] = attachments.filter((item) => item.uploadStatus !== "failed" && isImageAttachment(item)).map((attachment) => attachment.localUri
    ? { kind: "phone", uri: attachment.localUri, name: attachment.name, mimeType: attachment.mimeType }
    : imageReference(attachment.path, attachment.name, attachment.mimeType));
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
          <View key={attachment.id} style={styles.item}>
            <InterfaceComposerAttachmentChip
              attachment={attachment}
              gallery={gallery}
              chrome={chrome}
              onRemove={onRemoveAttachment}
            />
            {attachment.uploadStatus === "uploading" || attachment.uploadStatus === "queued" ? <View accessibilityLabel={`${attachment.uploadStatus === "queued" ? "Queued" : "Uploading"} ${attachment.name}`} style={styles.progress}>
              <ActivityIndicator size="small" color={chrome.textMuted} />
              {attachment.uploadStatus === "uploading" && attachment.uploadProgress?.fraction != null ? <Text style={{ color: chrome.textMuted }}>{Math.round(attachment.uploadProgress.fraction * 100)}%</Text> : null}
            </View> : null}
            {attachment.uploadStatus === "failed" && attachment.retryUpload ? <Pressable accessibilityRole="button" accessibilityLabel={`Retry upload of ${attachment.name}`} onPress={attachment.retryUpload} style={styles.retry}>
              <Ionicons name="refresh-outline" size={17} color={chrome.accent} />
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
  item: { alignItems: "flex-start", gap: 3 },
  progress: { minHeight: 24, flexDirection: "row", gap: 5, alignItems: "center" },
  retry: { minHeight: 34, minWidth: 62, flexDirection: "row", alignItems: "center", gap: 5 },
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
