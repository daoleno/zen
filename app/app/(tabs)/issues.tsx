import React, { useEffect, useMemo, useState } from "react";
import {
  Alert,
  Modal,
  Pressable,
  SectionList,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { Colors, Typography } from "../../constants/tokens";
import { useIssues } from "../../store/issues";
import { useAgents } from "../../store/agents";
import { IssueRow } from "../../components/issue/IssueRow";
import { getServers, type StoredServer } from "../../services/storage";
import { wsClient } from "../../services/websocket";

type IssueSection = {
  key: string;
  title: string;
  data: string[];
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
      servers.filter((server) => agentsState.serverConnections[server.id] === "connected"),
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

  const sections = useMemo(() => {
    const nextSections: IssueSection[] = [];
    const projectEntries = Object.entries(state.byProject).sort(([left], [right]) =>
      left.localeCompare(right),
    );

    for (const [projectKey, keys] of projectEntries) {
      if (keys.length === 0) {
        continue;
      }
      const first = state.byKey[keys[0]];
      if (!first) {
        continue;
      }

      const active = keys.filter((key) => !state.byKey[key]?.frontmatter.done);
      const done = keys.filter((key) => !!state.byKey[key]?.frontmatter.done);
      if (active.length > 0) {
        nextSections.push({
          key: `${projectKey}:active`,
          title: `${first.serverName} · ${first.project} · Active`,
          data: active,
        });
      }
      if (done.length > 0) {
        nextSections.push({
          key: `${projectKey}:done`,
          title: `${first.serverName} · ${first.project} · Done`,
          data: done,
        });
      }
    }

    return nextSections;
  }, [state.byKey, state.byProject]);

  const createIssue = async () => {
    if (!selectedServerId) {
      Alert.alert("No daemon", "Connect to a daemon before creating an issue.");
      return;
    }

    setSubmitting(true);
    try {
      const issue = await wsClient.writeIssue(selectedServerId, {
        project: projectDraft.trim() || "inbox",
        body: "# New issue\n\n",
        frontmatter: {},
      });
      setCreating(false);
      router.push({
        pathname: "/issue/[id]",
        params: { id: issue.id, serverId: selectedServerId },
      });
    } catch (error: any) {
      Alert.alert("Create failed", error?.message || "Could not create issue.");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <SafeAreaView style={styles.screen} edges={["top"]}>
      <View style={styles.header}>
        <Text style={styles.title}>Issues</Text>
        <Pressable onPress={() => setCreating(true)} style={styles.addButton}>
          <Text style={styles.addButtonText}>+</Text>
        </Pressable>
      </View>

      <SectionList
        sections={sections}
        keyExtractor={(item) => item}
        renderItem={({ item }) => {
          const issue = state.byKey[item];
          return issue ? <IssueRow issue={issue} /> : null;
        }}
        renderSectionHeader={({ section }) => (
          <Text style={styles.sectionHeader}>{section.title}</Text>
        )}
        stickySectionHeadersEnabled={false}
        ListEmptyComponent={
          <View style={styles.emptyState}>
            <Text style={styles.emptyTitle}>No issues yet</Text>
            <Text style={styles.emptyBody}>
              Create the first Markdown issue and dispatch it from the editor.
            </Text>
          </View>
        }
      />

      <Modal visible={creating} animationType="fade" transparent>
        <View style={styles.modalBackdrop}>
          <View style={styles.modalCard}>
            <Text style={styles.modalTitle}>New issue</Text>

            <Text style={styles.fieldLabel}>Daemon</Text>
            <View style={styles.serverList}>
              {connectedServers.length === 0 ? (
                <Text style={styles.emptyServerCopy}>No connected daemons.</Text>
              ) : (
                connectedServers.map((server) => {
                  const selected = server.id === selectedServerId;
                  return (
                    <Pressable
                      key={server.id}
                      onPress={() => setSelectedServerId(server.id)}
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
                })
              )}
            </View>

            <Text style={styles.fieldLabel}>Project</Text>
            <TextInput
              value={projectDraft}
              onChangeText={setProjectDraft}
              placeholder="inbox"
              placeholderTextColor={Colors.textSecondary}
              style={styles.input}
              autoCapitalize="none"
              autoCorrect={false}
            />

            <View style={styles.modalActions}>
              <Pressable
                onPress={() => setCreating(false)}
                style={[styles.modalButton, styles.modalButtonSecondary]}
              >
                <Text style={styles.modalButtonText}>Cancel</Text>
              </Pressable>
              <Pressable
                onPress={() => {
                  void createIssue();
                }}
                disabled={submitting || connectedServers.length === 0}
                style={[
                  styles.modalButton,
                  styles.modalButtonPrimary,
                  (submitting || connectedServers.length === 0) && styles.modalButtonDisabled,
                ]}
              >
                <Text style={[styles.modalButtonText, styles.modalButtonTextPrimary]}>
                  {submitting ? "Creating..." : "Create"}
                </Text>
              </Pressable>
            </View>
          </View>
        </View>
      </Modal>
    </SafeAreaView>
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
    paddingHorizontal: 18,
    paddingBottom: 10,
  },
  title: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 24,
  },
  addButton: {
    width: 34,
    height: 34,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 17,
    backgroundColor: Colors.bgSurface,
  },
  addButtonText: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 22,
    lineHeight: 22,
  },
  sectionHeader: {
    paddingHorizontal: 18,
    paddingTop: 18,
    paddingBottom: 8,
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
    textTransform: "uppercase",
  },
  emptyState: {
    paddingHorizontal: 24,
    paddingTop: 96,
    alignItems: "center",
  },
  emptyTitle: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 20,
  },
  emptyBody: {
    marginTop: 10,
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 14,
    textAlign: "center",
  },
  modalBackdrop: {
    flex: 1,
    padding: 20,
    justifyContent: "center",
    backgroundColor: "rgba(0,0,0,0.58)",
  },
  modalCard: {
    borderRadius: 18,
    backgroundColor: Colors.bgSurface,
    padding: 18,
  },
  modalTitle: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 18,
  },
  fieldLabel: {
    marginTop: 16,
    marginBottom: 8,
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
    textTransform: "uppercase",
  },
  serverList: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 8,
  },
  emptyServerCopy: {
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 13,
  },
  serverChip: {
    paddingHorizontal: 12,
    paddingVertical: 8,
    borderRadius: 14,
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
  },
  input: {
    borderRadius: 12,
    paddingHorizontal: 14,
    paddingVertical: 12,
    backgroundColor: Colors.bgElevated,
    color: Colors.textPrimary,
    fontFamily: Typography.uiFont,
    fontSize: 15,
  },
  modalActions: {
    flexDirection: "row",
    justifyContent: "flex-end",
    gap: 10,
    marginTop: 22,
  },
  modalButton: {
    borderRadius: 12,
    paddingHorizontal: 16,
    paddingVertical: 10,
  },
  modalButtonSecondary: {
    backgroundColor: Colors.bgElevated,
  },
  modalButtonPrimary: {
    backgroundColor: Colors.accent,
  },
  modalButtonDisabled: {
    opacity: 0.5,
  },
  modalButtonText: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 14,
  },
  modalButtonTextPrimary: {
    color: Colors.bgPrimary,
  },
});
