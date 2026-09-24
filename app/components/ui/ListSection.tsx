import React, { Children, Fragment, isValidElement, type ReactNode } from "react";
import {
  ActivityIndicator,
  Pressable,
  StyleSheet,
  View,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import {
  ContinuousCorners,
  Radii,
  TouchTarget,
  useAppTheme,
} from "../../constants/tokens";
import { AppText } from "./AppText";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

interface ListSectionProps {
  title?: string;
  footer?: string | null;
  /** Trailing header control, e.g. a small "Add" button. */
  accessory?: ReactNode;
  children: ReactNode;
  style?: StyleProp<ViewStyle>;
}

/**
 * Inset grouped section: a caption header, one rounded content card whose rows
 * are separated by inset hairlines, and an optional footnote.
 */
export function ListSection({ title, footer, accessory, children, style }: ListSectionProps) {
  const { colors, theme } = useAppTheme();
  const rows = Children.toArray(children).filter(isValidElement);
  return (
    <View style={[styles.section, style]}>
      {title || accessory ? (
        <View style={styles.header}>
          {title ? (
            <AppText variant="label" tone="tertiary" accessibilityRole="header" style={styles.headerText}>
              {title}
            </AppText>
          ) : <View />}
          {accessory}
        </View>
      ) : null}
      <View
        style={[
          styles.card,
          {
            backgroundColor: colors.bgSurface,
            borderColor: theme.isLight ? "transparent" : theme.materials.stroke,
          },
        ]}
      >
        {rows.map((row, index) => (
          <Fragment key={row.key ?? index}>
            {index > 0 ? (
              <View style={[styles.separator, { backgroundColor: theme.materials.separator }]} />
            ) : null}
            {row}
          </Fragment>
        ))}
      </View>
      {footer ? (
        <AppText variant="caption" tone="tertiary" style={styles.footer}>
          {footer}
        </AppText>
      ) : null}
    </View>
  );
}

type Accessory = "chevron" | "check" | "none";

export interface ListRowProps {
  title: string;
  subtitle?: string | null;
  icon?: IoniconName;
  /** Leading glyph tint; defaults to the accent. */
  iconColor?: string;
  /** Custom leading element, replaces `icon`. */
  leading?: ReactNode;
  value?: string | null;
  trailing?: ReactNode;
  accessory?: Accessory;
  destructive?: boolean;
  loading?: boolean;
  disabled?: boolean;
  selected?: boolean;
  onPress?: () => void;
  onLongPress?: () => void;
  accessibilityLabel?: string;
  accessibilityHint?: string;
  accessibilityRole?: "button" | "radio" | "link" | "switch" | "none";
  numberOfLines?: number;
}

/** One row of a ListSection. Pressable when `onPress` is set. */
export function ListRow({
  title,
  subtitle,
  icon,
  iconColor,
  leading,
  value,
  trailing,
  accessory = "none",
  destructive = false,
  loading = false,
  disabled = false,
  selected,
  onPress,
  onLongPress,
  accessibilityLabel,
  accessibilityHint,
  accessibilityRole,
  numberOfLines = 1,
}: ListRowProps) {
  const { colors, theme } = useAppTheme();
  const tint = destructive ? colors.dangerText : iconColor ?? colors.accentStrong;
  const leadingNode = leading ?? (icon ? (
    <View style={[styles.iconTile, { backgroundColor: destructive ? colors.dangerSoft : theme.materials.tint }]}>
      <Ionicons name={icon} size={18} color={tint} />
    </View>
  ) : null);
  const content = (
    <>
      {leadingNode}
      <View style={styles.copy}>
        <AppText
          variant="body"
          numberOfLines={numberOfLines}
          style={{ color: disabled ? colors.disabledText : destructive ? colors.dangerText : colors.textPrimary }}
        >
          {title}
        </AppText>
        {subtitle ? (
          <AppText variant="caption" tone="tertiary" numberOfLines={2}>
            {subtitle}
          </AppText>
        ) : null}
      </View>
      {value ? (
        <AppText variant="compact" tone="tertiary" numberOfLines={1} style={styles.value}>
          {value}
        </AppText>
      ) : null}
      {trailing}
      {loading ? <ActivityIndicator size="small" color={colors.textTertiary} /> : null}
      {accessory === "chevron" && !loading ? (
        <Ionicons name="chevron-forward" size={17} color={colors.textTertiary} />
      ) : null}
      {accessory === "check" ? (
        <Ionicons
          name="checkmark"
          size={20}
          color={selected ? colors.accentStrong : "transparent"}
        />
      ) : null}
    </>
  );

  if (!onPress && !onLongPress) {
    return (
      <View
        style={styles.row}
        accessible
        accessibilityLabel={accessibilityLabel ?? [title, subtitle, value].filter(Boolean).join(", ")}
      >
        {content}
      </View>
    );
  }

  const role = accessibilityRole ?? (accessory === "check" ? "radio" : "button");
  return (
    <Pressable
      onPress={() => {
        void Haptics.selectionAsync();
        onPress?.();
      }}
      onLongPress={onLongPress}
      disabled={disabled || loading}
      accessibilityRole={role === "none" ? undefined : role}
      accessibilityLabel={accessibilityLabel ?? title}
      accessibilityHint={accessibilityHint}
      accessibilityState={{
        disabled: disabled || loading,
        busy: loading,
        ...(role === "radio" ? { checked: Boolean(selected) } : {}),
      }}
      android_ripple={{ color: colors.surfacePressed }}
      style={({ pressed }) => [
        styles.row,
        pressed && { backgroundColor: colors.surfacePressed },
      ]}
    >
      {content}
    </Pressable>
  );
}

const ICON_TILE = 30;

const styles = StyleSheet.create({
  section: {
    marginBottom: 26,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    minHeight: 28,
    paddingHorizontal: 16,
    paddingBottom: 6,
  },
  headerText: {
    flexShrink: 1,
  },
  card: {
    borderRadius: Radii.card,
    ...ContinuousCorners,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  separator: {
    height: StyleSheet.hairlineWidth,
    marginLeft: 16 + ICON_TILE + 12,
  },
  footer: {
    paddingHorizontal: 16,
    paddingTop: 7,
  },
  row: {
    minHeight: Math.max(TouchTarget, 54),
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 10,
  },
  iconTile: {
    width: ICON_TILE,
    height: ICON_TILE,
    borderRadius: 9,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
  },
  copy: {
    flex: 1,
    minWidth: 0,
    gap: 1,
  },
  value: {
    maxWidth: "45%",
    textAlign: "right",
  },
});
