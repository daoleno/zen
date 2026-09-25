import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { ContinuousCorners, TypeScale, Typography } from "../../constants/tokens";
import { InterfaceComposerAttachmentIcon } from "./InterfaceComposerAttachmentIcon";
import { InterfaceComposerAttachmentRemoveButton } from "./InterfaceComposerAttachmentRemoveButton";
import { chromeTint } from "./composerMaterial";

import { isImageAttachment, type ZenImageSource } from "../../services/imageSource";
import { ZenImage } from "./ZenImage";

export type InterfaceComposerAttachment = import("./InterfaceChatSession").ComposerAttachment;

interface InterfaceComposerAttachmentChipProps {
  attachment: InterfaceComposerAttachment;
  chrome: TerminalThemeChrome;
  onRemove(id: string): void;
  gallery?: ZenImageSource[];
}

/** Shared tile geometry for every Composer attachment (file, image, upload). */
export const COMPOSER_ATTACHMENT_TILE_RADIUS = 14;
export const COMPOSER_ATTACHMENT_TILE_HEIGHT = 56;

export function InterfaceComposerAttachmentChip({
  attachment,
  chrome,
  onRemove,
  gallery,
}: InterfaceComposerAttachmentChipProps) {
  const failed = attachment.uploadStatus === "failed";
  const thumbnailUri = failed ? null : attachmentThumbnailUri(attachment);

  if (thumbnailUri) {
    return (
      <View
        style={[
          styles.thumbChip,
          { backgroundColor: chrome.composerInput, borderColor: chrome.border },
        ]}
      >
        <ZenImage source={{ kind: "phone", uri: thumbnailUri, name: attachment.name, mimeType: attachment.mimeType }} chrome={chrome} gallery={gallery} compact />
        <InterfaceComposerAttachmentRemoveButton
          attachmentName={attachment.name}
          chrome={chrome}
          placement="overlay"
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
        {
          backgroundColor: chrome.composerInput,
          borderColor: failed
            ? chromeTint(chrome.danger, 0.45, chrome.danger)
            : chrome.border,
        },
      ]}
    >
      <View
        style={[
          styles.iconTile,
          {
            backgroundColor: failed ? chrome.dangerSoft : chrome.accentSoft,
          },
        ]}
      >
        <InterfaceComposerAttachmentIcon
          fileName={attachment.name}
          chrome={chrome}
          color={failed ? chrome.danger : chrome.accent}
        />
      </View>
      <View style={styles.textGroup}>
        <Text style={[styles.name, { color: chrome.text }]} numberOfLines={1}>
          {attachment.name}
        </Text>
        {failed ? <Text style={[styles.path, styles.failed, { color: chrome.danger }]} numberOfLines={1}>Upload failed</Text>
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
    maxWidth: 232,
    minHeight: COMPOSER_ATTACHMENT_TILE_HEIGHT,
    borderRadius: COMPOSER_ATTACHMENT_TILE_RADIUS,
    ...ContinuousCorners,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 8,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
  },
  iconTile: {
    width: 36,
    height: 36,
    borderRadius: 10,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
  },
  thumbChip: {
    width: 96,
    height: 80,
    borderRadius: COMPOSER_ATTACHMENT_TILE_RADIUS,
    ...ContinuousCorners,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  thumbRemove: {
    position: "absolute",
    top: 0,
    right: 0,
  },
  textGroup: {
    flexShrink: 1,
    minWidth: 0,
  },
  name: {
    ...TypeScale.label,
  },
  path: {
    marginTop: 1,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  failed: {
    fontFamily: Typography.uiFontMedium,
  },
});
