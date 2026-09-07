import React, { useState } from "react";
import { Linking, ScrollView, StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import { TypeScale, Typography, useAppColors } from "../../constants/tokens";
import { ZenLogoMark } from "../ui/ZenLogoMark";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { CompactEmptyState } from "../ui/CompactEmptyState";

const GUIDE = "https://github.com/daoleno/zen/blob/main/docs/";
export const COMPUTER_SETUP_STEPS = [
  { title: "Check your computer", command: "zen doctor" },
  { title: "Start on trusted Wi-Fi", command: "zen --lan" },
  { title: "Run the pairing command printed by Zen" },
] as const;

export function OnboardingPresentation({ serverName, connection, error, onPair, onRetry, onSettings, onContinue }: {
  serverName?: string;
  connection?: string;
  error?: string;
  onPair(mode: "scanner" | "editor"): void;
  onRetry(): void;
  onSettings(): void;
  onContinue(): void;
}) {
  const colors = useAppColors();
  const [setup, setSetup] = useState(false);
  const paired = connection !== undefined;
  const connected = connection === "connected";
  const connecting = connection === "connecting";
  return (
    <SafeAreaView style={[styles.screen, { backgroundColor: colors.bgPrimary }]} edges={["top", "bottom"]}>
      <ScrollView contentContainerStyle={styles.content}>
        <View style={styles.brand}>
          <ZenLogoMark size={44} accessibilityIgnoresInvertColors />
          <Text style={[styles.brandName, { color: colors.textPrimary }]}>Zen</Text>
        </View>
        {paired ? (
          <CompactEmptyState
            title={connected ? "Your computer is connected" : connecting ? "Connecting to your computer" : "Your server is offline"}
            detail={error || serverName}
            icon={connected ? "checkmark-circle-outline" : "server-outline"}
            busy={connecting}
            action={connected ? { label: "Open Brain", icon: "arrow-forward", onPress: onContinue } : connecting ? undefined : { label: "Retry connection", icon: "refresh-outline", onPress: onRetry }}
            secondary={!connected ? { label: "Server settings", icon: "settings-outline", onPress: onSettings } : undefined}
          />
        ) : (
          <>
            <View style={styles.heading}>
              <Text accessibilityRole="header" style={[styles.title, { color: colors.textPrimary }]}>Connect your computer</Text>
            </View>
            <View style={styles.actions}>
              <AnimatedPressable accessibilityRole="button" accessibilityLabel="Scan pairing code"
                onPress={() => onPair("scanner")} style={[styles.primary, { backgroundColor: colors.accent }]}>
                <Ionicons name="qr-code-outline" size={22} color={colors.textOnAccent} />
                <Text style={[styles.buttonText, { color: colors.textOnAccent }]}>Scan pairing code</Text>
              </AnimatedPressable>
              <AnimatedPressable accessibilityRole="button" accessibilityLabel="Import pairing link"
                onPress={() => onPair("editor")} style={[styles.secondary, { borderColor: colors.borderSubtle }]}>
                <Ionicons name="link-outline" size={21} color={colors.textPrimary} />
                <Text style={[styles.buttonText, { color: colors.textPrimary }]}>Import pairing link</Text>
              </AnimatedPressable>
            </View>
            <View style={[styles.setup, { borderColor: colors.borderSubtle }]}>
              <AnimatedPressable accessibilityRole="button" accessibilityLabel="Computer setup"
                accessibilityState={{ expanded: setup }} onPress={() => setSetup(!setup)} style={styles.setupHeader}>
                <Ionicons name="desktop-outline" size={20} color={colors.textSecondary} />
                <Text style={[styles.setupTitle, { color: colors.textPrimary }]}>Computer setup</Text>
                <Ionicons name={setup ? "chevron-up" : "chevron-down"} size={18} color={colors.textSecondary} />
              </AnimatedPressable>
              {setup ? <View style={styles.steps}>
                <AnimatedPressable accessibilityRole="link" accessibilityLabel="Install Zen on your computer"
                  onPress={() => void Linking.openURL(GUIDE + "install-daemon.md")} style={styles.link}>
                  <Ionicons name="download-outline" size={18} color={colors.accent} />
                  <Text style={[styles.linkText, { color: colors.accent }]}>Install Zen</Text>
                </AnimatedPressable>
                {COMPUTER_SETUP_STEPS.map((step, index) => (
                  <View key={step.title} style={styles.step}>
                    <Text style={[styles.number, { color: colors.textTertiary }]}>{index + 1}</Text>
                    <View style={styles.stepContent}>
                      <Text style={[styles.stepTitle, { color: colors.textPrimary }]}>{step.title}</Text>
                      {"command" in step ? <Text selectable style={[styles.command, { color: colors.textSecondary }]}>{step.command}</Text> : null}
                    </View>
                  </View>
                ))}
                <AnimatedPressable accessibilityRole="link" accessibilityLabel="Remote HTTPS connection guide"
                  onPress={() => void Linking.openURL(GUIDE + "connect-and-pair.md")} style={styles.link}>
                  <Ionicons name="open-outline" size={18} color={colors.accent} />
                  <Text style={[styles.linkText, { color: colors.accent }]}>Remote connection options</Text>
                </AnimatedPressable>
              </View> : null}
            </View>
          </>
        )}
      </ScrollView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  content: { flexGrow: 1, width: "100%", maxWidth: 520, alignSelf: "center", padding: 24, paddingBottom: 32 },
  brand: { flexDirection: "row", alignItems: "center", gap: 12, paddingVertical: 18 },
  brandName: { ...TypeScale.heading, fontSize: 27, letterSpacing: 0 },
  heading: { gap: 10, paddingTop: 24, paddingBottom: 28 },
  title: { ...TypeScale.heading, fontSize: 26, lineHeight: 33, letterSpacing: 0 },
  actions: { gap: 12 },
  primary: { borderRadius: 8, minHeight: 54, padding: 15, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 12 },
  secondary: { borderWidth: StyleSheet.hairlineWidth, borderRadius: 8, minHeight: 54, padding: 15, flexDirection: "row", alignItems: "center", justifyContent: "center", gap: 12 },
  buttonText: { ...TypeScale.label, fontSize: 16, flexShrink: 1, textAlign: "center" },
  setup: { borderTopWidth: StyleSheet.hairlineWidth, marginTop: 32 },
  setupHeader: { minHeight: 60, paddingVertical: 14, gap: 10, flexDirection: "row", alignItems: "center" },
  setupTitle: { ...TypeScale.label, flex: 1 },
  steps: { gap: 20, paddingBottom: 16 },
  step: { flexDirection: "row", gap: 14 },
  number: { ...TypeScale.label, width: 18 },
  stepContent: { flex: 1, gap: 6 },
  stepTitle: { ...TypeScale.body },
  command: { fontFamily: Typography.terminalFont, fontSize: 14, lineHeight: 22 },
  link: { flexDirection: "row", alignItems: "center", gap: 10, minHeight: 44 },
  linkText: { ...TypeScale.label, flexShrink: 1 },
});
