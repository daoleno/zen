import React, { useState } from "react";
import { ActivityIndicator, ScrollView, StyleSheet, Text, TextInput, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useAppColors, TypeScale, UiTextMetrics } from "../../constants/tokens";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import type { TelegramConnectionStatus } from "../../services/websocket";

export interface TelegramConnectionPanelProps {
  status: TelegramConnectionStatus | null;
  connected: boolean;
  loading: boolean;
  busy: boolean;
  error: string | null;
  token: string;
  editingToken: boolean;
  onToken: (value: string) => void;
  onPaste: () => void;
  onConfigure: () => void;
  onBind: () => void;
  onOpen: () => void;
  onBotFather: () => void;
  onReconnect: () => void;
  onDisconnect: () => void;
  onEditToken: () => void;
  onCancelToken: () => void;
  onRevoke: () => void;
  onRemove: () => void;
  onRetry: () => void;
}

type Icon = keyof typeof Ionicons.glyphMap;

/**
 * Telegram detail page: identity, one primary next step, grouped secondary
 * actions, then diagnostics and destructive actions behind Advanced.
 */
export function TelegramConnectionPanel(props: TelegramConnectionPanelProps) {
  const colors = useAppColors();
  const insets = useSafeAreaInsets();
  const [advanced, setAdvanced] = useState(false);
  const { status, connected, loading, busy, error } = props;
  const configured = Boolean(status?.bot_username);
  const bound = Boolean(status?.owner_hint);
  const disabled = busy || !connected || loading;
  const label = !connected ? "Server offline" : loading ? "Loading" : !configured ? "Not connected"
    : !status?.enabled ? "Disconnected" : !bound ? "Awaiting connection" : status.state === "degraded" ? "Needs attention" : "Connected";
  const labelInk = !connected || label === "Needs attention" ? colors.warning
    : label === "Connected" ? colors.success : colors.textSecondary;
  const recipient = status?.topics_available ? "Brain" : status?.recipient_label || (status?.recipient_id ? "Session unavailable" : "Brain");

  // A primary action is the one filled call to action; everything else is a
  // row in a grouped card.
  const action = (name: string, icon: Icon, onPress: () => void, danger = false, primary = false, unavailable = disabled) => {
    const ink = primary ? colors.textOnAccent : danger ? colors.dangerText : colors.accentStrong;
    return (
      <AnimatedPressable key={name} onPress={onPress} disabled={unavailable} accessibilityRole="button"
        accessibilityLabel={name} accessibilityState={{ disabled: unavailable, busy }}
        style={[primary ? styles.primary : styles.row, primary ? { backgroundColor: colors.accent } : null, { opacity: unavailable ? 0.45 : 1 }]}>
        <Ionicons name={icon} size={primary ? 18 : 19} color={ink} />
        <Text numberOfLines={2} style={[primary ? styles.primaryText : styles.rowText, { color: primary ? ink : danger ? colors.dangerText : colors.textPrimary }]}>{name}</Text>
      </AnimatedPressable>
    );
  };
  const group = (children: React.ReactNode[]) => {
    const rows = children.filter(Boolean);
    if (rows.length === 0) return null;
    return (
      <View style={[styles.group, { backgroundColor: colors.bgSurface }]}>
        {rows.map((row, index) => (
          <View key={index} style={index > 0 ? [styles.divided, { borderTopColor: colors.borderSubtle }] : null}>{row}</View>
        ))}
      </View>
    );
  };
  const detail = (name: string, value: string) => (
    <View key={name} style={styles.detail}>
      <Text style={[styles.detailLabel, { color: colors.textPrimary }]}>{name}</Text>
      <Text selectable numberOfLines={3} style={[styles.detailValue, { color: colors.textSecondary }]}>{value}</Text>
    </View>
  );
  const sectionTitle = (name: string) => (
    <Text accessibilityRole="header" style={[styles.sectionTitle, { color: colors.textTertiary }]}>{name}</Text>
  );

  return <ScrollView keyboardShouldPersistTaps="handled" keyboardDismissMode="on-drag"
    contentContainerStyle={[styles.content, { paddingBottom: Math.max(insets.bottom, 16) + 24 }]}>
    <View style={styles.identity}>
      <View style={[styles.icon, { backgroundColor: colors.accentSoft }]}>
        <Ionicons name="paper-plane" size={26} color={colors.accentStrong} />
      </View>
      <Text numberOfLines={1} style={[styles.title, { color: colors.textPrimary }]}>{configured ? `@${status?.bot_username}` : "Telegram"}</Text>
      <View style={styles.statusLine}>
        {loading || busy ? <ActivityIndicator size="small" accessibilityLabel="Updating Telegram connection" color={colors.textTertiary} /> : null}
        <Text style={[styles.subtitle, { color: labelInk }]}>{label}</Text>
      </View>
      {status?.owner_hint ? <Text numberOfLines={1} style={[styles.caption, { color: colors.textTertiary }]}>{status.owner_hint}</Text> : null}
    </View>

    {error || status?.last_error ? <View accessibilityLiveRegion="polite" style={[styles.error, { backgroundColor: colors.dangerSoft }]}>
      <Ionicons name="alert-circle" size={18} color={colors.dangerText} />
      <Text style={[styles.errorText, { color: colors.dangerText }]}>{error || status?.last_error}</Text>
    </View> : null}

    {!connected ? <Text style={[styles.notice, { color: colors.textSecondary }]}>Reconnect the current server in Settings.</Text>
      : loading ? null : error ? action("Retry", "refresh-outline", props.onRetry, false, true)
      : !configured || props.editingToken ? <View>
        {sectionTitle("Bot token")}
        <View style={[styles.inputRow, { backgroundColor: colors.bgSurface }]}>
          <TextInput value={props.token} onChangeText={props.onToken} secureTextEntry editable={!disabled}
            accessibilityLabel="Telegram bot token" placeholder="Paste the BotFather token" autoCapitalize="none" autoCorrect={false}
            textContentType="none" importantForAutofill="no" returnKeyType="done" onSubmitEditing={props.onConfigure}
            placeholderTextColor={colors.textTertiary} selectionColor={colors.selectionBackground}
            style={[styles.input, { color: colors.textPrimary }]} />
          <AnimatedPressable onPress={props.onPaste} disabled={disabled} accessibilityRole="button"
            accessibilityLabel="Paste Telegram bot token from clipboard" accessibilityState={{ disabled }} style={styles.paste}>
            <Ionicons name="clipboard-outline" size={21} color={colors.accentStrong} />
          </AnimatedPressable>
        </View>
        <Text style={[styles.notice, { color: colors.textTertiary }]}>Bot chats are stored by Telegram. Your token stays on the connected server.</Text>
        <View style={styles.stack}>
          {action(busy ? "Verifying" : "Verify token", "checkmark-outline", props.onConfigure, false, true, disabled || !props.token.trim())}
          {group([props.editingToken ? action("Cancel", "close-outline", props.onCancelToken) : action("BotFather", "open-outline", props.onBotFather)])}
        </View>
      </View> : <View style={styles.stack}>
        {bound ? <>
          {status?.enabled ? action("Open Telegram", "open-outline", props.onOpen, false, true) : action("Reconnect", "refresh-outline", props.onReconnect, false, true)}
          {group([
            detail("Status", label),
            detail("Recipient", recipient),
            status?.enabled ? action("Disconnect", "pause-outline", props.onDisconnect) : action("Open Telegram", "open-outline", props.onOpen),
          ])}
        </> : <>
          {action(status?.binding_pending ? "Open connection link" : "Connect Telegram", "open-outline", props.onBind, false, true)}
          {group([detail("Status", label)])}
        </>}
      </View>}

    {configured && connected && !loading ? <>
      <AnimatedPressable onPress={() => setAdvanced(value => !value)} accessibilityRole="button" accessibilityLabel="Advanced"
        accessibilityState={{ expanded: advanced }} style={styles.advanced}>
        <Text style={[styles.sectionTitle, styles.advancedLabel, { color: colors.textTertiary }]}>Advanced</Text>
        <Ionicons name={advanced ? "chevron-up" : "chevron-down"} size={16} color={colors.textTertiary} />
      </AnimatedPressable>
      {advanced ? <View style={styles.stack}>
        {group([
          detail("Chat mode", status?.topics_available ? "Native topics" : "Private chat"),
          status?.topic_mappings ? detail("Session topics", String(status.topic_mappings)) : null,
          status?.brain_thread_id ? detail("Brain conversation", status.brain_thread_id) : null,
          status?.brain_topic_id ? detail("Brain topic", String(status.brain_topic_id)) : null,
          detail("Delivery checks", String((status?.ambiguous_delivery_count || 0) + (status?.topic_ambiguous_ops_count || 0))),
          status?.last_receive_at ? detail("Last received", new Date(status.last_receive_at).toLocaleString()) : null,
          status?.last_send_at ? detail("Last sent", new Date(status.last_send_at).toLocaleString()) : null,
        ])}
        {group([
          action("Refresh", "refresh-outline", props.onRetry),
          action("Replace token", "key-outline", props.onEditToken),
        ])}
        {group([
          bound ? action("Unlink account", "person-remove-outline", props.onRevoke, true) : null,
          action("Remove bot", "trash-outline", props.onRemove, true),
        ])}
      </View> : null}
    </> : null}
  </ScrollView>;
}

const styles = StyleSheet.create({
  content: { paddingHorizontal: 16, flexGrow: 1 },
  identity: { alignItems: "center", gap: 4, paddingTop: 20, paddingBottom: 24 },
  icon: { width: 60, height: 60, borderRadius: 18, alignItems: "center", justifyContent: "center", marginBottom: 8 },
  title: { ...UiTextMetrics, ...TypeScale.heading, flexShrink: 1, textAlign: "center" },
  statusLine: { flexDirection: "row", alignItems: "center", gap: 6 },
  subtitle: { ...UiTextMetrics, ...TypeScale.compact, flexShrink: 1 },
  caption: { ...UiTextMetrics, ...TypeScale.caption },
  stack: { gap: 18 },
  group: { borderRadius: 22, overflow: "hidden" },
  divided: { borderTopWidth: StyleSheet.hairlineWidth, marginLeft: 16 },
  row: { minHeight: 52, paddingHorizontal: 16, paddingVertical: 12, flexDirection: "row", alignItems: "center", gap: 12 },
  rowText: { ...UiTextMetrics, ...TypeScale.body, flex: 1 },
  primary: { minHeight: 52, paddingHorizontal: 20, borderRadius: 999, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 8 },
  primaryText: { ...UiTextMetrics, ...TypeScale.body, fontWeight: "600", flexShrink: 1, textAlign: "center" },
  detail: { minHeight: 52, flexDirection: "row", alignItems: "center", paddingHorizontal: 16, paddingVertical: 12, gap: 16 },
  detailLabel: { ...UiTextMetrics, ...TypeScale.body, flexShrink: 0 },
  detailValue: { ...UiTextMetrics, ...TypeScale.compact, flex: 1, textAlign: "right" },
  sectionTitle: { ...UiTextMetrics, ...TypeScale.caption, paddingHorizontal: 16, paddingBottom: 6 },
  inputRow: { flexDirection: "row", alignItems: "center", gap: 4, borderRadius: 22, paddingLeft: 16, paddingRight: 4 },
  input: { ...UiTextMetrics, ...TypeScale.body, minHeight: 52, flex: 1, minWidth: 0 },
  paste: { width: 48, height: 48, alignItems: "center", justifyContent: "center" },
  notice: { ...UiTextMetrics, ...TypeScale.caption, paddingHorizontal: 16, marginTop: 8, marginBottom: 18 },
  error: { flexDirection: "row", alignItems: "flex-start", gap: 8, paddingVertical: 12, paddingHorizontal: 14, borderRadius: 16, marginBottom: 18 },
  errorText: { ...UiTextMetrics, ...TypeScale.compact, flex: 1 },
  advanced: { minHeight: 48, flexDirection: "row", alignItems: "center", justifyContent: "space-between", marginTop: 28, paddingRight: 16 },
  advancedLabel: { paddingBottom: 0 },
});
