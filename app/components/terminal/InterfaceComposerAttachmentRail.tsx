import React from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import {
  InterfaceComposerAttachmentChip,
  type InterfaceComposerAttachment,
} from "./InterfaceComposerAttachmentChip";
import { InterfaceComposerUploadingChip } from "./InterfaceComposerUploadingChip";

export type { InterfaceComposerAttachment } from "./InterfaceComposerAttachmentChip";

interface InterfaceComposerAttachmentRailProps {
  attachments: InterfaceComposerAttachment[];
  uploading: boolean;
  chrome: TerminalThemeChrome;
  onRemoveAttachment(id: string): void;
}

export function InterfaceComposerAttachmentRail({
  attachments,
  uploading,
  chrome,
  onRemoveAttachment,
}: InterfaceComposerAttachmentRailProps) {
  if (attachments.length === 0 && !uploading) {
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
        {uploading ? <InterfaceComposerUploadingChip chrome={chrome} /> : null}
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
