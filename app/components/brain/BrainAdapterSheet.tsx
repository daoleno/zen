import { useEffect, useMemo, useState } from "react";
import { ActivityIndicator, StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import {
  Radii,
  TypeScale,
  Typography,
  UiTextMetrics,
  uiLineHeight,
  useAppTheme,
} from "../../constants/tokens";
import type { ResolvedZenTheme } from "../../theme";
import { surfacesFromTheme } from "../../constants/themedSurfaces";
import type { BrainAdapterRef } from "../../store/brain";
import { BrainAdapterIcon } from "./BrainAdapterIcon";
import {
  brainAdapterLabel,
  brainProviderLabel,
  type ExecutorTarget,
} from "./brainPresentation";

export type { ExecutorTarget };

interface BrainAdapterSheetProps {
  visible: boolean;
  adapters: BrainAdapterRef[];
  hostAdapterId?: string;
  delegatedAdapterId?: string;
  hostAdapter?: BrainAdapterRef | null;
  delegatedAdapter?: BrainAdapterRef | null;
  switchingAdapterId: string | null;
  switchingTarget: ExecutorTarget | null;
  error?: string | null;
  onClose: () => void;
  onSelect: (adapter: BrainAdapterRef, target: ExecutorTarget) => void;
}

export function BrainAdapterSheet({
  visible,
  adapters,
  hostAdapterId,
  delegatedAdapterId,
  hostAdapter,
  delegatedAdapter,
  switchingAdapterId,
  switchingTarget,
  error,
  onClose,
  onSelect,
}: BrainAdapterSheetProps) {
  const { theme } = useAppTheme();
  const colors = theme.colors;
  const themed = useMemo(() => surfacesFromTheme(theme), [theme]);
  const styles = useMemo(() => createStyles(theme), [theme]);
  const [target, setTarget] = useState<ExecutorTarget>("brain");

  useEffect(() => {
    if (visible) {
      setTarget("brain");
    }
  }, [visible]);

  const activeAdapterId =
    target === "brain" ? hostAdapterId : delegatedAdapterId;
  const interactionLocked = Boolean(switchingAdapterId && switchingTarget);

  return (
    <BottomSheetFrame
      visible={visible}
      onClose={onClose}
      keyboardAvoiding
      maxHeight="72%"
    >
      <Text style={styles.title}>Executors</Text>
      <Text style={styles.lead}>
        Running sessions keep their current executor.
      </Text>

      <View style={styles.targets}>
        <TargetChip
          label="Brain"
          adapter={hostAdapter}
          selected={target === "brain"}
          disabled={interactionLocked}
          styles={styles}
          colors={colors}
          themed={themed}
          onPress={() => setTarget("brain")}
        />
        <TargetChip
          label="Agents"
          adapter={delegatedAdapter}
          selected={target === "agents"}
          disabled={interactionLocked}
          styles={styles}
          colors={colors}
          themed={themed}
          onPress={() => setTarget("agents")}
        />
      </View>

      <View style={styles.list}>
        {adapters.map((adapter) => {
          const active = adapter.id === activeAdapterId;
          const rowShowsSpinner =
            interactionLocked &&
            switchingTarget === target &&
            switchingAdapterId === adapter.id;
          const provider = brainProviderLabel(adapter.provider);
          const label = brainAdapterLabel(adapter);
          const titleColor = interactionLocked
            ? colors.disabledText
            : colors.textPrimary;
          const metaColor = interactionLocked
            ? colors.disabledText
            : colors.textTertiary;

          return (
            <AnimatedPressable
              key={adapter.id}
              accessibilityRole="button"
              accessibilityState={{
                disabled: interactionLocked,
                busy: rowShowsSpinner,
              }}
              accessibilityLabel={
                target === "brain"
                  ? `Set Brain host to ${label}`
                  : `Set Agents executor to ${label}`
              }
              disabled={interactionLocked}
              preset="press"
              scale={0.98}
              style={[
                styles.row,
                {
                  borderColor: active ? colors.accent : themed.border,
                  backgroundColor: interactionLocked
                    ? colors.disabledSurface
                    : active
                      ? colors.surfaceActive
                      : themed.surface,
                },
              ]}
              onPress={() => {
                if (interactionLocked) {
                  return;
                }
                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                onSelect(adapter, target);
              }}
            >
              <BrainAdapterIcon adapter={adapter} size={17} />
              <View style={styles.rowMain}>
                <Text
                  style={[styles.rowTitle, { color: titleColor }]}
                  numberOfLines={1}
                >
                  {label}
                </Text>
                <Text
                  style={[styles.rowMeta, { color: metaColor }]}
                  numberOfLines={1}
                >
                  {provider}
                  {adapter.runtime?.trim()
                    ? ` · ${adapter.runtime.trim()}`
                    : ""}
                </Text>
              </View>
              {rowShowsSpinner ? (
                <ActivityIndicator size="small" color={colors.accent} />
              ) : active ? (
                <Ionicons
                  name="checkmark-circle"
                  size={20}
                  color={
                    interactionLocked ? colors.disabledText : colors.accent
                  }
                />
              ) : (
                <Ionicons
                  name="ellipse-outline"
                  size={18}
                  color={
                    interactionLocked
                      ? colors.disabledText
                      : colors.textTertiary
                  }
                />
              )}
            </AnimatedPressable>
          );
        })}
      </View>

      {error ? <Text style={styles.error}>{error}</Text> : null}
    </BottomSheetFrame>
  );
}

function TargetChip({
  label,
  adapter,
  selected,
  disabled,
  styles,
  colors,
  themed,
  onPress,
}: {
  label: string;
  adapter?: BrainAdapterRef | null;
  selected: boolean;
  disabled: boolean;
  styles: ReturnType<typeof createStyles>;
  colors: ResolvedZenTheme["colors"];
  themed: ReturnType<typeof surfacesFromTheme>;
  onPress: () => void;
}) {
  return (
    <AnimatedPressable
      accessibilityRole="button"
      accessibilityState={{ selected, disabled }}
      accessibilityLabel={`${label} executor${
        adapter ? `, ${brainAdapterLabel(adapter)}` : ""
      }`}
      disabled={disabled}
      preset="press"
      scale={0.98}
      style={[
        styles.targetChip,
        {
          borderColor: selected ? colors.accent : themed.border,
          backgroundColor: disabled
            ? colors.disabledSurface
            : selected
              ? colors.surfaceActive
              : themed.surface,
        },
      ]}
      onPress={() => {
        if (disabled) {
          return;
        }
        Haptics.selectionAsync();
        onPress();
      }}
    >
      {adapter ? <BrainAdapterIcon adapter={adapter} size={14} /> : null}
      <Text
        style={[
          styles.targetLabel,
          {
            color: disabled
              ? colors.disabledText
              : selected
                ? colors.textPrimary
                : colors.textSecondary,
          },
        ]}
        numberOfLines={1}
      >
        {label}
      </Text>
    </AnimatedPressable>
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
      marginBottom: 14,
    },
    targets: {
      flexDirection: "row",
      gap: 8,
      marginBottom: 14,
    },
    targetChip: {
      flex: 1,
      minHeight: 44,
      borderRadius: Radii.md,
      borderWidth: StyleSheet.hairlineWidth,
      paddingHorizontal: 12,
      paddingVertical: 8,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "center",
      gap: 8,
    },
    targetLabel: {
      ...UiTextMetrics,
      fontFamily: Typography.uiFontMedium,
      fontSize: 14,
      lineHeight: uiLineHeight(14),
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
      fontFamily: Typography.uiFontMedium,
      fontSize: 15,
      lineHeight: uiLineHeight(15),
    },
    rowMeta: {
      ...UiTextMetrics,
      fontFamily: Typography.uiFont,
      fontSize: 12,
      lineHeight: uiLineHeight(12),
    },
    error: {
      ...TypeScale.caption,
      marginTop: 12,
      color: colors.dangerText,
    },
  });
}
