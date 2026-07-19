import React, { useMemo } from "react";
import {
  Linking,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import * as Haptics from "expo-haptics";
import { useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import {
  Colors,
  Radii,
  Spacing,
  Typography,
  useAppColors,
  shadow,
} from "../constants/tokens";
import { AnimatedPressable } from "../components/ui/AnimatedPressable";
import { Enter } from "../components/ui/Enter";
import { ZenLogoMark } from "../components/ui/ZenLogoMark";

const CONNECT_GUIDE_URL =
  "https://github.com/daoleno/zen/blob/main/docs/connect-and-pair.md";

const SETUP_STEPS = [
  {
    command: "zen doctor",
    label: "Check your computer",
  },
  {
    command: "zen --lan",
    label: "Start Zen on trusted Wi-Fi",
  },
  {
    command: "zen pair http://192.168.1.42:9876",
    label: "Run the pair command Zen prints",
  },
] as const;

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
          <ZenLogoMark
            size={52}
            style={styles.logo}
            accessibilityIgnoresInvertColors
          />
        </Enter>

        <Enter preset="rise" delay={60}>
          <Text style={styles.title}>Your coding agents, wherever you are.</Text>
          <Text style={styles.subtitle}>
            Zen keeps agents running on your computer. Continue from this phone
            with Chat, Terminal, Sessions, and Brain—while your code and
            credentials stay on your computer.
          </Text>
        </Enter>

        <Enter preset="rise" delay={120}>
          <Text style={styles.sectionTitle}>Connect on trusted Wi-Fi</Text>
          <View style={styles.steps}>
            {SETUP_STEPS.map((step, index) => (
              <View key={step.command} style={styles.step}>
                <View style={styles.stepHeader}>
                  <View style={styles.stepBadge}>
                    <Text style={styles.stepNum}>{index + 1}</Text>
                  </View>
                  <Text style={styles.stepLabel}>{step.label}</Text>
                </View>
                <View style={styles.codeBlock}>
                  <Text selectable style={styles.code}>
                    {step.command}
                  </Text>
                </View>
              </View>
            ))}
          </View>
          <Text style={styles.pairHint}>
            Keep Zen running, then scan the QR or import its one-time pairing
            link.
          </Text>
          <AnimatedPressable
            accessibilityRole="link"
            accessibilityLabel="Remote HTTPS connection guide"
            style={styles.remoteLink}
            preset="press"
            scale={0.98}
            onPress={() => void Linking.openURL(CONNECT_GUIDE_URL)}
          >
            <Text style={styles.remoteLinkText}>
              Using remote HTTPS? Read the connect guide
            </Text>
          </AnimatedPressable>
        </Enter>
      </ScrollView>

      <View style={styles.actionArea}>
        <AnimatedPressable
          accessibilityRole="button"
          accessibilityLabel="Scan or import pairing code"
          accessibilityHint="Opens pairing options in Settings"
          style={styles.doneBtn}
          preset="press"
          scale={0.97}
          onPress={() => {
            void Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
            router.push({
              pathname: "/settings",
              params: {
                addServer: Date.now().toString(),
                pairingRequired: "1",
              },
            });
          }}
        >
          <Text style={styles.doneBtnText}>Scan or import pairing code</Text>
        </AnimatedPressable>
      </View>
    </SafeAreaView>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    container: { flex: 1, backgroundColor: colors.bgPrimary },
    content: {
      paddingHorizontal: Spacing.screenMargin,
      paddingTop: 10,
      paddingBottom: 18,
    },
    logo: {
      alignSelf: "center",
      marginBottom: 8,
    },
    title: {
      color: colors.textPrimary,
      fontSize: 27,
      lineHeight: 32,
      fontFamily: Typography.uiFontMedium,
      textAlign: "center",
      letterSpacing: -0.5,
    },
    subtitle: {
      color: colors.textSecondary,
      fontSize: 14,
      lineHeight: 20,
      fontFamily: Typography.uiFont,
      textAlign: "center",
      marginTop: 6,
      marginBottom: 20,
    },
    sectionTitle: {
      color: colors.textPrimary,
      fontSize: 16,
      lineHeight: 22,
      fontFamily: Typography.uiFontMedium,
      marginBottom: 10,
    },
    steps: { gap: 10 },
    step: { gap: 6 },
    stepHeader: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
    },
    stepBadge: {
      width: 22,
      height: 22,
      borderRadius: Radii.pill,
      backgroundColor: colors.accentSoft,
      alignItems: "center",
      justifyContent: "center",
    },
    stepNum: {
      color: colors.accent,
      fontSize: 12,
      lineHeight: 16,
      fontFamily: Typography.uiFontMedium,
    },
    stepLabel: {
      flex: 1,
      color: colors.textSecondary,
      fontSize: 13,
      lineHeight: 18,
      fontFamily: Typography.uiFont,
    },
    codeBlock: {
      backgroundColor: colors.bgSurface,
      borderRadius: Radii.sm,
      minHeight: 42,
      justifyContent: "center",
      paddingHorizontal: 12,
      paddingVertical: 9,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
    },
    code: {
      color: colors.accent,
      fontFamily: Typography.terminalFont,
      fontSize: 11,
      lineHeight: 16,
    },
    pairHint: {
      color: colors.textTertiary,
      fontSize: 12,
      lineHeight: 17,
      fontFamily: Typography.uiFont,
      textAlign: "center",
      marginTop: 12,
    },
    remoteLink: {
      alignSelf: "center",
      minHeight: 38,
      justifyContent: "center",
      paddingHorizontal: 8,
      marginTop: 2,
    },
    remoteLinkText: {
      color: colors.accent,
      fontSize: 12,
      lineHeight: 18,
      fontFamily: Typography.uiFont,
      textAlign: "center",
      textDecorationLine: "underline",
    },
    actionArea: {
      paddingHorizontal: Spacing.screenMargin,
      paddingTop: 10,
      paddingBottom: 8,
      backgroundColor: colors.bgPrimary,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    doneBtn: {
      backgroundColor: colors.accent,
      borderRadius: Radii.md,
      paddingHorizontal: 16,
      paddingVertical: 14,
      alignItems: "center",
      minHeight: 52,
      justifyContent: "center",
      ...shadow("float", colors.shadowColor),
    },
    doneBtnText: {
      color: colors.textOnAccent,
      fontFamily: Typography.uiFontMedium,
      fontSize: 16,
      lineHeight: 22,
      textAlign: "center",
    },
  });
}
