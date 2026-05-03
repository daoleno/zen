import React, { useEffect, useMemo, useState } from "react";
import {
  Alert,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  SectionList,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { useRouter } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView, useSafeAreaInsets } from "react-native-safe-area-context";
import { Colors, Radii, Spacing, Typography, useAppColors } from "../../constants/tokens";
import { useIssues, type Issue } from "../../store/issues";
import { useAgents } from "../../store/agents";
import { IssueRow } from "../../components/issue/IssueRow";
import { getServers, type StoredServer } from "../../services/storage";
import { wsClient } from "../../services/websocket";

type SectionItem =
  | { kind: "issue"; key: string }
  | { kind: "done-toggle"; projectKey: string; count: number; expanded: boolean };

type IssueSection = {
  key: string;
  projectKey: string;
  title: string;
  subtitle: string | null;
  activeCount: number;
  doneCount: number;
  data: SectionItem[];
};

export default function IssuesScreen() {
  const router = useRouter();
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const { state } = useIssues();
  const { state: agentsState } = useAgents();
  const [servers, setServers] = useState<StoredServer[]>([]);
  const [creating, setCreating] = useState(false);
  const [selectedServerId, setSelectedServerId] = useState<string | null>(null);
  const [projectDraft, setProjectDraft] = useState("inbox");
  const [submitting, setSubmitting] = useState(false);
  const [expandedDone, setExpandedDone] = useState<Record<string, boolean>>({});

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const nextServers = await getServers();
      if (!cancelled) {
        setServers(nextServers);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const connectedServers = useMemo(
    () =>
      servers.filter(
        (server) => agentsState.serverConnections[server.id] === "connected",
      ),
    [agentsState.serverConnections, servers],
  );

  useEffect(() => {
    if (
      !selectedServerId ||
      !connectedServers.some((server) => server.id === selectedServerId)
    ) {
      setSelectedServerId(connectedServers[0]?.id ?? null);
    }
  }, [connectedServers, selectedServerId]);

  const showServerPrefix = useMemo(() => {
    const ids = new Set<string>();
    for (const issue of Object.values(state.byKey)) {
      ids.add(issue.serverId);
      if (ids.size > 1) return true;
    }
    return false;
  }, [state.byKey]);

  const sections = useMemo<IssueSection[]>(() => {
    const out: IssueSection[] = [];
    const entries = Object.entries(state.byProject).sort(([left], [right]) =>
      left.localeCompare(right),
    );

    for (const [projectKey, keys] of entries) {
      if (keys.length === 0) continue;
      const first = state.byKey[keys[0]];
      if (!first) continue;

      const active: SectionItem[] = [];
      const done: SectionItem[] = [];
      for (const key of keys) {
        const issue = state.byKey[key];
        if (!issue) continue;
        if (issue.frontmatter.done) {
          done.push({ kind: "issue", key });
        } else {
          active.push({ kind: "issue", key });
        }
      }

      const items: SectionItem[] = [...active];
      if (done.length > 0) {
        const expanded = !!expandedDone[projectKey];
        items.push({
          kind: "done-toggle",
          projectKey,
          count: done.length,
          expanded,
        });
        if (expanded) {
          items.push(...done);
        }
      }

      if (items.length === 0) continue;

      out.push({
        key: projectKey,
        projectKey,
        title: first.project,
        subtitle: showServerPrefix ? first.serverName : null,
        activeCount: active.length,
        doneCount: done.length,
        data: items,
      });
    }

    return out;
  }, [expandedDone, showServerPrefix, state.byKey, state.byProject]);

  const createIssue = async (serverId: string, project: string) => {
    setSubmitting(true);
    try {
      const issue = await wsClient.writeIssue(serverId, {
        project: project.trim() || "inbox",
        body: "# New issue\n\n",
        frontmatter: {},
      });
      setCreating(false);
      router.push({
        pathname: "/issue/[id]",
        params: { id: issue.id, serverId },
      });
    } catch (error: any) {
      Alert.alert("Create failed", error?.message || "Could not create issue.");
    } finally {
      setSubmitting(false);
    }
  };

  const onCreatePress = () => {
    if (connectedServers.length === 0) {
      Alert.alert(
        "No daemon",
        "Connect to a daemon before creating an issue.",
      );
      return;
    }
    setProjectDraft("inbox");
    setSelectedServerId((prev) => prev ?? connectedServers[0]?.id ?? null);
    setCreating(true);
  };

  const toggleDone = (projectKey: string) =>
    setExpandedDone((prev) => ({ ...prev, [projectKey]: !prev[projectKey] }));

  const isEmpty = sections.length === 0;
  const summary = useMemo(
    () => issueSummary(Object.values(state.byKey)),
    [state.byKey],
  );

  return (
    <SafeAreaView style={styles.screen} edges={["top"]}>
      <View style={styles.header}>
        <View style={styles.headerCopy}>
          <Text style={styles.title}>Issues</Text>
          <Text style={styles.subtitle}>{summary}</Text>
        </View>
        <Pressable
          onPress={onCreatePress}
          hitSlop={8}
          style={({ pressed }) => [styles.addButton, pressed && styles.addButtonPressed]}
        >
          <Ionicons name="add" size={19} color={colors.textPrimary} />
        </Pressable>
      </View>

      {isEmpty ? (
        <View style={styles.emptyState}>
          <Text style={styles.emptyGlyph}>∷</Text>
          <Text style={styles.emptyTitle}>No issues</Text>
          <Text style={styles.emptyBody}>Everything is quiet.</Text>
          <Pressable
            onPress={onCreatePress}
            style={({ pressed }) => [styles.emptyAction, pressed && styles.emptyActionPressed]}
          >
            <Text style={styles.emptyActionText}>New issue</Text>
          </Pressable>
        </View>
      ) : (
        <SectionList
          sections={sections}
          keyExtractor={(item, index) =>
            item.kind === "issue"
              ? item.key
              : `toggle:${item.projectKey}:${index}`
          }
          renderItem={({ item }) => {
            if (item.kind === "done-toggle") {
              return (
                <Pressable
                  onPress={() => toggleDone(item.projectKey)}
                  style={({ pressed }) => [
                    styles.doneToggle,
                    pressed && styles.doneTogglePressed,
                  ]}
                >
                  <Ionicons
                    name={item.expanded ? "chevron-down" : "chevron-forward"}
                    size={14}
                    color={colors.textSecondary}
                  />
                  <Text style={styles.doneToggleText}>
                    {item.expanded ? "hide done" : "show done"} · {item.count}
                  </Text>
                </Pressable>
              );
            }
            const issue = state.byKey[item.key];
            return issue ? <IssueRow issue={issue} /> : null;
          }}
          renderSectionHeader={({ section }) => (
            <View style={styles.sectionHeader}>
              <View style={styles.sectionHeaderCopy}>
                <View style={styles.sectionPrompt}>
                  <Text style={styles.sectionTitle} numberOfLines={1}>{section.title}</Text>
                  <Text style={styles.sectionArrow}>❯</Text>
                  <Text style={styles.sectionState}>
                    {section.activeCount}
                  </Text>
                </View>
                {section.subtitle ? (
                  <Text style={styles.sectionSubtitle} numberOfLines={1}>
                    {section.subtitle}
                  </Text>
                ) : null}
              </View>
            </View>
          )}
          SectionSeparatorComponent={() => <View style={styles.sectionSeparator} />}
          stickySectionHeadersEnabled={false}
          contentContainerStyle={styles.listContent}
          showsVerticalScrollIndicator={false}
          removeClippedSubviews={false}
          windowSize={15}
        />
      )}

      <CreateIssueSheet
        visible={creating}
        servers={connectedServers}
        selectedServerId={selectedServerId}
        onSelectServer={setSelectedServerId}
        project={projectDraft}
        onChangeProject={setProjectDraft}
        submitting={submitting}
        onClose={() => setCreating(false)}
        onSubmit={() => {
          if (!selectedServerId) return;
          void createIssue(selectedServerId, projectDraft);
        }}
      />
    </SafeAreaView>
  );
}

function issueSummary(issues: Issue[]): string {
  if (issues.length === 0) {
    return "quiet";
  }
  let open = 0;
  let sent = 0;
  let done = 0;
  const projects = new Set<string>();
  for (const issue of issues) {
    projects.add(`${issue.serverId}:${issue.project}`);
    if (issue.frontmatter.done) {
      done++;
    } else if (issue.frontmatter.dispatched) {
      sent++;
      open++;
    } else {
      open++;
    }
  }

  const parts = [`${open} open`];
  if (sent > 0) parts.push(`${sent} sent`);
  if (done > 0) parts.push(`${done} done`);
  parts.push(`${projects.size} workspace${projects.size === 1 ? "" : "s"}`);
  return parts.join(" · ");
}

function CreateIssueSheet({
  visible,
  servers,
  selectedServerId,
  onSelectServer,
  project,
  onChangeProject,
  submitting,
  onClose,
  onSubmit,
}: {
  visible: boolean;
  servers: StoredServer[];
  selectedServerId: string | null;
  onSelectServer: (id: string) => void;
  project: string;
  onChangeProject: (value: string) => void;
  submitting: boolean;
  onClose: () => void;
  onSubmit: () => void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const insets = useSafeAreaInsets();
  const disabled = submitting || servers.length === 0 || !selectedServerId;
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      <KeyboardAvoidingView
        style={styles.sheetRoot}
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        <Pressable style={styles.sheetBackdrop} onPress={onClose} />
        <View style={{ paddingBottom: Math.max(insets.bottom, Spacing.md) }}>
          <View style={styles.sheetCard}>
            <View style={styles.sheetHandle} />
            <Text style={styles.sheetTitle}>New issue</Text>

            {servers.length > 1 ? (
              <>
                <Text style={styles.fieldLabel}>Daemon</Text>
                <View style={styles.serverList}>
                  {servers.map((server) => {
                    const selected = server.id === selectedServerId;
                    return (
                      <Pressable
                        key={server.id}
                        onPress={() => onSelectServer(server.id)}
                        style={[styles.serverChip, selected && styles.serverChipSelected]}
                      >
                        <Text
                          style={[
                            styles.serverChipText,
                            selected && styles.serverChipTextSelected,
                          ]}
                        >
                          {server.name}
                        </Text>
                      </Pressable>
                    );
                  })}
                </View>
              </>
            ) : null}

            <Text style={styles.fieldLabel}>Project</Text>
            <TextInput
              value={project}
              onChangeText={onChangeProject}
              placeholder="inbox"
              placeholderTextColor={colors.textSecondary}
              style={styles.input}
              autoCapitalize="none"
              autoCorrect={false}
            />

            <View style={styles.sheetActions}>
              <Pressable
                onPress={onClose}
                style={({ pressed }) => [
                  styles.sheetButton,
                  styles.sheetButtonSecondary,
                  pressed && styles.sheetButtonPressed,
                ]}
              >
                <Text style={styles.sheetButtonText}>Cancel</Text>
              </Pressable>
              <Pressable
                onPress={onSubmit}
                disabled={disabled}
                style={({ pressed }) => [
                  styles.sheetButton,
                  styles.sheetButtonPrimary,
                  disabled && styles.sheetButtonDisabled,
                  pressed && styles.sheetButtonPressed,
                ]}
              >
                <Text style={[styles.sheetButtonText, styles.sheetButtonTextPrimary]}>
                  {submitting ? "Creating…" : "Create"}
                </Text>
              </Pressable>
            </View>
          </View>
        </View>
      </KeyboardAvoidingView>
    </Modal>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
  screen: {
    flex: 1,
    backgroundColor: colors.bgPrimary,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 16,
    paddingTop: 8,
    paddingBottom: 10,
  },
  headerCopy: {
    flex: 1,
    minWidth: 0,
    paddingRight: Spacing.md,
  },
  title: {
    color: colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 22,
    lineHeight: 28,
    letterSpacing: 0.6,
    opacity: 0.9,
  },
  subtitle: {
    marginTop: 2,
    color: colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 10,
    lineHeight: 13,
    opacity: 0.58,
  },
  addButton: {
    width: 34,
    height: 34,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 11,
    backgroundColor: colors.surfacePressed,
  },
  addButtonPressed: {
    opacity: 0.82,
  },
  listContent: {
    paddingHorizontal: 16,
    paddingTop: 6,
    paddingBottom: 28,
  },
  sectionHeader: {
    minHeight: 31,
    paddingTop: 7,
    paddingBottom: 3,
    flexDirection: "row",
    alignItems: "flex-end",
    gap: 10,
    backgroundColor: colors.bgPrimary,
  },
  sectionHeaderCopy: {
    flex: 1,
    minWidth: 0,
  },
  sectionTitle: {
    maxWidth: "76%",
    flexShrink: 1,
    color: colors.promptGreen,
    fontFamily: Typography.terminalFont,
    fontSize: 13,
    lineHeight: 17,
    letterSpacing: -0.1,
  },
  sectionPrompt: {
    flexDirection: "row",
    alignItems: "center",
    minWidth: 0,
  },
  sectionArrow: {
    marginHorizontal: 7,
    color: colors.promptYellow,
    fontFamily: Typography.terminalFontBold,
    fontSize: 12,
    lineHeight: 17,
  },
  sectionState: {
    color: colors.textSecondary,
    fontFamily: Typography.terminalFont,
    fontSize: 10,
    lineHeight: 14,
    opacity: 0.5,
  },
  sectionSubtitle: {
    marginTop: 2,
    color: colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 10,
    lineHeight: 12,
    opacity: 0.48,
  },
  sectionSeparator: {
    height: 4,
  },
  doneToggle: {
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.sm,
    height: 28,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.borderSubtle,
  },
  doneTogglePressed: {
    opacity: 0.7,
  },
  doneToggleText: {
    color: colors.textSecondary,
    fontFamily: Typography.terminalFont,
    fontSize: 10,
    opacity: 0.56,
  },
  emptyState: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: Spacing.xl,
  },
  emptyGlyph: {
    color: colors.textSecondary,
    fontSize: 34,
    marginBottom: 12,
    opacity: 0.38,
  },
  emptyTitle: {
    color: colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 16,
  },
  emptyBody: {
    marginTop: Spacing.sm,
    color: colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
    textAlign: "center",
    opacity: 0.58,
  },
  emptyAction: {
    marginTop: 18,
    paddingHorizontal: 14,
    paddingVertical: 8,
    borderRadius: Radii.pill,
    backgroundColor: colors.accent,
  },
  emptyActionPressed: {
    opacity: 0.8,
  },
  emptyActionText: {
    color: colors.textOnAccent,
    fontFamily: Typography.uiFontMedium,
    fontSize: 13,
  },
  sheetRoot: {
    flex: 1,
    justifyContent: "flex-end",
  },
  sheetBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: colors.modalBackdrop,
  },
  sheetCard: {
    marginHorizontal: Spacing.md,
    marginBottom: Spacing.md,
    padding: 20,
    borderRadius: 20,
    backgroundColor: colors.modalSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
  },
  sheetHandle: {
    alignSelf: "center",
    width: 40,
    height: 3,
    borderRadius: 1.5,
    backgroundColor: colors.surfaceActive,
    marginBottom: Spacing.md,
  },
  sheetTitle: {
    color: colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 17,
  },
  fieldLabel: {
    marginTop: Spacing.lg,
    marginBottom: Spacing.sm,
    color: colors.textSecondary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 11,
    letterSpacing: 1.1,
    textTransform: "uppercase",
  },
  serverList: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: Spacing.sm,
  },
  serverChip: {
    paddingHorizontal: 10,
    paddingVertical: 6,
    borderRadius: Radii.pill,
    backgroundColor: colors.bgElevated,
  },
  serverChipSelected: {
    backgroundColor: colors.accent,
  },
  serverChipText: {
    color: colors.textPrimary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
  },
  serverChipTextSelected: {
    color: colors.textOnAccent,
    fontFamily: Typography.uiFontMedium,
  },
  input: {
    paddingHorizontal: Spacing.md,
    paddingVertical: Spacing.md,
    borderRadius: Radii.md,
    backgroundColor: colors.inputBackground,
    color: colors.textPrimary,
    fontFamily: Typography.uiFont,
    fontSize: 15,
  },
  sheetActions: {
    flexDirection: "row",
    justifyContent: "flex-end",
    gap: 8,
    marginTop: Spacing.xl,
  },
  sheetButton: {
    minHeight: 34,
    paddingHorizontal: 13,
    paddingVertical: 8,
    borderRadius: Radii.md,
  },
  sheetButtonPressed: {
    opacity: 0.8,
  },
  sheetButtonSecondary: {
    backgroundColor: colors.bgElevated,
  },
  sheetButtonPrimary: {
    backgroundColor: colors.accent,
  },
  sheetButtonDisabled: {
    opacity: 0.5,
  },
  sheetButtonText: {
    color: colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 13,
  },
  sheetButtonTextPrimary: {
    color: colors.textOnAccent,
  },
  });
}
