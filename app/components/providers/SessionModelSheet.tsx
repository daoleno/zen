import React, { useCallback, useEffect, useMemo, useRef } from "react";
import {
  ActivityIndicator,
  Pressable,
  StyleSheet,
  Text,
  useWindowDimensions,
  View,
} from "react-native";
import BottomSheet, {
  BottomSheetScrollView,
} from "@expo/ui/community/bottom-sheet";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale, UiTextMetrics } from "../../constants/tokens";
import type {
  ProviderError,
  ProviderSessionSelection,
} from "../../services/providers";
import {
  reasoningEffortLabel,
  type ProviderPickerModelRow,
  type SessionEffortContract,
} from "../../services/providers/sessionModelHelpers";
import { resolveModelSheetListMaxHeight } from "./sessionModelSheetModel";

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
  /**
   * Enabled+available Models of the Settings-selected (preferred) Provider
   * only. No Provider groups, names, or cross-Provider inventory ever
   * reaches this sheet.
   */
  rows: ProviderPickerModelRow[];  /**
   * Model-required state: the Session route still runs another Provider than
   * the preferred one, so sending is blocked and the user must pick a model
   * here to activate the exact preferred connection_id + model_id.
   */
  modelRequired: boolean;
  /**
   * Authoritative daemon-projected Reasoning Effort contract of the Session's
   * model. Null hides the section entirely (unsupported client/model) — a
   * dead or misleading Effort control is never rendered.
   */
  effortContract?: SessionEffortContract | null;
  /** Current effort choice (daemon projection rebased; wire value). */
  effortChoice?: string;
  /** True while an activation is in flight (effort rows disabled). */
  effortDisabled?: boolean;
  onEffortChange(value: string): void;
  /**
   * Truthful managed-Codex handoff warning: the route switched, but the
   * running Codex window could not be switched to the new identity.
   */
  handoffWarning?: string | null;
  chrome: TerminalThemeChrome;
  onRetry(): void;
  onClose(): void;
  onActivate(choice: SessionModelChoice): void;
}

/**
 * Native bottom-sheet Model picker for the current Session. Presented by the
 * platform sheet (SwiftUI on iOS, Material3 ModalBottomSheet on Android), so
 * safe areas, keyboard avoidance, scrim dismiss, and swipe-down dismissal are
 * handled natively — the sheet never positions itself on screen coordinates.
 *
 * `visible` is the single open/close truth: opening presents the sheet,
 * closing dismisses it. Native dismissals (swipe, scrim tap, Android back)
 * arrive through `onClose`, and the sheet's closedRef guard keeps the
 * programmatic dismiss on the next `visible=false` from double-firing.
 *
 * Inventory is the Settings-selected (preferred) Provider's enabled+available
 * Models only. Each selectable row carries the exact stable (connection_id,
 * model_id) pair and activates the current Session only — never other
 * Providers, never other Sessions, never catalog defaults, and never a
 * substituted model. The check appears only on the running pair. In the
 * model-required state nothing is checked and a concise request to choose a
 * Model is shown; the daemon never falls back. Loading, error, and retry
 * appear only when genuinely needed.
 */
export function SessionModelSheet({
  visible,
  loading,
  activating,
  error,
  selection,
  rows,
  modelRequired,
  effortContract,
  effortChoice = "",
  effortDisabled = false,
  onEffortChange,
  handoffWarning,
  chrome,
  onRetry,
  onClose,
  onActivate,
}: SessionModelSheetProps) {
  const sheetRef = useRef<BottomSheet>(null);
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const { height: windowHeight } = useWindowDimensions();
  const errorMessage =
    typeof error === "string" ? error : error?.message ?? null;
  const refreshable =
    typeof error === "object" && error != null ? error.refreshable : true;
  const effortCount = effortContract?.supported.length ?? 0;
  const listMaxHeight = useMemo(
    () =>
      resolveModelSheetListMaxHeight({
        windowHeight,
        groupCount: 0,
        modelCount: rows.length,
        effortCount,
      }),
    [rows.length, windowHeight, effortCount],
  );

  // Controlled open/close: the ref-driven native sheet follows `visible`.
  // Programmatic dismiss after a native dismissal is guarded inside the sheet,
  // so the close callbacks cannot loop.
  useEffect(() => {
    if (visible) {
      sheetRef.current?.present();
    } else {
      sheetRef.current?.dismiss();
    }
  }, [visible]);

  const handleClose = useCallback(() => {
    onClose();
  }, [onClose]);

  const handleActivateRow = useCallback(
    (row: ProviderPickerModelRow) => {
      if (row.disabled) return;
      onActivate({ connectionId: row.connectionId, modelId: row.modelId });
    },
    [onActivate],
  );

  return (
    <BottomSheet
      ref={sheetRef}
      index={-1}
      enablePanDownToClose
      backgroundStyle={{ backgroundColor: chrome.surface }}
      onClose={handleClose}
      onDismiss={handleClose}
    >
      <View style={styles.header}>
        <Text
          style={[styles.title, { color: chrome.text }]}
          accessibilityRole="header"
          numberOfLines={1}
        >
          Model
        </Text>
        <Pressable
          accessibilityLabel="Close model selection"
          accessibilityRole="button"
          style={[styles.close, { backgroundColor: chrome.surfaceMuted }]}
          onPress={handleClose}
        >
          <Ionicons name="close" size={18} color={chrome.textSubtle} />
        </Pressable>
      </View>
      <Text style={[styles.nextMessageNote, { color: chrome.textMuted }]}>
        Applies to the next message in this chat.
      </Text>

      {loading && !selection ? (
        <View style={styles.center}>
          <ActivityIndicator color={chrome.accent} />
        </View>
      ) : null}

      {errorMessage && rows.length === 0 ? (
        <View style={styles.stateBlock}>
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
      ) : (
        <BottomSheetScrollView
          style={{ maxHeight: listMaxHeight }}
          contentContainerStyle={styles.body}
          showsVerticalScrollIndicator={false}
        >
          {modelRequired && rows.length > 0 ? (
            <View style={styles.requiredBlock}>
              <Ionicons
                name="alert-circle-outline"
                size={16}
                color={chrome.accent}
              />
              <Text style={[styles.requiredText, { color: chrome.text }]}>
                Choose a model to continue this chat. Sending is paused until
                you select one.
              </Text>
            </View>
          ) : null}

          {rows.map((row) => {
            const checked = row.current && !modelRequired;
            return (
              <Pressable
                key={row.key}
                style={[
                  styles.modelRow,
                  {
                    backgroundColor: checked
                      ? chrome.accentSoft
                      : chrome.surfaceMuted,
                    opacity: row.disabled && !checked ? 0.55 : 1,
                  },
                ]}
                disabled={row.disabled}
                accessibilityRole="button"
                accessibilityState={{
                  disabled: row.disabled,
                  selected: checked,
                }}
                accessibilityLabel={`Use ${row.modelId}`}
                onPress={() => handleActivateRow(row)}
              >
                <View style={styles.modelRowText}>
                  <Text
                    style={[styles.modelText, { color: chrome.text }]}
                    numberOfLines={1}
                    ellipsizeMode="tail"
                  >
                    {row.label}
                  </Text>
                  {row.unavailableCurrent ? (
                    <Text
                      style={[
                        styles.modelCaption,
                        { color: chrome.textMuted },
                      ]}
                    >
                      Currently running; no longer available for switching.
                    </Text>
                  ) : null}
                  {row.unsupported ? (
                    <Text
                      style={[
                        styles.modelCaption,
                        { color: chrome.textMuted },
                      ]}
                    >
                      Not supported by Zen for managed Codex.
                    </Text>
                  ) : null}
                </View>
                {checked ? (
                  <Ionicons name="checkmark" size={16} color={chrome.accent} />
                ) : null}
              </Pressable>
            );
          })}

          {!modelRequired && effortContract && effortCount > 0 ? (
            <View style={styles.effortSection}>
              <Text
                style={[styles.effortHeader, { color: chrome.textMuted }]}
                accessibilityRole="header"
              >
                Reasoning Effort
              </Text>
              {effortContract.supported.map((value) => {
                const checked = effortChoice === value;
                const disabled = effortDisabled;
                return (
                  <Pressable
                    key={`effort:${value}`}
                    style={[
                      styles.effortRow,
                      {
                        backgroundColor: checked
                          ? chrome.accentSoft
                          : chrome.surfaceMuted,
                        opacity: disabled && !checked ? 0.55 : 1,
                      },
                    ]}
                    disabled={disabled}
                    accessibilityRole="button"
                    accessibilityState={{
                      disabled,
                      selected: checked,
                    }}
                    accessibilityLabel={`Reasoning effort ${reasoningEffortLabel(value)}`}
                    onPress={() => onEffortChange(value)}
                  >
                    <Text
                      style={[styles.effortText, { color: chrome.text }]}
                      numberOfLines={1}
                    >
                      {reasoningEffortLabel(value)}
                    </Text>
                    {value === effortContract.defaultEffort &&
                    effortContract.override === "" ? (
                      <Text
                        style={[
                          styles.effortCaption,
                          { color: chrome.textMuted },
                        ]}
                      >
                        Model default
                      </Text>
                    ) : null}
                    {checked ? (
                      <Ionicons
                        name="checkmark"
                        size={16}
                        color={chrome.accent}
                      />
                    ) : null}
                  </Pressable>
                );
              })}
              {handoffWarning ? (
                <View style={styles.stateRow}>
                  <Text
                    style={[styles.stateBody, { color: chrome.textMuted }]}
                    accessibilityRole="summary"
                  >
                    {handoffWarning}
                  </Text>
                </View>
              ) : null}
            </View>
          ) : null}

          {activating ? (
            <View style={styles.activatingRow}>
              <ActivityIndicator size="small" color={chrome.accent} />
              <Text
                style={[styles.activatingText, { color: chrome.textMuted }]}
              >
                Switching…
              </Text>
            </View>
          ) : null}

          {errorMessage && rows.length > 0 ? (
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

          {!activating && !errorMessage && rows.length === 0 ? (
            <Text style={[styles.stateBody, { color: chrome.textMuted }]}>
              No models available yet. Sync models in Settings.
            </Text>
          ) : null}
        </BottomSheetScrollView>
      )}
    </BottomSheet>
  );
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    header: {
      flexDirection: "row",
      alignItems: "center",
      paddingHorizontal: 16,
      paddingTop: 10,
      paddingBottom: 6,
      gap: 10,
    },
    title: { ...TypeScale.title, flex: 1 },
    nextMessageNote: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      paddingHorizontal: 16,
      paddingBottom: 6,
    },
    close: {
      width: 30,
      height: 30,
      borderRadius: 15,
      alignItems: "center",
      justifyContent: "center",
    },
    center: { paddingVertical: 32, alignItems: "center" },
    stateBlock: { paddingHorizontal: 16, gap: 8, paddingBottom: 16 },
    stateRow: { gap: 4, paddingTop: 4, paddingHorizontal: 16 },
    stateBody: { ...TypeScale.body },
    link: { ...TypeScale.body, fontWeight: "600" },
    body: { paddingHorizontal: 16, gap: 6, paddingBottom: 16 },
    requiredBlock: {
      flexDirection: "row",
      alignItems: "flex-start",
      gap: 8,
      borderRadius: 10,
      paddingHorizontal: 12,
      paddingVertical: 10,
      backgroundColor: chrome.accentSoft,
    },
    requiredText: { ...TypeScale.body, flex: 1 },
    modelRow: {
      borderRadius: 10,
      paddingHorizontal: 12,
      paddingVertical: 10,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 8,
    },
    modelRowText: { flexShrink: 1, gap: 2 },
    modelText: { ...TypeScale.body, flexShrink: 1 },
    modelCaption: { ...TypeScale.caption },
    effortSection: { gap: 6, marginTop: 10 },
    effortHeader: { ...TypeScale.caption, fontWeight: "600", paddingLeft: 2 },
    effortRow: {
      borderRadius: 10,
      paddingHorizontal: 12,
      paddingVertical: 9,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 8,
    },
    effortText: { ...TypeScale.body, flexShrink: 1 },
    effortCaption: { ...TypeScale.caption },
    activatingRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
      paddingVertical: 6,
      paddingHorizontal: 16,
    },
    activatingText: { ...TypeScale.body },
  });
}
