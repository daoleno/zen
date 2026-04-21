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
import { Colors, Radii, Spacing, Typography } from "../../constants/tokens";
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
  data: SectionItem[];
};

export default function IssuesScreen() {
  const router = useRouter();
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

      const title = showServerPrefix
        ? `${first.serverName} · ${first.project}`
        : first.project;

      out.push({
        key: projectKey,
        projectKey,
        title,
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

  return (
    <SafeAreaView style={styles.screen} edges={["top"]}>
      <View style={styles.header}>
        <Text style={styles.title}>Issues</Text>
        <Pressable
          onPress={onCreatePress}
          hitSlop={8}
          style={({ pressed }) => [styles.addButton, pressed && styles.addButtonPressed]}
        >
          <Ionicons name="add" size={20} color={Colors.textPrimary} />
        </Pressable>
      </View>

      {isEmpty ? (
        <View style={styles.emptyState}>
          <Text style={styles.emptyGlyph}>☯</Text>
          <Text style={styles.emptyTitle}>No issues yet</Text>
          <Text style={styles.emptyBody}>
            Capture a thought, dispatch to an agent.
          </Text>
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
                    color={Colors.textSecondary}
                  />
                  <Text style={styles.doneToggleText}>Done ({item.count})</Text>
                </Pressable>
              );
            }
            const issue = state.byKey[item.key];
            return issue ? <IssueRow issue={issue} /> : null;
          }}
          renderSectionHeader={({ section }) => (
            <Text style={styles.sectionHeader}>{section.title}</Text>
          )}
          SectionSeparatorComponent={null}
          stickySectionHeadersEnabled={false}
          contentContainerStyle={styles.listContent}
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
              placeholderTextColor={Colors.textSecondary}
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

const styles = StyleSheet.create({
  screen: {
    flex: 1,
    backgroundColor: Colors.bgPrimary,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: Spacing.lg,
    paddingVertical: Spacing.sm,
  },
  title: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 22,
  },
  addButton: {
    width: 32,
    height: 32,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: Radii.pill,
    backgroundColor: Colors.bgSurface,
  },
  addButtonPressed: {
    opacity: 0.7,
  },
  listContent: {
    paddingBottom: Spacing.xxl,
  },
  sectionHeader: {
    paddingHorizontal: Spacing.lg,
    paddingTop: Spacing.xl,
    paddingBottom: Spacing.sm,
    color: Colors.textSecondary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 11,
    letterSpacing: 1.2,
    textTransform: "uppercase",
  },
  doneToggle: {
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.sm,
    paddingHorizontal: Spacing.lg,
    height: 36,
  },
  doneTogglePressed: {
    opacity: 0.7,
  },
  doneToggleText: {
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
  },
  emptyState: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: Spacing.xl,
  },
  emptyGlyph: {
    color: Colors.textSecondary,
    fontSize: 48,
    marginBottom: Spacing.lg,
    opacity: 0.5,
  },
  emptyTitle: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 17,
  },
  emptyBody: {
    marginTop: Spacing.sm,
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 13,
    textAlign: "center",
  },
  emptyAction: {
    marginTop: Spacing.xl,
    paddingHorizontal: Spacing.xl,
    paddingVertical: Spacing.md,
    borderRadius: Radii.pill,
    backgroundColor: Colors.accent,
  },
  emptyActionPressed: {
    opacity: 0.8,
  },
  emptyActionText: {
    color: Colors.bgPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 14,
  },
  sheetRoot: {
    flex: 1,
    justifyContent: "flex-end",
  },
  sheetBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: "rgba(0,0,0,0.58)",
  },
  sheetCard: {
    marginHorizontal: Spacing.md,
    marginBottom: Spacing.md,
    padding: Spacing.lg,
    borderRadius: Radii.lg,
    backgroundColor: Colors.bgSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: Colors.bgElevated,
  },
  sheetHandle: {
    alignSelf: "center",
    width: 40,
    height: 4,
    borderRadius: 2,
    backgroundColor: Colors.bgElevated,
    marginBottom: Spacing.md,
  },
  sheetTitle: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 17,
  },
  fieldLabel: {
    marginTop: Spacing.lg,
    marginBottom: Spacing.sm,
    color: Colors.textSecondary,
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
    paddingHorizontal: Spacing.md,
    paddingVertical: Spacing.sm,
    borderRadius: Radii.pill,
    backgroundColor: Colors.bgElevated,
  },
  serverChipSelected: {
    backgroundColor: Colors.accent,
  },
  serverChipText: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFont,
    fontSize: 13,
  },
  serverChipTextSelected: {
    color: Colors.bgPrimary,
    fontFamily: Typography.uiFontMedium,
  },
  input: {
    paddingHorizontal: Spacing.md,
    paddingVertical: Spacing.md,
    borderRadius: Radii.md,
    backgroundColor: Colors.bgElevated,
    color: Colors.textPrimary,
    fontFamily: Typography.uiFont,
    fontSize: 15,
  },
  sheetActions: {
    flexDirection: "row",
    justifyContent: "flex-end",
    gap: Spacing.sm,
    marginTop: Spacing.xl,
  },
  sheetButton: {
    paddingHorizontal: Spacing.lg,
    paddingVertical: Spacing.md,
    borderRadius: Radii.md,
  },
  sheetButtonPressed: {
    opacity: 0.8,
  },
  sheetButtonSecondary: {
    backgroundColor: Colors.bgElevated,
  },
  sheetButtonPrimary: {
    backgroundColor: Colors.accent,
  },
  sheetButtonDisabled: {
    opacity: 0.5,
  },
  sheetButtonText: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 14,
  },
  sheetButtonTextPrimary: {
    color: Colors.bgPrimary,
  },
});
