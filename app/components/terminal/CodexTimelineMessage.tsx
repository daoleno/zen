import React from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  MessageBody,
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
      </View>
    </View>
  );
}

export function ZenAssistantMessage({
  item,
  chrome,
  theme,
}: {
  item: ZenMessageTimelineItem & { role: "assistant" };
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  return (
    <View style={styles.assistantRow}>
      <MessageBody
        value={item.body}
        chrome={chrome}
        theme={theme}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  userRow: {
    marginBottom: 17,
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
  assistantRow: {
    marginBottom: 20,
    paddingRight: 8,
  },
});
