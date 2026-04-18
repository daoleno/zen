import React, { useEffect, useMemo, useState } from "react";
import {
  Alert,
  Platform,
  Pressable,
  ScrollView,
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
import {
  StatusFilterBar,
  IssueFilter,
} from "../../components/issue/StatusFilterBar";
import { CreateIssueSheet } from "../../components/issue/CreateIssueSheet";
import { getServers, StoredServer } from "../../services/storage";
import { CLAUDE_CODE_COMMAND } from "../../services/agentCommands";
import { formatTaskIssueId } from "../../services/taskIdentity";
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
  const parts: string[] = [project.key];
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
  const [filter, setFilter] = useState<IssueFilter>("active");
  const [projectFilterId, setProjectFilterId] = useState<string | null>(null);
  const [createVisible, setCreateVisible] = useState(false);
  const [projectEditorVisible, setProjectEditorVisible] = useState(false);
  const [editingProject, setEditingProject] = useState<Project | null>(null);
  const [scopeOpen, setScopeOpen] = useState(false);
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
      const metaParts = [formatTaskIssueId(task)];

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


  const emptyIssueCopy = getIssueEmptyStateCopy(filter, selectedProject?.name);
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

  const openSession = (serverId: string, sessionId: string) => {
    router.push({
      pathname: "/terminal/[id]",
      params: { id: sessionId, serverId },
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
      `Delete ${formatTaskIssueId(task)} permanently?`,
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

    if (item.sessionIsLive && item.run?.agentSessionId) {
      actions.push({
        text: "Open running session",
        onPress: () => openSession(item.task.serverId, item.run!.agentSessionId!),
      });
    }

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

    Alert.alert(formatTaskIssueId(item.task), item.task.title, actions);
  };

  const handleFilterChange = (nextFilter: IssueFilter) => {
    setFilter(nextFilter);
  };

  const selectProject = (id: string | null) => {
    setProjectFilterId(id);
    if (id) {
      setFilter("all");
      const project = taskState.projects.find((p) => p.id === id);
      if (project) setSelectedServerId(project.serverId);
    }
  };

  return (
    <SafeAreaView style={styles.container} edges={["top"]}>
      {/* Header: scope-as-title on left, add on right */}
      <View style={styles.header}>
        {taskState.projects.length > 0 ? (
          <TouchableOpacity
            style={styles.scopeTitle}
            onPress={() => setScopeOpen((v) => !v)}
            activeOpacity={0.75}
          >
            <Text style={styles.scopeTitleText} numberOfLines={1}>
              {selectedProject ? selectedProject.name : "All Issues"}
            </Text>
            <Ionicons
              name={scopeOpen ? "chevron-up" : "chevron-down"}
              size={13}
              color={Colors.textSecondary}
            />
          </TouchableOpacity>
        ) : (
          <Text style={styles.scopeTitleText}>All Issues</Text>
        )}

        <View style={styles.headerSpacer} />

        <TouchableOpacity
          style={styles.headerBtn}
          onPress={openCreateSheet}
          activeOpacity={0.82}
        >
          <Ionicons name="add" size={22} color={Colors.textPrimary} />
        </TouchableOpacity>
      </View>

      {/* Inline scope dropdown — overlays content below header */}
      {scopeOpen ? (
        <>
          <Pressable
            style={styles.scopeDropdownBackdrop}
            onPress={() => setScopeOpen(false)}
          />
          <View style={styles.scopeDropdown}>
            <TouchableOpacity
              style={styles.scopeDropdownRow}
              onPress={() => { selectProject(null); setScopeOpen(false); }}
              activeOpacity={0.82}
            >
              <Text style={[styles.scopeDropdownLabel, !projectFilterId && styles.scopeDropdownLabelActive]}>
                All issues
              </Text>
              {!projectFilterId ? (
                <Ionicons name="checkmark" size={14} color={Colors.accent} />
              ) : null}
            </TouchableOpacity>

            {taskState.projects.length > 0 ? (
              <View style={styles.scopeDropdownDivider} />
            ) : null}

            {taskState.projects.map((project) => {
              const active = projectFilterId === project.id;
              return (
                <TouchableOpacity
                  key={project.id}
                  style={styles.scopeDropdownRow}
                  onPress={() => { selectProject(project.id); setScopeOpen(false); }}
                  onLongPress={() => {
                    setScopeOpen(false);
                    const item = projectItems.find((i) => i.project.id === project.id);
                    if (item) handleProjectActions(item);
                  }}
                  activeOpacity={0.82}
                >
                  <Text
                    style={[styles.scopeDropdownLabel, active && styles.scopeDropdownLabelActive]}
                    numberOfLines={1}
                  >
                    {project.name}
                  </Text>
                  {active ? (
                    <Ionicons name="checkmark" size={14} color={Colors.accent} />
                  ) : null}
                </TouchableOpacity>
              );
            })}

            <View style={styles.scopeDropdownDivider} />
            <TouchableOpacity
              style={styles.scopeDropdownRow}
              onPress={() => { setScopeOpen(false); openProjectEditor(null); }}
              activeOpacity={0.82}
            >
              <Ionicons name="add" size={14} color={Colors.textSecondary} style={{ marginRight: 8 }} />
              <Text style={styles.scopeDropdownLabel}>New project</Text>
            </TouchableOpacity>
          </View>
        </>
      ) : null}

      {/* Status filter bar */}
      <StatusFilterBar
        selected={filter}
        onSelect={handleFilterChange}
        counts={filterCounts}
      />

      {/* Content — direct child of SafeAreaView, no wrapper */}
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
          style={styles.list}
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
              onOpenSession={
                item.sessionIsLive && item.run?.agentSessionId
                  ? () => openSession(item.task.serverId, item.run!.agentSessionId!)
                  : undefined
              }
              onLongPress={() => handleIssueLongPress(item)}
            />
          )}
          ItemSeparatorComponent={() => <View style={styles.rowDivider} />}
          SectionSeparatorComponent={() =>
            showSectionHeaders ? <View style={styles.sectionSpacer} /> : null
          }
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


const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.bgPrimary,
  },
  // ── Header ───────────────────────────────────────────────────────────
  header: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 16,
    paddingTop: 4,
    paddingBottom: 4,
  },
  // Scope title doubles as page title — one element, no redundant label
  scopeTitle: {
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    paddingVertical: 6,
    flexShrink: 1,
  },
  scopeTitleText: {
    color: Colors.textPrimary,
    fontSize: 17,
    fontFamily: Typography.uiFontMedium,
    flexShrink: 1,
  },
  headerSpacer: {
    flex: 1,
  },
  headerBtn: {
    width: 32,
    height: 32,
    alignItems: "center",
    justifyContent: "center",
  },
  // ── Scope dropdown ────────────────────────────────────────────────────
  scopeDropdownBackdrop: {
    ...StyleSheet.absoluteFillObject,
    top: 46,
    zIndex: 49,
  },
  scopeDropdown: {
    position: "absolute",
    top: 46,
    left: 0,
    right: 0,
    zIndex: 50,
    backgroundColor: Colors.bgSurface,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: "rgba(255,255,255,0.07)",
    paddingVertical: 4,
  },
  scopeDropdownRow: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 18,
    minHeight: 44,
    gap: 10,
  },
  scopeDropdownLabel: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  scopeDropdownLabelActive: {
    color: Colors.accent,
  },
  scopeDropdownDivider: {
    height: StyleSheet.hairlineWidth,
    backgroundColor: "rgba(255,255,255,0.06)",
    marginHorizontal: 18,
    marginVertical: 2,
  },
  // ── Issue list ────────────────────────────────────────────────────────
  list: {
    flex: 1,
  },
  listContent: {
    paddingHorizontal: 16,
    paddingTop: 2,
    paddingBottom: 32,
  },
  sectionHeader: {
    minHeight: 22,
    marginTop: 4,
    marginBottom: 2,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 10,
  },
  sectionTitle: {
    flex: 1,
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
    textTransform: "uppercase",
    letterSpacing: 0.5,
    opacity: 0.7,
  },
  sectionCount: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.terminalFont,
    opacity: 0.7,
  },
  rowDivider: {
    height: StyleSheet.hairlineWidth,
    marginLeft: 29,
    backgroundColor: "rgba(255,255,255,0.05)",
  },
  sectionSpacer: {
    height: 6,
  },
  // ── Empty state ───────────────────────────────────────────────────────
  emptyContainer: {
    flex: 1,
    justifyContent: "center",
    alignItems: "center",
    paddingHorizontal: 32,
  },
  emptyIcon: {
    fontSize: 32,
    color: Colors.textSecondary,
    marginBottom: 14,
    opacity: 0.4,
  },
  emptyText: {
    color: Colors.textPrimary,
    fontSize: 15,
    textAlign: "center",
    fontFamily: Typography.uiFontMedium,
  },
  emptySubtext: {
    color: Colors.textSecondary,
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
    marginTop: 6,
    maxWidth: 260,
    textAlign: "center",
    opacity: 0.7,
  },
  emptyActionBtn: {
    marginTop: 20,
    paddingHorizontal: 18,
    minHeight: 36,
    borderRadius: 8,
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
