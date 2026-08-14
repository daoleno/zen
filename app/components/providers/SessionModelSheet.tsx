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
import type { ProviderPickerGroup } from "../../services/providers/sessionModelHelpers";
import {
  buildSessionProviderPickerRows,
  resolveModelSheetListMaxHeight,
  sessionProviderPickerRowCount,
  type SessionProviderPickerRow,
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
  /**
   * Every saved Provider connection compatible with this Session's client,
   * grouped by Provider Name with each Provider's enabled+available models
   * beneath it. Uncredentialed and failed-discovery Providers appear with
   * honest non-selectable states.
   */
  groups: ProviderPickerGroup[];
  chrome: TerminalThemeChrome;
  onRetry(): void;
  onClose(): void;
  onActivate(choice: SessionModelChoice): void;
}

/**
 * Native bottom-sheet Provider + Model picker for the current Session.
 * Presented by the platform sheet (SwiftUI on iOS, Material3
 * ModalBottomSheet on Android), so safe areas, keyboard avoidance, scrim
 * dismiss, and swipe-down dismissal are handled natively — the sheet never
 * positions itself on screen coordinates.
 *
 * `visible` is the single open/close truth: opening presents the sheet,
 * closing dismisses it. Native dismissals (swipe, scrim tap, Android back)
 * arrive through `onClose`, and the sheet's closedRef guard keeps the
 * programmatic dismiss on the next `visible=false` from double-firing.
 *
 * Inventory is every saved Provider connection compatible with the Session
 * client, grouped by Provider Name (Base-URL hostname secondary). Each
 * selectable model row carries the exact stable (connection_id, model_id)
 * pair and activates the current Session only — never other Sessions, never
 * catalog defaults, and never a substituted model. The check appears only on
 * the running pair. Uncredentialed, failed-discovery, unsupported and
 * unavailable states are honest and non-selectable; the sheet never routes
 * to Settings. Loading, error, and retry appear only when genuinely needed.
 */
export function SessionModelSheet({
  visible,
  loading,
  activating,
  error,
  selection,
  groups,
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
    () => buildSessionProviderPickerRows(groups, activating),
    [activating, groups],
  );
  const listMaxHeight = useMemo(
    () =>
      resolveModelSheetListMaxHeight({
        windowHeight,
        groupCount: groups.length,
        modelCount: groups.reduce(
          (count, group) => count + group.models.length,
          0,
        ),
      }),
    [groups, windowHeight],
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
    (row: SessionProviderPickerRow) => {
      if (row.kind !== "model") return;
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
          Provider & Model
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

      {errorMessage && groups.length === 0 ? (
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
          {rows.map((row) =>
            row.kind === "group" ? (
              <View key={row.key} style={styles.groupBlock}>
                <View
                  style={[
                    styles.groupHeader,
                    rows[0] === row ? styles.firstGroupHeader : null,
                  ]}
                >
                  <Text
                    style={[styles.groupName, { color: chrome.text }]}
                    accessibilityRole="header"
                    numberOfLines={1}
                    ellipsizeMode="tail"
                  >
                    {row.connectionName}
                  </Text>
                  {row.hostname ? (
                    <Text
                      style={[styles.groupHostname, { color: chrome.textMuted }]}
                      numberOfLines={1}
                      ellipsizeMode="tail"
                    >
                      {row.hostname}
                    </Text>
                  ) : null}
                  {!row.credentialReady ? (
                    <View
                      style={[
                        styles.stateBadge,
                        { backgroundColor: chrome.surfaceMuted },
                      ]}
                    >
                      <Text
                        style={[styles.stateBadgeText, { color: chrome.textMuted }]}
                      >
                        No API key
                      </Text>
                    </View>
                  ) : null}
                </View>
                {groupRowsEmpty(rows, row) ? (
                  <Text
                    style={[styles.groupEmpty, { color: chrome.textMuted }]}
                  >
                    No models discovered.
                  </Text>
                ) : null}
              </View>
            ) : (
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
                </View>
                {row.selected ? (
                  <Ionicons name="checkmark" size={16} color={chrome.accent} />
                ) : null}
              </Pressable>
            ),
          )}

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

          {errorMessage && groups.length > 0 ? (
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

          {!activating && !errorMessage && groups.length === 0 ? (
            <Text style={[styles.stateBody, { color: chrome.textMuted }]}>
              No Provider connections for this Session yet.
            </Text>
          ) : null}
        </BottomSheetScrollView>
      )}
    </BottomSheet>
  );
}

function groupRowsEmpty(
  rows: SessionProviderPickerRow[],
  row: SessionProviderPickerRow,
): boolean {
  if (row.kind !== "group") return false;
  const next = rows[rows.indexOf(row) + 1];
  return next === undefined || next.kind !== "model";
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
    groupBlock: { gap: 4 },
    groupEmpty: { ...TypeScale.caption, paddingHorizontal: 4, paddingBottom: 2 },
    groupHeader: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      paddingHorizontal: 4,
      paddingTop: 10,
      paddingBottom: 2,
    },
    firstGroupHeader: { paddingTop: 2 },
    groupName: { ...TypeScale.body, fontWeight: "600", flexShrink: 1 },
    groupHostname: { ...TypeScale.caption, flexShrink: 1 },
    stateBadge: {
      borderRadius: 8,
      paddingHorizontal: 8,
      paddingVertical: 2,
    },
    stateBadgeText: { ...TypeScale.caption },
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
