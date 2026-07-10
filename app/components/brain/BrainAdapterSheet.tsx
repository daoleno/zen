import React, { useMemo } from "react";
import { ActivityIndicator, StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import {
  Colors,
  Radii,
  TypeScale,
  Typography,
  UiTextMetrics,
  uiLineHeight,
  useAppTheme,
} from "../../constants/tokens";
import type { ResolvedZenTheme } from "../../theme";
import { createThemedSurfaces } from "../../constants/themedSurfaces";
import type { BrainAdapterRef } from "../../store/brain";
import { BrainAdapterIcon } from "./BrainAdapterIcon";
import { brainAdapterLabel, brainProviderLabel } from "./brainPresentation";

interface BrainAdapterSheetProps {
  visible: boolean;
  adapters: BrainAdapterRef[];
  activeAdapterId?: string;
  switchingAdapterId: string | null;
  error?: string | null;
  onClose: () => void;
  onSelect: (adapter: BrainAdapterRef) => void;
}

export function BrainAdapterSheet({
  visible,
  adapters,
  activeAdapterId,
  switchingAdapterId,
  error,
  onClose,
  onSelect,
}: BrainAdapterSheetProps) {
  const { theme } = useAppTheme();
  const colors = theme.colors;
  const themed = useMemo(() => createThemedSurfaces(theme), [theme]);
  const styles = useMemo(() => createStyles(theme), [theme]);

  return (
    <BottomSheetFrame
      visible={visible}
      onClose={onClose}
      keyboardAvoiding
      maxHeight="72%"
    >
      <Text style={styles.title}>Host Executor</Text>
      <Text style={styles.lead}>
        Choose which executor runs Brain chat and orchestration.
      </Text>

      <View style={styles.list}>
        {adapters.map((adapter) => {
          const active = adapter.id === activeAdapterId;
          const busy = switchingAdapterId === adapter.id;
          const provider = brainProviderLabel(adapter.provider);
          const label = brainAdapterLabel(adapter);

          return (
            <AnimatedPressable
              key={adapter.id}
              accessibilityRole="button"
              accessibilityLabel={`Switch to ${label}`}
              disabled={busy}
              preset="press"
              scale={0.98}
              style={[
                styles.row,
                {
                  borderColor: active ? colors.accent : themed.border,
                  backgroundColor: busy
                    ? colors.disabledSurface
                    : active
                      ? colors.surfaceActive
                      : themed.surface,
                },
              ]}
              onPress={() => {
                if (busy) {
                  return;
                }
                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                onSelect(adapter);
              }}
            >
              <BrainAdapterIcon adapter={adapter} size={17} />
              <View style={styles.rowMain}>
                <Text style={styles.rowTitle} numberOfLines={1}>
                  {label}
                </Text>
                <Text style={styles.rowMeta} numberOfLines={1}>
                  {provider}
                  {adapter.runtime?.trim() ? ` · ${adapter.runtime.trim()}` : ""}
                </Text>
              </View>
              {busy ? (
                <ActivityIndicator size="small" color={colors.accent} />
              ) : active ? (
                <View style={[styles.activePill, { backgroundColor: colors.accentSoft }]}>
                  <Text style={[styles.activePillText, { color: colors.accent }]}>Host</Text>
                </View>
              ) : adapter.delegated ? (
                <View style={[styles.activePill, { backgroundColor: themed.subtle }]}>
                  <Text style={[styles.activePillText, { color: colors.textSecondary }]}>Delegated</Text>
                </View>
              ) : (
                <Ionicons name="ellipse-outline" size={18} color={colors.textTertiary} />
              )}
            </AnimatedPressable>
          );
        })}
      </View>

      {error ? <Text style={styles.error}>{error}</Text> : null}
    </BottomSheetFrame>
  );
}

function createStyles(theme: ResolvedZenTheme) {
  const colors = theme.colors;
  return StyleSheet.create({
    title: {
      ...UiTextMetrics,
      ...TypeScale.title,
      color: colors.textPrimary,
      marginBottom: 6,
    },
    lead: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      color: colors.textSecondary,
      marginBottom: 16,
    },
    list: {
      gap: 10,
    },
    row: {
      minHeight: 64,
      borderRadius: Radii.md,
      borderWidth: StyleSheet.hairlineWidth,
      paddingHorizontal: 14,
      paddingVertical: 12,
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
    },
    rowMain: {
      flex: 1,
      minWidth: 0,
      gap: 2,
    },
    rowTitle: {
      ...UiTextMetrics,
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 15,
      lineHeight: uiLineHeight(15),
    },
    rowMeta: {
      ...UiTextMetrics,
      color: colors.textTertiary,
      fontFamily: Typography.uiFont,
      fontSize: 12,
      lineHeight: uiLineHeight(12),
    },
    activePill: {
      borderRadius: Radii.pill,
      paddingHorizontal: 10,
      paddingVertical: 4,
    },
    activePillText: {
      fontFamily: Typography.uiFontMedium,
      fontSize: 11,
      lineHeight: 14,
    },
    error: {
      ...TypeScale.caption,
      marginTop: 12,
      color: colors.dangerText,
    },
  });
}
