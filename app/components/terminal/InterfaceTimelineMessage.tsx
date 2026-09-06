import React from "react";
import { StyleSheet, Text, View } from "react-native";

import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography, useAppTheme } from "../../constants/tokens";
import type { MessagePresentation } from "./InterfaceTimelineGrouping";
import { MessageBubbleFooter } from "./MessageBubbleFooter";
import {
  chatgptUserBubbleRadii,
  messageRowSpacing,
  userBubbleRadii,
} from "./messageBubbleShape";

import { MessageBody } from "./InterfaceMessageBody";
import { InterfaceTimelineAttachmentPreviewList } from "./InterfaceTimelineAttachmentPreviewList";
import { PendingSendStatusMark } from "./PendingSendStatusMark";
import {
  PENDING_SEND_STATUS_MARK_SIZE,
  PENDING_SEND_STATUS_OUTSIDE_RIGHT,
} from "./pendingSendStatusGeometry";
import { showsPendingSendStatusMark } from "./pendingUserMessageLifecycle";

export type DisplayAttachment = {
  name: string;
  path: string;
  localUri?: string;
  mimeType?: string;
};

export interface ZenMessageTimelineItem {
  type: "message";
  id: string;
  role: "user" | "assistant";
  timestamp?: string;
  body: string;
  attachments: DisplayAttachment[];
  pending?: boolean;
  pendingLifecycle?: "pending" | "failed";
  pendingLifecycleLabel?: string;
  pendingFailureMessage?: string;
  onRetryPending?: () => void;
  streaming?: boolean;

  /** Process-local presentation alias; provider id/body remain canonical. */
  turnFocusAnchorId?: string;
}

const DEFAULT_PRESENTATION: MessagePresentation = {
  showAvatar: false,
  groupPosition: "single",
  compactTop: false,
  compactBottom: false,
};

export function ZenUserMessage({
  item,
  presentation = DEFAULT_PRESENTATION,
  chrome,
  theme,
}: {
  item: ZenMessageTimelineItem & { role: "user" };
  presentation?: MessagePresentation;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const { theme: zenTheme } = useAppTheme();
  const chatLayout = zenTheme.chat.layout;
  const isChatGpt = chatLayout === "chatgpt";

  const hasBody = item.body.trim().length > 0;
  const sentBubbleColor = zenTheme.chat.sentBubble;
  const sentChrome = {
    ...chrome,
    text: zenTheme.chat.sentText,
    textMuted: zenTheme.chat.sentTimestamp,
    textSubtle: zenTheme.chat.sentTimestamp,
    link: zenTheme.chat.sentText,
  };
  const spacing = messageRowSpacing(
    presentation.compactTop,
    presentation.compactBottom,
    chatLayout,
    "user",
  );
  const bubbleRadii = isChatGpt
    ? chatgptUserBubbleRadii()
    : userBubbleRadii(presentation.groupPosition);
  const showPendingSendMark = showsPendingSendStatusMark({
    pending: item.pending,
    lifecycle: item.pendingLifecycle,
  });
  const showFooter =
    item.pendingLifecycle === "failed" ||
    Boolean(item.pendingLifecycleLabel) ||
    Boolean(item.onRetryPending) ||
    zenTheme.chat.showTimestamps;

  return (
    <View style={[styles.userRow, spacing]}>
      <View
        // Busy only — never a status-only accessibilityLabel that replaces body.
        accessibilityState={item.pending ? { busy: true } : undefined}
        style={[
          isChatGpt ? styles.userBubbleChatGpt : styles.userBubble,
          bubbleRadii,
          { backgroundColor: sentBubbleColor },
        ]}
      >
        {hasBody ? (
          <MessageBody value={item.body} chrome={sentChrome} theme={theme} />
        ) : null}
        {item.attachments.length > 0 ? (
          <InterfaceTimelineAttachmentPreviewList
            attachments={item.attachments}
            chrome={sentChrome}
            compact={hasBody}
          />
        ) : null}
        {showFooter ? (
          <MessageBubbleFooter
            timestamp={item.timestamp}
            tone="sent"
            lifecycleLabel={item.pendingLifecycleLabel}
            failureMessage={item.pendingFailureMessage}
            failureColor={chrome.danger}
            onRetry={item.onRetryPending}
          />
        ) : null}
        {showPendingSendMark ? (
          <View style={styles.pendingSendMark} pointerEvents="none">
            <PendingSendStatusMark color={zenTheme.chat.outboundSentClock} />
          </View>
        ) : null}
      </View>
    </View>
  );
}

export function ZenAssistantMessage({
  item,
  presentation = DEFAULT_PRESENTATION,
  chrome,
  theme,
  senderLabel,
}: {
  item: ZenMessageTimelineItem & { role: "assistant" };
  presentation?: MessagePresentation;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  senderLabel?: string;
}) {
  const { theme: zenTheme } = useAppTheme();
  const chatLayout = zenTheme.chat.layout;
  const spacing = messageRowSpacing(
    presentation.compactTop,
    presentation.compactBottom,
    chatLayout,
    "assistant",
  );
  const assistantChrome = {
    ...chrome,
    text: zenTheme.chat.receivedText,
    link: zenTheme.chat.link,
  };
  const showSender =
    senderLabel &&
    presentation.groupPosition !== "middle" &&
    presentation.groupPosition !== "last";

  return (
    <View style={[styles.assistantRow, spacing]}>
      {showSender ? (
        <Text style={[styles.assistantSender, { color: chrome.accent }]}>
          {senderLabel}
        </Text>
      ) : null}
      <View style={styles.assistantContent}>
        <MessageBody
          value={item.body}
          chrome={assistantChrome}
          theme={theme}
          streaming={item.streaming}
        />
        {zenTheme.chat.showTimestamps ? (
          <MessageBubbleFooter timestamp={item.timestamp} tone="received" />
        ) : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  userRow: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
    flexDirection: "row",
    justifyContent: "flex-end",
    overflow: "visible",
  },
  userBubble: {
    position: "relative",
    maxWidth: "86%",
    paddingHorizontal: 13,
    paddingTop: 9,
    paddingBottom: 8,
    overflow: "visible",
  },
  userBubbleChatGpt: {
    position: "relative",
    maxWidth: "88%",
    paddingHorizontal: 14,
    paddingVertical: 10,
    overflow: "visible",
  },
  pendingSendMark: {
    position: "absolute",
    right: PENDING_SEND_STATUS_OUTSIDE_RIGHT,
    bottom: 0,
    width: PENDING_SEND_STATUS_MARK_SIZE,
    height: PENDING_SEND_STATUS_MARK_SIZE,
    alignItems: "center",
    justifyContent: "center",
  },
  assistantRow: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
  },
  assistantSender: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 12,
    lineHeight: 15,
    marginBottom: 3,
  },
  assistantContent: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
  },

});
