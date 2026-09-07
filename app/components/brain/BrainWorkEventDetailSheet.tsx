import React from "react";
import { Ionicons } from "@expo/vector-icons";
import { Pressable, ScrollView, StyleSheet, Text, View } from "react-native";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import { brainWorkEventSummary, brainWorkEventWorkTitle } from "./brainWorkEventPresentation";

export function BrainWorkEventDetailSheet({ event, chrome, onClose, onOpenSession }: {
  event: BrainWorkResultEvent | null;
  chrome: TerminalThemeChrome;
  onClose(): void;
  onOpenSession?: () => void;
}) {
  let diagnostics = event?.details_json || "";
  try { diagnostics = diagnostics ? JSON.stringify(JSON.parse(diagnostics), null, 2) : ""; } catch { /* Retain malformed diagnostics for inspection. */ }
  return <BottomSheetFrame visible={Boolean(event)} onClose={onClose} cardStyle={{ backgroundColor: chrome.surface }}>
    {event ? <>
      <View style={styles.header}>
        <Text selectable style={[styles.title, { color: chrome.text }]}>{brainWorkEventWorkTitle(event)}</Text>
        <Pressable accessibilityRole="button" accessibilityLabel="Close work details" onPress={onClose} style={styles.icon}>
          <Ionicons name="close" size={22} color={chrome.textMuted} />
        </Pressable>
      </View>
      <ScrollView contentContainerStyle={styles.content}>
        <Text selectable style={{ color: chrome.text }}>{brainWorkEventSummary(event)}</Text>
        {event.wait_for ? <Text selectable style={{ color: chrome.textMuted }}>{event.wait_for}</Text> : null}
        {event.next_action ? <Text selectable style={{ color: chrome.textMuted }}>{event.next_action}</Text> : null}
        {diagnostics ? <Text selectable style={[styles.diagnostics, { color: chrome.textMuted }]}>{diagnostics}</Text> : null}
      </ScrollView>
      {onOpenSession ? <Pressable accessibilityRole="button" onPress={onOpenSession} style={styles.action}>
        <Ionicons name="terminal-outline" size={20} color={chrome.accent} />
        <Text style={{ color: chrome.accent }}>Open session</Text>
      </Pressable> : null}
    </> : null}
  </BottomSheetFrame>;
}

const styles = StyleSheet.create({
  header: { flexDirection: "row", alignItems: "center", gap: 8 },
  title: { flex: 1, fontSize: 18, fontWeight: "600" },
  icon: { width: 44, height: 44, alignItems: "center", justifyContent: "center" },
  content: { gap: 16, paddingVertical: 12 },
  diagnostics: { fontSize: 13 },
  action: { minHeight: 48, flexDirection: "row", alignItems: "center", gap: 10 },
});
