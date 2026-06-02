import { Ionicons } from "@expo/vector-icons";
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { wsClient, type BrainChatMessage } from "../../services/websocket";
import type { ConnectionState } from "../../store/agents";
import { MessageBody } from "../terminal/CodexMessageBody";

interface BrainTmuxChatSurfaceProps {
  visible: boolean;
  serverId: string;
  agentId: string;
  threadId?: string;
  connectionState: ConnectionState;
  theme: TerminalThemePalette;
  chrome: TerminalThemeChrome;
  screenFocused: boolean;
  placeholder?: string;
  keyboardVerticalOffset?: number;
}

type BrainTmuxChatItem = {
  id: string;
  role: "user" | "assistant";
  body: string;
  pending?: boolean;
};

export function BrainTmuxChatSurface({
  visible,
  serverId,
  agentId,
  threadId = "",
  connectionState,
  theme,
  chrome,
  screenFocused,
  placeholder = "Message Brain",
  keyboardVerticalOffset = 0,
}: BrainTmuxChatSurfaceProps) {
  const scrollRef = useRef<ScrollView | null>(null);
  const refreshInFlightRef = useRef(false);
  const [draft, setDraft] = useState("");
  const [messages, setMessages] = useState<BrainChatMessage[]>([]);
  const [pendingMessage, setPendingMessage] = useState<BrainTmuxChatItem | null>(null);
  const [loading, setLoading] = useState(false);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const brainThreadId = threadId.trim() || agentId;
  const canUseSession =
    visible && screenFocused && connectionState === "connected" && Boolean(serverId && agentId);

  const refreshMessages = useCallback(async () => {
    if (!canUseSession) {
      return;
    }
    if (refreshInFlightRef.current) {
      return;
    }
    refreshInFlightRef.current = true;
    try {
      setError(null);
      const snapshot = await wsClient.getBrainChatSnapshot(serverId, agentId, brainThreadId);
      setMessages(snapshot);
    } catch (err: any) {
      setError(err?.message || "Could not load Brain.");
    } finally {
      refreshInFlightRef.current = false;
    }
  }, [agentId, brainThreadId, canUseSession, serverId]);

  useEffect(() => {
    if (!canUseSession) {
      return;
    }
    let cancelled = false;
    setLoading(true);
    void refreshMessages().finally(() => {
      if (!cancelled) {
        setLoading(false);
      }
    });
    const interval = setInterval(() => {
      void refreshMessages();
    }, 1400);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [canUseSession, refreshMessages]);

  useEffect(() => {
    setDraft("");
    setMessages([]);
    setPendingMessage(null);
    setError(null);
    setSending(false);
    refreshInFlightRef.current = false;
  }, [serverId, brainThreadId]);

  const chatItems = useMemo(
    () => buildTmuxChatItems(messages, pendingMessage),
    [messages, pendingMessage],
  );

  useEffect(() => {
    if (chatItems.length === 0) {
      return;
    }
    requestAnimationFrame(() => {
      scrollRef.current?.scrollToEnd({ animated: true });
    });
  }, [chatItems]);

  const submit = useCallback(() => {
    const text = draft.trim();
    if (!text || !canUseSession || sending) {
      return;
    }
    const pending: BrainTmuxChatItem = {
      id: `pending:${Date.now().toString(36)}:${Math.random().toString(36).slice(2, 8)}`,
      role: "user",
      body: text,
      pending: true,
    };
    setSending(true);
    setDraft("");
    setPendingMessage(pending);
    void wsClient
      .sendBrainChatMessage(serverId, agentId, text, brainThreadId)
      .then((nextMessages) => {
        setMessages(nextMessages);
        setPendingMessage(null);
        setTimeout(() => {
          void refreshMessages();
        }, 700);
      })
      .catch((err: any) => {
        setDraft(text);
        setPendingMessage(null);
        setError(err?.message || "Message not sent.");
      })
      .finally(() => {
        setSending(false);
      });
  }, [agentId, brainThreadId, canUseSession, draft, refreshMessages, sending, serverId]);

  const disabled = !canUseSession || sending;

  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === "ios" ? "padding" : undefined}
      keyboardVerticalOffset={keyboardVerticalOffset}
      style={[styles.root, { backgroundColor: theme.background }]}
    >
      <ScrollView
        ref={scrollRef}
        style={styles.scroll}
        contentContainerStyle={styles.scrollContent}
        keyboardShouldPersistTaps="handled"
        showsVerticalScrollIndicator={false}
      >
        {loading && chatItems.length === 0 ? (
          <View style={styles.emptyState}>
            <ActivityIndicator color={chrome.accent} size="small" />
          </View>
        ) : chatItems.length > 0 ? (
          chatItems.map((item) => (
            <BrainTmuxMessage
              key={item.id}
              item={item}
              chrome={chrome}
              theme={theme}
            />
          ))
        ) : (
          <View style={styles.emptyState}>
            <Text style={[styles.emptyTitle, { color: chrome.text }]}>Ready</Text>
            <Text style={[styles.emptyBody, { color: chrome.textSubtle }]}>
              Send a message to get started.
            </Text>
          </View>
        )}
        {error ? (
          <Text style={[styles.errorText, { color: theme.red }]}>
            {error}
          </Text>
        ) : null}
      </ScrollView>

      <View style={[styles.composerOuter, { borderTopColor: chrome.border }]}>
        <View
          style={[
            styles.composer,
            {
              backgroundColor: chrome.surface,
              borderColor: chrome.border,
            },
          ]}
        >
          <TextInput
            style={[styles.input, { color: chrome.text }]}
            value={draft}
            placeholder={placeholder}
            placeholderTextColor={chrome.textSubtle}
            editable={!disabled}
            multiline
            autoCorrect={false}
            autoCapitalize="none"
            autoComplete="off"
            spellCheck={false}
            keyboardType={Platform.OS === "android" ? "visible-password" : "default"}
            disableFullscreenUI
            importantForAutofill="no"
            selectionColor={chrome.accent}
            returnKeyType="send"
            enterKeyHint="send"
            submitBehavior="submit"
            onChangeText={setDraft}
            onSubmitEditing={submit}
          />
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Send"
            disabled={disabled || !draft.trim()}
            hitSlop={8}
            onPress={submit}
            style={({ pressed }) => [
              styles.sendButton,
              { backgroundColor: chrome.accent },
              pressed && !disabled ? styles.pressed : null,
              disabled || !draft.trim() ? styles.disabled : null,
            ]}
          >
            {sending ? (
              <ActivityIndicator color={theme.background} size="small" />
            ) : (
              <Ionicons name="arrow-up" size={18} color={theme.background} />
            )}
          </Pressable>
        </View>
      </View>
    </KeyboardAvoidingView>
  );
}

function BrainTmuxMessage({
  item,
  chrome,
  theme,
}: {
  item: BrainTmuxChatItem;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  if (item.role === "user") {
    return (
      <View style={styles.userRow}>
        <View
          style={[
            styles.userBubble,
            {
              backgroundColor: chrome.surfaceMuted,
              borderColor: item.pending ? chrome.borderStrong : "transparent",
              opacity: item.pending ? 0.82 : 1,
            },
          ]}
        >
          <MessageBody value={item.body} chrome={chrome} theme={theme} compact />
        </View>
      </View>
    );
  }
  return (
    <View style={styles.assistantRow}>
      <MessageBody value={item.body} chrome={chrome} theme={theme} />
    </View>
  );
}

function buildTmuxChatItems(
  messages: BrainChatMessage[],
  pendingMessage: BrainTmuxChatItem | null,
): BrainTmuxChatItem[] {
  const items = messages
    .map((message): BrainTmuxChatItem => ({
      id: message.id,
      role: message.role === "user" ? "user" : "assistant",
      body: message.body.trim(),
    }))
    .filter((item) => item.id && item.body);
  if (pendingMessage) {
    items.push(pendingMessage);
  }
  return items;
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    minHeight: 0,
  },
  scroll: {
    flex: 1,
    minHeight: 0,
  },
  scrollContent: {
    flexGrow: 1,
    paddingHorizontal: 16,
    paddingTop: 16,
    paddingBottom: 18,
  },
  emptyState: {
    flex: 1,
    minHeight: 220,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
  },
  emptyTitle: {
    marginTop: 12,
    fontFamily: Typography.chatFontMedium,
    fontSize: 16,
    lineHeight: 22,
    textAlign: "center",
  },
  emptyBody: {
    marginTop: 6,
    fontFamily: Typography.chatFont,
    fontSize: 13,
    lineHeight: 19,
    textAlign: "center",
  },
  errorText: {
    marginTop: 12,
    fontFamily: Typography.chatFont,
    fontSize: 12,
    lineHeight: 17,
  },
  userRow: {
    marginBottom: 17,
    flexDirection: "row",
    justifyContent: "flex-end",
  },
  userBubble: {
    maxWidth: "86%",
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingVertical: 9,
  },
  assistantRow: {
    marginBottom: 20,
    paddingRight: 8,
  },
  composerOuter: {
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    paddingTop: 8,
    paddingBottom: Platform.OS === "ios" ? 12 : 10,
  },
  composer: {
    minHeight: 48,
    maxHeight: 132,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 18,
    flexDirection: "row",
    alignItems: "flex-end",
    gap: 6,
    paddingLeft: 10,
    paddingRight: 6,
    paddingVertical: 5,
  },
  input: {
    flex: 1,
    minHeight: 38,
    maxHeight: 112,
    paddingTop: Platform.OS === "android" ? 8 : 7,
    paddingBottom: Platform.OS === "android" ? 6 : 7,
    fontFamily: Typography.chatFont,
    fontSize: 15,
    lineHeight: 22,
    includeFontPadding: false,
  },
  sendButton: {
    width: 36,
    height: 36,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
    marginBottom: 1,
  },
  pressed: {
    opacity: 0.78,
  },
  disabled: {
    opacity: 0.48,
  },
});
