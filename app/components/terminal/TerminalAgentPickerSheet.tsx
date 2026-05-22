import React from "react";
import {
  Modal,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Colors, Typography, statusColor } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { presentAgent } from "../../services/agentPresentation";
import type { AgentDirectorySection } from "../../services/serverSelection";
import { AgentKindIcon } from "./AgentKindIcon";

interface TerminalAgentPickerSheetProps {
  visible: boolean;
  sections: AgentDirectorySection[];
  agentCount: number;
  activeSessionKey: string | null;
  showServerNames: boolean;
  agentAliases: Record<string, string | undefined>;
  creatingSession: boolean;
  chrome: TerminalThemeChrome;
  onClose(): void;
  onOpenAgent(sessionKey: string): void;
  onNewTerminal(): void;
}

export function TerminalAgentPickerSheet({
  visible,
  sections,
  agentCount,
  activeSessionKey,
  showServerNames,
  agentAliases,
  creatingSession,
  chrome,
  onClose,
  onOpenAgent,
  onNewTerminal,
}: TerminalAgentPickerSheetProps) {
  return (
    <Modal
      visible={visible}
      transparent
      animationType="fade"
      onRequestClose={onClose}
    >
      <View style={styles.modalRoot}>
        <TouchableOpacity
          style={styles.modalBackdrop}
          activeOpacity={1}
          onPress={onClose}
        />

        <View
          style={[
            styles.sheetCard,
            {
              backgroundColor: chrome.surface,
              borderColor: chrome.border,
            },
          ]}
        >
          <View
            style={[
              styles.sheetHandle,
              { backgroundColor: chrome.textSubtle },
            ]}
          />

          <ScrollView
            style={styles.sheetScroll}
            contentContainerStyle={styles.sheetScrollContent}
            showsVerticalScrollIndicator={false}
          >
            {agentCount === 0 ? (
              <Text style={[styles.sheetEmpty, { color: chrome.textMuted }]}>
                No agents available.
              </Text>
            ) : (
              sections.map((section) => (
                <View key={section.key} style={styles.sheetSection}>
                  <View style={styles.sheetSectionHeader}>
                    <View style={styles.sheetSectionBody}>
                      <Text
                        style={[styles.sheetSectionTitle, { color: chrome.text }]}
                        numberOfLines={1}
                      >
                        {section.title}
                      </Text>
                      <Text
                        style={[
                          styles.sheetSectionSubtitle,
                          { color: chrome.textMuted },
                        ]}
                        numberOfLines={1}
                      >
                        {section.subtitle}
                      </Text>
                    </View>
                    <Text
                      style={[
                        styles.sheetSectionCount,
                        {
                          color: chrome.textMuted,
                          backgroundColor: chrome.surfaceMuted,
                        },
                      ]}
                    >
                      {section.data.length}
                    </Text>
                  </View>

                  {section.data.map((item) => {
                    const isActive = item.key === activeSessionKey;
                    const presented = presentAgent(item, agentAliases[item.key]);
                    const meta = [
                      presented.typeLabel,
                      showServerNames ? item.serverName : null,
                    ]
                      .filter(Boolean)
                      .join(" · ");

                    return (
                      <TouchableOpacity
                        key={item.key}
                        style={[
                          styles.agentRow,
                          { borderBottomColor: chrome.border },
                          isActive && styles.agentRowActive,
                        ]}
                        onPress={() => onOpenAgent(item.key)}
                        activeOpacity={0.84}
                      >
                        <AgentKindIcon kind={presented.kind} size={15} />
                        <View style={styles.agentRowBody}>
                          <Text
                            style={[styles.agentRowTitle, { color: chrome.text }]}
                            numberOfLines={1}
                          >
                            {presented.title}
                          </Text>
                          <Text
                            style={[
                              styles.agentRowMeta,
                              { color: chrome.textMuted },
                            ]}
                            numberOfLines={1}
                          >
                            {meta}
                          </Text>
                        </View>
                        <View
                          style={[
                            styles.agentRowStatusDot,
                            { backgroundColor: statusColor(item.status) },
                          ]}
                        />
                      </TouchableOpacity>
                    );
                  })}
                </View>
              ))
            )}
          </ScrollView>

          <TouchableOpacity
            style={[
              styles.sheetCreateButton,
              {
                backgroundColor: chrome.surfaceMuted,
                borderColor: chrome.border,
              },
              creatingSession && styles.sheetCreateButtonDisabled,
            ]}
            onPress={onNewTerminal}
            disabled={creatingSession}
            activeOpacity={0.84}
          >
            <Ionicons name="add" size={16} color={chrome.textMuted} />
            <Text
              style={[styles.sheetCreateButtonText, { color: chrome.textMuted }]}
            >
              {creatingSession ? "Starting…" : "New Terminal"}
            </Text>
          </TouchableOpacity>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  modalRoot: {
    flex: 1,
    justifyContent: "flex-end",
  },
  modalBackdrop: {
    ...StyleSheet.absoluteFill,
    backgroundColor: "rgba(6, 8, 12, 0.58)",
  },
  sheetCard: {
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    paddingHorizontal: 18,
    paddingTop: 12,
    paddingBottom: 28,
    backgroundColor: "#121A25",
    borderTopWidth: 1,
    borderColor: "rgba(255,255,255,0.06)",
    maxHeight: "82%",
  },
  sheetHandle: {
    alignSelf: "center",
    width: 42,
    height: 4,
    borderRadius: 2,
    backgroundColor: "#3A475B",
    marginBottom: 14,
  },
  sheetCreateButton: {
    marginTop: 12,
    height: 40,
    borderRadius: 12,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    borderStyle: "dashed" as const,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
  },
  sheetCreateButtonDisabled: {
    opacity: 0.5,
  },
  sheetCreateButtonText: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
  },
  sheetScroll: {
    marginTop: 4,
  },
  sheetScrollContent: {
    paddingBottom: 8,
  },
  sheetSection: {
    paddingTop: 18,
  },
  sheetSectionHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingBottom: 10,
  },
  sheetSectionBody: {
    flex: 1,
    minWidth: 0,
  },
  sheetSectionTitle: {
    color: Colors.textPrimary,
    fontSize: 15,
    lineHeight: 20,
    fontFamily: Typography.uiFontMedium,
  },
  sheetSectionSubtitle: {
    marginTop: 2,
    color: Colors.textSecondary,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
    opacity: 0.55,
  },
  sheetSectionCount: {
    minWidth: 24,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 999,
    overflow: "hidden",
    textAlign: "center",
    color: Colors.textSecondary,
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFontMedium,
    backgroundColor: "rgba(255,255,255,0.05)",
  },
  sheetEmpty: {
    color: "#7D8CA0",
    fontSize: 13,
    fontFamily: Typography.uiFont,
    paddingVertical: 12,
  },
  agentRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingVertical: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: "rgba(255,255,255,0.04)",
  },
  agentRowActive: {
    opacity: 1,
  },
  agentRowBody: {
    flex: 1,
    minWidth: 0,
    gap: 2,
  },
  agentRowStatusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  agentRowTitle: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  agentRowMeta: {
    color: Colors.textSecondary,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
    opacity: 0.55,
  },
});
