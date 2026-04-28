import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  Alert,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { useLocalSearchParams, useRouter } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView, useSafeAreaInsets } from "react-native-safe-area-context";
import { Colors, Radii, Spacing, Typography } from "../../constants/tokens";
import { useIssues, type Issue } from "../../store/issues";
import { useAgents } from "../../store/agents";
import {
  MarkdownEditor,
  type ActiveMention,
  type MarkdownEditorHandle,
} from "../../components/issue/MarkdownEditor";
import {
  MentionPicker,
  type MentionCandidate,
} from "../../components/issue/MentionPicker";
import { relativeTime } from "../../components/issue/IssueRow";
import { wsClient } from "../../services/websocket";

const AUTOSAVE_DELAY_MS = 600;
type IconName = React.ComponentProps<typeof Ionicons>["name"];

function issueKey(serverId: string, id: string) {
  return `${serverId}:${id}`;
}

function agentRole(command?: string) {
  const first = (command || "").trim().split(/\s+/)[0] || "agent";
  const parts = first.split("/");
  return parts[parts.length - 1] || "agent";
}

export default function IssueDetailScreen() {
  const params = useLocalSearchParams<{ id?: string; serverId?: string }>();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { state } = useIssues();
  const { state: agentsState } = useAgents();

  const issueId = typeof params.id === "string" ? params.id : "";
  const serverId = typeof params.serverId === "string" ? params.serverId : "";
  const issue = state.byKey[issueKey(serverId, issueId)] as Issue | undefined;

  const editorRef = useRef<MarkdownEditorHandle>(null);
  const [draftBody, setDraftBody] = useState(issue?.body ?? "");
  const [baseMtime, setBaseMtime] = useState(issue?.mtime ?? "");
  const [dirty, setDirty] = useState(false);
  const [remoteBanner, setRemoteBanner] = useState(false);
  const [saving, setSaving] = useState(false);
  const [mention, setMention] = useState<ActiveMention | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const savingRef = useRef(false);
  const draftBodyRef = useRef(draftBody);
  useEffect(() => {
    draftBodyRef.current = draftBody;
  }, [draftBody]);

  useEffect(() => {
    if (!issue) {
      return;
    }
    if (!dirty) {
      setDraftBody(issue.body);
      setBaseMtime(issue.mtime);
      setRemoteBanner(false);
      return;
    }
    if (issue.mtime !== baseMtime && issue.body !== draftBody) {
      setRemoteBanner(true);
    }
  }, [baseMtime, dirty, draftBody, issue]);

  const candidates = useMemo<MentionCandidate[]>(() => {
    if (!issue) {
      return [];
    }
    const roles = (state.executorsByServer[issue.serverId] || []).map<MentionCandidate>(
      (name) => ({ kind: "role", name }),
    );
    const sessions = agentsState.agents
      .filter(
        (agent) => agent.serverId === issue.serverId && agent.project === issue.project,
      )
      .map<MentionCandidate>((agent) => ({
        kind: "session",
        role: agentRole(agent.command),
        sessionId: agent.id,
        project: agent.project || issue.project,
      }));
    return [...roles, ...sessions];
  }, [agentsState.agents, issue, state.executorsByServer]);

  const saveIssue = async (frontmatter = issue?.frontmatter) => {
    if (!issue || !serverId || !frontmatter) {
      return null;
    }
    if (savingRef.current) {
      return null;
    }
    savingRef.current = true;
    setSaving(true);
    const bodyAtSave = draftBodyRef.current;
    try {
      const written = await wsClient.writeIssue(serverId, {
        id: issue.id,
        project: issue.project,
        path: issue.path,
        body: bodyAtSave,
        frontmatter,
        baseMtime,
      });
      setBaseMtime(written.mtime);
      // If the user didn't type during the save, normalize body and clear
      // dirty. Otherwise leave their newer text alone; the next autosave
      // tick will capture it.
      if (draftBodyRef.current === bodyAtSave) {
        if (written.body !== bodyAtSave) {
          setDraftBody(written.body);
        }
        setDirty(false);
      }
      setRemoteBanner(false);
      return written;
    } catch (error: any) {
      if (error?.code === "conflict" && error?.current) {
        setRemoteBanner(true);
        setBaseMtime(error.current.mtime || baseMtime);
      } else {
        Alert.alert("Save failed", error?.message || "Could not save issue.");
      }
      return null;
    } finally {
      savingRef.current = false;
      setSaving(false);
    }
  };

  // Debounced autosave whenever the body changes.
  useEffect(() => {
    if (!dirty || remoteBanner || !issue) {
      return;
    }
    const timer = setTimeout(() => {
      void saveIssue();
    }, AUTOSAVE_DELAY_MS);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dirty, draftBody, remoteBanner]);

  const handleSend = async () => {
    if (!issue || !serverId) {
      return;
    }
    const written = await saveIssue();
    if (!written) {
      return;
    }
    try {
      await wsClient.sendIssue(serverId, issue.id);
    } catch (error: any) {
      Alert.alert("Send failed", error?.message || "Could not dispatch issue.");
    }
  };

  const handleRedispatch = async () => {
    if (!issue || !serverId) {
      return;
    }
    const written = await saveIssue();
    if (!written) {
      return;
    }
    try {
      await wsClient.redispatchIssue(serverId, issue.id);
    } catch (error: any) {
      Alert.alert(
        "Redispatch failed",
        error?.message || "Could not redispatch issue.",
      );
    }
  };

  const handleToggleDone = async () => {
    if (!issue) {
      return;
    }
    const nextFrontmatter = {
      ...issue.frontmatter,
      done: issue.frontmatter.done ? null : new Date().toISOString(),
    };
    await saveIssue(nextFrontmatter);
  };

  const handleDelete = () => {
    if (!issue || !serverId) {
      return;
    }
    Alert.alert("Delete issue", "Remove this Markdown issue file?", [
      { text: "Cancel", style: "cancel" },
      {
        text: "Delete",
        style: "destructive",
        onPress: () => {
          void (async () => {
            try {
              await wsClient.deleteIssue(serverId, issue.id);
              router.back();
            } catch (error: any) {
              Alert.alert(
                "Delete failed",
                error?.message || "Could not delete issue.",
              );
            }
          })();
        },
      },
    ]);
  };

  const handleSelectMention = (candidate: MentionCandidate) => {
    editorRef.current?.insertMention(candidate);
  };

  if (!issue) {
    return (
      <SafeAreaView style={styles.emptyScreen} edges={["top"]}>
        <Text style={styles.emptyTitle}>Issue not found</Text>
      </SafeAreaView>
    );
  }

  const done = !!issue.frontmatter.done;
  const dispatched = !!issue.frontmatter.dispatched;
  const primaryLabel = dispatched ? "Resend" : "Send";
  const draftTitle = titleFromMarkdown(draftBody) || issue.title || "Untitled issue";
  const status = issueStatusInfo(done, dispatched);
  const updatedLabel = relativeTime(issue.mtime || issue.frontmatter.created);

  return (
    <SafeAreaView style={styles.screen} edges={["top"]}>
      <KeyboardAvoidingView
        style={styles.kav}
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        <View style={styles.header}>
          <Pressable
            onPress={() => router.back()}
            hitSlop={10}
            style={({ pressed }) => [styles.iconButton, pressed && styles.iconButtonPressed]}
          >
            <Ionicons name="chevron-back" size={22} color={Colors.textPrimary} />
          </Pressable>

          <View style={styles.headerCenter}>
            <Text style={styles.headerTitle} numberOfLines={1}>
              {issue.project}
            </Text>
          </View>

          <Pressable
            onPress={() => setMenuOpen(true)}
            hitSlop={10}
            style={({ pressed }) => [styles.iconButton, pressed && styles.iconButtonPressed]}
          >
            <Ionicons name="ellipsis-horizontal" size={20} color={Colors.textPrimary} />
          </Pressable>
        </View>

        <View style={styles.context}>
          <View style={styles.statusRow}>
            <StatusPill
              icon={status.icon}
              label={status.label}
              color={status.color}
            />
            <Text style={styles.contextPath} numberOfLines={1}>
              {issue.serverName ? `${issue.serverName} · ${issue.project}` : issue.project}
              {updatedLabel ? ` · ${updatedLabel}` : ""}
            </Text>
          </View>

          <Text style={styles.issueTitle} numberOfLines={2}>
            {draftTitle}
          </Text>
        </View>

        {remoteBanner ? (
          <Pressable
            onPress={() => {
              setDraftBody(issue.body);
              setBaseMtime(issue.mtime);
              setDirty(false);
              setRemoteBanner(false);
            }}
            style={styles.banner}
          >
            <Ionicons
              name="cloud-download-outline"
              size={14}
              color={Colors.textPrimary}
            />
            <Text style={styles.bannerText}>
              Remote changes — tap to load.
            </Text>
          </Pressable>
        ) : null}

        <View style={styles.editorShell}>
          <MarkdownEditor
            ref={editorRef}
            value={draftBody}
            onChange={(next) => {
              setDraftBody(next);
              setDirty(true);
            }}
            onActiveMentionChange={setMention}
            onBlur={() => {
              if (dirty && !remoteBanner) {
                void saveIssue();
              }
            }}
            autoFocus
          />
        </View>

        {mention ? (
          <MentionPicker
            candidates={candidates}
            query={mention.query}
            onSelect={handleSelectMention}
          />
        ) : null}

        <View style={[styles.footer, { paddingBottom: Math.max(insets.bottom, Spacing.md) }]}>
          <SaveState saving={saving} dirty={dirty} />
          <View style={styles.footerActions}>
            <Pressable
              onPress={() => {
                void handleToggleDone();
              }}
              disabled={saving}
              style={({ pressed }) => [
                styles.secondaryButton,
                saving && styles.primaryButtonDisabled,
                pressed && styles.secondaryButtonPressed,
              ]}
            >
              <Ionicons
                name={done ? "return-up-back-outline" : "checkmark"}
                size={14}
                color={Colors.textPrimary}
              />
              <Text style={styles.secondaryButtonText}>{done ? "Reopen" : "Done"}</Text>
            </Pressable>
            <Pressable
              onPress={() => {
                void (dispatched ? handleRedispatch() : handleSend());
              }}
              disabled={saving}
              style={({ pressed }) => [
                styles.primaryButton,
                saving && styles.primaryButtonDisabled,
                pressed && styles.primaryButtonPressed,
              ]}
            >
              <Ionicons
                name={dispatched ? "refresh" : "paper-plane"}
                size={14}
                color={Colors.bgPrimary}
              />
              <Text style={styles.primaryButtonText}>{primaryLabel}</Text>
            </Pressable>
          </View>
        </View>
      </KeyboardAvoidingView>

      <OverflowMenu
        visible={menuOpen}
        onClose={() => setMenuOpen(false)}
        done={done}
        onToggleDone={() => {
          setMenuOpen(false);
          void handleToggleDone();
        }}
        onDelete={() => {
          setMenuOpen(false);
          handleDelete();
        }}
      />
    </SafeAreaView>
  );
}

function StatusPill({
  icon,
  label,
  color,
}: {
  icon: IconName;
  label: string;
  color: string;
}) {
  return (
    <View style={styles.statusPill}>
      <Ionicons name={icon} size={13} color={color} />
      <Text style={[styles.statusPillText, { color }]}>{label}</Text>
    </View>
  );
}

function titleFromMarkdown(value: string): string {
  const firstHeading = value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find((line) => line.length > 0);
  if (!firstHeading) {
    return "";
  }
  return firstHeading.replace(/^#{1,6}\s+/, "").trim();
}

function issueStatusInfo(done: boolean, dispatched: boolean): {
  icon: IconName;
  label: string;
  color: string;
} {
  if (done) {
    return { icon: "checkmark-circle", label: "Done", color: Colors.textSecondary };
  }
  if (dispatched) {
    return { icon: "paper-plane", label: "Sent", color: Colors.statusRunning };
  }
  return { icon: "ellipse", label: "Open", color: Colors.accent };
}

function SaveState({ saving, dirty }: { saving: boolean; dirty: boolean }) {
  const label = saving ? "Saving" : dirty ? "Unsaved" : "Saved";
  return (
    <View style={styles.savingTag}>
      <View style={[
        styles.savingDot,
        dirty && !saving && styles.unsavedDot,
        !dirty && !saving && styles.savedDot,
      ]} />
      <Text style={[styles.savingText, !dirty && !saving && styles.savedText]}>
        {label}
      </Text>
    </View>
  );
}

function OverflowMenu({
  visible,
  onClose,
  done,
  onToggleDone,
  onDelete,
}: {
  visible: boolean;
  onClose: () => void;
  done: boolean;
  onToggleDone: () => void;
  onDelete: () => void;
}) {
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      <View style={menuStyles.root}>
        <Pressable style={menuStyles.backdrop} onPress={onClose} />
        <View style={menuStyles.card}>
          <Pressable
            onPress={onToggleDone}
            style={({ pressed }) => [menuStyles.item, pressed && menuStyles.itemPressed]}
          >
            <Ionicons
              name={done ? "refresh-outline" : "checkmark-circle-outline"}
              size={18}
              color={Colors.textPrimary}
            />
            <Text style={menuStyles.itemText}>{done ? "Reopen" : "Mark done"}</Text>
          </Pressable>
          <View style={menuStyles.divider} />
          <Pressable
            onPress={onDelete}
            style={({ pressed }) => [menuStyles.item, pressed && menuStyles.itemPressed]}
          >
            <Ionicons name="trash-outline" size={18} color={Colors.statusFailed} />
            <Text style={[menuStyles.itemText, menuStyles.itemTextDestructive]}>
              Delete
            </Text>
          </Pressable>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  screen: {
    flex: 1,
    backgroundColor: Colors.bgPrimary,
  },
  kav: {
    flex: 1,
  },
  emptyScreen: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: Colors.bgPrimary,
  },
  emptyTitle: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 17,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 10,
    paddingTop: 3,
    paddingBottom: 5,
  },
  iconButton: {
    width: 36,
    height: 36,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: Radii.pill,
  },
  iconButtonPressed: {
    backgroundColor: "rgba(255,255,255,0.05)",
  },
  headerCenter: {
    flex: 1,
    alignItems: "center",
    minWidth: 0,
  },
  headerTitle: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 14,
    lineHeight: 18,
    opacity: 0.86,
  },
  context: {
    paddingHorizontal: 16,
    paddingTop: 6,
    paddingBottom: 11,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: "rgba(235,225,207,0.055)",
  },
  statusRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.sm,
  },
  statusPill: {
    height: 22,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    paddingHorizontal: 8,
    borderRadius: Radii.pill,
    backgroundColor: "rgba(255,255,255,0.035)",
  },
  statusPillText: {
    fontFamily: Typography.terminalFont,
    fontSize: 10,
    lineHeight: 13,
    textTransform: "uppercase",
  },
  contextPath: {
    flex: 1,
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 11,
    lineHeight: 15,
    opacity: 0.56,
  },
  issueTitle: {
    marginTop: 9,
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 20,
    lineHeight: 26,
    letterSpacing: -0.2,
    opacity: 0.94,
  },
  banner: {
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.sm,
    marginHorizontal: Spacing.md,
    marginBottom: Spacing.sm,
    paddingHorizontal: Spacing.md,
    paddingVertical: Spacing.sm,
    borderRadius: Radii.md,
    backgroundColor: "rgba(255,183,77,0.14)",
  },
  bannerText: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFont,
    fontSize: 13,
  },
  editorShell: {
    flex: 1,
    marginHorizontal: 10,
    marginTop: 8,
    marginBottom: 8,
    borderRadius: 14,
    overflow: "hidden",
    backgroundColor: "rgba(255,255,255,0.018)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(235,225,207,0.052)",
  },
  footer: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: Spacing.sm,
    paddingHorizontal: 14,
    paddingTop: 8,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: "rgba(235,225,207,0.055)",
    backgroundColor: "rgba(15,15,20,0.98)",
  },
  footerActions: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    flexShrink: 0,
  },
  savingTag: {
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.xs,
    flexShrink: 1,
  },
  savingDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    backgroundColor: Colors.statusUnknown,
  },
  unsavedDot: {
    backgroundColor: Colors.statusUnknown,
  },
  savedDot: {
    backgroundColor: Colors.statusDone,
  },
  savingText: {
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
  },
  savedText: {
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
    opacity: 0.46,
  },
  secondaryButton: {
    height: 34,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    paddingHorizontal: 10,
    borderRadius: Radii.pill,
    backgroundColor: "rgba(255,255,255,0.052)",
  },
  secondaryButtonPressed: {
    opacity: 0.78,
  },
  secondaryButtonText: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 12,
    lineHeight: 16,
  },
  primaryButton: {
    height: 34,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    paddingHorizontal: 12,
    borderRadius: Radii.pill,
    backgroundColor: Colors.accent,
  },
  primaryButtonPressed: {
    opacity: 0.85,
  },
  primaryButtonDisabled: {
    opacity: 0.5,
  },
  primaryButtonText: {
    color: Colors.bgPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 13,
    lineHeight: 17,
  },
});

const menuStyles = StyleSheet.create({
  root: {
    flex: 1,
    justifyContent: "flex-end",
  },
  backdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: "rgba(0,0,0,0.6)",
  },
  card: {
    marginHorizontal: Spacing.md,
    marginBottom: Spacing.xl,
    borderRadius: Radii.lg,
    backgroundColor: Colors.bgSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: Colors.bgElevated,
    overflow: "hidden",
  },
  item: {
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.md,
    paddingHorizontal: Spacing.lg,
    paddingVertical: Spacing.lg,
  },
  itemPressed: {
    backgroundColor: Colors.bgElevated,
  },
  divider: {
    height: StyleSheet.hairlineWidth,
    backgroundColor: Colors.bgElevated,
    marginHorizontal: Spacing.lg,
  },
  itemText: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFont,
    fontSize: 15,
  },
  itemTextDestructive: {
    color: Colors.statusFailed,
  },
});
