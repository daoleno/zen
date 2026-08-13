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
import { TypeScale } from "../../constants/tokens";
import type {
  ProviderError,
  ProviderModelChoice,
  ProviderSessionSelection,
} from "../../services/providers";
import {
  buildModelSheetRows,
  resolveModelSheetListMaxHeight,
  type SessionModelSheetRow,
} from "./sessionModelSheetModel";

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
 * Native bottom-sheet model picker for the current Session. Presented by the
 * platform sheet (SwiftUI on iOS, Material3 ModalBottomSheet on Android), so
 * safe areas, keyboard avoidance, scrim dismiss, and swipe-down dismissal are
 * handled natively — the sheet never positions itself on screen coordinates.
 *
 * `visible` is the single open/close truth: opening presents the sheet,
 * closing dismisses it. Native dismissals (swipe, scrim tap, Android back)
 * arrive through `onClose`, and the sheet's closedRef guard keeps the
 * programmatic dismiss on the next `visible=false` from double-firing.
 *
 * Lists only the models of the bound connection with the current selection
 * checked; loading, error, and retry appear only when genuinely needed.
 * Provider configuration stays in Settings — this surface never lists
 * connections, shows Base URLs, or opens Provider Settings.
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
  const sheetRef = useRef<BottomSheet>(null);
  const styles = useMemo(() => createStyles(chrome), [chrome]);
  const { height: windowHeight } = useWindowDimensions();
  const errorMessage =
    typeof error === "string" ? error : error?.message ?? null;
  const refreshable =
    typeof error === "object" && error != null ? error.refreshable : true;
  const rows = useMemo(
    () => buildModelSheetRows(choices, activating),
    [activating, choices],
  );
  const listMaxHeight = useMemo(
    () =>
      resolveModelSheetListMaxHeight({
        windowHeight,
        rowCount: rows.length,
      }),
    [rows.length, windowHeight],
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
    (row: SessionModelSheetRow) => {
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
          accessibilityLabel="Close model"
          accessibilityRole="button"
          style={[styles.close, { backgroundColor: chrome.surfaceMuted }]}
          onPress={handleClose}
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
          {rows.map((row) => (
            <Pressable
              key={row.key}
              style={[
                styles.modelRow,
                {
                  backgroundColor: row.selected
                    ? chrome.accentSoft
                    : chrome.surfaceMuted,
                  opacity: row.disabled && !row.selected ? 0.55 : 1,
                },
              ]}
              disabled={row.disabled}
              accessibilityRole="button"
              accessibilityState={{
                disabled: row.disabled,
                selected: row.selected,
              }}
              accessibilityLabel={`Use ${row.modelId}`}
              onPress={() => handleActivateRow(row)}
            >
              <Text
                style={[styles.modelText, { color: chrome.text }]}
                numberOfLines={1}
                ellipsizeMode="tail"
              >
                {row.label}
              </Text>
              {row.selected ? (
                <Ionicons
                  name="checkmark"
                  size={16}
                  color={chrome.accent}
                />
              ) : null}
            </Pressable>
          ))}

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
    modelRow: {
      borderRadius: 10,
      paddingHorizontal: 12,
      paddingVertical: 10,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 8,
    },
    modelText: { ...TypeScale.body, flexShrink: 1 },
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
