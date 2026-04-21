import React, { useEffect, useMemo, useState } from "react";
import { Alert, Pressable, StyleSheet, Text, View } from "react-native";
import { useLocalSearchParams, useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { Colors, Typography } from "../../constants/tokens";
import { useIssues, type Issue } from "../../store/issues";
import { useAgents } from "../../store/agents";
import { MarkdownEditor } from "../../components/issue/MarkdownEditor";
import type { MentionCandidate } from "../../components/issue/MentionPicker";
import { wsClient } from "../../services/websocket";

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
  const { state } = useIssues();
  const { state: agentsState } = useAgents();

  const issueId = typeof params.id === "string" ? params.id : "";
  const serverId = typeof params.serverId === "string" ? params.serverId : "";
  const issue = state.byKey[issueKey(serverId, issueId)] as Issue | undefined;

  const [draftBody, setDraftBody] = useState(issue?.body ?? "");
  const [baseMtime, setBaseMtime] = useState(issue?.mtime ?? "");
  const [dirty, setDirty] = useState(false);
  const [remoteBanner, setRemoteBanner] = useState(false);
  const [saving, setSaving] = useState(false);

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
    const roles = (state.executorsByServer[issue.serverId] || []).map<MentionCandidate>((name) => ({
      kind: "role",
      name,
    }));
    const sessions = agentsState.agents
      .filter((agent) => agent.serverId === issue.serverId && agent.project === issue.project)
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

    setSaving(true);
    try {
      const written = await wsClient.writeIssue(serverId, {
        id: issue.id,
        project: issue.project,
        path: issue.path,
        body: draftBody,
        frontmatter,
        baseMtime,
      });
      setDraftBody(written.body);
      setBaseMtime(written.mtime);
      setDirty(false);
      setRemoteBanner(false);
      return written;
    } catch (error: any) {
      if (error?.code === "conflict" && error?.current) {
        setRemoteBanner(true);
        setBaseMtime(error.current.mtime || baseMtime);
      }
      Alert.alert("Save failed", error?.message || "Could not save issue.");
      return null;
    } finally {
      setSaving(false);
    }
  };

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
      Alert.alert("Redispatch failed", error?.message || "Could not redispatch issue.");
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
              Alert.alert("Delete failed", error?.message || "Could not delete issue.");
            }
          })();
        },
      },
    ]);
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

  return (
    <SafeAreaView style={styles.screen} edges={["top"]}>
      <View style={styles.header}>
        <Pressable onPress={() => router.back()} style={styles.headerButton}>
          <Text style={styles.headerButtonText}>Back</Text>
        </Pressable>
        <View style={styles.headerCenter}>
          <Text style={styles.project}>{issue.project}</Text>
          <Text style={styles.server}>{issue.serverName}</Text>
        </View>
        <Pressable onPress={handleDelete} style={styles.headerButton}>
          <Text style={styles.headerButtonText}>Delete</Text>
        </Pressable>
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
          <Text style={styles.bannerText}>Remote changes detected. Tap to load them.</Text>
        </Pressable>
      ) : null}

      <MarkdownEditor
        value={draftBody}
        onChange={(next) => {
          setDraftBody(next);
          setDirty(true);
        }}
        candidates={candidates}
        autoFocus
      />

      <View style={styles.footer}>
        <Pressable
          onPress={() => {
            void handleToggleDone();
          }}
          style={[styles.footerButton, styles.secondaryButton]}
        >
          <Text style={styles.footerButtonText}>{done ? "Reopen" : "Mark done"}</Text>
        </Pressable>
        <Pressable
          onPress={() => {
            void saveIssue();
          }}
          disabled={saving}
          style={[styles.footerButton, styles.secondaryButton, saving && styles.buttonDisabled]}
        >
          <Text style={styles.footerButtonText}>{saving ? "Saving..." : "Save"}</Text>
        </Pressable>
        <Pressable
          onPress={() => {
            void (dispatched ? handleRedispatch() : handleSend());
          }}
          disabled={saving}
          style={[styles.footerButton, styles.primaryButton, saving && styles.buttonDisabled]}
        >
          <Text style={[styles.footerButtonText, styles.primaryButtonText]}>
            {dispatched ? "Redispatch" : "Send"}
          </Text>
        </Pressable>
      </View>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  screen: {
    flex: 1,
    backgroundColor: Colors.bgPrimary,
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
    fontSize: 18,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 14,
    paddingBottom: 10,
  },
  headerButton: {
    minWidth: 56,
    paddingVertical: 8,
  },
  headerButtonText: {
    color: Colors.accent,
    fontFamily: Typography.uiFontMedium,
    fontSize: 14,
  },
  headerCenter: {
    flex: 1,
    alignItems: "center",
  },
  project: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 16,
  },
  server: {
    marginTop: 2,
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
  },
  banner: {
    marginHorizontal: 14,
    marginBottom: 8,
    borderRadius: 12,
    backgroundColor: Colors.bgElevated,
    paddingHorizontal: 14,
    paddingVertical: 10,
  },
  bannerText: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFont,
    fontSize: 13,
    textAlign: "center",
  },
  footer: {
    flexDirection: "row",
    justifyContent: "flex-end",
    gap: 10,
    paddingHorizontal: 14,
    paddingVertical: 12,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: Colors.bgElevated,
  },
  footerButton: {
    borderRadius: 12,
    paddingHorizontal: 16,
    paddingVertical: 11,
  },
  secondaryButton: {
    backgroundColor: Colors.bgSurface,
  },
  primaryButton: {
    backgroundColor: Colors.accent,
  },
  buttonDisabled: {
    opacity: 0.5,
  },
  footerButtonText: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 14,
  },
  primaryButtonText: {
    color: Colors.bgPrimary,
  },
});
