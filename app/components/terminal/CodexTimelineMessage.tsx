import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import {
  MessageBody,
  StreamingMessageBody,
} from "./CodexMessageBody";
import { CodexTimelineAttachmentPreviewList } from "./CodexTimelineAttachmentPreviewList";

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
  pending?: boolean;
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
      <View
        style={[
          styles.userBubble,
          {
            backgroundColor: chrome.surfaceMuted,
            borderColor: item.pending ? chrome.borderStrong : "transparent",
            opacity: item.pending ? 0.88 : 1,
          },
        ]}
      >
        {hasBody ? (
          <MessageBody value={item.body} chrome={chrome} theme={theme} compact />
        ) : null}
        {item.attachments.length > 0 ? (
          <CodexTimelineAttachmentPreviewList
            attachments={item.attachments}
            chrome={chrome}
            compact={hasBody}
          />
        ) : null}
        {item.pending ? (
          <View style={[styles.pendingRow, hasBody || item.attachments.length > 0 ? styles.pendingRowSpaced : null]}>
            <ActivityIndicator size="small" color={chrome.accent} />
            <Text style={[styles.pendingText, { color: chrome.textMuted }]}>
              Sending
            </Text>
          </View>
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

const styles = StyleSheet.create({
  userRow: {
    marginBottom: 16,
    flexDirection: "row",
    justifyContent: "flex-end",
  },
  userBubble: {
    maxWidth: "86%",
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingVertical: 9,
  },
  pendingRow: {
    minHeight: 18,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  pendingRowSpaced: {
    marginTop: 8,
  },
  pendingText: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
  assistantRow: {
    marginBottom: 18,
    paddingRight: 10,
  },
});
