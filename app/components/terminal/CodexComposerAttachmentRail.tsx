import React from "react";
import {
  ScrollView,
  StyleSheet,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import {
  CodexComposerAttachmentChip,
  type CodexComposerAttachment,
} from "./CodexComposerAttachmentChip";
import { CodexComposerUploadingChip } from "./CodexComposerUploadingChip";

export type { CodexComposerAttachment } from "./CodexComposerAttachmentChip";

interface CodexComposerAttachmentRailProps {
  attachments: CodexComposerAttachment[];
  uploading: boolean;
  chrome: TerminalThemeChrome;
  onRemoveAttachment(id: string): void;
}

export function CodexComposerAttachmentRail({
  attachments,
  uploading,
  chrome,
  onRemoveAttachment,
}: CodexComposerAttachmentRailProps) {
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
          <CodexComposerAttachmentChip
            key={attachment.id}
            attachment={attachment}
            chrome={chrome}
            onRemove={onRemoveAttachment}
          />
        ))}
        {uploading ? <CodexComposerUploadingChip chrome={chrome} /> : null}
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  rail: {
    marginBottom: 7,
  },
  list: {
    minHeight: 38,
    alignItems: "center",
    gap: 7,
    paddingHorizontal: 2,
  },
});
