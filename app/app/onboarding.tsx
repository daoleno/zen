import React, { useMemo } from "react";
import { Image, ScrollView, View, Text, StyleSheet } from "react-native";
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
      <ScrollView
        contentContainerStyle={styles.content}
        showsVerticalScrollIndicator={false}
        bounces={false}
      >
        <Enter preset="pop">
          <Image
            source={require("../assets/branding/zen-logo-mark-transparent.png")}
            style={styles.logo}
            resizeMode="contain"
          />
        </Enter>

        <Enter preset="rise" delay={80}>
          <Text style={styles.title}>Connect to Zen</Text>
          <Text style={styles.subtitle}>Pair this phone with your daemon.</Text>
        </Enter>

        <View style={styles.steps}>
          <Enter preset="rise" delay={160}>
            <View style={styles.step}>
              <View style={styles.stepBadge}>
                <Text style={styles.stepNum}>1</Text>
              </View>
              <View style={styles.stepContent}>
                <Text style={styles.stepTitle}>Start Zen on your computer</Text>
                <View style={styles.codeBlock}>
                  <Text style={styles.code}>zen</Text>
                </View>
                <Text style={styles.stepHint}>
                  Download the daemon from GitHub Releases if it is not installed yet.
                </Text>
              </View>
            </View>
          </Enter>

          <Enter preset="rise" delay={220}>
            <View style={styles.step}>
              <View style={styles.stepBadge}>
                <Text style={styles.stepNum}>2</Text>
              </View>
              <View style={styles.stepContent}>
                <Text style={styles.stepTitle}>Create a pairing link</Text>
                <View style={styles.codeBlock}>
                  <Text style={styles.code}>
                    zen pair https://your-host.example
                  </Text>
                </View>
                <Text style={styles.stepHint}>
                  Use an HTTPS address this phone can reach. Zen prints a one-time link and QR code.
                </Text>
              </View>
            </View>
          </Enter>

        </View>

        <Enter preset="rise" delay={280}>
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
            <Text style={styles.doneBtnText}>Pair a daemon</Text>
          </AnimatedPressable>
        </Enter>
      </ScrollView>
    </SafeAreaView>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
  container: { flex: 1, backgroundColor: colors.bgPrimary },
  content: {
    flexGrow: 1,
    paddingHorizontal: Spacing.screenMargin,
    paddingTop: 20,
    paddingBottom: 24,
  },
  logo: {
    width: 72,
    height: 72,
    alignSelf: "center",
    marginBottom: 12,
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
    marginTop: 6,
    marginBottom: 28,
    paddingHorizontal: 8,
  },
  steps: {
    gap: 22,
    marginBottom: 28,
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
    marginTop: "auto",
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
