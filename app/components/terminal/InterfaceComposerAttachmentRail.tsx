import React from "react";
import { ScrollView, StyleSheet, View } from "react-native";
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
          <InterfaceComposerAttachmentChip
            key={attachment.id}
            attachment={attachment}
            chrome={chrome}
            onRemove={onRemoveAttachment}
          />
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
