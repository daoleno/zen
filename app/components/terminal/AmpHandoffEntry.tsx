import React, { useEffect, useMemo, useRef, useState } from "react";
import { ActivityIndicator, ScrollView, StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Clipboard from "expo-clipboard";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Radii, TypeScale, UiTextMetrics, useAppColors, type AppColors } from "../../constants/tokens";
import { copyAmpHandoff, getAmpHandoffCapability, type AmpHandoffResult } from "../../services/ampHandoff";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { RisingSheet } from "../ui/RisingSheet";

type CopyState = "idle" | "copying" | AmpHandoffResult["kind"];

export function AmpHandoffEntry() {
  const colors = useAppColors();
  const insets = useSafeAreaInsets();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const capability = getAmpHandoffCapability();
  const [visible, setVisible] = useState(false);
  const [copyState, setCopyState] = useState<CopyState>("idle");
  const generation = useRef(0);
  const copying = useRef(false);

  useEffect(() => () => { generation.current += 1; }, []);

  const close = () => {
    generation.current += 1;
    copying.current = false;
    setVisible(false);
    setCopyState("idle");
  };

  const copy = async () => {
    if (copying.current) return;
    copying.current = true;
    const request = ++generation.current;
    setCopyState("copying");
    const result = await copyAmpHandoff((command) => Clipboard.setStringAsync(command));
    if (request !== generation.current) return;
    copying.current = false;
    setCopyState(result.kind);
  };

  const copyFeedback = copyState === "copied"
    ? "Command copied. No session started."
    : copyState === "unavailable"
      ? "Clipboard unavailable. Command not copied."
      : copyState === "copying" ? "Copying command..." : null;

  return (
    <>
      <AnimatedPressable
        style={styles.entry}
        preset="press"
        scale={0.99}
        accessibilityRole="button"
        accessibilityLabel="Amp, External handoff"
        accessibilityHint="Open Amp external command"
        accessibilityState={{ expanded: visible }}
        onPress={() => setVisible(true)}
      >
        <Ionicons name="terminal-outline" size={22} color={colors.textSecondary} />
        <View style={styles.entryCopy}>
          <Text style={styles.body}>Amp</Text>
          <Text style={styles.secondary}>External handoff</Text>
        </View>
        <Ionicons name="chevron-forward" size={18} color={colors.textTertiary} />
      </AnimatedPressable>

      <RisingSheet visible={visible} onClose={close} cardStyle={styles.sheet}>
        <View style={styles.header}>
          <View style={styles.entryCopy}>
            <Text style={styles.heading} accessibilityRole="header">Amp</Text>
            <Text style={styles.secondary}>External handoff</Text>
          </View>
          <AnimatedPressable
            style={styles.iconButton}
            preset="press"
            accessibilityRole="button"
            accessibilityLabel="Close Amp handoff"
            onPress={close}
          >
            <Ionicons name="close" size={22} color={colors.textSecondary} />
          </AnimatedPressable>
        </View>
        <ScrollView
          style={styles.scroll}
          contentContainerStyle={[styles.content, { paddingBottom: Math.max(insets.bottom, 16) }]}
        >
          <View style={styles.detail}>
            <Text style={styles.label}>Amp account / BYOK</Text>
            <Text style={styles.body}>Not verified in Zen</Text>
            <Text style={styles.secondary}>Amp-native routing. No verified OpenCode Go connection.</Text>
          </View>
          <View style={styles.detail}>
            <Text style={styles.label}>Free Agent</Text>
            <Text style={styles.secondary}>BYOK billing policy, not unlimited inference or compute. Provider and Amp compute charges may apply.</Text>
          </View>
          <View style={styles.detail}>
            <Text style={styles.label}>External terminal command</Text>
            <Text style={styles.command} selectable accessibilityLabel={`Amp command: ${capability.command}`}>
              {capability.command}
            </Text>
            <Text style={styles.secondary}>Requires Amp CLI and its own login on the host. Private threads may sync to Amp.</Text>
          </View>
          <View style={styles.actions}>
            <AnimatedPressable
              style={[styles.action, styles.copyAction, copyState === "copying" && styles.disabled]}
              preset="press"
              accessibilityRole="button"
              accessibilityLabel="Copy Amp command"
              accessibilityState={{ disabled: copyState === "copying", busy: copyState === "copying" }}
              disabled={copyState === "copying"}
              onPress={() => void copy()}
            >
              {copyState === "copying"
                ? <ActivityIndicator size="small" color={colors.textOnAccent} />
                : <Ionicons name="copy-outline" size={18} color={colors.textOnAccent} />}
              <Text style={[styles.actionText, styles.copyText]}>Copy command</Text>
            </AnimatedPressable>
            {copyFeedback ? (
              <Text
                accessibilityLiveRegion="polite"
                accessibilityRole={copyState === "unavailable" ? "alert" : "text"}
                style={[styles.secondary, copyState === "unavailable" && { color: colors.dangerText }]}
              >
                {copyFeedback}
              </Text>
            ) : null}
            <AnimatedPressable
              style={[styles.action, styles.disabled]}
              preset="press"
              accessibilityRole="button"
              accessibilityLabel="Launch Amp in Zen unavailable"
              accessibilityHint="External terminal required; no verified Amp launch capability"
              accessibilityState={{ disabled: capability.launch === "unavailable" }}
              disabled={capability.launch === "unavailable"}
            >
              <Ionicons name="open-outline" size={18} color={colors.textSecondary} />
              <Text style={styles.actionText}>Launch unavailable</Text>
            </AnimatedPressable>
          </View>
          <Text style={styles.secondary}>External terminal required</Text>
        </ScrollView>
      </RisingSheet>
    </>
  );
}

function createStyles(colors: AppColors) {
  return StyleSheet.create({
    entry: {
      minHeight: 72, flexDirection: "row", alignItems: "center", gap: 12,
      paddingHorizontal: 14, paddingVertical: 12, borderRadius: Radii.sm,
      borderWidth: StyleSheet.hairlineWidth, borderColor: colors.border, backgroundColor: colors.bgSurface,
    },
    entryCopy: { flex: 1, minWidth: 0 },
    sheet: {
      width: "100%", maxWidth: 480, maxHeight: "88%", alignSelf: "center",
      borderRadius: Radii.md, backgroundColor: colors.modalSurface,
      borderWidth: StyleSheet.hairlineWidth, borderColor: colors.border,
    },
    header: { flexDirection: "row", alignItems: "center", gap: 12, padding: 20, paddingBottom: 12 },
    heading: { ...UiTextMetrics, ...TypeScale.heading, color: colors.textPrimary },
    body: { ...UiTextMetrics, ...TypeScale.body, color: colors.textPrimary },
    secondary: { ...UiTextMetrics, ...TypeScale.caption, color: colors.textSecondary },
    label: { ...UiTextMetrics, ...TypeScale.label, color: colors.textSecondary },
    command: { ...UiTextMetrics, ...TypeScale.mono, color: colors.textPrimary, flexShrink: 1 },
    scroll: { flexGrow: 0 },
    content: { paddingHorizontal: 20, gap: 14 },
    detail: { gap: 6 },
    iconButton: { width: 44, height: 44, alignItems: "center", justifyContent: "center" },
    actions: { gap: 10 },
    action: {
      minHeight: 44, paddingHorizontal: 12, paddingVertical: 10, gap: 8,
      flexDirection: "row", alignItems: "center", justifyContent: "center",
      borderRadius: Radii.xs, backgroundColor: colors.surfacePressed,
    },
    actionText: { ...UiTextMetrics, ...TypeScale.label, color: colors.textSecondary, flexShrink: 1 },
    copyAction: { backgroundColor: colors.accent },
    copyText: { color: colors.textOnAccent },
    disabled: { opacity: 0.55 },
  });
}
