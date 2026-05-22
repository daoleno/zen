import React from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";

export interface CodexComposerAttachment {
  id: string;
  name: string;
  path: string;
}

interface CodexComposerAttachmentChipProps {
  attachment: CodexComposerAttachment;
  chrome: TerminalThemeChrome;
  onRemove(id: string): void;
}

export function CodexComposerAttachmentChip({
  attachment,
  chrome,
  onRemove,
}: CodexComposerAttachmentChipProps) {
  return (
    <View
      style={[
        styles.chip,
        { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
      ]}
    >
      <Ionicons
        name={
          looksLikeImagePath(attachment.name)
            ? "image-outline"
            : "document-attach-outline"
        }
        size={14}
        color={chrome.textMuted}
      />
      <View style={styles.textGroup}>
        <Text style={[styles.name, { color: chrome.text }]} numberOfLines={1}>
          {attachment.name}
        </Text>
        <Text
          style={[styles.path, { color: chrome.textSubtle }]}
          numberOfLines={1}
        >
          {basename(attachment.path)}
        </Text>
      </View>
      <TouchableOpacity
        accessibilityLabel={`Remove ${attachment.name}`}
        style={styles.remove}
        onPress={() => onRemove(attachment.id)}
        activeOpacity={0.72}
      >
        <Ionicons name="close" size={13} color={chrome.textSubtle} />
      </TouchableOpacity>
    </View>
  );
}

export function CodexComposerUploadingChip({
  chrome,
}: {
  chrome: TerminalThemeChrome;
}) {
  return (
    <View
      style={[
        styles.chip,
        styles.uploading,
        { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
      ]}
    >
      <ActivityIndicator size="small" color={chrome.accent} />
      <Text style={[styles.name, { color: chrome.textMuted }]}>Uploading</Text>
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
  chip: {
    maxWidth: 220,
    minHeight: 36,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingLeft: 9,
    paddingRight: 5,
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
  },
  uploading: {
    paddingRight: 10,
  },
  textGroup: {
    flex: 1,
    minWidth: 0,
  },
  name: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
  path: {
    marginTop: 1,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.terminalFont,
  },
  remove: {
    width: 24,
    height: 28,
    alignItems: "center",
    justifyContent: "center",
  },
});
