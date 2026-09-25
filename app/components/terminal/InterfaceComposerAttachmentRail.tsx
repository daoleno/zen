import React from "react";
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TouchTarget, TypeScale } from "../../constants/tokens";
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
            {attachment.uploadStatus === "uploading" || attachment.uploadStatus === "queued" ? <View accessible accessibilityLabel={`${attachment.uploadStatus === "queued" ? "Queued" : "Uploading"} ${attachment.name}`} style={styles.progress}>
              <ActivityIndicator size="small" color={chrome.textMuted} />
              <Text style={[styles.statusText, { color: chrome.textMuted }]}>
                {attachment.uploadStatus === "queued"
                  ? "Queued"
                  : attachment.uploadProgress?.fraction != null
                    ? `${Math.round(attachment.uploadProgress.fraction * 100)}%`
                    : "Uploading"}
              </Text>
            </View> : null}
            {attachment.uploadStatus === "failed" && attachment.retryUpload ? <Pressable accessibilityRole="button" accessibilityLabel={`Retry upload of ${attachment.name}`} hitSlop={RETRY_HIT_SLOP} onPress={attachment.retryUpload} style={({ pressed }) => [styles.retry, { backgroundColor: chrome.accentSoft, opacity: pressed ? 0.64 : 1 }]}>
              <Ionicons name="refresh" size={14} color={chrome.accent} />
              <Text style={[styles.statusText, styles.retryText, { color: chrome.accent }]}>Retry</Text>
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

/** 28 pt capsule + vertical hitSlop = platform touch target, inside the rail. */
const RETRY_CAPSULE_HEIGHT = 28;
const RETRY_HIT_SLOP = {
  top: (TouchTarget - RETRY_CAPSULE_HEIGHT) / 2,
  bottom: (TouchTarget - RETRY_CAPSULE_HEIGHT) / 2,
};

const styles = StyleSheet.create({
  item: { alignItems: "flex-start", gap: 6 },
  progress: { minHeight: 22, flexDirection: "row", gap: 6, alignItems: "center", paddingHorizontal: 4 },
  retry: { minHeight: RETRY_CAPSULE_HEIGHT, minWidth: 72, borderRadius: 14, paddingHorizontal: 10, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 5 },
  statusText: { ...TypeScale.caption },
  retryText: { fontFamily: TypeScale.label.fontFamily },
  rail: {
    marginBottom: 8,
  },
  list: {
    minHeight: 56,
    alignItems: "flex-start",
    gap: 8,
    paddingHorizontal: 4,
    paddingVertical: (TouchTarget - RETRY_CAPSULE_HEIGHT) / 2,
  },
});
