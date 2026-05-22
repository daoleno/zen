import React from "react";
import {
  Modal,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Colors, Typography } from "../../constants/tokens";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { AgentDirectorySection } from "../../services/serverSelection";
import { TerminalAgentPickerList } from "./TerminalAgentPickerList";

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

          <TerminalAgentPickerList
            sections={sections}
            agentCount={agentCount}
            activeSessionKey={activeSessionKey}
            showServerNames={showServerNames}
            agentAliases={agentAliases}
            chrome={chrome}
            onOpenAgent={onOpenAgent}
          />

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
});
