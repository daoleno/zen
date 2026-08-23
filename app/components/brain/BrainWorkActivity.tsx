import { Ionicons } from "@expo/vector-icons";
import React, { useCallback, useMemo, useState } from "react";
import {
  FlatList,
  Pressable,
  StyleSheet,
  Text,
  View,
  useWindowDimensions,
} from "react-native";
import { formatChatBubbleTime } from "../../constants/telegramPresentation";
import {
  Typography,
  UiTextMetrics,
  uiLineHeight,
  useAppTheme,
} from "../../constants/tokens";
import type { ResolvedZenTheme } from "../../theme";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import type {
  BrainWorkActivityModel,
  BrainWorkActivityRow,
} from "./brainWorkActivityModel";

export function BrainWorkActivityButton({
  model,
  onPress,
}: {
  model: BrainWorkActivityModel;
  onPress(): void;
}) {
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);
  const badgeCount = model.activeCount;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={model.accessibilityLabel}
      hitSlop={6}
      onPress={onPress}
      style={({ pressed }) => [
        styles.headerButton,
        pressed ? styles.pressed : null,
      ]}
    >
      <Ionicons
        name="briefcase-outline"
        size={20}
        color={theme.colors.textPrimary}
      />
      {badgeCount > 0 ? (
        <View
          accessibilityElementsHidden
          style={[
            styles.badge,
            model.attentionCount > 0 ? styles.badgeAttention : null,
          ]}
        >
          <Text style={styles.badgeText} maxFontSizeMultiplier={1}>
            {badgeCount > 99 ? "99+" : badgeCount}
          </Text>
        </View>
      ) : null}
    </Pressable>
  );
}

export function BrainWorkActivityPanel({
  visible,
  model,
  onActivate,
  onClose,
  onOpenActions,
}: {
  visible: boolean;
  model: BrainWorkActivityModel;
  onActivate?: (event: BrainWorkResultEvent, canOpenSession: boolean) => void;
  onClose(): void;
  onOpenActions(): void;
}) {
  const { width } = useWindowDimensions();
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);
  const wide = width >= 720;
  const [view, setView] = useState<"active" | "history">("active");
  const selectedView =
    view === "active" && model.active.length === 0 && model.history.length > 0
      ? "history"
      : view;
  const rows = selectedView === "active" ? model.active : model.history;
  const activateRow = useCallback(
    (row: BrainWorkActivityRow) => {
      if (!row.event || !onActivate) {
        return;
      }
      onActivate(row.event, row.canOpenSession);
      onClose();
    },
    [onActivate, onClose],
  );
  const renderRow = useCallback(
    ({ item }: { item: BrainWorkActivityRow }) => {
      const actionable = Boolean(
        item.event && onActivate && (item.event.unread || item.canOpenSession),
      );
      return (
        <BrainWorkActivityRowView
          row={item}
          statusColor={activityStatusColor(item, theme)}
          chevronColor={theme.colors.textTertiary}
          styles={styles}
          onPress={actionable ? () => activateRow(item) : undefined}
        />
      );
    },
    [activateRow, onActivate, styles, theme],
  );
  const renderSeparator = useCallback(
    () => <View style={styles.separator} />,
    [styles.separator],
  );

  return (
    <BottomSheetFrame
      visible={visible}
      onClose={onClose}
      maxHeight={wide ? "100%" : "82%"}
      dragToDismiss={!wide}
      showHandle={!wide}
      rootStyle={wide ? styles.drawerRoot : undefined}
      cardStyle={wide ? styles.drawerCard : styles.sheetCard}
      contentStyle={styles.panelContent}
    >
      <View style={styles.panelHeader}>
        <Text accessibilityRole="header" style={styles.panelTitle}>
          Work
        </Text>
        <View style={styles.headerActions}>
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Brain actions"
            hitSlop={6}
            onPress={() => {
              onClose();
              onOpenActions();
            }}
            style={({ pressed }) => [
              styles.iconButton,
              pressed ? styles.pressed : null,
            ]}
          >
            <Ionicons
              name="ellipsis-horizontal"
              size={20}
              color={theme.colors.textSecondary}
            />
          </Pressable>
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Close Work activity"
            hitSlop={6}
            onPress={onClose}
            style={({ pressed }) => [
              styles.iconButton,
              pressed ? styles.pressed : null,
            ]}
          >
            <Ionicons
              name="close"
              size={21}
              color={theme.colors.textSecondary}
            />
          </Pressable>
        </View>
      </View>

      <View accessibilityRole="tablist" style={styles.tabs}>
        <ActivityTab
          active={selectedView === "active"}
          count={model.active.length}
          label="Active"
          onPress={() => setView("active")}
          styles={styles}
        />
        <ActivityTab
          active={selectedView === "history"}
          count={model.history.length}
          label="History"
          onPress={() => setView("history")}
          styles={styles}
        />
      </View>

      <FlatList
        data={rows}
        keyExtractor={workRowKey}
        renderItem={renderRow}
        ItemSeparatorComponent={renderSeparator}
        contentContainerStyle={styles.rows}
        showsVerticalScrollIndicator={false}
        ListEmptyComponent={
          <Text style={styles.empty}>
            {selectedView === "active" ? "No active Work" : "No Work history"}
          </Text>
        }
      />
    </BottomSheetFrame>
  );
}

function ActivityTab({
  active,
  count,
  label,
  onPress,
  styles,
}: {
  active: boolean;
  count: number;
  label: string;
  onPress(): void;
  styles: ReturnType<typeof createStyles>;
}) {
  return (
    <Pressable
      accessibilityRole="tab"
      accessibilityState={{ selected: active }}
      onPress={onPress}
      style={[styles.tab, active ? styles.tabActive : null]}
    >
      <Text style={[styles.tabText, active ? styles.tabTextActive : null]}>
        {label} {count}
      </Text>
    </Pressable>
  );
}

function BrainWorkActivityRowView({
  row,
  statusColor,
  chevronColor,
  styles,
  onPress,
}: {
  row: BrainWorkActivityRow;
  statusColor: string;
  chevronColor: string;
  styles: ReturnType<typeof createStyles>;
  onPress?: () => void;
}) {
  const updatedAt = row.updatedAt
    ? formatChatBubbleTime(row.updatedAt)
    : undefined;
  const accessibilityLabel = [
    row.presentation.label,
    `Work ${row.title}`,
    row.summary,
    row.sourceCount
      ? `${row.sourceCount} delegated ${row.sourceCount === 1 ? "source" : "sources"}`
      : undefined,
    updatedAt ? `Updated ${updatedAt}` : undefined,
  ]
    .filter(Boolean)
    .join(". ");
  return (
    <Pressable
      accessibilityRole={onPress ? "button" : undefined}
      accessibilityLabel={accessibilityLabel}
      disabled={!onPress}
      onPress={onPress}
      style={({ pressed }) => [
        styles.row,
        row.presentation.lifecycle === "needs_you" ? styles.rowAttention : null,
        pressed ? styles.pressed : null,
      ]}
    >
      <Ionicons
        name={row.presentation.icon}
        size={19}
        color={statusColor}
        style={styles.rowIcon}
      />
      <View style={styles.rowBody}>
        <View style={styles.rowTitleLine}>
          <Text numberOfLines={2} style={styles.rowTitle}>
            {row.title}
          </Text>
          <Text style={[styles.rowStatus, { color: statusColor }]}>
            {row.presentation.label}
          </Text>
        </View>
        {row.summary && !row.presentation.terminal ? (
          <Text numberOfLines={2} style={styles.rowSummary}>
            {row.summary}
          </Text>
        ) : null}
        <View style={styles.rowMeta}>
          {row.sourceCount ? (
            <Text style={styles.metaText}>
              {row.sourceCount} {row.sourceCount === 1 ? "source" : "sources"}
            </Text>
          ) : null}
          {updatedAt ? <Text style={styles.metaText}>{updatedAt}</Text> : null}
        </View>
      </View>
      {onPress ? (
        <Ionicons name="chevron-forward" size={16} color={chevronColor} />
      ) : null}
    </Pressable>
  );
}

function workRowKey(row: BrainWorkActivityRow) {
  return row.id;
}

function activityStatusColor(
  row: BrainWorkActivityRow,
  theme: ResolvedZenTheme,
) {
  switch (row.presentation.tone) {
    case "danger":
      return theme.colors.statusFailed;
    case "attention":
      return theme.colors.warning;
    case "accent":
      return theme.colors.accent;
    case "neutral":
      return theme.colors.textSecondary;
  }
}

function createStyles(theme: ResolvedZenTheme) {
  const colors = theme.colors;
  return StyleSheet.create({
    headerButton: {
      width: 52,
      minHeight: 52,
      alignItems: "center",
      justifyContent: "center",
    },
    badge: {
      position: "absolute",
      top: 7,
      right: 5,
      minWidth: 18,
      height: 18,
      borderRadius: 9,
      paddingHorizontal: 4,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.accent,
      borderWidth: 2,
      borderColor: colors.bgPrimary,
    },
    badgeAttention: { backgroundColor: colors.warning },
    badgeText: {
      color: colors.textOnAccent,
      fontSize: 10,
      lineHeight: 12,
      fontFamily: Typography.uiFontMedium,
    },
    pressed: { opacity: 0.58 },
    drawerRoot: { alignItems: "flex-end" },
    drawerCard: {
      width: 400,
      maxWidth: "42%",
      height: "100%",
      borderTopLeftRadius: 8,
      borderTopRightRadius: 0,
      borderLeftWidth: StyleSheet.hairlineWidth,
    },
    sheetCard: { height: "82%" },
    panelContent: { flex: 1, minHeight: 0 },
    panelHeader: {
      minHeight: 44,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
    },
    panelTitle: {
      ...UiTextMetrics,
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 20,
      lineHeight: uiLineHeight(20),
      flex: 1,
      minWidth: 0,
    },
    headerActions: { flexDirection: "row", alignItems: "center" },
    iconButton: {
      width: 40,
      height: 40,
      alignItems: "center",
      justifyContent: "center",
    },
    tabs: {
      height: 38,
      flexDirection: "row",
      padding: 3,
      borderRadius: 8,
      backgroundColor: colors.surfaceSubtle,
      marginTop: 8,
      marginBottom: 10,
    },
    tab: {
      flex: 1,
      alignItems: "center",
      justifyContent: "center",
      borderRadius: 6,
    },
    tabActive: { backgroundColor: colors.bgElevated },
    tabText: {
      ...UiTextMetrics,
      color: colors.textSecondary,
      fontSize: 13,
      lineHeight: uiLineHeight(13),
    },
    tabTextActive: {
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
    },
    rows: { paddingBottom: 18, flexGrow: 1 },
    separator: {
      height: StyleSheet.hairlineWidth,
      backgroundColor: colors.borderSubtle,
      marginLeft: 34,
    },
    row: {
      minHeight: 70,
      flexDirection: "row",
      alignItems: "center",
      paddingVertical: 11,
      paddingHorizontal: 4,
      gap: 10,
    },
    rowAttention: {
      backgroundColor: colors.warningSoft,
      marginHorizontal: -6,
      paddingHorizontal: 10,
      borderRadius: 8,
    },
    rowIcon: { alignSelf: "flex-start", marginTop: 2 },
    rowBody: { flex: 1, minWidth: 0 },
    rowTitleLine: {
      flexDirection: "row",
      alignItems: "flex-start",
      gap: 8,
    },
    rowTitle: {
      ...UiTextMetrics,
      flex: 1,
      minWidth: 0,
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 15,
      lineHeight: uiLineHeight(15),
    },
    rowStatus: {
      ...UiTextMetrics,
      flexShrink: 0,
      fontFamily: Typography.uiFontMedium,
      fontSize: 12,
      lineHeight: uiLineHeight(12),
    },
    rowSummary: {
      ...UiTextMetrics,
      color: colors.textSecondary,
      fontSize: 13,
      lineHeight: uiLineHeight(13),
      marginTop: 4,
    },
    rowMeta: { flexDirection: "row", gap: 10, marginTop: 5 },
    metaText: {
      ...UiTextMetrics,
      color: colors.textTertiary,
      fontSize: 11,
      lineHeight: uiLineHeight(11),
    },
    empty: {
      ...UiTextMetrics,
      color: colors.textTertiary,
      textAlign: "center",
      paddingVertical: 36,
      fontSize: 14,
      lineHeight: uiLineHeight(14),
    },
  });
}
