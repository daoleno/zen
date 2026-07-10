import React from "react";
import {
  Image,
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
  localUri?: string;
  mimeType?: string;
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
  const thumbnailUri = attachmentThumbnailUri(attachment);

  if (thumbnailUri) {
    return (
      <View
        style={[
          styles.thumbChip,
          { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
        ]}
      >
        <Image
          source={{ uri: thumbnailUri }}
          style={styles.thumb}
          resizeMode="cover"
          accessibilityLabel={attachment.name}
        />
        <CodexComposerAttachmentRemoveButton
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

function attachmentThumbnailUri(attachment: CodexComposerAttachment) {
  if (attachment.localUri && isImageAttachment(attachment)) {
    return attachment.localUri;
  }
  return null;
}

function isImageAttachment(attachment: CodexComposerAttachment) {
  if (attachment.mimeType?.startsWith("image/")) {
    return true;
  }
  return looksLikeImagePath(attachment.name) || looksLikeImagePath(attachment.path);
}

function looksLikeImagePath(value: string) {
  return /\.(png|jpe?g|gif|webp|bmp|heic|heif)$/i.test(value.trim());
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
    width: 56,
    height: 56,
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  thumb: {
    width: "100%",
    height: "100%",
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
