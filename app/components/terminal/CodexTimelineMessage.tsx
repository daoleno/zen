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
import { Typography } from "../../constants/tokens";
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

export function ZenUserMessage({
  item,
  chrome,
  theme,
}: {
  item: ZenMessageTimelineItem & { role: "user" };
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
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
            backgroundColor: chrome.surfaceMuted,
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
