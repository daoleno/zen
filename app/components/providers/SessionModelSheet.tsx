import React, { useMemo } from "react";
import {
  ActivityIndicator,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale } from "../../constants/tokens";
import type {
  ProviderError,
  ProviderModelChoice,
  ProviderSessionSelection,
} from "../../services/providers";
import { BottomSheetFrame } from "../ui";

export type SessionModelChoice = {
  connectionId: string;
  modelId: string;
};

interface SessionModelSheetProps {
  visible: boolean;
  loading: boolean;
  activating: boolean;
  error?: ProviderError | string | null;
  selection?: ProviderSessionSelection | null;
  /** Models of this Session's bound connection only (never other connections). */
  choices: ProviderModelChoice[];
  chrome: TerminalThemeChrome;
  onRetry(): void;
  onClose(): void;
  onActivate(choice: SessionModelChoice): void;
}

/**
 * Minimal current-Session model picker. Lists only the models of the bound
 * connection with the current selection checked; loading, error, and retry
 * appear only when genuinely needed. Provider configuration stays in
 * Settings — this surface never lists connections, shows Base URLs, or
 * opens Provider Settings.
 */
export function SessionModelSheet({
  visible,
  loading,
  activating,
  error,
  selection,
  choices,
  chrome,
  onRetry,
  onClose,
  onActivate,
}: SessionModelSheetProps) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const errorMessage =
    typeof error === "string" ? error : error?.message ?? null;
  const refreshable =
    typeof error === "object" && error != null ? error.refreshable : true;
  return (
    <BottomSheetFrame
      visible={visible}
      maxHeight="52%"
      cardStyle={styles.sheet}
      onClose={onClose}
    >
      <View style={styles.header}>
        <Text style={[styles.title, { color: chrome.text }]} accessibilityRole="header">
          Model
        </Text>
        <Pressable
          accessibilityLabel="Close model"
          accessibilityRole="button"
          style={[styles.close, { backgroundColor: chrome.surfaceMuted }]}
          onPress={onClose}
        >
          <Ionicons name="close" size={18} color={chrome.textSubtle} />
        </Pressable>
      </View>

      {loading && !selection ? (
        <View style={styles.center}>
          <ActivityIndicator color={chrome.accent} />
        </View>
      ) : null}

      {errorMessage && choices.length === 0 ? (
        <View style={styles.stateBlock}>
          <Text style={[styles.stateBody, { color: chrome.textMuted }]}>
            {errorMessage}
          </Text>
          {refreshable ? (
            <Pressable onPress={onRetry} accessibilityRole="button">
              <Text style={[styles.link, { color: chrome.accent }]}>Retry</Text>
            </Pressable>
          ) : null}
        </View>
      ) : (
        <ScrollView
          contentContainerStyle={styles.body}
          showsVerticalScrollIndicator={false}
        >
          {choices.map((choice) => {
            const selected = choice.current;
            const disabled = activating || choice.disabled || selected;
            return (
              <Pressable
                key={`${choice.connection.id}:${choice.model.id}`}
                style={[
                  styles.modelRow,
                  {
                    backgroundColor: selected
                      ? chrome.accentSoft
                      : chrome.surfaceMuted,
                    opacity: disabled && !selected ? 0.55 : 1,
                  },
                ]}
                disabled={disabled}
                accessibilityRole="button"
                accessibilityState={{
                  disabled,
                  selected,
                }}
                accessibilityLabel={`Use ${choice.model.id}`}
                onPress={() =>
                  onActivate({
                    connectionId: choice.connection.id,
                    modelId: choice.model.id,
                  })
                }
              >
                <Text style={[styles.modelText, { color: chrome.text }]}>
                  {choice.model.id}
                </Text>
                {selected ? (
                  <Ionicons name="checkmark" size={16} color={chrome.accent} />
                ) : null}
              </Pressable>
            );
          })}

          {activating ? (
            <View style={styles.activatingRow}>
              <ActivityIndicator size="small" color={chrome.accent} />
              <Text style={[styles.activatingText, { color: chrome.textMuted }]}>
                Switching…
              </Text>
            </View>
          ) : null}

          {errorMessage && choices.length > 0 ? (
            <View style={styles.stateRow}>
              <Text style={[styles.stateBody, { color: chrome.textMuted }]}>
                {errorMessage}
              </Text>
              {refreshable ? (
                <Pressable onPress={onRetry} accessibilityRole="button">
                  <Text style={[styles.link, { color: chrome.accent }]}>
                    Retry
                  </Text>
                </Pressable>
              ) : null}
            </View>
          ) : null}

          {!activating && !errorMessage && choices.length === 0 ? (
            <Text style={[styles.stateBody, { color: chrome.textMuted }]}>
              No models discovered for this Session yet.
            </Text>
          ) : null}
        </ScrollView>
      )}
    </BottomSheetFrame>
  );
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    sheet: {
      backgroundColor: chrome.surface,
      paddingBottom: 20,
    },
    header: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingTop: 14,
      paddingBottom: 10,
      gap: 10,
    },
    title: { ...TypeScale.title, flex: 1 },
    close: {
      width: 32,
      height: 32,
      borderRadius: 16,
      alignItems: "center",
      justifyContent: "center",
    },
    center: { paddingVertical: 40, alignItems: "center" },
    stateBlock: { paddingHorizontal: 16, gap: 8, paddingBottom: 16 },
    stateRow: { gap: 4, paddingTop: 4 },
    stateBody: { ...TypeScale.body },
    link: { ...TypeScale.body, fontWeight: "600" },
    body: { paddingHorizontal: 16, paddingBottom: 24, gap: 8 },
    modelRow: {
      borderRadius: 12,
      paddingHorizontal: 12,
      paddingVertical: 12,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
    },
    modelText: { ...TypeScale.body, flex: 1 },
    activatingRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
      paddingVertical: 6,
    },
    activatingText: { ...TypeScale.body },
  });
}
