import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { InterfaceComposerAttachmentIcon } from "./InterfaceComposerAttachmentIcon";
import { InterfaceComposerAttachmentRemoveButton } from "./InterfaceComposerAttachmentRemoveButton";

import { isImageAttachment, type ZenImageSource } from "../../services/imageSource";
import { ZenImage } from "./ZenImage";

export type InterfaceComposerAttachment = import("./InterfaceChatSession").ComposerAttachment;

interface InterfaceComposerAttachmentChipProps {
  attachment: InterfaceComposerAttachment;
  chrome: TerminalThemeChrome;
  onRemove(id: string): void;
  gallery?: ZenImageSource[];
}

export function InterfaceComposerAttachmentChip({
  attachment,
  chrome,
  onRemove,
  gallery,
}: InterfaceComposerAttachmentChipProps) {
  const thumbnailUri = attachment.uploadStatus === "failed" ? null : attachmentThumbnailUri(attachment);

  if (thumbnailUri) {
    return (
      <View
        style={[
          styles.thumbChip,
          { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
        ]}
      >
        <ZenImage source={{ kind: "phone", uri: thumbnailUri, name: attachment.name, mimeType: attachment.mimeType }} chrome={chrome} gallery={gallery} compact />
        <InterfaceComposerAttachmentRemoveButton
          attachmentName={attachment.name}
          chrome={chrome}
          onPress={() => onRemove(attachment.id)}
          style={styles.thumbRemove}
        />
      </View>
    );
  }

  return (
    <View
      style={[
        styles.chip,
        { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
      ]}
    >
      <InterfaceComposerAttachmentIcon
        fileName={attachment.name}
        chrome={chrome}
      />
      <View style={styles.textGroup}>
        <Text style={[styles.name, { color: chrome.text }]} numberOfLines={1}>
          {attachment.name}
        </Text>
        {attachment.uploadStatus === "failed" ? <Text style={[styles.path, { color: chrome.danger }]} numberOfLines={1}>Upload failed</Text>
          : attachment.path ? <Text style={[styles.path, { color: chrome.textSubtle }]} numberOfLines={1}>{basename(attachment.path)}</Text> : null}
      </View>
      <InterfaceComposerAttachmentRemoveButton
        attachmentName={attachment.name}
        chrome={chrome}
        onPress={() => onRemove(attachment.id)}
      />
    </View>
  );
}

function attachmentThumbnailUri(attachment: InterfaceComposerAttachment) {
  if (attachment.localUri && isImageAttachment(attachment)) {
    return attachment.localUri;
  }
  return null;
}

function basename(value: string) {
  const parts = value.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || value;
}

const styles = StyleSheet.create({
  chip: {
    maxWidth: 220,
    minHeight: 44,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 9,
    paddingRight: 5,
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
  },
  thumbChip: {
    width: 96,
    height: 80,
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  thumbRemove: {
    position: "absolute",
    top: 0,
    right: 0,
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
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
});
