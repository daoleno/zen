import React from "react";
import {
  ActivityIndicator,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

export interface CodexComposerAttachment {
  id: string;
  name: string;
  path: string;
}

interface CodexComposerAttachmentRailProps {
  attachments: CodexComposerAttachment[];
  uploading: boolean;
  chrome: TerminalThemeChrome;
  onRemoveAttachment(id: string): void;
}

export function CodexComposerAttachmentRail({
  attachments,
  uploading,
  chrome,
  onRemoveAttachment,
}: CodexComposerAttachmentRailProps) {
  if (attachments.length === 0 && !uploading) {
    return null;
  }

  return (
    <View style={styles.rail}>
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        keyboardShouldPersistTaps="handled"
        contentContainerStyle={styles.list}
      >
        {attachments.map((attachment) => (
          <View
            key={attachment.id}
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
              <Text
                style={[styles.name, { color: chrome.text }]}
                numberOfLines={1}
              >
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
              onPress={() => onRemoveAttachment(attachment.id)}
              activeOpacity={0.72}
            >
              <Ionicons name="close" size={13} color={chrome.textSubtle} />
            </TouchableOpacity>
          </View>
        ))}
        {uploading ? (
          <View
            style={[
              styles.chip,
              styles.uploading,
              { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
            ]}
          >
            <ActivityIndicator size="small" color={chrome.accent} />
            <Text style={[styles.name, { color: chrome.textMuted }]}>
              Uploading
            </Text>
          </View>
        ) : null}
      </ScrollView>
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
  rail: {
    marginBottom: 7,
  },
  list: {
    minHeight: 38,
    alignItems: "center",
    gap: 7,
    paddingHorizontal: 2,
  },
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
