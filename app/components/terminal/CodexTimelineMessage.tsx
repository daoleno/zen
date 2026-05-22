import React from "react";
import { StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  MessageBody,
  StreamingMessageBody,
} from "./CodexMessageBody";

export type DisplayAttachment = {
  name: string;
  path: string;
};

export interface ZenMessageTimelineItem {
  type: "message";
  id: string;
  role: "user" | "assistant";
  timestamp?: string;
  body: string;
  attachments: DisplayAttachment[];
}

export function ZenUserMessage({
  item,
  chrome,
  theme,
}: {
  item: ZenMessageTimelineItem & { role: "user" };
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const hasBody = item.body.trim().length > 0;
  return (
    <View style={styles.userRow}>
      <View style={[styles.userBubble, { backgroundColor: chrome.surfaceMuted }]}>
        {hasBody ? (
          <MessageBody value={item.body} chrome={chrome} theme={theme} compact />
        ) : null}
        {item.attachments.length > 0 ? (
          <AttachmentPreviewList
            attachments={item.attachments}
            chrome={chrome}
            compact={hasBody}
          />
        ) : null}
      </View>
    </View>
  );
}

export function ZenAssistantMessage({
  item,
  chrome,
  theme,
  stream,
}: {
  item: ZenMessageTimelineItem & { role: "assistant" };
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  stream: boolean;
}) {
  return (
    <View style={styles.assistantRow}>
      <StreamingMessageBody
        value={item.body}
        chrome={chrome}
        theme={theme}
        stream={stream}
      />
    </View>
  );
}

function AttachmentPreviewList({
  attachments,
  chrome,
  compact,
}: {
  attachments: DisplayAttachment[];
  chrome: TerminalThemeChrome;
  compact?: boolean;
}) {
  return (
    <View style={[styles.attachments, compact ? styles.attachmentsCompact : null]}>
      {attachments.map((attachment) => (
        <View
          key={`${attachment.name}:${attachment.path}`}
          style={[styles.attachmentPill, { borderColor: chrome.border }]}
        >
          <Ionicons
            name={looksLikeImagePath(attachment.name) ? "image-outline" : "document-attach-outline"}
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
      ))}
    </View>
  );
}

function looksLikeImagePath(value: string) {
  return /\.(png|jpe?g|gif|webp|bmp)$/i.test(value.trim());
}

function basename(value: string) {
  const parts = value.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || value;
}

const styles = StyleSheet.create({
  userRow: {
    marginBottom: 16,
    flexDirection: "row",
    justifyContent: "flex-end",
  },
  userBubble: {
    maxWidth: "86%",
    borderRadius: 8,
    paddingHorizontal: 12,
    paddingVertical: 9,
  },
  assistantRow: {
    marginBottom: 18,
    paddingRight: 10,
  },
  attachments: {
    gap: 6,
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
  attachmentPillText: {
    flexShrink: 1,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
});
