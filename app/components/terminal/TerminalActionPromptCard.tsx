import React, {
  useEffect,
  useState,
} from "react";
import {
  ActivityIndicator,
  Pressable,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { TerminalThemePalette } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { TerminalActionPrompt } from "./TerminalActionPromptModel";

interface TerminalActionPromptCardProps {
  prompt: TerminalActionPrompt;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onSendKey(key: string): Promise<void> | void;
  onSwitchToTerminal?: () => void;
}

export function TerminalActionPromptCard({
  prompt,
  chrome,
  theme,
  onSendKey,
  onSwitchToTerminal,
}: TerminalActionPromptCardProps) {
  const [sendingOptionId, setSendingOptionId] = useState<string | null>(null);
  const [sentOptionId, setSentOptionId] = useState<string | null>(null);
  const [failedOptionId, setFailedOptionId] = useState<string | null>(null);

  useEffect(() => {
    setSendingOptionId(null);
    setSentOptionId(null);
    setFailedOptionId(null);
  }, [prompt.id]);

  const handleOptionPress = (option: TerminalActionPrompt["options"][number]) => {
    if (sendingOptionId) {
      return;
    }
    setSendingOptionId(option.id);
    setSentOptionId(null);
    setFailedOptionId(null);
    Promise.resolve()
      .then(() => onSendKey(option.key))
      .then(() => {
        setSentOptionId(option.id);
      })
      .catch(() => {
        setFailedOptionId(option.id);
      })
      .finally(() => {
        setSendingOptionId(null);
      });
  };
  const statusOption = prompt.options.find(
    (option) => option.id === (sendingOptionId || sentOptionId || failedOptionId),
  );
  const statusTone =
    failedOptionId ? "failed" : sendingOptionId ? "sending" : sentOptionId ? "sent" : null;
  const statusText =
    statusTone === "failed"
      ? "Could not send. Check the connection and try again."
      : statusTone === "sending"
        ? `Sending ${statusOption?.label ?? "decision"}...`
        : statusTone === "sent"
          ? `Sent ${statusOption?.label ?? "decision"}. Waiting for Codex to continue.`
          : null;

  return (
    <View
      style={[
        styles.card,
        {
          backgroundColor: chrome.surface,
          borderColor: chrome.borderStrong,
        },
      ]}
    >
      <View style={styles.header}>
        <View style={[styles.iconBadge, { backgroundColor: chrome.accentSoft }]}>
          <Ionicons name="alert-circle" size={15} color={chrome.accent} />
        </View>
        <View style={styles.headerText}>
          <Text style={[styles.title, { color: chrome.text }]} numberOfLines={1}>
            {prompt.title}
          </Text>
          <Text style={[styles.detail, { color: chrome.textMuted }]} numberOfLines={2}>
            {prompt.detail}
          </Text>
        </View>
      </View>

      {prompt.requestText ? (
        <View
          style={[
            styles.requestBlock,
            {
              backgroundColor: chrome.surfaceMuted,
              borderColor: chrome.border,
            },
          ]}
        >
          {prompt.requestLabel ? (
            <Text style={[styles.requestLabel, { color: chrome.textMuted }]}>
              {prompt.requestLabel}
            </Text>
          ) : null}
          <Text style={[styles.requestText, { color: chrome.text }]} numberOfLines={3}>
            {prompt.requestText}
          </Text>
        </View>
      ) : null}

      <View style={styles.optionColumn}>
        {prompt.options.map((option) => (
          <Pressable
            key={option.id}
            accessibilityRole="button"
            accessibilityLabel={[
              option.label,
              option.default ? "Default action" : "",
              option.description,
            ].filter(Boolean).join(", ")}
            accessibilityState={{
              disabled: Boolean(sendingOptionId),
              selected: option.id === prompt.defaultOptionId,
            }}
            disabled={Boolean(sendingOptionId)}
            onPress={() => handleOptionPress(option)}
            style={({ pressed }) => [
              styles.optionButton,
              {
                backgroundColor: option.primary ? chrome.accent : chrome.surfaceMuted,
                borderColor: option.primary ? chrome.accent : chrome.border,
              },
              option.id === sentOptionId ? { borderColor: chrome.accent } : null,
              option.id === failedOptionId ? { borderColor: "#FCA5A5" } : null,
              pressed ? styles.pressed : null,
              sendingOptionId ? styles.optionButtonDisabled : null,
            ]}
          >
            <View style={styles.optionCopy}>
              <View style={styles.optionTitleRow}>
                <Text
                  style={[
                    styles.optionText,
                    { color: option.primary ? theme.background : option.destructive ? "#FCA5A5" : chrome.text },
                  ]}
                  numberOfLines={2}
                >
                  {option.label}
                </Text>
                {option.default ? (
                  <View
                    style={[
                      styles.defaultPill,
                      {
                        backgroundColor: option.primary ? "rgba(0,0,0,0.18)" : chrome.surface,
                        borderColor: option.primary ? "rgba(0,0,0,0.16)" : chrome.border,
                      },
                    ]}
                  >
                    <Text
                      style={[
                        styles.defaultText,
                        { color: option.primary ? theme.background : chrome.textMuted },
                      ]}
                    >
                      Default
                    </Text>
                  </View>
                ) : null}
              </View>
              {option.description ? (
                <Text
                  style={[
                    styles.optionDescription,
                    { color: option.primary ? theme.background : chrome.textMuted },
                  ]}
                  numberOfLines={2}
                >
                  {option.description}
                </Text>
              ) : null}
            </View>
            {sendingOptionId === option.id ? (
              <ActivityIndicator
                size="small"
                color={option.primary ? theme.background : chrome.accent}
              />
            ) : sentOptionId === option.id ? (
              <Ionicons
                name="checkmark-circle"
                size={17}
                color={option.primary ? theme.background : chrome.accent}
              />
            ) : failedOptionId === option.id ? (
              <Ionicons name="alert-circle" size={17} color="#FCA5A5" />
            ) : null}
          </Pressable>
        ))}
      </View>

      {statusText ? (
        <Text
          style={[
            styles.statusText,
            { color: statusTone === "failed" ? "#FCA5A5" : chrome.textMuted },
          ]}
          numberOfLines={2}
        >
          {statusText}
        </Text>
      ) : null}

      <View style={styles.secondaryRow}>
        {onSwitchToTerminal ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Open raw terminal"
            onPress={onSwitchToTerminal}
            style={({ pressed }) => [styles.secondaryButton, pressed ? styles.pressed : null]}
          >
            <Text style={[styles.secondaryText, { color: chrome.accent }]}>Terminal</Text>
          </Pressable>
        ) : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  card: {
    marginHorizontal: 12,
    marginBottom: 8,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 8,
    padding: 10,
    gap: 10,
  },
  header: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 9,
  },
  iconBadge: {
    width: 24,
    height: 24,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
    marginTop: 1,
  },
  headerText: {
    flex: 1,
    minWidth: 0,
  },
  title: {
    fontFamily: Typography.chatFontMedium,
    fontSize: 13,
    lineHeight: 18,
    letterSpacing: 0,
  },
  detail: {
    fontFamily: Typography.chatFont,
    fontSize: 12,
    lineHeight: 17,
    letterSpacing: 0,
  },
  requestBlock: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 8,
    paddingHorizontal: 9,
    paddingVertical: 8,
    gap: 3,
  },
  requestLabel: {
    fontFamily: Typography.chatFontMedium,
    fontSize: 10,
    lineHeight: 13,
    letterSpacing: 0,
    textTransform: "uppercase",
  },
  requestText: {
    fontFamily: Typography.chatMonoFont,
    fontSize: 12,
    lineHeight: 17,
    letterSpacing: 0,
  },
  optionColumn: {
    gap: 8,
  },
  optionButton: {
    minHeight: 48,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "space-between",
    gap: 10,
    paddingHorizontal: 10,
    paddingVertical: 8,
  },
  optionButtonDisabled: {
    opacity: 0.72,
  },
  optionCopy: {
    flex: 1,
    minWidth: 0,
    gap: 2,
  },
  optionTitleRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    minWidth: 0,
  },
  optionText: {
    flexShrink: 1,
    minWidth: 0,
    fontFamily: Typography.chatFontMedium,
    fontSize: 12,
    lineHeight: 16,
    letterSpacing: 0,
  },
  optionDescription: {
    fontFamily: Typography.chatFont,
    fontSize: 11,
    lineHeight: 15,
    letterSpacing: 0,
    opacity: 0.78,
  },
  defaultPill: {
    height: 17,
    paddingHorizontal: 6,
    borderRadius: 4,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  defaultText: {
    fontFamily: Typography.chatFontMedium,
    fontSize: 9,
    lineHeight: 12,
    letterSpacing: 0,
  },
  statusText: {
    fontFamily: Typography.chatFont,
    fontSize: 11,
    lineHeight: 15,
    letterSpacing: 0,
  },
  secondaryRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 8,
  },
  secondaryButton: {
    minHeight: 28,
    justifyContent: "center",
    paddingHorizontal: 8,
  },
  secondaryText: {
    fontFamily: Typography.chatMonoFont,
    fontSize: 12,
    lineHeight: 16,
    letterSpacing: 0,
  },
  pressed: {
    opacity: 0.72,
  },
});
