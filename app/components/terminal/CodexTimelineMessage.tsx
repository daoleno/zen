import React from "react";
import {
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography, useAppTheme } from "../../constants/tokens";
import { isAmbientChatChrome } from "../../constants/themedSurfaces";
import type { MessagePresentation } from "./CodexTimelineGrouping";
import {
  chromeForSentBubble,
  resolveSentBubbleColor,
} from "./chatBubbleColors";
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
  heartbeatWake?: HeartbeatWakeEvent;
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
  const sentBubbleColor = isChatGpt
    ? zenTheme.chat.sentBubble
    : resolveSentBubbleColor(chrome);
  const sentChrome = chromeForSentBubble(
    isChatGpt
      ? { ...chrome, text: zenTheme.chat.sentText }
      : chrome,
    sentBubbleColor,
  );
  const spacing = messageRowSpacing(
    presentation.compactTop,
    presentation.compactBottom,
    chatLayout,
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
            borderColor: item.pending ? chrome.borderStrong : "transparent",
            opacity: item.pending ? 0.88 : 1,
          },
        ]}
      >
        {hasBody ? (
          <MessageBody value={item.body} chrome={sentChrome} theme={theme} compact />
        ) : null}
        {item.attachments.length > 0 ? (
          <CodexTimelineAttachmentPreviewList
            attachments={item.attachments}
            chrome={sentChrome}
            compact={hasBody}
          />
        ) : null}
        {zenTheme.chat.showTimestamps ? (
          <MessageBubbleFooter
            timestamp={item.timestamp}
            tone="sent"
            pending={item.pending}
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

  return (
    <View style={styles.eventRow}>
      <View
        style={[
          styles.heartbeatCard,
          {
            backgroundColor: isAmbientChatChrome(chrome)
              ? chrome.surface
              : chrome.surfaceMuted,
            borderColor: chrome.borderStrong,
          },
        ]}
      >
        <View style={styles.heartbeatHeader}>
          <View
            style={[
              styles.heartbeatIcon,
              { backgroundColor: chrome.accentSoft },
            ]}
          >
            <Ionicons name="pulse-outline" size={15} color={chrome.accent} />
          </View>
          <View style={styles.heartbeatHeaderCopy}>
            <Text style={[styles.heartbeatTitle, { color: chrome.text }]} numberOfLines={1}>
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
            chrome={chrome}
            monospace={!event.agentName}
          />
          {showAgentId ? (
            <HeartbeatField
              label="ID"
              value={event.agentId || ""}
              chrome={chrome}
              monospace
            />
          ) : null}
          <HeartbeatField
            label={event.oldState || event.newState ? "State" : "Status"}
            value={formatHeartbeatStateChange(event)}
            chrome={chrome}
          />
          {showSeparateStatus ? (
            <HeartbeatField
              label="Status"
              value={formatHeartbeatValue(event.status)}
              chrome={chrome}
            />
          ) : null}
          {event.workspace ? (
            <HeartbeatField
              label="Workspace"
              value={event.workspace}
              chrome={chrome}
              monospace
            />
          ) : null}
        </View>

        {event.summary ? (
          <View
            style={[
              styles.heartbeatSummary,
              {
                backgroundColor: chrome.surface,
                borderColor: chrome.border,
              },
            ]}
          >
            <Text
              style={[styles.heartbeatSummaryText, { color: chrome.textMuted }]}
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
  const isChatGpt = chatLayout === "chatgpt";
  const spacing = messageRowSpacing(
    presentation.compactTop,
    presentation.compactBottom,
    chatLayout,
  );
  const assistantChrome = isChatGpt
    ? { ...chrome, text: zenTheme.chat.receivedText }
    : chrome;
  const showSender =
    senderLabel &&
    presentation.groupPosition !== "middle" &&
    presentation.groupPosition !== "last";

  return (
    <View
      style={[
        isChatGpt ? styles.assistantRowChatGpt : styles.assistantRowFlat,
        spacing,
      ]}
    >
      {showSender ? (
        <Text style={[styles.assistantSender, { color: chrome.accent }]}>
          {senderLabel}
        </Text>
      ) : null}
      <MessageBody
        value={item.body}
        chrome={assistantChrome}
        theme={theme}
        dense
      />
      {zenTheme.chat.showTimestamps ? (
        <MessageBubbleFooter
          timestamp={item.timestamp}
          tone="received"
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  userRow: {
    flexDirection: "row",
    justifyContent: "flex-end",
  },
  userBubble: {
    maxWidth: "92%",
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 13,
    paddingTop: 9,
    paddingBottom: 6,
  },
  userBubbleChatGpt: {
    maxWidth: "88%",
    borderWidth: 0,
    paddingHorizontal: 14,
    paddingVertical: 10,
  },
  assistantRowFlat: {
    alignSelf: "stretch",
    paddingRight: 2,
  },
  assistantRowChatGpt: {
    alignSelf: "stretch",
    paddingRight: 4,
  },
  assistantSender: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 12.5,
    lineHeight: 16,
    marginBottom: 4,
  },
  eventRow: {
    marginBottom: 17,
    paddingRight: 2,
  },
  heartbeatCard: {
    alignSelf: "stretch",
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 11,
    paddingVertical: 10,
    gap: 9,
  },
  heartbeatHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    minWidth: 0,
  },
  heartbeatIcon: {
    width: 28,
    height: 28,
    borderRadius: 8,
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
    flexShrink: 0,
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },
  heartbeatReason: {
    flex: 1,
    minWidth: 0,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  heartbeatFields: {
    gap: 5,
    minWidth: 0,
  },
  heartbeatFieldRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    minWidth: 0,
  },
  heartbeatFieldLabel: {
    width: 68,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFontMedium,
    textTransform: "uppercase",
    opacity: 0.76,
  },
  heartbeatFieldValue: {
    flex: 1,
    minWidth: 0,
    fontSize: 11,
    lineHeight: 15,
  },
  heartbeatSummary: {
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 9,
    paddingVertical: 7,
    minWidth: 0,
  },
  heartbeatSummaryText: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFont,
  },
});