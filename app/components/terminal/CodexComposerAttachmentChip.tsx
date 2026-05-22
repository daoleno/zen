import React from "react";
import {
  StyleSheet,
  Text,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { CodexComposerAttachmentIcon } from "./CodexComposerAttachmentIcon";
import { CodexComposerAttachmentRemoveButton } from "./CodexComposerAttachmentRemoveButton";

export interface CodexComposerAttachment {
  id: string;
  name: string;
  path: string;
}

interface CodexComposerAttachmentChipProps {
  attachment: CodexComposerAttachment;
  chrome: TerminalThemeChrome;
  onRemove(id: string): void;
}

export function CodexComposerAttachmentChip({
  attachment,
  chrome,
  onRemove,
}: CodexComposerAttachmentChipProps) {
  return (
    <View
      style={[
        styles.chip,
        { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
      ]}
    >
      <CodexComposerAttachmentIcon
        fileName={attachment.name}
        chrome={chrome}
      />
      <View style={styles.textGroup}>
        <Text style={[styles.name, { color: chrome.text }]} numberOfLines={1}>
          {attachment.name}
        </Text>
        <Text
          style={[styles.path, { color: chrome.textSubtle }]}
          numberOfLines={1}
        >
          {basename(attachment.path)}
        </Text>
      </View>
      <CodexComposerAttachmentRemoveButton
        attachmentName={attachment.name}
        chrome={chrome}
        onPress={() => onRemove(attachment.id)}
      />
    </View>
  );
}

function basename(value: string) {
  const parts = value.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || value;
}

const styles = StyleSheet.create({
  chip: {
    maxWidth: 220,
    minHeight: 36,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 9,
    paddingRight: 5,
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
  },
  textGroup: {
    flex: 1,
    minWidth: 0,
  },
  name: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
  path: {
    marginTop: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.terminalFont,
  },
});
