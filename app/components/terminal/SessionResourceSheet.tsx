import { Ionicons } from "@expo/vector-icons";
import React, { useMemo } from "react";
import {
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TypeScale, Typography } from "../../constants/tokens";
import type { SessionResourceSnapshot } from "../../services/sessionResourceSnapshot";
import { BottomSheetFrame } from "../ui";
import { ComposerLoadingDots } from "./ComposerLoadingDots";
import {
  buildSessionResourceViewModel,
  resolveSessionResourceHostSections,
} from "./SessionResourceSheetModel";

interface Props {
  visible: boolean;
  loading: boolean;
  error?: string | null;
  snapshot?: SessionResourceSnapshot | null;
  chrome: TerminalThemeChrome;
  onRetry(): void;
  onClose(): void;
}

export function SessionResourceSheet({
  visible,
  loading,
  error,
  snapshot,
  chrome,
  onRetry,
  onClose,
}: Props) {
  const m = useMemo(() => buildSessionResourceViewModel(snapshot), [snapshot]);
  const bar = m?.bar;
  const secondary = m
    ? [m.peakLabel, m.tasksLabel].filter(Boolean).join(" · ")
    : "";
  const hostSections = m ? resolveSessionResourceHostSections(m.host) : {};

  return (
    <BottomSheetFrame
      visible={visible}
      maxHeight="72%"
      cardStyle={styles.sheet}
      onClose={onClose}
    >
      <View style={styles.header}>
        <View style={[styles.icon, { backgroundColor: chrome.accentSoft }]}>
          <Ionicons
            name="hardware-chip-outline"
            size={18}
            color={chrome.accent}
          />
        </View>
        <View style={styles.headerCopy}>
          <Text
            style={[styles.title, { color: chrome.text }]}
            numberOfLines={1}
          >
            Resource usage
          </Text>
        </View>
        <TouchableOpacity
          accessibilityLabel="Close resource usage"
          accessibilityRole="button"
          style={[styles.close, { backgroundColor: chrome.surfaceMuted }]}
          onPress={onClose}
          activeOpacity={0.78}
        >
          <Ionicons name="close" size={18} color={chrome.textSubtle} />
        </TouchableOpacity>
      </View>

      {m ? (
        <ScrollView
          style={styles.scroll}
          contentContainerStyle={styles.body}
          showsVerticalScrollIndicator={false}
          accessibilityLabel={m.accessibilityLabel}
        >
          {m.showSessionHero ? (
            <View
              style={[
                styles.card,
                {
                  backgroundColor: chrome.surfaceMuted,
                  borderColor: chrome.border,
                },
              ]}
            >
              <Text style={[styles.micro, { color: chrome.textSubtle }]}>
                This Session
              </Text>
              <Text style={[styles.hero, { color: chrome.text }]} selectable>
                {m.memoryLabel ?? "—"}
              </Text>
              {secondary ? (
                <Text style={[styles.label, { color: chrome.textMuted }]}>
                  {secondary}
                </Text>
              ) : null}
              {m.qualifier ? (
                <Text
                  style={[
                    styles.micro,
                    { color: chrome.textSubtle, marginTop: 4 },
                  ]}
                >
                  {m.qualifier}
                </Text>
              ) : null}
            </View>
          ) : m.unmanagedNote ? (
            <View
              style={[
                styles.card,
                {
                  backgroundColor: chrome.surfaceMuted,
                  borderColor: chrome.border,
                },
              ]}
            >
              <Text style={[styles.label, { color: chrome.text }]}>
                {m.unmanagedNote}
              </Text>
            </View>
          ) : null}

          {m.showPoolCard ? (
            <View
              style={[
                styles.card,
                {
                  backgroundColor: chrome.surfaceMuted,
                  borderColor: chrome.border,
                },
              ]}
            >
              <Text style={[styles.label, { color: chrome.text }]}>
                Shared Agent pool
              </Text>
              {m.poolSummary ? (
                <Text style={[styles.label, { color: chrome.textMuted }]}>
                  {m.poolSummary}
                </Text>
              ) : null}
              {bar ? (
                <View style={styles.barWrap}>
                  <View
                    style={[
                      styles.track,
                      {
                        backgroundColor: chrome.surface,
                        borderColor: chrome.border,
                      },
                    ]}
                    accessibilityLabel={
                      bar.split
                        ? "Shared pool composition"
                        : "Shared pool usage"
                    }
                  >
                    {bar.session > 0 ? (
                      <View
                        style={[
                          styles.seg,
                          {
                            flexGrow: bar.session,
                            backgroundColor: chrome.accent,
                          },
                        ]}
                      />
                    ) : null}
                    {bar.split && bar.other > 0 ? (
                      <View
                        style={[
                          styles.seg,
                          {
                            flexGrow: bar.other,
                            backgroundColor: chrome.accentSoft,
                          },
                        ]}
                      />
                    ) : null}
                    {bar.remaining > 0 ? (
                      <View
                        style={[
                          styles.seg,
                          {
                            flexGrow: bar.remaining,
                            backgroundColor: chrome.surface,
                          },
                        ]}
                      />
                    ) : null}
                  </View>
                  {typeof bar.protectionAt === "number" ? (
                    <View
                      pointerEvents="none"
                      style={[
                        styles.marker,
                        { left: `${bar.protectionAt * 100}%` },
                      ]}
                      accessibilityLabel="Protection starts"
                    >
                      <View
                        style={[
                          styles.markerLine,
                          { backgroundColor: chrome.text },
                        ]}
                      />
                      <Text
                        style={[
                          styles.markerText,
                          { color: chrome.textSubtle },
                        ]}
                      >
                        Protection starts
                      </Text>
                    </View>
                  ) : null}
                </View>
              ) : null}
              {m.skewNote ? (
                <Text style={[styles.micro, { color: chrome.textSubtle }]}>
                  {m.skewNote}
                </Text>
              ) : null}
              {bar?.split ? (
                <View style={styles.legend}>
                  <View
                    style={[styles.swatch, { backgroundColor: chrome.accent }]}
                  />
                  <Text style={[styles.micro, { color: chrome.textSubtle }]}>
                    This Session
                  </Text>
                  <View
                    style={[
                      styles.swatch,
                      { backgroundColor: chrome.accentSoft },
                    ]}
                  />
                  <Text style={[styles.micro, { color: chrome.textSubtle }]}>
                    {m.otherLabel ?? "Other Agents"}
                  </Text>
                </View>
              ) : null}
              {hostSections.poolSupport ? (
                <Text
                  style={[styles.micro, { color: chrome.textSubtle }]}
                  accessibilityLabel={
                    hostSections.poolSupport.accessibilityLabel
                  }
                  selectable
                >
                  {hostSections.poolSupport.label}
                </Text>
              ) : null}
            </View>
          ) : null}

          {hostSections.warning ? (
            <View
              style={[
                styles.card,
                {
                  backgroundColor: chrome.dangerSoft,
                  borderColor: chrome.danger,
                },
              ]}
              accessibilityLabel={hostSections.warning.accessibilityLabel}
            >
              <View style={styles.hostRow}>
                <View
                  style={[styles.chip, { backgroundColor: chrome.surface }]}
                >
                  <Ionicons
                    name="warning-outline"
                    size={16}
                    color={chrome.danger}
                  />
                  <Text style={[styles.microMed, { color: chrome.text }]}>
                    {hostSections.warning.title}
                  </Text>
                </View>
                {hostSections.warning.available ? (
                  <Text
                    style={[styles.label, { color: chrome.text }]}
                    selectable
                  >
                    {hostSections.warning.available} available
                  </Text>
                ) : null}
              </View>
              <Text style={[styles.micro, { color: chrome.textSubtle }]}>
                {hostSections.warning.note}
              </Text>
            </View>
          ) : null}

          {hostSections.footerSupport || m.metaLine || m.workspace ? (
            <View style={styles.footer}>
              {hostSections.footerSupport ? (
                <Text
                  style={[styles.micro, { color: chrome.textSubtle }]}
                  accessibilityLabel={
                    hostSections.footerSupport.accessibilityLabel
                  }
                  selectable
                >
                  {hostSections.footerSupport.label}
                </Text>
              ) : null}
              {m.metaLine ? (
                <Text
                  style={[styles.microMed, { color: chrome.textMuted }]}
                  numberOfLines={1}
                >
                  {m.metaLine}
                </Text>
              ) : null}
              {m.workspace ? (
                <Text
                  style={[styles.micro, { color: chrome.textSubtle }]}
                  numberOfLines={1}
                >
                  {m.workspace}
                </Text>
              ) : null}
            </View>
          ) : null}
        </ScrollView>
      ) : loading ? (
        <View style={styles.state}>
          <ComposerLoadingDots color={chrome.accent} size={10} />
          <Text
            style={[styles.title, { color: chrome.text, textAlign: "center" }]}
          >
            Loading resource usage
          </Text>
          <Text
            style={[
              styles.micro,
              { color: chrome.textSubtle, textAlign: "center" },
            ]}
          >
            One on-demand snapshot from the daemon
          </Text>
        </View>
      ) : (
        <View style={styles.state}>
          <Text
            style={[styles.title, { color: chrome.text, textAlign: "center" }]}
          >
            {error ? "Could not load details" : "Unavailable"}
          </Text>
          <Text
            style={[
              styles.micro,
              { color: chrome.textSubtle, textAlign: "center" },
            ]}
          >
            {error ||
              "Resource measurements are not available for this Session."}
          </Text>
          <TouchableOpacity
            accessibilityLabel="Retry resource usage"
            accessibilityRole="button"
            style={[styles.retry, { backgroundColor: chrome.accent }]}
            onPress={onRetry}
            activeOpacity={0.82}
          >
            <Ionicons name="refresh-outline" size={15} color={chrome.text} />
            <Text style={[styles.label, { color: chrome.text }]}>Retry</Text>
          </TouchableOpacity>
        </View>
      )}
    </BottomSheetFrame>
  );
}

const styles = StyleSheet.create({
  sheet: { paddingHorizontal: 18, paddingTop: 14, paddingBottom: 24 },
  header: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    marginBottom: 14,
  },
  icon: {
    width: 36,
    height: 36,
    borderRadius: 18,
    alignItems: "center",
    justifyContent: "center",
  },
  headerCopy: { flex: 1, minWidth: 0, gap: 2 },
  title: { ...TypeScale.body, fontFamily: Typography.uiFontMedium },
  micro: { ...TypeScale.micro, fontFamily: Typography.uiFont },
  microMed: { ...TypeScale.micro, fontFamily: Typography.uiFontMedium },
  label: { ...TypeScale.label, fontFamily: Typography.uiFontMedium },
  close: {
    width: 32,
    height: 32,
    borderRadius: 16,
    alignItems: "center",
    justifyContent: "center",
  },
  scroll: { maxHeight: 420 },
  body: { paddingBottom: 8, gap: 12 },
  card: {
    borderRadius: 16,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 14,
    paddingVertical: 14,
    gap: 6,
  },
  hero: { fontSize: 34, lineHeight: 40, fontFamily: Typography.uiFontMedium },
  barWrap: { marginTop: 4, marginBottom: 2, paddingBottom: 18 },
  track: {
    height: 14,
    borderRadius: 7,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
    flexDirection: "row",
  },
  seg: { height: "100%", flexBasis: 0 },
  marker: {
    position: "absolute",
    top: -2,
    bottom: 0,
    width: 1,
    marginLeft: -0.5,
    alignItems: "center",
  },
  markerLine: { width: StyleSheet.hairlineWidth * 2, flex: 1, opacity: 0.7 },
  markerText: {
    ...TypeScale.micro,
    fontFamily: Typography.uiFont,
    fontSize: 10,
    lineHeight: 12,
    marginTop: 2,
    width: 88,
    textAlign: "center",
  },
  legend: {
    flexDirection: "row",
    alignItems: "center",
    flexWrap: "wrap",
    gap: 6,
  },
  swatch: { width: 8, height: 8, borderRadius: 2 },
  hostRow: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 10,
    flexWrap: "wrap",
  },
  chip: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    borderRadius: 999,
    paddingHorizontal: 10,
    paddingVertical: 6,
  },
  footer: { paddingHorizontal: 4, gap: 2 },
  state: {
    alignItems: "center",
    justifyContent: "center",
    gap: 10,
    paddingVertical: 28,
    paddingHorizontal: 18,
  },
  retry: {
    marginTop: 8,
    minHeight: 40,
    borderRadius: 12,
    paddingHorizontal: 14,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
});
