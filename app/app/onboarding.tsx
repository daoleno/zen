import React, { useMemo } from "react";
import { Image, View, Text, StyleSheet } from "react-native";
import * as Haptics from "expo-haptics";
import { useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { Colors, Radii, Spacing, Typography, useAppColors, shadow } from "../constants/tokens";
import { AnimatedPressable } from "../components/ui/AnimatedPressable";
import { Enter } from "../components/ui/Enter";

export default function OnboardingScreen() {
  const router = useRouter();
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);

  return (
    <SafeAreaView style={styles.container} edges={["top", "bottom"]}>
      <View style={styles.content}>
        <Enter preset="pop">
          <Image
            source={require("../assets/branding/zen-logo-transparent.png")}
            style={styles.logo}
            resizeMode="contain"
          />
        </Enter>

        <Enter preset="rise" delay={80}>
          <Text style={styles.title}>Welcome to Zen</Text>
          <Text style={styles.subtitle}>
            Pair your phone with a trusted daemon identity
          </Text>
        </Enter>

        <View style={styles.steps}>
          <Enter preset="rise" delay={160}>
            <View style={styles.step}>
              <View style={styles.stepBadge}>
                <Text style={styles.stepNum}>1</Text>
              </View>
              <View style={styles.stepContent}>
                <Text style={styles.stepTitle}>Install zen on your server</Text>
                <View style={styles.codeBlock}>
                  <Text style={styles.code}>
                    go install github.com/daoleno/zen/daemon/cmd/zen@latest
                  </Text>
                </View>
              </View>
            </View>
          </Enter>

          <Enter preset="rise" delay={220}>
            <View style={styles.step}>
              <View style={styles.stepBadge}>
                <Text style={styles.stepNum}>2</Text>
              </View>
              <View style={styles.stepContent}>
                <Text style={styles.stepTitle}>Run zen</Text>
                <View style={styles.codeBlock}>
                  <Text style={styles.code}>
                    zen -advertise-url https://your-host.example/ws
                  </Text>
                </View>
                <Text style={styles.stepHint}>
                  zen listens on 127.0.0.1:9876 by default. Expose that local
                  port through Cloudflare Tunnel, Tailscale, or your own reverse
                  proxy, then pass the public /ws URL with -advertise-url.
                </Text>
              </View>
            </View>
          </Enter>

          <Enter preset="rise" delay={280}>
            <View style={styles.step}>
              <View style={styles.stepBadge}>
                <Text style={styles.stepNum}>3</Text>
              </View>
              <View style={styles.stepContent}>
                <Text style={styles.stepTitle}>Import the pairing link</Text>
                <Text style={styles.stepHint}>
                  Scan the QR or paste the pairing link printed by zen.
                </Text>
              </View>
            </View>
          </Enter>
        </View>

        <Enter preset="rise" delay={340}>
          <AnimatedPressable
            style={styles.doneBtn}
            preset="press"
            scale={0.97}
            onPress={() => {
              Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
              router.push({
                pathname: "/settings",
                params: {
                  addServer: Date.now().toString(),
                  pairingRequired: "1",
                },
              });
            }}
          >
            <Text style={styles.doneBtnText}>Get Started</Text>
          </AnimatedPressable>
        </Enter>
      </View>
    </SafeAreaView>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.bgPrimary },
  content: {
    flex: 1,
    paddingHorizontal: Spacing.screenMargin * 2,
    justifyContent: "center",
    paddingVertical: 28,
  },
  logo: {
    width: 104,
    height: 104,
    alignSelf: "center",
    marginBottom: 18,
  },
  title: {
    color: colors.textPrimary,
    fontSize: 30,
    lineHeight: 36,
    fontFamily: Typography.uiFontMedium,
    textAlign: "center",
    letterSpacing: -0.6,
  },
  subtitle: {
    color: colors.textSecondary,
    fontSize: 15,
    lineHeight: 22,
    fontFamily: Typography.uiFont,
    textAlign: "center",
    marginTop: 10,
    marginBottom: 40,
    paddingHorizontal: 8,
  },
  steps: {
    gap: 18,
    marginBottom: 36,
  },
  step: {
    flexDirection: "row",
    gap: 14,
  },
  stepBadge: {
    width: 30,
    height: 30,
    borderRadius: Radii.pill,
    backgroundColor: colors.accentSoft,
    alignItems: "center",
    justifyContent: "center",
    marginTop: 1,
  },
  stepNum: {
    color: colors.accent,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  stepContent: { flex: 1, minWidth: 0 },
  stepTitle: {
    color: colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 10,
  },
  stepHint: {
    color: colors.textTertiary,
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
    marginTop: 8,
  },
  codeBlock: {
    backgroundColor: colors.bgSurface,
    borderRadius: Radii.sm,
    padding: 13,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.borderSubtle,
    ...shadow("card", colors.shadowColor),
  },
  code: {
    color: colors.accent,
    fontFamily: Typography.terminalFont,
    fontSize: 12,
  },
  doneBtn: {
    backgroundColor: colors.accent,
    borderRadius: Radii.md,
    paddingVertical: 17,
    alignItems: "center",
    minHeight: 56,
    justifyContent: "center",
    ...shadow("float", colors.shadowColor),
  },
  doneBtnText: {
    color: colors.textOnAccent,
    fontFamily: Typography.uiFontMedium,
    fontSize: 17,
  },
  });
}
