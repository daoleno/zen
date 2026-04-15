import React, { useEffect, useMemo, useState } from "react";
import {
  Alert,
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Colors, Typography } from "../../constants/tokens";
import type { Project } from "../../store/tasks";
import { wsClient } from "../../services/websocket";
import { DirectoryPicker } from "../terminal/DirectoryPicker";

export type ProjectServerOption = {
  id: string;
  name: string;
};

interface ProjectEditorSheetProps {
  visible: boolean;
  project?: Project | null;
  serverOptions: ProjectServerOption[];
  initialServerId?: string | null;
  projectIssueCount?: number;
  onClose: () => void;
}

type DirectoryTarget = "repo" | "worktree" | null;

export function ProjectEditorSheet({
  visible,
  project,
  serverOptions,
  initialServerId,
  projectIssueCount = 0,
  onClose,
}: ProjectEditorSheetProps) {
  const insets = useSafeAreaInsets();
  const [serverId, setServerId] = useState("");
  const [name, setName] = useState("");
  const [repoRoot, setRepoRoot] = useState("");
  const [worktreeRoot, setWorktreeRoot] = useState("");
  const [baseBranch, setBaseBranch] = useState("");
  const [busy, setBusy] = useState(false);
  const [directoryTarget, setDirectoryTarget] = useState<DirectoryTarget>(null);

  const isEditing = !!project;
  const resolvedServerId = project?.serverId || serverId;

  const serverName = useMemo(() => {
    return (
      serverOptions.find((option) => option.id === resolvedServerId)?.name || ""
    );
  }, [resolvedServerId, serverOptions]);

  useEffect(() => {
    if (!visible) {
      return;
    }

    setServerId(project?.serverId || initialServerId || serverOptions[0]?.id || "");
    setName(project?.name || "");
    setRepoRoot(project?.repoRoot || "");
    setWorktreeRoot(project?.worktreeRoot || "");
    setBaseBranch(project?.baseBranch || "");
    setBusy(false);
    setDirectoryTarget(null);
  }, [initialServerId, project, serverOptions, visible]);

  const handleSave = async () => {
    const trimmedName = name.trim();
    if (!trimmedName) {
      Alert.alert("Project name required", "Add a project name before saving.");
      return;
    }

    if (!resolvedServerId) {
      Alert.alert("No server selected", "Pick a connected server first.");
      return;
    }

    setBusy(true);
    try {
      if (project) {
        await wsClient.updateProject(project.serverId, {
          projectId: project.id,
          name: trimmedName,
          repoRoot: repoRoot.trim(),
          worktreeRoot: worktreeRoot.trim(),
          baseBranch: baseBranch.trim(),
        });
      } else {
        await wsClient.createProject(resolvedServerId, {
          name: trimmedName,
          repoRoot: repoRoot.trim(),
          worktreeRoot: worktreeRoot.trim(),
          baseBranch: baseBranch.trim(),
        });
      }
      onClose();
    } catch (error: any) {
      Alert.alert(
        project ? "Could not save project" : "Could not create project",
        error?.message || "Try again.",
      );
    } finally {
      setBusy(false);
    }
  };

  const handleDelete = () => {
    if (!project || busy) {
      return;
    }

    const detail =
      projectIssueCount > 0
        ? `This will remove ${project.name} from ${projectIssueCount} issue${projectIssueCount === 1 ? "" : "s"}.`
        : `Delete ${project.name}?`;

    Alert.alert("Delete project?", detail, [
      { text: "Cancel", style: "cancel" },
      {
        text: "Delete",
        style: "destructive",
        onPress: () => {
          void (async () => {
            setBusy(true);
            try {
              await wsClient.deleteProject(project.serverId, project.id);
              onClose();
            } catch (error: any) {
              Alert.alert(
                "Could not delete project",
                error?.message || "Try again.",
              );
            } finally {
              setBusy(false);
            }
          })();
        },
      },
    ]);
  };

  return (
    <>
      <Modal
        visible={visible}
        transparent
        animationType="slide"
        onRequestClose={onClose}
      >
        <View style={styles.root}>
          <Pressable style={styles.backdrop} onPress={busy ? undefined : onClose} />

          <View style={[styles.sheet, { paddingBottom: insets.bottom + 18 }]}>
            <View style={styles.handle} />

            <View style={styles.header}>
              <Text style={styles.title}>
                {project ? "Edit project" : "New project"}
              </Text>
              <TouchableOpacity
                style={styles.closeButton}
                onPress={onClose}
                disabled={busy}
                activeOpacity={0.82}
              >
                <Ionicons name="close" size={16} color={Colors.textPrimary} />
              </TouchableOpacity>
            </View>

            <ScrollView
              showsVerticalScrollIndicator={false}
              contentContainerStyle={styles.content}
              keyboardShouldPersistTaps="handled"
            >
              {!project && serverOptions.length > 1 ? (
                <View style={styles.group}>
                  <Text style={styles.label}>Server</Text>
                  <View style={styles.serverRow}>
                    {serverOptions.map((option) => {
                      const active = option.id === resolvedServerId;
                      return (
                        <TouchableOpacity
                          key={option.id}
                          style={[
                            styles.serverChip,
                            active && styles.serverChipActive,
                          ]}
                          onPress={() => setServerId(option.id)}
                          disabled={busy}
                          activeOpacity={0.82}
                        >
                          <Text
                            style={[
                              styles.serverChipText,
                              active && styles.serverChipTextActive,
                            ]}
                          >
                            {option.name}
                          </Text>
                        </TouchableOpacity>
                      );
                    })}
                  </View>
                </View>
              ) : serverName ? (
                <View style={styles.group}>
                  <Text style={styles.label}>Server</Text>
                  <Text style={styles.serverValue}>{serverName}</Text>
                </View>
              ) : null}

              <View style={styles.group}>
                <Text style={styles.label}>Project name</Text>
                <TextInput
                  value={name}
                  onChangeText={setName}
                  placeholder="Project name"
                  placeholderTextColor={Colors.textSecondary}
                  style={styles.textInput}
                  autoCapitalize="words"
                  editable={!busy}
                />
              </View>

              <View style={styles.group}>
                <Text style={styles.label}>Repo root</Text>
                <DirectoryField
                  value={repoRoot}
                  placeholder="Pick the repository root"
                  onPress={() => setDirectoryTarget("repo")}
                />
              </View>

              <View style={styles.group}>
                <Text style={styles.label}>Worktree root</Text>
                <DirectoryField
                  value={worktreeRoot}
                  placeholder="Optional. Defaults beside the repo"
                  onPress={() => setDirectoryTarget("worktree")}
                />
              </View>

              <View style={styles.group}>
                <Text style={styles.label}>Base branch</Text>
                <TextInput
                  value={baseBranch}
                  onChangeText={setBaseBranch}
                  placeholder="Optional. Auto-detect if left empty"
                  placeholderTextColor={Colors.textSecondary}
                  style={styles.textInput}
                  autoCapitalize="none"
                  autoCorrect={false}
                  editable={!busy}
                />
              </View>

              <Text style={styles.hint}>
                {repoRoot
                  ? "Assign will create issue worktrees from this repo context."
                  : "Add a repo root to make project assignment executable."}
              </Text>

              <View style={styles.actions}>
                <TouchableOpacity
                  style={[
                    styles.primaryButton,
                    busy && styles.primaryButtonDisabled,
                  ]}
                  onPress={() => {
                    void handleSave();
                  }}
                  disabled={busy}
                  activeOpacity={0.82}
                >
                  <Text style={styles.primaryButtonText}>
                    {busy
                      ? "Saving..."
                      : project
                        ? "Save project"
                        : "Create project"}
                  </Text>
                </TouchableOpacity>

                {project ? (
                  <TouchableOpacity
                    style={styles.deleteButton}
                    onPress={handleDelete}
                    disabled={busy}
                    activeOpacity={0.82}
                  >
                    <Text style={styles.deleteButtonText}>Delete project</Text>
                  </TouchableOpacity>
                ) : null}
              </View>
            </ScrollView>
          </View>
        </View>
      </Modal>

      <DirectoryPicker
        visible={directoryTarget !== null}
        serverId={resolvedServerId}
        initialPath={directoryTarget === "repo" ? repoRoot : worktreeRoot}
        onClose={() => setDirectoryTarget(null)}
        onSelect={(path) => {
          if (directoryTarget === "repo") {
            setRepoRoot(path);
          } else if (directoryTarget === "worktree") {
            setWorktreeRoot(path);
          }
          setDirectoryTarget(null);
        }}
      />
    </>
  );
}

function DirectoryField({
  value,
  placeholder,
  onPress,
}: {
  value: string;
  placeholder: string;
  onPress: () => void;
}) {
  return (
    <TouchableOpacity
      style={styles.directoryField}
      onPress={onPress}
      activeOpacity={0.82}
    >
      <Text
        style={[
          styles.directoryValue,
          !value && styles.directoryPlaceholder,
        ]}
        numberOfLines={2}
      >
        {value || placeholder}
      </Text>
      <Ionicons
        name="folder-open-outline"
        size={16}
        color={Colors.textSecondary}
      />
    </TouchableOpacity>
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
    maxHeight: "88%",
    borderTopLeftRadius: 30,
    borderTopRightRadius: 30,
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
    gap: 12,
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
    paddingHorizontal: 16,
    gap: 14,
  },
  group: {
    gap: 8,
  },
  label: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  serverRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 10,
  },
  serverChip: {
    minHeight: 34,
    paddingHorizontal: 12,
    borderRadius: 17,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  serverChipActive: {
    backgroundColor: Colors.accent,
    borderColor: Colors.accent,
  },
  serverChipText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  serverChipTextActive: {
    color: Colors.bgPrimary,
  },
  serverValue: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  textInput: {
    minHeight: 46,
    paddingHorizontal: 14,
    borderRadius: 16,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  directoryField: {
    minHeight: 54,
    paddingHorizontal: 14,
    borderRadius: 16,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  directoryValue: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
  },
  directoryPlaceholder: {
    color: Colors.textSecondary,
  },
  hint: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
  },
  actions: {
    gap: 10,
    paddingBottom: 2,
  },
  primaryButton: {
    minHeight: 46,
    borderRadius: 16,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: Colors.accent,
  },
  primaryButtonDisabled: {
    opacity: 0.45,
  },
  primaryButtonText: {
    color: Colors.bgPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  deleteButton: {
    minHeight: 40,
    borderRadius: 14,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,82,82,0.1)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,82,82,0.22)",
  },
  deleteButtonText: {
    color: Colors.statusFailed,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
});
