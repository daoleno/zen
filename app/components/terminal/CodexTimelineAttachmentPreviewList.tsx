import React from "react";
import {
  Image,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { DisplayAttachment } from "./CodexTimelineMessage";

interface CodexTimelineAttachmentPreviewListProps {
  attachments: DisplayAttachment[];
  chrome: TerminalThemeChrome;
  compact?: boolean;
}

export function CodexTimelineAttachmentPreviewList({
  attachments,
  chrome,
  compact,
}: CodexTimelineAttachmentPreviewListProps) {
  return (
    <View style={[styles.attachments, compact ? styles.attachmentsCompact : null]}>
      {attachments.map((attachment) => (
        <CodexTimelineAttachmentPreviewPill
          key={`${attachment.name}:${attachment.path}:${attachment.localUri ?? ""}`}
          attachment={attachment}
          chrome={chrome}
        />
      ))}
    </View>
  );
}

function CodexTimelineAttachmentPreviewPill({
  attachment,
  chrome,
}: {
  attachment: DisplayAttachment;
  chrome: TerminalThemeChrome;
}) {
  const thumbnailUri = attachmentThumbnailUri(attachment);

  if (thumbnailUri) {
    return (
      <View
        style={[
          styles.thumbPill,
          { borderColor: chrome.border, backgroundColor: chrome.surfaceMuted },
        ]}
      >
        <Image
          source={{ uri: thumbnailUri }}
          style={styles.thumb}
          resizeMode="cover"
          accessibilityLabel={attachment.name || basename(attachment.path)}
        />
      </View>
    );
  }

  return (
    <View style={[styles.attachmentPill, { borderColor: chrome.border }]}>
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

function attachmentThumbnailUri(attachment: DisplayAttachment) {
  if (!attachment.localUri) {
    return null;
  }
  if (attachment.mimeType?.startsWith("image/")) {
    return attachment.localUri;
  }
  if (
    looksLikeImagePath(attachment.name) ||
    looksLikeImagePath(attachment.path) ||
    looksLikeImagePath(attachment.localUri)
  ) {
    return attachment.localUri;
  }
  return null;
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
    gap: 6,
    flexDirection: "row",
    flexWrap: "wrap",
  },
  attachmentsCompact: {
    marginTop: 8,
  },
  attachmentPill: {
    alignSelf: "flex-start",
    maxWidth: "100%",
    minHeight: 28,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 8,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  thumbPill: {
    width: 72,
    height: 72,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  thumb: {
    width: "100%",
    height: "100%",
  },
  attachmentPillText: {
    flexShrink: 1,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
});
