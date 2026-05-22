import React from "react";
import { StyleSheet, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
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
          <CodexTimelineAttachmentPreviewList
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
});
