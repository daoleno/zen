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
  const recipient = status?.topics_available ? "Brain" : status?.recipient_label || (status?.recipient_id ? "Session unavailable" : "Brain");
  const action = (name: string, icon: keyof typeof Ionicons.glyphMap, onPress: () => void, danger = false, primary = false, unavailable = disabled) => (
    <AnimatedPressable onPress={onPress} disabled={unavailable} accessibilityRole="button"
      accessibilityLabel={name} accessibilityState={{ disabled: unavailable, busy }}
      style={[styles.action, { borderColor: colors.border, backgroundColor: primary ? colors.accentStrong : colors.bgSurface, opacity: unavailable ? 0.5 : 1 }]}>
      <Ionicons name={icon} size={19} color={primary ? colors.textOnAccent : danger ? colors.dangerText : colors.textPrimary} />
      <Text style={[styles.actionText, { color: primary ? colors.textOnAccent : danger ? colors.dangerText : colors.textPrimary }]}>{name}</Text>
    </AnimatedPressable>
  );
  const detail = (name: string, value: string) => (
    <View style={[styles.detail, { borderBottomColor: colors.borderSubtle }]}>
      <Text style={[styles.detailLabel, { color: colors.textSecondary }]}>{name}</Text>
      <Text selectable style={[styles.detailValue, { color: colors.textPrimary }]}>{value}</Text>
    </View>
  );
  return <ScrollView keyboardShouldPersistTaps="handled" keyboardDismissMode="on-drag"
    contentContainerStyle={[styles.content, { paddingBottom: Math.max(insets.bottom, 16) + 24 }]}>
    <View style={styles.identity}>
      <View style={[styles.icon, { backgroundColor: colors.accentStrong }]}><Ionicons name="paper-plane" size={24} color={colors.textOnAccent} /></View>
      <View style={styles.identityText}>
        <Text style={[styles.title, { color: colors.textPrimary }]}>{configured ? `@${status?.bot_username}` : "Telegram"}</Text>
        <Text style={[styles.subtitle, { color: colors.textSecondary }]}>{status?.owner_hint || label}</Text>
      </View>
      {loading || busy ? <ActivityIndicator accessibilityLabel="Updating Telegram connection" color={colors.accentStrong} /> : null}
    </View>
    {configured ? detail("Status", label) : null}
    {bound ? detail("Recipient", recipient) : null}
    {error || status?.last_error ? <View accessibilityLiveRegion="polite" style={styles.error}>
      <Ionicons name="alert-circle-outline" size={20} color={colors.dangerText} />
      <Text style={[styles.errorText, { color: colors.dangerText }]}>{error || status?.last_error}</Text>
    </View> : null}
    {!connected ? <Text style={[styles.notice, { color: colors.textSecondary }]}>Reconnect the current server in Settings.</Text>
      : loading ? null : error ? action("Retry", "refresh-outline", props.onRetry, false, true)
      : !configured || props.editingToken ? <View style={styles.editor}>
        <Text style={[styles.detailLabel, { color: colors.textSecondary }]}>Bot token</Text>
        <View style={styles.inputRow}>
          <TextInput value={props.token} onChangeText={props.onToken} secureTextEntry editable={!disabled}
            accessibilityLabel="Telegram bot token" placeholder="BotFather token" autoCapitalize="none" autoCorrect={false}
            textContentType="none" importantForAutofill="no" returnKeyType="done" onSubmitEditing={props.onConfigure}
            placeholderTextColor={colors.textTertiary} selectionColor={colors.selectionBackground}
            style={[styles.input, { color: colors.textPrimary, borderColor: colors.border, backgroundColor: colors.bgSurface }]} />
          <AnimatedPressable onPress={props.onPaste} disabled={disabled} accessibilityRole="button"
            accessibilityLabel="Paste Telegram bot token from clipboard" accessibilityState={{ disabled }} style={styles.paste}>
            <Ionicons name="clipboard-outline" size={22} color={colors.accentStrong} />
          </AnimatedPressable>
        </View>
        <View style={styles.actions}>
          {props.editingToken ? action("Cancel", "close-outline", props.onCancelToken) : action("BotFather", "open-outline", props.onBotFather)}
          {action(busy ? "Verifying" : "Verify token", "checkmark-outline", props.onConfigure, false, true, disabled || !props.token.trim())}
        </View>
        <Text style={[styles.notice, { color: colors.textTertiary }]}>Bot chats are stored by Telegram. Your token stays on the connected server.</Text>
      </View> : <View style={styles.actions}>
        {bound ? <>
          {action("Open Telegram", "open-outline", props.onOpen, false, Boolean(status?.enabled))}
          {status?.enabled ? action("Disconnect", "pause-outline", props.onDisconnect) : action("Reconnect", "refresh-outline", props.onReconnect, false, true)}
        </> : action(status?.binding_pending ? "Open connection link" : "Connect Telegram", "open-outline", props.onBind, false, true)}
      </View>}
    {configured && connected && !loading ? <>
      <AnimatedPressable onPress={() => setAdvanced(value => !value)} accessibilityRole="button" accessibilityLabel="Advanced"
        accessibilityState={{ expanded: advanced }} style={styles.advanced}>
        <Text style={[styles.subtitle, { color: colors.textSecondary }]}>Advanced</Text>
        <Ionicons name={advanced ? "chevron-up" : "chevron-down"} size={18} color={colors.textSecondary} />
      </AnimatedPressable>
      {advanced ? <>
        {detail("Chat mode", status?.topics_available ? "Native topics" : "Private chat")}
        {status?.topic_mappings ? detail("Session topics", String(status.topic_mappings)) : null}
        {status?.brain_thread_id ? detail("Brain conversation", status.brain_thread_id) : null}
        {status?.brain_topic_id ? detail("Brain topic", String(status.brain_topic_id)) : null}
        {detail("Delivery checks", String((status?.ambiguous_delivery_count || 0) + (status?.topic_ambiguous_ops_count || 0)))}
        {status?.last_receive_at ? detail("Last received", new Date(status.last_receive_at).toLocaleString()) : null}
        {status?.last_send_at ? detail("Last sent", new Date(status.last_send_at).toLocaleString()) : null}
        <View style={styles.actions}>
          {action("Refresh", "refresh-outline", props.onRetry)}
          {action("Replace token", "key-outline", props.onEditToken)}
          {bound ? action("Unlink account", "person-remove-outline", props.onRevoke, true) : null}
          {action("Remove bot", "trash-outline", props.onRemove, true)}
        </View>
      </> : null}
    </> : null}
  </ScrollView>;
}

const styles = StyleSheet.create({
  content: { paddingHorizontal: 20, flexGrow: 1 },
  identity: { flexDirection: "row", alignItems: "center", gap: 12, paddingVertical: 24 },
  icon: { width: 48, height: 48, borderRadius: 8, alignItems: "center", justifyContent: "center" },
  identityText: { flex: 1, minWidth: 0 },
  title: { ...UiTextMetrics, ...TypeScale.body, fontWeight: "600", flexShrink: 1 },
  subtitle: { ...UiTextMetrics, ...TypeScale.compact, flexShrink: 1 },
  detail: { flexDirection: "row", alignItems: "flex-start", paddingVertical: 14, gap: 16, borderBottomWidth: StyleSheet.hairlineWidth },
  detailLabel: { ...UiTextMetrics, ...TypeScale.compact, flexShrink: 1 },
  detailValue: { ...UiTextMetrics, ...TypeScale.compact, flex: 1, textAlign: "right" },
  actions: { flexDirection: "row", flexWrap: "wrap", gap: 10, marginTop: 20 },
  action: { minHeight: 48, paddingVertical: 10, paddingHorizontal: 12, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 8, borderWidth: StyleSheet.hairlineWidth, borderRadius: 6, flexGrow: 1, flexBasis: 130 },
  actionText: { ...UiTextMetrics, ...TypeScale.compact, flexShrink: 1, textAlign: "center" },
  editor: { paddingTop: 20 },
  inputRow: { flexDirection: "row", alignItems: "center", gap: 8, marginTop: 10 },
  input: { ...UiTextMetrics, ...TypeScale.body, minHeight: 48, paddingHorizontal: 12, borderWidth: 1, borderRadius: 6, flex: 1, minWidth: 0 },
  paste: { width: 48, height: 48, alignItems: "center", justifyContent: "center" },
  notice: { ...UiTextMetrics, ...TypeScale.caption, marginTop: 18 },
  error: { flexDirection: "row", alignItems: "flex-start", gap: 8, paddingVertical: 14 },
  errorText: { ...UiTextMetrics, ...TypeScale.compact, flex: 1 },
  advanced: { minHeight: 48, flexDirection: "row", alignItems: "center", justifyContent: "space-between", marginTop: 28 },
});
