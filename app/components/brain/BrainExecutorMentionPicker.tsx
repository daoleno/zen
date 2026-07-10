import React, { useMemo } from "react";
import { FlatList, Pressable, StyleSheet, Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import {
  Radii,
  TypeScale,
  Typography,
  UiTextMetrics,
} from "../../constants/tokens";
import type { BrainAdapterRef } from "../../store/brain";
import { BrainAdapterIcon } from "./BrainAdapterIcon";
import { brainAdapterLabel, brainProviderLabel } from "./brainPresentation";

interface BrainExecutorMentionPickerProps {
  adapters: BrainAdapterRef[];
  activeAdapterId?: string;
  query: string;
  chrome: TerminalThemeChrome;
  onSelect(adapter: BrainAdapterRef): void;
}

export function BrainExecutorMentionPicker({
  adapters,
  activeAdapterId,
  query,
  chrome,
  onSelect,
}: BrainExecutorMentionPickerProps) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    const candidates = adapters.filter((adapter) => adapter.id.trim());
    if (!needle) {
      return candidates;
    }
    return candidates.filter((adapter) => {
      const id = adapter.id.toLowerCase();
      const label = brainAdapterLabel(adapter).toLowerCase();
      const provider = brainProviderLabel(adapter.provider).toLowerCase();
      return (
        id.startsWith(needle) ||
        label.startsWith(needle) ||
        provider.startsWith(needle)
      );
    });
  }, [adapters, query]);

  if (filtered.length === 0) {
    return null;
  }

  return (
    <View style={styles.wrap}>
      <FlatList
        horizontal
        keyboardShouldPersistTaps="always"
        showsHorizontalScrollIndicator={false}
        data={filtered}
        keyExtractor={(adapter) => adapter.id}
        contentContainerStyle={styles.content}
        ItemSeparatorComponent={() => <View style={styles.separator} />}
        renderItem={({ item }) => {
          const active = item.id === activeAdapterId;
          return (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={`Mention ${brainAdapterLabel(item)}`}
              onPress={() => onSelect(item)}
              style={({ pressed }) => [
                styles.chip,
                active ? styles.chipActive : null,
                pressed ? styles.chipPressed : null,
              ]}
            >
              <BrainAdapterIcon adapter={item} size={15} />
              <Text style={styles.handle} numberOfLines={1}>
                @{item.id}
              </Text>
              {active ? (
                <Text style={styles.meta} numberOfLines={1}>
                  host
                </Text>
              ) : item.delegated ? (
                <Text style={styles.meta} numberOfLines={1}>
                  delegated
                </Text>
              ) : null}
            </Pressable>
          );
        }}
      />
    </View>
  );
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    wrap: {
      marginHorizontal: 10,
      marginBottom: 6,
      borderRadius: 16,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: chrome.border,
      backgroundColor: chrome.composerInput,
      overflow: "hidden",
    },
    content: {
      paddingHorizontal: 10,
      paddingVertical: 8,
    },
    separator: {
      width: 8,
    },
    chip: {
      minHeight: 44,
      maxWidth: 180,
      flexDirection: "row",
      alignItems: "center",
      gap: 7,
      borderRadius: Radii.pill,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: chrome.border,
      backgroundColor: chrome.surface,
      paddingHorizontal: 11,
    },
    chipActive: {
      borderColor: chrome.accent,
      backgroundColor: chrome.surfaceActive,
    },
    chipPressed: {
      backgroundColor: chrome.surfaceActive,
    },
    handle: {
      ...UiTextMetrics,
      ...TypeScale.label,
      color: chrome.text,
    },
    meta: {
      ...UiTextMetrics,
      ...TypeScale.micro,
      color: chrome.textMuted,
      fontFamily: Typography.uiFont,
      fontWeight: "400",
    },
  });
}
