import React from 'react';
import { View, Text, TouchableOpacity, StyleSheet } from 'react-native';
import { useRouter } from 'expo-router';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Colors, Spacing, Typography } from '../constants/tokens';
import { markOnboarded } from '../services/storage';

export default function OnboardingScreen() {
  const router = useRouter();

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.content}>
        <Text style={styles.logo}>☯</Text>
        <Text style={styles.title}>Welcome to zen</Text>
        <Text style={styles.subtitle}>Connect to your homelab agent control plane</Text>

        <View style={styles.step}>
          <Text style={styles.stepNum}>1</Text>
          <View style={styles.stepContent}>
            <Text style={styles.stepTitle}>Install zen-daemon on your server</Text>
            <View style={styles.codeBlock}>
              <Text style={styles.code}>go install github.com/daoleno/zen/daemon/cmd/zen-daemon@latest</Text>
            </View>
          </View>
        </View>

        <View style={styles.step}>
          <Text style={styles.stepNum}>2</Text>
          <View style={styles.stepContent}>
            <Text style={styles.stepTitle}>Run zen-daemon</Text>
            <View style={styles.codeBlock}>
              <Text style={styles.code}>zen-daemon</Text>
            </View>
            <Text style={styles.stepHint}>Note the pairing code displayed</Text>
          </View>
        </View>

        <View style={styles.step}>
          <Text style={styles.stepNum}>3</Text>
          <View style={styles.stepContent}>
            <Text style={styles.stepTitle}>Enter the server URL in Settings</Text>
            <Text style={styles.stepHint}>Use your Tailscale Funnel or Cloudflare Tunnel URL</Text>
          </View>
        </View>

        <TouchableOpacity style={styles.doneBtn} onPress={async () => {
          await markOnboarded();
          router.replace('/(tabs)');
        }}>
          <Text style={styles.doneBtnText}>Get Started</Text>
        </TouchableOpacity>
      </View>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: Colors.bgPrimary },
  content: { flex: 1, padding: Spacing.screenMargin * 2, justifyContent: 'center' },
  logo: { fontSize: 56, textAlign: 'center', marginBottom: 16 },
  title: { color: Colors.textPrimary, fontSize: 28, fontFamily: Typography.uiFontMedium, textAlign: 'center' },
  subtitle: { color: Colors.textSecondary, fontSize: 15, fontFamily: Typography.uiFont, textAlign: 'center', marginTop: 8, marginBottom: 40 },
  step: { flexDirection: 'row', marginBottom: 24 },
  stepNum: {
    color: Colors.accent,
    fontSize: 20,
    fontFamily: Typography.uiFontMedium,
    width: 32,
    marginTop: 2,
  },
  stepContent: { flex: 1 },
  stepTitle: { color: Colors.textPrimary, fontSize: 16, fontFamily: Typography.uiFontMedium, marginBottom: 8 },
  stepHint: { color: Colors.textSecondary, fontSize: 13, fontFamily: Typography.uiFont, marginTop: 6 },
  codeBlock: {
    backgroundColor: Colors.bgSurface,
    borderRadius: 8,
    padding: 12,
  },
  code: { color: Colors.accent, fontFamily: Typography.terminalFont, fontSize: 12 },
  doneBtn: {
    backgroundColor: Colors.accent,
    borderRadius: 12,
    paddingVertical: 16,
    alignItems: 'center',
    marginTop: 32,
    minHeight: 52,
    justifyContent: 'center',
  },
  doneBtnText: { color: Colors.bgPrimary, fontFamily: Typography.uiFontMedium, fontSize: 17 },
});
