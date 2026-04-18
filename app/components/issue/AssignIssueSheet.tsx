import React from "react";
import {
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import {
  Colors,
  Typography,
  statusColor,
  type AgentStatus,
} from "../../constants/tokens";
import { AgentKindIcon } from "../terminal/AgentKindIcon";

export type AssignAgentPreset = {
  key: "claude" | "codex";
  label: string;
  description: string;
  command: string;
};

interface AssignIssueSheetProps {
  visible: boolean;
  busy?: boolean;
  presets: AssignAgentPreset[];
  projectName?: string;
  workspaceCwd?: string;
  repoRoot?: string;
  worktreeRoot?: string;
  baseBranch?: string;
  currentSessionLabel?: string;
  currentSessionStatus?: AgentStatus;
  currentSessionSubtitle?: string;
  canAssign: boolean;
  onClose: () => void;
  onConfigureProject: () => void;
  onOpenCurrentSession?: () => void;
  onAssign: (preset: AssignAgentPreset) => void;
}

export function AssignIssueSheet({
  visible,
  busy = false,
  presets,
  projectName,
  workspaceCwd,
  repoRoot,
  worktreeRoot,
  baseBranch,
  currentSessionLabel,
  currentSessionStatus = "unknown",
  currentSessionSubtitle,
  canAssign,
  onClose,
  onConfigureProject,
  onOpenCurrentSession,
  onAssign,
}: AssignIssueSheetProps) {
  const insets = useSafeAreaInsets();
  const title = currentSessionLabel ? "Change agent" : "Assign agent";

  return (
    <Modal
      visible={visible}
      transparent
      animationType="slide"
      onRequestClose={onClose}
    >
      <View style={styles.root}>
        <Pressable style={styles.backdrop} onPress={onClose} />

        <View style={[styles.sheet, { paddingBottom: insets.bottom + 18 }]}>
          <View style={styles.handle} />

          <View style={styles.header}>
            <Text style={styles.title}>{title}</Text>
            <TouchableOpacity
              style={styles.closeButton}
              onPress={onClose}
              activeOpacity={0.82}
            >
              <Ionicons name="close" size={16} color={Colors.textPrimary} />
            </TouchableOpacity>
          </View>

          <ScrollView
            showsVerticalScrollIndicator={false}
            contentContainerStyle={styles.content}
          >
            <View style={styles.summary}>
              <Text style={styles.summaryTitle}>
                {projectName
                  ? projectName
                  : workspaceCwd
                    ? "Existing workspace"
                    : "No project selected"}
              </Text>
              {workspaceCwd ? (
                <Text style={styles.summaryLine} numberOfLines={1}>
                  Workspace · {workspaceCwd}
                </Text>
              ) : null}
              {repoRoot ? (
                <Text style={styles.summaryLine} numberOfLines={1}>
                  Repo · {repoRoot}
                </Text>
              ) : null}
              {worktreeRoot ? (
                <Text style={styles.summaryLine} numberOfLines={1}>
                  Worktrees · {worktreeRoot}
                </Text>
              ) : null}
              {baseBranch ? (
                <Text style={styles.summaryLine}>Base branch · {baseBranch}</Text>
              ) : null}
            </View>

            {currentSessionLabel && onOpenCurrentSession ? (
              <View style={styles.currentSessionCard}>
                <View style={styles.currentSessionHeader}>
                  <Text style={styles.currentSessionEyebrow}>Current session</Text>
                  <TouchableOpacity
                    style={styles.currentSessionAction}
                    onPress={onOpenCurrentSession}
                    activeOpacity={0.82}
                  >
                    <Ionicons
                      name="terminal-outline"
                      size={13}
                      color={Colors.accent}
                    />
                    <Text style={styles.currentSessionActionText}>
                      Open session
                    </Text>
                  </TouchableOpacity>
                </View>

                <View style={styles.currentSessionRow}>
                  <View
                    style={[
                      styles.currentSessionDot,
                      { backgroundColor: statusColor(currentSessionStatus) },
                    ]}
                  />
                  <View style={styles.currentSessionCopy}>
                    <Text style={styles.currentSessionTitle} numberOfLines={1}>
                      {currentSessionLabel}
                    </Text>
                    {currentSessionSubtitle ? (
                      <Text
                        style={styles.currentSessionSubtitle}
                        numberOfLines={2}
                      >
                        {currentSessionSubtitle}
                      </Text>
                    ) : null}
                  </View>
                </View>
              </View>
            ) : null}

            {!canAssign ? (
              <View style={styles.warning}>
                <Text style={styles.warningTitle}>Project setup needed</Text>
                <Text style={styles.warningBody}>
                  Set a repo root before starting a new run.
                </Text>
                <TouchableOpacity
                  style={styles.configureButton}
                  onPress={onConfigureProject}
                  activeOpacity={0.82}
                >
                  <Text style={styles.configureButtonText}>Configure project</Text>
                </TouchableOpacity>
              </View>
            ) : null}

            <View style={styles.optionList}>
              {presets.map((preset) => (
                <TouchableOpacity
                  key={preset.key}
                  style={[
                    styles.optionRow,
                    (!canAssign || busy) && styles.optionRowDisabled,
                  ]}
                  onPress={() => onAssign(preset)}
                  disabled={!canAssign || busy}
                  activeOpacity={0.82}
                >
                  <AgentKindIcon kind={preset.key} size={18} />
                  <View style={styles.optionCopy}>
                    <Text style={styles.optionTitle}>{preset.label}</Text>
                    <Text style={styles.optionBody}>{preset.description}</Text>
                  </View>
                  <Ionicons
                    name="chevron-forward"
                    size={16}
                    color={Colors.textSecondary}
                  />
                </TouchableOpacity>
              ))}
            </View>
          </ScrollView>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    justifyContent: "flex-end",
  },
  backdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: "rgba(0,0,0,0.56)",
  },
  sheet: {
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    backgroundColor: "#16161D",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    paddingTop: 8,
  },
  handle: {
    alignSelf: "center",
    width: 44,
    height: 4,
    borderRadius: 2,
    backgroundColor: "rgba(255,255,255,0.14)",
    marginBottom: 10,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 16,
    paddingBottom: 10,
  },
  title: {
    color: Colors.textPrimary,
    fontSize: 18,
    fontFamily: Typography.uiFontMedium,
  },
  closeButton: {
    width: 32,
    height: 32,
    borderRadius: 16,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.04)",
  },
  content: {
    paddingHorizontal: 12,
    gap: 14,
  },
  summary: {
    borderRadius: 18,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    paddingHorizontal: 14,
    paddingVertical: 12,
    gap: 4,
  },
  summaryTitle: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  summaryLine: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  currentSessionCard: {
    borderRadius: 18,
    backgroundColor: "rgba(91,157,255,0.08)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(91,157,255,0.22)",
    paddingHorizontal: 14,
    paddingVertical: 12,
    gap: 10,
  },
  currentSessionHeader: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
  },
  currentSessionEyebrow: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 0.5,
    textTransform: "uppercase",
  },
  currentSessionAction: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  currentSessionActionText: {
    color: Colors.accent,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  currentSessionRow: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 10,
  },
  currentSessionDot: {
    width: 9,
    height: 9,
    borderRadius: 4.5,
    marginTop: 4,
  },
  currentSessionCopy: {
    flex: 1,
    gap: 4,
  },
  currentSessionTitle: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  currentSessionSubtitle: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.uiFont,
  },
  warning: {
    borderRadius: 18,
    backgroundColor: "rgba(91,157,255,0.08)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(91,157,255,0.22)",
    paddingHorizontal: 14,
    paddingVertical: 12,
    gap: 8,
  },
  warningTitle: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  warningBody: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  configureButton: {
    alignSelf: "flex-start",
    minHeight: 34,
    paddingHorizontal: 12,
    borderRadius: 17,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.08)",
  },
  configureButtonText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  optionList: {
    gap: 10,
  },
  optionRow: {
    minHeight: 66,
    borderRadius: 18,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    paddingHorizontal: 14,
    paddingVertical: 12,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  optionRowDisabled: {
    opacity: 0.46,
  },
  optionCopy: {
    flex: 1,
    gap: 3,
  },
  optionTitle: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  optionBody: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.uiFont,
  },
});
