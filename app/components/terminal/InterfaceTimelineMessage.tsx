import React from "react";
import { StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { TypeScale, Typography, useAppTheme } from "../../constants/tokens";
import type { MessagePresentation } from "./InterfaceTimelineGrouping";
import { MessageBubbleFooter } from "./MessageBubbleFooter";
import {
  chatgptUserBubbleRadii,
  messageRowSpacing,
  userBubbleRadii,
} from "./messageBubbleShape";
import type { HeartbeatWakeEvent } from "./CodexHeartbeatWake";
import {
  formatHeartbeatReason,
  formatHeartbeatStateChange,
  formatHeartbeatValue,
} from "./CodexHeartbeatWake";
import { MessageBody } from "./InterfaceMessageBody";
import { InterfaceTimelineAttachmentPreviewList } from "./InterfaceTimelineAttachmentPreviewList";

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
  pendingLifecycleAccessibilityLabel?: string;
  pendingFailureMessage?: string;
  onRetryPending?: () => void;
  streaming?: boolean;
  heartbeatWake?: HeartbeatWakeEvent;
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

  if (item.heartbeatWake) {
    return (
      <HeartbeatWakeCard
        event={item.heartbeatWake}
        chrome={chrome}
        theme={theme}
      />
    );
  }

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

  return (
    <View style={[styles.userRow, spacing]}>
      <View
        style={[
          isChatGpt ? styles.userBubbleChatGpt : styles.userBubble,
          bubbleRadii,
          {
            backgroundColor: sentBubbleColor,
            borderColor:
              item.pendingLifecycle === "failed"
                ? chrome.danger
                : item.pending
                  ? chrome.borderStrong
                  : "transparent",
          },
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
        {item.pending || zenTheme.chat.showTimestamps ? (
          <MessageBubbleFooter
            timestamp={item.timestamp}
            tone="sent"
            pending={item.pending}
            lifecycleLabel={item.pendingLifecycleLabel}
            lifecycleAccessibilityLabel={
              item.pendingLifecycleAccessibilityLabel
            }
            failureMessage={item.pendingFailureMessage}
            failureColor={chrome.danger}
            onRetry={item.onRetryPending}
          />
        ) : null}
      </View>
    </View>
  );
}

function HeartbeatWakeCard({
  event,
  chrome,
  theme,
}: {
  event: HeartbeatWakeEvent;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const agentTitle = event.agentName || event.agentId || "Unknown agent";
  const showAgentId = Boolean(event.agentName && event.agentId);
  const showSeparateStatus = Boolean(
    event.status &&
    event.newState &&
    event.status.trim().toLowerCase() !== event.newState.trim().toLowerCase(),
  );
  const { theme: zenTheme } = useAppTheme();
  const cardColor = zenTheme.chat.sentBubble;
  const cardChrome = {
    ...chrome,
    text: zenTheme.chat.sentText,
    textMuted: zenTheme.chat.sentTimestamp,
    textSubtle: zenTheme.chat.sentTimestamp,
    link: zenTheme.chat.sentText,
  };

  return (
    <View style={styles.eventRow}>
      <View
        style={[
          styles.heartbeatCard,
          {
            backgroundColor: cardColor,
            borderColor: cardChrome.borderStrong,
          },
        ]}
      >
        <View style={styles.heartbeatHeader}>
          <View
            style={[
              styles.heartbeatIcon,
              {
                backgroundColor: cardChrome.accentSoft,
                borderColor: cardChrome.borderStrong,
              },
            ]}
          >
            <Ionicons
              name="pulse-outline"
              size={15}
              color={cardChrome.accent}
            />
          </View>
          <View style={styles.heartbeatHeaderCopy}>
            <Text
              style={[styles.heartbeatTitle, { color: cardChrome.text }]}
              numberOfLines={1}
            >
              Heartbeat
            </Text>
            <Text
              style={[styles.heartbeatReason, { color: theme.yellow }]}
              numberOfLines={1}
            >
              {formatHeartbeatReason(event.reason)}
            </Text>
          </View>
        </View>

        <View style={styles.heartbeatFields}>
          <HeartbeatField
            label="Agent"
            value={agentTitle}
            chrome={cardChrome}
            monospace={!event.agentName}
          />
          {showAgentId ? (
            <HeartbeatField
              label="ID"
              value={event.agentId || ""}
              chrome={cardChrome}
              monospace
            />
          ) : null}
          <HeartbeatField
            label={event.oldState || event.newState ? "State" : "Status"}
            value={formatHeartbeatStateChange(event)}
            chrome={cardChrome}
          />
          {showSeparateStatus ? (
            <HeartbeatField
              label="Status"
              value={formatHeartbeatValue(event.status)}
              chrome={cardChrome}
            />
          ) : null}
          {event.workspace ? (
            <HeartbeatField
              label="Workspace"
              value={event.workspace}
              chrome={cardChrome}
              monospace
            />
          ) : null}
        </View>

        {event.summary ? (
          <View
            style={[
              styles.heartbeatSummary,
              {
                backgroundColor: cardChrome.surface,
                borderColor: cardChrome.borderStrong,
              },
            ]}
          >
            <Text
              style={[
                styles.heartbeatSummaryText,
                { color: cardChrome.textMuted },
              ]}
              numberOfLines={3}
            >
              {event.summary}
            </Text>
          </View>
        ) : null}
      </View>
    </View>
  );
}

function HeartbeatField({
  label,
  value,
  chrome,
  monospace = false,
}: {
  label: string;
  value: string;
  chrome: TerminalThemeChrome;
  monospace?: boolean;
}) {
  if (!value.trim()) {
    return null;
  }

  return (
    <View style={styles.heartbeatFieldRow}>
      <Text style={[styles.heartbeatFieldLabel, { color: chrome.textSubtle }]}>
        {label}
      </Text>
      <Text
        style={[
          styles.heartbeatFieldValue,
          {
            color: chrome.textMuted,
            fontFamily: monospace ? Typography.terminalFont : Typography.uiFont,
          },
        ]}
        numberOfLines={1}
      >
        {value}
      </Text>
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
  },
  userBubble: {
    maxWidth: "86%",
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 13,
    paddingTop: 9,
    paddingBottom: 8,
  },
  userBubbleChatGpt: {
    maxWidth: "88%",
    borderWidth: 0,
    paddingHorizontal: 14,
    paddingVertical: 10,
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
  eventRow: {
    marginBottom: 12,
    paddingRight: 2,
  },
  heartbeatCard: {
    alignSelf: "stretch",
    borderRadius: 14,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingVertical: 10,
    gap: 8,
  },
  heartbeatHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    minWidth: 0,
  },
  heartbeatIcon: {
    width: 26,
    height: 26,
    borderRadius: 13,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  heartbeatHeaderCopy: {
    flex: 1,
    minWidth: 0,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  heartbeatTitle: {
    ...TypeScale.label,
    flexShrink: 0,
  },
  heartbeatReason: {
    flex: 1,
    minWidth: 0,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  heartbeatFields: {
    gap: 4,
    minWidth: 0,
  },
  heartbeatFieldRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    minWidth: 0,
  },
  heartbeatFieldLabel: {
    ...TypeScale.micro,
    width: 68,
    textTransform: "uppercase",
  },
  heartbeatFieldValue: {
    ...TypeScale.caption,
    flex: 1,
    minWidth: 0,
  },
  heartbeatSummary: {
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
    paddingVertical: 8,
    minWidth: 0,
  },
  heartbeatSummaryText: {
    ...TypeScale.caption,
  },
});
