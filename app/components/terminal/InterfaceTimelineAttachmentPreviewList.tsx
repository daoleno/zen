import React from "react";
import { StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { DisplayAttachment } from "./InterfaceTimelineMessage";

import { ZenImage } from "./ZenImage";
import { imageReference, isImageAttachment, type ZenImageSource } from "../../services/imageSource";

function attachmentSource(attachment: DisplayAttachment): ZenImageSource {
  return attachment.localUri ? { kind: "phone", uri: attachment.localUri, name: attachment.name } : imageReference(attachment.path, attachment.name);
}

interface InterfaceTimelineAttachmentPreviewListProps {
  attachments: DisplayAttachment[];
  chrome: TerminalThemeChrome;
  compact?: boolean;
}

export function InterfaceTimelineAttachmentPreviewList({
  attachments,
  chrome,
  compact,
}: InterfaceTimelineAttachmentPreviewListProps) {
  const gallery = attachments.filter(isImageAttachment).map(attachmentSource);
  return (
    <View
      style={[styles.attachments, compact ? styles.attachmentsCompact : null]}
    >
      {attachments.map((attachment) => (
        <InterfaceTimelineAttachmentPreviewPill
          key={`${attachment.name}:${attachment.path}:${attachment.localUri ?? ""}`}
          attachment={attachment}
          chrome={chrome}
          gallery={gallery}
        />
      ))}
    </View>
  );
}

function InterfaceTimelineAttachmentPreviewPill({
  attachment,
  chrome,
  gallery,
}: {
  attachment: DisplayAttachment;
  gallery: ZenImageSource[];
  chrome: TerminalThemeChrome;
}) {
  if (isImageAttachment(attachment)) {
    return <ZenImage source={attachmentSource(attachment)} gallery={gallery} chrome={chrome} />;
  }

  return (
    <View
      style={[
        styles.attachmentPill,
        {
          borderColor: chrome.border,
          backgroundColor: chrome.surfaceMuted,
        },
      ]}
    >
      <Ionicons
        name={
          looksLikeImagePath(attachment.name)
            ? "image-outline"
            : "document-attach-outline"
        }
        size={13}
        color={chrome.textSubtle}
      />
      <Text
        style={[styles.attachmentPillText, { color: chrome.textMuted }]}
        numberOfLines={1}
      >
        {attachment.name || basename(attachment.path)}
      </Text>
    </View>
  );
}

function looksLikeImagePath(value: string) {
  return /\.(png|jpe?g|gif|webp|bmp|heic|heif)$/i.test(value.trim());
}

function basename(value: string) {
  const parts = value.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || value;
}

const styles = StyleSheet.create({
  attachments: {
    gap: 7,
    flexDirection: "row",
    flexWrap: "wrap",
  },
  attachmentsCompact: {
    marginTop: 8,
  },
  attachmentPill: {
    alignSelf: "flex-start",
    maxWidth: "100%",
    minHeight: 30,
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 9,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  thumbPill: {
    width: 84,
    height: 84,
    borderRadius: 14,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  thumb: {
    width: "100%",
    height: "100%",
  },
  attachmentPillText: {
    flexShrink: 1,
    fontSize: 11.5,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
});
