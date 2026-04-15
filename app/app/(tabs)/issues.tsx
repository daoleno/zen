import React, { useEffect, useMemo, useState } from "react";
import {
  Alert,
  FlatList,
  Platform,
  SectionList,
  StyleSheet,
  Text,
  TouchableOpacity,
  UIManager,
  View,
} from "react-native";
import { useFocusEffect, useRouter } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import { Colors, Typography } from "../../constants/tokens";
import { useTasks, Project, Run, Task } from "../../store/tasks";
import { Agent, useAgents } from "../../store/agents";
import { IssueRow } from "../../components/issue/IssueRow";
import { ProjectEditorSheet } from "../../components/issue/ProjectEditorSheet";
import { ProjectRow } from "../../components/issue/ProjectRow";
import {
  StatusFilterBar,
  IssueFilter,
} from "../../components/issue/StatusFilterBar";
import { CreateIssueSheet } from "../../components/issue/CreateIssueSheet";
import { getServers, StoredServer } from "../../services/storage";
import { CLAUDE_CODE_COMMAND } from "../../services/agentCommands";
import { wsClient } from "../../services/websocket";
import {
  getRunMoment,
  getTaskSecondaryText,
  getTaskSectionKey,
  getTaskSortRank,
  getTaskStatusPresentation,
  isActiveRunStatus,
  pickCurrentRun,
} from "../../services/taskFeed";

type BrowseMode = "issues" | "projects";
type IssueSectionKey = "active" | "backlog" | "done";

type IssueListItem = {
  key: string;
  task: Task;
  run?: Run | null;
  agent?: Agent | null;
  runCount: number;
  metaText: string;
  secondaryText: string;
  statusLabel: string;
  statusTone: string;
  sectionKey: IssueSectionKey;
  sortRank: number;
  sortTime: number;
  hasLiveSession: boolean;
  sessionIsLive: boolean;
};

type IssueListSection = {
  key: IssueSectionKey;
  title: string;
  data: IssueListItem[];
};

type ProjectListItem = {
  key: string;
  project: Project;
  metaText: string;
  issueCount: number;
  activeCount: number;
  backlogCount: number;
  doneCount: number;
  openCount: number;
};

const FILTER_SECTION_KEYS: Record<IssueFilter, IssueSectionKey[]> = {
  active: ["active"],
  backlog: ["backlog"],
  done: ["done"],
  all: ["active", "backlog", "done"],
};

const SECTION_META: Record<IssueSectionKey, { title: string }> = {
  active: { title: "Active" },
  backlog: { title: "Backlog" },
  done: { title: "Done" },
};

function getRowStatusLabel(task: Task, run?: Run | null) {
  if (!run) {
    return undefined;
  }

  const presentation = getTaskStatusPresentation(task, run);

  switch (run.status) {
    case "queued":
    case "running":
    case "blocked":
    case "failed":
      return presentation;
    case "done":
      if (task.status !== "done" && task.status !== "cancelled") {
        return presentation;
      }
      return undefined;
    default:
      return undefined;
  }
}

function getIssueEmptyStateCopy(filter: IssueFilter, projectName?: string) {
  switch (filter) {
    case "active":
      return {
        title: projectName ? `No active issues in ${projectName}` : "No active issues",
        body: projectName
          ? "Nothing is running in this project right now."
          : "Nothing is running right now.",
      };
    case "backlog":
      return {
        title: projectName ? `${projectName} backlog is empty` : "Backlog is empty",
        body: projectName
          ? "Create an issue to queue work in this project."
          : "Create an issue to queue work.",
      };
    case "done":
      return {
        title: projectName
          ? `No completed issues in ${projectName}`
          : "No completed issues",
        body: "Completed work will appear here.",
      };
    case "all":
    default:
      return {
        title: projectName ? `No issues in ${projectName}` : "No issues yet",
        body: projectName
          ? "Create the first issue in this project."
          : "Create your first issue.",
      };
  }
}

function getProjectEmptyStateCopy() {
  return {
    title: "No projects yet",
    body: "Create a project to define repo context, worktrees, and issue grouping.",
  };
}

function getPathLabel(path?: string) {
  const normalized = (path || "").trim().replace(/\/+$/, "");
  if (!normalized) {
    return "";
  }

  const parts = normalized.split("/").filter(Boolean);
  return parts[parts.length - 1] || normalized;
}

function getProjectMeta(
  project: Project,
  serverName: string,
  showServerName: boolean,
) {
  const parts: string[] = [];
  const repoLabel = getPathLabel(project.repoRoot);
  const worktreeLabel = getPathLabel(project.worktreeRoot);

  if (repoLabel) {
    parts.push(repoLabel);
  } else if (worktreeLabel) {
    parts.push(worktreeLabel);
  } else {
    parts.push("No repo root");
  }

  if (showServerName && serverName) {
    parts.push(serverName);
  }

  return parts.join(" · ");
}

export default function IssuesScreen() {
  const router = useRouter();
  const { state: taskState } = useTasks();
  const { state: agentState } = useAgents();
  const [browseMode, setBrowseMode] = useState<BrowseMode>("issues");
  const [filter, setFilter] = useState<IssueFilter>("active");
  const [projectFilterId, setProjectFilterId] = useState<string | null>(null);
  const [createVisible, setCreateVisible] = useState(false);
  const [projectEditorVisible, setProjectEditorVisible] = useState(false);
  const [editingProject, setEditingProject] = useState<Project | null>(null);
  const [servers, setServers] = useState<StoredServer[]>([]);
  const [selectedServerId, setSelectedServerId] = useState<string | null>(null);

  useEffect(() => {
    if (
      Platform.OS === "android" &&
      UIManager.setLayoutAnimationEnabledExperimental
    ) {
      UIManager.setLayoutAnimationEnabledExperimental(true);
    }
  }, []);

  useFocusEffect(
    React.useCallback(() => {
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
    }, []),
  );

  const connectedServers = useMemo(
    () =>
      servers.filter((server) => agentState.serverConnections[server.id] === "connected"),
    [servers, agentState.serverConnections],
  );

  const serverOptions = useMemo(
    () => connectedServers.map((server) => ({ id: server.id, name: server.name })),
    [connectedServers],
  );

  const serverNameById = useMemo(() => {
    return Object.fromEntries(
      servers.map((server) => [server.id, server.name] as const),
    ) as Record<string, string>;
  }, [servers]);

  const taskRunsByKey = useMemo(() => {
    const map: Record<string, Run[]> = {};

    for (const run of taskState.runs) {
      const key = `${run.serverId}:${run.taskId}`;
      if (!map[key]) {
        map[key] = [];
      }
      map[key].push(run);
    }

    for (const runs of Object.values(map)) {
      runs.sort((left, right) => getRunMoment(right) - getRunMoment(left));
    }

    return map;
  }, [taskState.runs]);

  const liveSessionByKey = useMemo(() => {
    const entries = agentState.agents.map(
      (agent) => [`${agent.serverId}:${agent.id}`, agent] as const,
    );
    return Object.fromEntries(entries) as Record<string, Agent>;
  }, [agentState.agents]);

  const hasMultipleTaskServers = useMemo(
    () => new Set(taskState.tasks.map((task) => task.serverId)).size > 1,
    [taskState.tasks],
  );

  const hasMultipleProjectServers = useMemo(
    () => new Set(taskState.projects.map((project) => project.serverId)).size > 1,
    [taskState.projects],
  );

  const issueItems = useMemo(() => {
    return taskState.tasks.map((task) => {
      const runs = taskRunsByKey[`${task.serverId}:${task.id}`] || [];
      const currentRun = pickCurrentRun(task, runs);
      const sessionKey = currentRun?.agentSessionId
        ? `${task.serverId}:${currentRun.agentSessionId}`
        : "";
      const agent = sessionKey ? liveSessionByKey[sessionKey] || null : null;
      const sectionKey = getTaskSectionKey(task, currentRun);
      const rowStatus = getRowStatusLabel(task, currentRun);
      const metaParts = [`ZEN-${task.number}`];

      if (hasMultipleTaskServers) {
        metaParts.push(task.serverName);
      }

      if (!agent && currentRun?.executionMode === "attach_existing_session") {
        metaParts.push("linked");
      }

      return {
        key: `${task.serverId}:${task.id}`,
        task,
        run: currentRun,
        agent,
        runCount: runs.length,
        metaText: metaParts.join(" · "),
        secondaryText: getTaskSecondaryText(task, currentRun, agent),
        statusLabel: rowStatus?.label || "",
        statusTone: rowStatus?.tone || Colors.textSecondary,
        sectionKey,
        sortRank: getTaskSortRank(sectionKey, task, currentRun),
        sortTime: Math.max(task.updatedAt, getRunMoment(currentRun)),
        hasLiveSession: !!currentRun?.agentSessionId,
        sessionIsLive: !!agent,
      } satisfies IssueListItem;
    });
  }, [
    hasMultipleTaskServers,
    liveSessionByKey,
    taskRunsByKey,
    taskState.tasks,
  ]);

  const selectedProject = useMemo(
    () =>
      projectFilterId
        ? taskState.projects.find((project) => project.id === projectFilterId) || null
        : null,
    [projectFilterId, taskState.projects],
  );

  useEffect(() => {
    if (projectFilterId && !selectedProject) {
      setProjectFilterId(null);
    }
  }, [projectFilterId, selectedProject]);

  const filteredIssueItems = useMemo(() => {
    if (!projectFilterId) {
      return issueItems;
    }

    return issueItems.filter((item) => item.task.projectId === projectFilterId);
  }, [issueItems, projectFilterId]);

  const projectItems = useMemo(() => {
    return taskState.projects
      .map((project) => {
        const related = issueItems.filter((item) => item.task.projectId === project.id);
        const activeCount = related.filter((item) => item.sectionKey === "active").length;
        const backlogCount = related.filter((item) => item.sectionKey === "backlog").length;
        const doneCount = related.filter((item) => item.sectionKey === "done").length;
        const issueCount = related.length;

        return {
          key: `${project.serverId}:${project.id}`,
          project,
          metaText: getProjectMeta(
            project,
            serverNameById[project.serverId] || project.serverId,
            hasMultipleProjectServers,
          ),
          issueCount,
          activeCount,
          backlogCount,
          doneCount,
          openCount: activeCount + backlogCount,
        } satisfies ProjectListItem;
      })
      .sort((left, right) => {
        if (left.openCount !== right.openCount) {
          return right.openCount - left.openCount;
        }
        if (left.issueCount !== right.issueCount) {
          return right.issueCount - left.issueCount;
        }
        return left.project.name.localeCompare(right.project.name);
      });
  }, [hasMultipleProjectServers, issueItems, serverNameById, taskState.projects]);

  const countsBySection = useMemo(() => {
    return filteredIssueItems.reduce<Record<IssueSectionKey, number>>(
      (acc, item) => {
        acc[item.sectionKey] += 1;
        return acc;
      },
      {
        active: 0,
        backlog: 0,
        done: 0,
      },
    );
  }, [filteredIssueItems]);

  const filterCounts = useMemo(
    () => ({
      active: countsBySection.active,
      backlog: countsBySection.backlog,
      done: countsBySection.done,
      all: filteredIssueItems.length,
    }),
    [countsBySection, filteredIssueItems.length],
  );

  const sections = useMemo(() => {
    const visibleKeys = FILTER_SECTION_KEYS[filter];

    return visibleKeys
      .map((sectionKey) => {
        const items = filteredIssueItems
          .filter((item) => item.sectionKey === sectionKey)
          .sort((left, right) => {
            if (left.sortRank !== right.sortRank) {
              return left.sortRank - right.sortRank;
            }

            const leftPriority = left.task.priority === 0 ? 5 : left.task.priority;
            const rightPriority =
              right.task.priority === 0 ? 5 : right.task.priority;
            if (leftPriority !== rightPriority) {
              return leftPriority - rightPriority;
            }

            return right.sortTime - left.sortTime;
          });

        return {
          key: sectionKey,
          title: SECTION_META[sectionKey].title,
          data: items,
        } satisfies IssueListSection;
      })
      .filter((section) => countsBySection[section.key] > 0);
  }, [countsBySection, filter, filteredIssueItems]);

  const totalVisibleItems = FILTER_SECTION_KEYS[filter].reduce(
    (sum, sectionKey) => sum + countsBySection[sectionKey],
    0,
  );

  const browseCounts = useMemo(
    () => ({
      issues: issueItems.length,
      projects: projectItems.length,
    }),
    [issueItems.length, projectItems.length],
  );

  const emptyIssueCopy = getIssueEmptyStateCopy(filter, selectedProject?.name);
  const emptyProjectCopy = getProjectEmptyStateCopy();
  const showSectionHeaders = filter === "all" && sections.length > 1;
  const editorIssueCount = useMemo(
    () =>
      editingProject
        ? projectItems.find((item) => item.project.id === editingProject.id)?.issueCount || 0
        : 0,
    [editingProject, projectItems],
  );

  const openIssue = (task: Task) => {
    router.push({
      pathname: "/issue/[id]",
      params: { id: task.id, serverId: task.serverId },
    });
  };

  const ensureDefaultServer = (preferredServerId?: string | null) => {
    if (connectedServers.length === 0) {
      Alert.alert("No server connected", "Connect to a daemon first.");
      return null;
    }

    const resolvedServerId =
      preferredServerId && connectedServers.some((server) => server.id === preferredServerId)
        ? preferredServerId
        : selectedServerId &&
            connectedServers.some((server) => server.id === selectedServerId)
          ? selectedServerId
          : connectedServers[0].id;

    setSelectedServerId(resolvedServerId);
    return resolvedServerId;
  };

  const openCreateSheet = () => {
    const resolvedServerId = ensureDefaultServer(selectedProject?.serverId);
    if (!resolvedServerId) {
      return;
    }
    setSelectedServerId(resolvedServerId);
    setCreateVisible(true);
  };

  const openProjectEditor = (project?: Project | null) => {
    if (!project && !ensureDefaultServer()) {
      return;
    }
    if (project) {
      setSelectedServerId(project.serverId);
    }
    setEditingProject(project || null);
    setProjectEditorVisible(true);
  };

  const handleQuickDelegate = async (item: IssueListItem) => {
    try {
      const result = await wsClient.createRun(item.task.serverId, {
        taskId: item.task.id,
        executionMode: "spawn_new_session",
        agentCmd: CLAUDE_CODE_COMMAND,
      });

      if (result.run?.agent_session_id) {
        router.push({
          pathname: "/terminal/[id]",
          params: {
            id: result.run.agent_session_id,
            serverId: item.task.serverId,
          },
        });
      }
    } catch (error: any) {
      Alert.alert(
        "Could not delegate issue",
        error?.message || "Try again from the task detail screen.",
      );
    }
  };

  const deleteIssue = async (task: Task) => {
    try {
      await wsClient.deleteTask(task.serverId, task.id);
    } catch (error: any) {
      Alert.alert(
        "Could not delete issue",
        error?.message || "Try again.",
      );
    }
  };

  const confirmDeleteIssue = (task: Task) => {
    Alert.alert(
      "Delete issue?",
      `Delete ZEN-${task.number} permanently?`,
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Delete",
          style: "destructive",
          onPress: () => {
            void deleteIssue(task);
          },
        },
      ],
    );
  };

  const deleteProject = async (item: ProjectListItem) => {
    try {
      await wsClient.deleteProject(item.project.serverId, item.project.id);
      if (projectFilterId === item.project.id) {
        setProjectFilterId(null);
      }
    } catch (error: any) {
      Alert.alert(
        "Could not delete project",
        error?.message || "Try again.",
      );
    }
  };

  const handleProjectActions = (item: ProjectListItem) => {
    Alert.alert(item.project.name, item.metaText, [
      {
        text: "Open issues",
        onPress: () => {
          setProjectFilterId(item.project.id);
          setBrowseMode("issues");
          setFilter("all");
          setSelectedServerId(item.project.serverId);
        },
      },
      {
        text: "Edit project",
        onPress: () => openProjectEditor(item.project),
      },
      {
        text: "Delete project",
        style: "destructive",
        onPress: () => {
          Alert.alert(
            "Delete project?",
            item.issueCount > 0
              ? `This will remove ${item.project.name} from ${item.issueCount} issue${item.issueCount === 1 ? "" : "s"}.`
              : `Delete ${item.project.name}?`,
            [
              { text: "Cancel", style: "cancel" },
              {
                text: "Delete",
                style: "destructive",
                onPress: () => {
                  void deleteProject(item);
                },
              },
            ],
          );
        },
      },
      { text: "Dismiss", style: "cancel" },
    ]);
  };

  const handleIssueLongPress = (item: IssueListItem) => {
    const canDelegate =
      !isActiveRunStatus(item.run?.status) &&
      item.task.status !== "done" &&
      item.task.status !== "cancelled";
    const canMarkDone =
      item.task.status === "todo" || item.task.status === "in_progress";
    const canCancel =
      item.task.status !== "done" && item.task.status !== "cancelled";

    const actions: {
      text: string;
      style?: "default" | "cancel" | "destructive";
      onPress?: () => void;
    }[] = [{ text: "Open issue", onPress: () => openIssue(item.task) }];

    if (canDelegate) {
      actions.push({
        text: "Start Claude run",
        onPress: () => {
          void handleQuickDelegate(item);
        },
      });
    }

    if (canMarkDone) {
      actions.push({
        text: "Mark done",
        onPress: () =>
          wsClient.updateTask(item.task.serverId, item.task.id, {
            status: "done",
          }),
      });
    }

    if (canCancel) {
      actions.push({
        text: "Cancel issue",
        style: "destructive",
        onPress: () =>
          wsClient.updateTask(item.task.serverId, item.task.id, {
            status: "cancelled",
          }),
      });
    }

    actions.push({
      text: "Delete issue",
      style: "destructive",
      onPress: () => confirmDeleteIssue(item.task),
    });
    actions.push({ text: "Dismiss", style: "cancel" });

    Alert.alert(`ZEN-${item.task.number}`, item.task.title, actions);
  };

  const handleFilterChange = (nextFilter: IssueFilter) => {
    setFilter(nextFilter);
  };

  return (
    <SafeAreaView style={styles.container} edges={["top"]}>
      <View style={styles.header}>
        <Text style={styles.title}>
          {browseMode === "projects" ? "Projects" : "Issues"}
        </Text>
        <TouchableOpacity
          style={styles.addBtn}
          onPress={browseMode === "projects" ? () => openProjectEditor(null) : openCreateSheet}
          activeOpacity={0.82}
        >
          <Ionicons name="add" size={18} color={Colors.textPrimary} />
        </TouchableOpacity>
      </View>

      <View style={styles.modeBar}>
        <ModeChip
          label="Issues"
          count={browseCounts.issues}
          active={browseMode === "issues"}
          onPress={() => setBrowseMode("issues")}
        />
        <ModeChip
          label="Projects"
          count={browseCounts.projects}
          active={browseMode === "projects"}
          onPress={() => setBrowseMode("projects")}
        />
      </View>

      {browseMode === "issues" ? (
        <>
          {selectedProject ? (
            <View style={styles.scopeBar}>
              <TouchableOpacity
                style={styles.scopeChip}
                onPress={() => setBrowseMode("projects")}
                activeOpacity={0.82}
              >
                <Ionicons
                  name="folder-open-outline"
                  size={14}
                  color={Colors.accent}
                />
                <Text style={styles.scopeChipText}>{selectedProject.name}</Text>
              </TouchableOpacity>

              <TouchableOpacity
                style={styles.scopeClear}
                onPress={() => setProjectFilterId(null)}
                activeOpacity={0.82}
              >
                <Ionicons name="close" size={14} color={Colors.textSecondary} />
              </TouchableOpacity>
            </View>
          ) : null}

          <StatusFilterBar
            selected={filter}
            onSelect={handleFilterChange}
            counts={filterCounts}
          />

          {totalVisibleItems === 0 ? (
            <View style={styles.emptyContainer}>
              <Text style={styles.emptyIcon}>◇</Text>
              <Text style={styles.emptyText}>{emptyIssueCopy.title}</Text>
              <Text style={styles.emptySubtext}>{emptyIssueCopy.body}</Text>
              {connectedServers.length > 0 ? (
                <TouchableOpacity
                  style={styles.emptyActionBtn}
                  onPress={openCreateSheet}
                  activeOpacity={0.82}
                >
                  <Text style={styles.emptyActionText}>New Issue</Text>
                </TouchableOpacity>
              ) : null}
            </View>
          ) : (
            <SectionList
              sections={sections}
              keyExtractor={(item) => item.key}
              stickySectionHeadersEnabled={false}
              contentContainerStyle={styles.listContent}
              renderSectionHeader={({ section }) =>
                showSectionHeaders ? (
                  <View style={styles.sectionHeader}>
                    <Text style={styles.sectionTitle}>{section.title}</Text>
                    <Text style={styles.sectionCount}>
                      {countsBySection[section.key]}
                    </Text>
                  </View>
                ) : null
              }
              renderItem={({ item }) => (
                <IssueRow
                  task={item.task}
                  run={item.run}
                  metaText={item.metaText}
                  secondaryText={item.secondaryText}
                  statusLabel={item.statusLabel}
                  statusTone={item.statusTone}
                  hasLiveSession={item.hasLiveSession}
                  sessionIsLive={item.sessionIsLive}
                  runCount={item.runCount}
                  onPress={() => openIssue(item.task)}
                  onLongPress={() => handleIssueLongPress(item)}
                />
              )}
              ItemSeparatorComponent={() => <View style={styles.rowDivider} />}
              SectionSeparatorComponent={() =>
                showSectionHeaders ? <View style={styles.sectionSpacer} /> : null
              }
            />
          )}
        </>
      ) : projectItems.length === 0 ? (
        <View style={styles.emptyContainer}>
          <Text style={styles.emptyIcon}>▣</Text>
          <Text style={styles.emptyText}>{emptyProjectCopy.title}</Text>
          <Text style={styles.emptySubtext}>{emptyProjectCopy.body}</Text>
          {connectedServers.length > 0 ? (
            <TouchableOpacity
              style={styles.emptyActionBtn}
              onPress={() => openProjectEditor(null)}
              activeOpacity={0.82}
            >
              <Text style={styles.emptyActionText}>New Project</Text>
            </TouchableOpacity>
          ) : null}
        </View>
      ) : (
        <FlatList
          data={projectItems}
          keyExtractor={(item) => item.key}
          contentContainerStyle={styles.listContent}
          renderItem={({ item }) => (
            <ProjectRow
              name={item.project.name}
              meta={item.metaText}
              issueCount={item.issueCount}
              activeCount={item.activeCount}
              backlogCount={item.backlogCount}
              doneCount={item.doneCount}
              onPress={() => {
                setProjectFilterId(item.project.id);
                setBrowseMode("issues");
                setFilter("all");
                setSelectedServerId(item.project.serverId);
              }}
              onMore={() => handleProjectActions(item)}
            />
          )}
          ItemSeparatorComponent={() => <View style={styles.projectDivider} />}
        />
      )}

      <CreateIssueSheet
        visible={createVisible}
        serverOptions={serverOptions}
        selectedServerId={selectedServerId}
        onSelectServer={setSelectedServerId}
        onClose={() => setCreateVisible(false)}
        initialProjectId={selectedProject?.id}
      />

      <ProjectEditorSheet
        visible={projectEditorVisible}
        project={editingProject}
        serverOptions={serverOptions}
        initialServerId={selectedServerId}
        projectIssueCount={editorIssueCount}
        onClose={() => {
          setProjectEditorVisible(false);
          setEditingProject(null);
        }}
      />
    </SafeAreaView>
  );
}

function ModeChip({
  label,
  count,
  active,
  onPress,
}: {
  label: string;
  count: number;
  active: boolean;
  onPress: () => void;
}) {
  return (
    <TouchableOpacity
      style={[styles.modeChip, active && styles.modeChipActive]}
      onPress={onPress}
      activeOpacity={0.82}
    >
      <Text style={[styles.modeChipText, active && styles.modeChipTextActive]}>
        {label}
      </Text>
      {count > 0 ? (
        <View style={[styles.modeCount, active && styles.modeCountActive]}>
          <Text
            style={[styles.modeCountText, active && styles.modeCountTextActive]}
          >
            {count}
          </Text>
        </View>
      ) : null}
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.bgPrimary,
  },
  header: {
    flexDirection: "row",
    justifyContent: "space-between",
    alignItems: "center",
    paddingHorizontal: 20,
    paddingTop: 8,
    paddingBottom: 12,
  },
  title: {
    color: Colors.textPrimary,
    fontSize: 22,
    lineHeight: 28,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 0.3,
  },
  addBtn: {
    width: 34,
    height: 34,
    borderRadius: 17,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    marginTop: 2,
  },
  modeBar: {
    flexDirection: "row",
    gap: 8,
    paddingHorizontal: 20,
    paddingBottom: 10,
  },
  modeChip: {
    minHeight: 36,
    paddingLeft: 13,
    paddingRight: 8,
    borderRadius: 999,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  modeChipActive: {
    backgroundColor: "rgba(91,157,255,0.12)",
    borderColor: "rgba(91,157,255,0.4)",
  },
  modeChipText: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  modeChipTextActive: {
    color: Colors.textPrimary,
  },
  modeCount: {
    minWidth: 22,
    height: 22,
    paddingHorizontal: 6,
    borderRadius: 11,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.06)",
  },
  modeCountActive: {
    backgroundColor: "rgba(91,157,255,0.18)",
  },
  modeCountText: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.terminalFont,
  },
  modeCountTextActive: {
    color: Colors.accent,
  },
  scopeBar: {
    paddingHorizontal: 20,
    paddingBottom: 10,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  scopeChip: {
    minHeight: 32,
    paddingHorizontal: 12,
    borderRadius: 16,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    backgroundColor: "rgba(91,157,255,0.12)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(91,157,255,0.32)",
  },
  scopeChipText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  scopeClear: {
    width: 28,
    height: 28,
    borderRadius: 14,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.04)",
  },
  listContent: {
    paddingHorizontal: 20,
    paddingBottom: 32,
  },
  sectionHeader: {
    minHeight: 28,
    marginTop: 4,
    marginBottom: 6,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 10,
  },
  sectionTitle: {
    flex: 1,
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
    textTransform: "uppercase",
    letterSpacing: 0.4,
  },
  sectionCount: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.terminalFont,
  },
  rowDivider: {
    height: StyleSheet.hairlineWidth,
    marginLeft: 31,
    backgroundColor: "rgba(255,255,255,0.06)",
  },
  projectDivider: {
    height: StyleSheet.hairlineWidth,
    marginLeft: 0,
    backgroundColor: "rgba(255,255,255,0.06)",
  },
  sectionSpacer: {
    height: 8,
  },
  emptyContainer: {
    flex: 1,
    justifyContent: "center",
    alignItems: "center",
    paddingHorizontal: 28,
  },
  emptyIcon: {
    fontSize: 44,
    color: Colors.textSecondary,
    marginBottom: 16,
    opacity: 0.6,
  },
  emptyText: {
    color: Colors.textPrimary,
    fontSize: 17,
    textAlign: "center",
    fontFamily: Typography.uiFontMedium,
  },
  emptySubtext: {
    color: Colors.textSecondary,
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
    marginTop: 8,
    maxWidth: 300,
    textAlign: "center",
  },
  emptyActionBtn: {
    marginTop: 22,
    paddingHorizontal: 20,
    minHeight: 40,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: Colors.accent,
  },
  emptyActionText: {
    color: Colors.bgPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
});
