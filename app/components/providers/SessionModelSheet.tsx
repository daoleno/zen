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
  ProviderConnection,
  ProviderError,
  ProviderModel,
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
  durabilityWarning?: string | null;
  requiresRefreshBeforeMutation?: boolean;
  managedReadOnly?: boolean;
  activationEnabled?: boolean;
  selection?: ProviderSessionSelection | null;
  connections: ProviderConnection[];
  modelsByConnection: Record<string, ProviderModel[]>;
  chrome: TerminalThemeChrome;
  onRetry(): void;
  onClose(): void;
  onOpenProvidersSettings(): void;
  onActivate(choice: SessionModelChoice): void;
}

function clientLabel(client: string): string {
  const c = client.trim().toLowerCase();
  if (c === "codex") return "Codex";
  if (c === "claude") return "Claude";
  return client;
}

export function SessionModelSheet({
  visible,
  loading,
  activating,
  error,
  durabilityWarning,
  requiresRefreshBeforeMutation,
  managedReadOnly,
  activationEnabled,
  selection,
  connections,
  modelsByConnection,
  chrome,
  onRetry,
  onClose,
  onOpenProvidersSettings,
  onActivate,
}: SessionModelSheetProps) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const errorMessage =
    typeof error === "string" ? error : error?.message ?? null;
  const refreshable =
    typeof error === "object" && error != null ? error.refreshable : true;
  const canActivate = activationEnabled === true && managedReadOnly !== true;

  return (
    <BottomSheetFrame
      visible={visible}
      maxHeight="78%"
      cardStyle={styles.sheet}
      onClose={onClose}
    >
      <View style={styles.header}>
        <View style={[styles.icon, { backgroundColor: chrome.accentSoft }]}>
          <Ionicons name="sparkles-outline" size={18} color={chrome.accent} />
        </View>
        <View style={styles.headerCopy}>
          <Text
            style={[styles.title, { color: chrome.text }]}
            accessibilityRole="header"
          >
            Model
          </Text>
        </View>
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

      {errorMessage && !selection ? (
        <View style={styles.stateBlock}>
          <Text style={[styles.stateTitle, { color: chrome.text }]}>
            {refreshable ? "Refresh required" : "Could not load"}
          </Text>
          <Text style={[styles.stateBody, { color: chrome.textMuted }]}>
            {errorMessage}
          </Text>
          {refreshable ? (
            <Pressable onPress={onRetry} accessibilityRole="button">
              <Text style={[styles.link, { color: chrome.accent }]}>
                Refresh
              </Text>
            </Pressable>
          ) : null}
        </View>
      ) : null}

      {selection ? (
        <ScrollView
          contentContainerStyle={styles.body}
          showsVerticalScrollIndicator={false}
        >
          <View style={styles.currentCard}>
            <Text style={[styles.sectionLabel, { color: chrome.textSubtle }]}>
              Current
            </Text>
            <Text style={[styles.currentTitle, { color: chrome.text }]}>
              {selection.connection_name}
            </Text>
            <Text style={[styles.currentMeta, { color: chrome.textMuted }]}>
              {selection.model_id} · {clientLabel(selection.client)}
            </Text>
            {managedReadOnly ? (
              <Text style={[styles.hint, { color: chrome.textMuted }]}>
                This Session is managed read-only. Model switch is unavailable.
              </Text>
            ) : null}
          </View>

          {durabilityWarning ? (
            <Text style={[styles.hint, { color: chrome.textMuted }]}>
              {durabilityWarning}
            </Text>
          ) : null}

          {requiresRefreshBeforeMutation ? (
            <Pressable onPress={onRetry} accessibilityRole="button">
              <Text style={[styles.link, { color: chrome.accent }]}>
                Refresh before switching
              </Text>
            </Pressable>
          ) : null}

          {errorMessage ? (
            <Text style={[styles.hint, { color: chrome.textMuted }]}>
              {errorMessage}
            </Text>
          ) : null}

          <Text style={[styles.sectionLabel, { color: chrome.textSubtle }]}>
            Providers
          </Text>

          {connections.length === 0 ? (
            <Text style={[styles.hint, { color: chrome.textMuted }]}>
              No Providers match this Session client. Add one in Settings.
            </Text>
          ) : null}

          {connections.map((connection) => {
            const models = (modelsByConnection[connection.id] ?? []).filter(
              (m) => m.available,
            );
            const ready = connection.credential_ready;
            return (
              <View key={connection.id} style={styles.group}>
                <Text style={[styles.groupTitle, { color: chrome.text }]}>
                  {connection.name}
                </Text>
                {!ready ? (
                  <Pressable
                    onPress={onOpenProvidersSettings}
                    accessibilityRole="button"
                    accessibilityLabel="Add API key in Settings"
                  >
                    <Text style={[styles.link, { color: chrome.accent }]}>
                      Add API key in Settings → Providers
                    </Text>
                  </Pressable>
                ) : models.length === 0 ? (
                  <Text style={[styles.hint, { color: chrome.textMuted }]}>
                    No models discovered yet.
                  </Text>
                ) : (
                  models.map((model) => {
                    const selected =
                      selection.connection_id === connection.id &&
                      selection.model_id === model.id;
                    const disabled =
                      !canActivate ||
                      activating ||
                      requiresRefreshBeforeMutation ||
                      selected;
                    return (
                      <Pressable
                        key={`${connection.id}:${model.id}`}
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
                        accessibilityLabel={`Use ${model.id}`}
                        onPress={() =>
                          onActivate({
                            connectionId: connection.id,
                            modelId: model.id,
                          })
                        }
                      >
                        <Text
                          style={[styles.modelText, { color: chrome.text }]}
                        >
                          {model.id}
                        </Text>
                        {selected ? (
                          <Ionicons
                            name="checkmark"
                            size={16}
                            color={chrome.accent}
                          />
                        ) : null}
                      </Pressable>
                    );
                  })
                )}
              </View>
            );
          })}
        </ScrollView>
      ) : null}
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
    icon: {
      width: 32,
      height: 32,
      borderRadius: 10,
      alignItems: "center",
      justifyContent: "center",
    },
    headerCopy: { flex: 1 },
    title: { ...TypeScale.title },
    close: {
      width: 32,
      height: 32,
      borderRadius: 16,
      alignItems: "center",
      justifyContent: "center",
    },
    center: { paddingVertical: 40, alignItems: "center" },
    stateBlock: { paddingHorizontal: 16, gap: 8 },
    stateTitle: { ...TypeScale.title },
    stateBody: { ...TypeScale.body },
    link: { ...TypeScale.body, fontWeight: "600" },
    body: { paddingHorizontal: 16, paddingBottom: 24, gap: 12 },
    currentCard: { gap: 4 },
    sectionLabel: {
      ...TypeScale.caption,
      textTransform: "uppercase",
      letterSpacing: 0.4,
      marginTop: 4,
    },
    currentTitle: { ...TypeScale.title },
    currentMeta: { ...TypeScale.caption },
    hint: { ...TypeScale.caption },
    group: { gap: 8 },
    groupTitle: { ...TypeScale.body, fontWeight: "700" },
    modelRow: {
      borderRadius: 12,
      paddingHorizontal: 12,
      paddingVertical: 12,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
    },
    modelText: { ...TypeScale.body, flex: 1 },
  });
}
