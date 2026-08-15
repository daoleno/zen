import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Pressable,
  StyleSheet,
  Text,
  useWindowDimensions,
  View,
} from "react-native";
import BottomSheet, { BottomSheetScrollView } from "@expo/ui/community/bottom-sheet";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale } from "../../constants/tokens";
import type { ProviderError, ThreadRuntimeChoice } from "../../services/providers";
import {
  reasoningEffortLabel,
  runtimeChoiceForRow,
  type ProviderPickerModelRow,
} from "../../services/providers/sessionModelHelpers";
import {
  effectRowsForRuntime,
  groupRuntimeRows,
  resolveModelSheetListMaxHeight,
  runtimeRowSurfaceKey,
} from "./sessionModelSheetModel";

interface SessionModelSheetProps {
  visible: boolean;
  loading: boolean;
  activating: boolean;
  error?: ProviderError | string | null;
  rows: ProviderPickerModelRow[];
  chrome: TerminalThemeChrome;
  onRetry(): void;
  onClose(): void;
  onActivate(runtime: ThreadRuntimeChoice): void;
}

export function SessionModelSheet({
  visible,
  loading,
  activating,
  error,
  rows,
  chrome,
  onRetry,
  onClose,
  onActivate,
}: SessionModelSheetProps) {
  const sheetRef = useRef<BottomSheet>(null);
  const [effectTarget, setEffectTarget] = useState<ProviderPickerModelRow | null>(null);
  const styles = useMemo(() => createStyles(), []);
  const { height: windowHeight } = useWindowDimensions();
  const groups = useMemo(() => groupRuntimeRows(rows), [rows]);
  const rowCount = effectTarget ? effectTarget.effects.length : rows.length + groups.length;
  const listMaxHeight = resolveModelSheetListMaxHeight({ windowHeight, rowCount });
  const errorMessage = typeof error === "string" ? error : error?.message ?? null;

  useEffect(() => {
    if (visible) sheetRef.current?.present();
    else sheetRef.current?.dismiss();
  }, [visible]);
  useEffect(() => {
    if (!visible) setEffectTarget(null);
  }, [visible]);

  const activateRow = useCallback(
    (row: ProviderPickerModelRow) => {
      if (row.disabled) return;
      if (row.effects.length > 0) {
        setEffectTarget(row);
        return;
      }
      const runtime = runtimeChoiceForRow(row);
      if (runtime) onActivate(runtime);
    },
    [onActivate],
  );

  return (
    <BottomSheet
      ref={sheetRef}
      index={-1}
      enablePanDownToClose
      onDismiss={onClose}
    >
      <View style={styles.header}>
        {effectTarget ? (
          <Pressable
            style={styles.iconButton}
            onPress={() => setEffectTarget(null)}
            accessibilityRole="button"
            accessibilityLabel="Back to Provider and Model"
          >
            <Ionicons name="chevron-back" size={20} color={chrome.text} />
          </Pressable>
        ) : null}
        <Text style={[styles.title, { color: chrome.text }]}>
          {effectTarget ? "Effect" : "Provider & Model"}
        </Text>
        <Pressable
          style={styles.iconButton}
          onPress={onClose}
          accessibilityRole="button"
          accessibilityLabel="Close runtime selection"
        >
          <Ionicons name="close" size={20} color={chrome.textMuted} />
        </Pressable>
      </View>

      {loading ? (
        <View style={styles.center}>
          <ActivityIndicator color={chrome.accent} />
        </View>
      ) : (
        <BottomSheetScrollView
          style={{ maxHeight: listMaxHeight }}
          contentContainerStyle={styles.body}
          showsVerticalScrollIndicator={false}
        >
          {effectTarget ? (
            <>
              <Text style={[styles.context, { color: chrome.textMuted }]}>
                {effectTarget.connectionName} · {effectTarget.modelId}
              </Text>
              {effectRowsForRuntime(effectTarget).map(({ key, effect, selected }) => {
                return (
                  <Pressable
                    key={key}
                    style={[
                      styles.row,
                      {
                        backgroundColor: chrome[runtimeRowSurfaceKey(selected)],
                      },
                    ]}
                    disabled={activating}
                    accessibilityRole="button"
                    accessibilityState={{ selected, disabled: activating }}
                    accessibilityLabel={`Use ${reasoningEffortLabel(effect)} effect`}
                    onPress={() => {
                      const runtime = runtimeChoiceForRow(effectTarget, effect);
                      if (runtime) onActivate(runtime);
                    }}
                  >
                    <Text style={[styles.rowText, { color: chrome.text }]}>
                      {reasoningEffortLabel(effect)}
                    </Text>
                    {selected ? (
                      <Ionicons name="checkmark" size={16} color={chrome.accent} />
                    ) : null}
                  </Pressable>
                );
              })}
            </>
          ) : (
            groups.map((group) => (
              <View key={group.connectionId} style={styles.group}>
                <Text style={[styles.groupTitle, { color: chrome.textMuted }]}>
                  {group.connectionName}
                </Text>
                {group.rows.map((row) => (
                  <Pressable
                    key={row.key}
                    style={[
                      styles.row,
                      {
                        backgroundColor: chrome[runtimeRowSurfaceKey(row.current)],
                        opacity: row.disabled && !row.current ? 0.55 : 1,
                      },
                    ]}
                    disabled={row.disabled}
                    accessibilityRole="button"
                    accessibilityState={{
                      selected: row.current,
                      disabled: row.disabled,
                    }}
                    accessibilityLabel={`Use ${group.connectionName}, ${row.modelId}`}
                    onPress={() => activateRow(row)}
                  >
                    <View style={styles.rowCopy}>
                      <Text style={[styles.rowText, { color: chrome.text }]} numberOfLines={1}>
                        {row.label}
                      </Text>
                      {row.unavailableCurrent ? (
                        <Text style={[styles.caption, { color: chrome.textMuted }]}>
                          Current runtime is no longer selectable.
                        </Text>
                      ) : null}
                      {row.unsupported ? (
                        <Text style={[styles.caption, { color: chrome.textMuted }]}>
                          Unsupported by Zen.
                        </Text>
                      ) : null}
                    </View>
                    {row.effects.length > 0 ? (
                      <Ionicons name="chevron-forward" size={16} color={chrome.textMuted} />
                    ) : row.current ? (
                      <Ionicons name="checkmark" size={16} color={chrome.accent} />
                    ) : null}
                  </Pressable>
                ))}
              </View>
            ))
          )}

          {activating ? (
            <View style={styles.status}>
              <ActivityIndicator size="small" color={chrome.accent} />
              <Text style={[styles.caption, { color: chrome.textMuted }]}>Switching…</Text>
            </View>
          ) : null}
          {errorMessage ? (
            <View style={styles.status}>
              <Text style={[styles.rowText, { color: chrome.textMuted }]}>{errorMessage}</Text>
              <Pressable onPress={onRetry} accessibilityRole="button">
                <Text style={[styles.retry, { color: chrome.accent }]}>Retry</Text>
              </Pressable>
            </View>
          ) : null}
          {!activating && !errorMessage && rows.length === 0 ? (
            <Text style={[styles.rowText, { color: chrome.textMuted }]}>
              No configured runtime is available. Sync models in Settings.
            </Text>
          ) : null}
        </BottomSheetScrollView>
      )}
    </BottomSheet>
  );
}

function createStyles() {
  return StyleSheet.create({
    header: { flexDirection: "row", alignItems: "center", paddingHorizontal: 16, paddingTop: 10, paddingBottom: 6, gap: 8 },
    title: { ...TypeScale.title, flex: 1 },
    iconButton: { width: 30, height: 30, alignItems: "center", justifyContent: "center" },
    center: { paddingVertical: 32, alignItems: "center" },
    body: { paddingHorizontal: 16, gap: 10, paddingBottom: 16 },
    context: { ...TypeScale.caption, paddingHorizontal: 2 },
    group: { gap: 6 },
    groupTitle: { ...TypeScale.caption, fontWeight: "600", paddingHorizontal: 2 },
    row: { borderRadius: 10, paddingHorizontal: 12, paddingVertical: 10, flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 },
    rowCopy: { flex: 1, minWidth: 0, gap: 2 },
    rowText: { ...TypeScale.body },
    caption: { ...TypeScale.caption },
    status: { gap: 6, paddingVertical: 6 },
    retry: { ...TypeScale.body, fontWeight: "600" },
  });
}
