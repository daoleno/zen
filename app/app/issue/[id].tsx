import React, { useEffect, useMemo, useState } from "react";
import {
  Alert,
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
  useWindowDimensions,
} from "react-native";
import { useLocalSearchParams, useRouter } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import {
  SafeAreaView,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
import {
  Colors,
  Typography,
  issueStatusColor,
  priorityColor,
  runStatusColor,
} from "../../constants/tokens";
import type { IssuePriority, IssueStatus } from "../../constants/tokens";
import {
  AssignIssueSheet,
  type AssignAgentPreset,
} from "../../components/issue/AssignIssueSheet";
import { AttachmentStack } from "../../components/issue/AttachmentStack";
import { DueDatePicker } from "../../components/issue/DueDatePicker";
import { IssueStatusIcon } from "../../components/issue/IssueStatusIcon";
import { PriorityPicker } from "../../components/issue/PriorityPicker";
import { DirectoryPicker } from "../../components/terminal/DirectoryPicker";
import {
  formatDueDateLong,
  formatDueDateShort,
  getDueDateState,
} from "../../services/dueDate";
import {
  SUPPORTED_AGENT_TARGETS,
  findSupportedAgentMention,
  stripSupportedAgentMentions,
} from "../../services/agentCommands";
import {
  RUN_STATUS_LABEL,
  TASK_STATUS_LABEL,
  getTaskSecondaryText,
  getTaskStatusPresentation,
  pickCurrentRun,
} from "../../services/taskFeed";
import {
  deriveProjectIssuePrefix,
  formatTaskIssueId,
  sanitizeIssuePrefixInput,
} from "../../services/taskIdentity";
import { uploadDocumentForServer } from "../../services/uploads";
import { wsClient } from "../../services/websocket";
import { useAgents } from "../../store/agents";
import type { Agent } from "../../store/agents";
import { useTasks } from "../../store/tasks";
import type {
  Attachment,
  Project,
  Run,
  Task,
  TaskComment,
} from "../../store/tasks";

const STATUS_OPTIONS: { key: IssueStatus; label: string }[] = [
  { key: "backlog", label: "Backlog" },
  { key: "todo", label: "Todo" },
  { key: "in_progress", label: "In Progress" },
  { key: "done", label: "Done" },
  { key: "cancelled", label: "Cancelled" },
];

const PRIORITY_LABEL: Record<number, string> = {
  0: "No priority",
  1: "Urgent",
  2: "High",
  3: "Medium",
  4: "Low",
};

const ASSIGN_PRESETS: AssignAgentPreset[] = SUPPORTED_AGENT_TARGETS.map(
  (target) => ({
    key: target.id,
    label: target.label,
    description: target.description,
    command: target.command,
  }),
);

const NEW_PROJECT_ID = "__new__";

type ActivityItem = {
  key: string;
  title: string;
  timestamp: number;
  tone: string;
  meta?: string;
  body?: string;
  sessionId?: string;
  sessionLabel?: string;
  sessionLive?: boolean;
};

type CommentNode = TaskComment & {
  replies: CommentNode[];
};

type ComposerSelection = {
  start: number;
  end: number;
};

type MentionMatch = {
  start: number;
  end: number;
  query: string;
};

function formatTimestamp(timestamp?: number) {
  if (!timestamp) {
    return "";
  }

  return new Date(timestamp).toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

function formatAgentLabel(agent?: Agent | null) {
  if (!agent) {
    return "";
  }

  return agent.project?.trim() || agent.name || agent.id;
}

function collapseCopy(text?: string, limit = 180) {
  const normalized = (text || "").replace(/\s+/g, " ").trim();
  if (!normalized) {
    return "";
  }
  if (normalized.length <= limit) {
    return normalized;
  }
  return `${normalized.slice(0, limit - 3)}...`;
}

function getRunMoment(run?: Run | null) {
  if (!run) {
    return 0;
  }

  return run.updatedAt || run.endedAt || run.startedAt || run.createdAt || 0;
}

function getRunTitle(run: Run) {
  switch (run.status) {
    case "queued":
      return "Run queued";
    case "running":
      return "Run running";
    case "blocked":
      return "Run blocked";
    case "done":
      return "Run finished";
    case "failed":
      return "Run failed";
    case "cancelled":
      return "Run stopped";
    default:
      return "Run updated";
  }
}

function getRunBody(run: Run, liveAgent?: Agent | null) {
  const text =
    run.waitingReason ||
    run.lastError ||
    liveAgent?.summary ||
    run.summary ||
    "";
  if (text.trim()) {
    return collapseCopy(text, 220);
  }

  switch (run.status) {
    case "queued":
      return "Waiting to start.";
    case "running":
      return "Agent is actively working.";
    case "blocked":
      return "The run is waiting for input.";
    case "done":
      return "Latest execution completed.";
    case "failed":
      return "Latest execution stopped with an error.";
    case "cancelled":
      return "Execution was cancelled.";
    default:
      return "";
  }
}

function buildActivityItems(
  task: Task,
  runs: Run[],
  liveSessionById: Record<string, Agent>,
) {
  const items: ActivityItem[] = [
    {
      key: `issue-created-${task.id}`,
      title: "Issue created",
      timestamp: task.createdAt,
      tone: issueStatusColor(task.status),
      meta: formatTaskIssueId(task),
      body: collapseCopy(task.description, 220) || undefined,
    },
  ];

  for (const run of runs) {
    const liveAgent = run.agentSessionId
      ? liveSessionById[run.agentSessionId]
      : undefined;

    items.push({
      key: `run-${run.id}`,
      title: getRunTitle(run),
      timestamp: getRunMoment(run),
      tone: runStatusColor(run.status),
      meta: `Attempt ${run.attemptNumber || 1} · ${
        RUN_STATUS_LABEL[run.status] || run.status
      }`,
      body: getRunBody(run, liveAgent),
      sessionId: run.agentSessionId,
      sessionLabel: liveAgent
        ? formatAgentLabel(liveAgent)
        : run.agentSessionId,
      sessionLive: !!liveAgent,
    });
  }

  return items.sort((left, right) => right.timestamp - left.timestamp);
}

function buildCommentThreads(comments: TaskComment[]) {
  const sorted = comments
    .slice()
    .sort((left, right) => left.createdAt - right.createdAt);
  const nodesById: Record<string, CommentNode> = {};

  for (const comment of sorted) {
    nodesById[comment.id] = { ...comment, replies: [] };
  }

  const roots: CommentNode[] = [];
  for (const comment of sorted) {
    const node = nodesById[comment.id];
    if (comment.parentId && nodesById[comment.parentId]) {
      nodesById[comment.parentId].replies.push(node);
    } else {
      roots.push(node);
    }
  }

  return roots;
}

function getCommentRoutingLabel(comment: TaskComment) {
  switch (comment.deliveryMode) {
    case "spawn_new_session":
      return comment.targetLabel
        ? `Started ${comment.targetLabel}`
        : "Started new run";
    case "current_run":
      return comment.targetLabel
        ? `Sent to ${comment.targetLabel}`
        : "Sent to current session";
    case "attach_existing_session":
      return comment.targetLabel
        ? `Sent to ${comment.targetLabel}`
        : "Sent to linked session";
    default:
      return "";
  }
}

function getActiveMentionMatch(
  value: string,
  selection: ComposerSelection,
): MentionMatch | null {
  if (selection.start !== selection.end) {
    return null;
  }

  const cursor = selection.start;
  const beforeCursor = value.slice(0, cursor);
  const match = beforeCursor.match(/(^|\s)@([a-z0-9_-]*)$/i);
  if (!match) {
    return null;
  }

  return {
    start: cursor - match[2].length - 1,
    end: cursor,
    query: match[2].toLowerCase(),
  };
}

function loadProjectDraft(projects: Project[], projectId?: string) {
  const project = projectId
    ? projects.find((candidate) => candidate.id === projectId) || null
    : null;

  if (!project) {
    return {
      projectId: "",
      name: "",
      key: deriveProjectIssuePrefix(""),
      repoRoot: "",
      worktreeRoot: "",
      baseBranch: "",
    };
  }

  return {
    projectId: project.id,
    name: project.name,
    key: project.key,
    repoRoot: project.repoRoot || "",
    worktreeRoot: project.worktreeRoot || "",
    baseBranch: project.baseBranch || "",
  };
}

export default function IssueDetailScreen() {
  const { id, serverId: routeServerId } = useLocalSearchParams<{
    id: string;
    serverId?: string;
  }>();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { width } = useWindowDimensions();
  const { state: taskState } = useTasks();
  const { state: agentState } = useAgents();

  const [editVisible, setEditVisible] = useState(false);
  const [statusVisible, setStatusVisible] = useState(false);
  const [priorityVisible, setPriorityVisible] = useState(false);
  const [dueDateVisible, setDueDateVisible] = useState(false);
  const [projectVisible, setProjectVisible] = useState(false);
  const [assignVisible, setAssignVisible] = useState(false);
  const [commentVisible, setCommentVisible] = useState(false);
  const [assigning, setAssigning] = useState(false);
  const [deletingIssue, setDeletingIssue] = useState(false);
  const [projectSaving, setProjectSaving] = useState(false);
  const [commentSaving, setCommentSaving] = useState(false);
  const [uploadingCommentAttachment, setUploadingCommentAttachment] =
    useState(false);
  const [replyTargetId, setReplyTargetId] = useState("");
  const [commentDraft, setCommentDraft] = useState("");
  const [commentAttachments, setCommentAttachments] = useState<Attachment[]>(
    [],
  );
  const [commentSelection, setCommentSelection] = useState<ComposerSelection>({
    start: 0,
    end: 0,
  });
  const [projectDraftId, setProjectDraftId] = useState("");
  const [projectNameDraft, setProjectNameDraft] = useState("");
  const [projectKeyDraft, setProjectKeyDraft] = useState(
    deriveProjectIssuePrefix(""),
  );
  const [projectKeyDirty, setProjectKeyDirty] = useState(false);
  const [projectRepoRootDraft, setProjectRepoRootDraft] = useState("");
  const [projectWorktreeRootDraft, setProjectWorktreeRootDraft] = useState("");
  const [projectBaseBranchDraft, setProjectBaseBranchDraft] = useState("");
  const [directoryTarget, setDirectoryTarget] = useState<
    "repo" | "worktree" | null
  >(null);

  const task = useMemo(() => {
    return (
      taskState.tasks.find((candidate) => {
        if (candidate.id !== id) {
          return false;
        }
        if (routeServerId && candidate.serverId !== routeServerId) {
          return false;
        }
        return true;
      }) || null
    );
  }, [id, routeServerId, taskState.tasks]);

  const serverId = task?.serverId || routeServerId || "";

  useEffect(() => {
    if (!serverId) {
      return;
    }

    wsClient.listProjects(serverId);
    wsClient.listAgentSessions(serverId);
  }, [serverId]);

  const runsForTask = useMemo(() => {
    if (!task) {
      return [];
    }

    return taskState.runs
      .filter((run) => run.serverId === task.serverId && run.taskId === task.id)
      .slice()
      .sort((left, right) => getRunMoment(right) - getRunMoment(left));
  }, [task, taskState.runs]);

  const liveSessionById = useMemo(() => {
    const entries = agentState.agents
      .filter((agent) => agent.serverId === serverId)
      .map((agent) => [agent.id, agent] as const);
    return Object.fromEntries(entries) as Record<string, Agent>;
  }, [agentState.agents, serverId]);

  const currentRun = useMemo(
    () => (task ? pickCurrentRun(task, runsForTask) : null),
    [runsForTask, task],
  );

  const currentAgent = useMemo(() => {
    if (!currentRun?.agentSessionId) {
      return null;
    }
    return liveSessionById[currentRun.agentSessionId] || null;
  }, [currentRun, liveSessionById]);

  const projects = useMemo(() => {
    return taskState.projects
      .filter((project) => project.serverId === serverId)
      .slice()
      .sort((left, right) => left.name.localeCompare(right.name));
  }, [serverId, taskState.projects]);

  const project = useMemo(() => {
    if (!task?.projectId) {
      return null;
    }

    return (
      projects.find((candidate) => candidate.id === task.projectId) || null
    );
  }, [projects, task?.projectId]);

  const commentThreads = useMemo(
    () => (task ? buildCommentThreads(task.comments) : []),
    [task],
  );

  const commentsById = useMemo(() => {
    return Object.fromEntries(
      (task?.comments || []).map((comment) => [comment.id, comment]),
    ) as Record<string, TaskComment>;
  }, [task?.comments]);

  const replyTarget = replyTargetId
    ? commentsById[replyTargetId] || null
    : null;

  const activityItems = useMemo(() => {
    return task ? buildActivityItems(task, runsForTask, liveSessionById) : [];
  }, [liveSessionById, runsForTask, task]);

  const activeMention = useMemo(
    () => getActiveMentionMatch(commentDraft, commentSelection),
    [commentDraft, commentSelection],
  );

  const mentionSuggestions = useMemo(() => {
    if (!activeMention) {
      return [];
    }

    const query = activeMention.query.trim().toLowerCase();
    return SUPPORTED_AGENT_TARGETS.filter((target) => {
      if (!query) {
        return true;
      }
      return (
        target.handle.includes(query) ||
        target.label.toLowerCase().includes(query)
      );
    });
  }, [activeMention]);

  useEffect(() => {
    if (!projectVisible) {
      return;
    }

    const draft = loadProjectDraft(projects, task?.projectId);
    setProjectDraftId(draft.projectId);
    setProjectNameDraft(draft.name);
    setProjectKeyDraft(draft.key);
    setProjectKeyDirty(false);
    setProjectRepoRootDraft(draft.repoRoot);
    setProjectWorktreeRootDraft(draft.worktreeRoot);
    setProjectBaseBranchDraft(draft.baseBranch);
  }, [projectVisible, projects, task?.projectId]);

  if (!task) {
    return (
      <SafeAreaView style={styles.container}>
        <View style={styles.header}>
          <TouchableOpacity
            style={styles.iconButton}
            onPress={() => router.back()}
            activeOpacity={0.82}
          >
            <Ionicons name="arrow-back" size={18} color={Colors.textPrimary} />
          </TouchableOpacity>
          <Text style={styles.missingTitle}>Issue not found</Text>
        </View>
      </SafeAreaView>
    );
  }

  const pageMaxWidth = width >= 1024 ? 920 : width >= 768 ? 760 : undefined;
  const dueDateState = getDueDateState(task.dueDate);
  const projectReady = !!(task.cwd?.trim() || project?.repoRoot?.trim());
  const workspaceValue =
    task.cwd?.trim() ||
    project?.worktreeRoot?.trim() ||
    project?.repoRoot?.trim() ||
    "";
  const taskStatusLabel = TASK_STATUS_LABEL[task.status] || task.status;
  const issueId = formatTaskIssueId(task);
  const statusPresentation = getTaskStatusPresentation(task, currentRun);
  const secondaryCopy = getTaskSecondaryText(task, currentRun, currentAgent);
  const showExecutionBadge =
    !!currentRun && statusPresentation.label !== taskStatusLabel;
  const doneToggleLabel = task.status === "done" ? "Reopen" : "Mark done";

  const openSession = (sessionId: string) => {
    router.push({
      pathname: "/terminal/[id]",
      params: { id: sessionId, serverId: task.serverId },
    });
  };

  const handleUpdateStatus = (status: IssueStatus) => {
    wsClient.updateTask(task.serverId, task.id, { status });
    setStatusVisible(false);
  };

  const handleUpdatePriority = (priority: IssuePriority) => {
    wsClient.updateTask(task.serverId, task.id, { priority });
    setPriorityVisible(false);
  };

  const handleUpdateDueDate = (dueDate: string | null) => {
    wsClient.updateTask(task.serverId, task.id, { dueDate });
    setDueDateVisible(false);
  };

  const handleToggleDone = () => {
    wsClient.updateTask(task.serverId, task.id, {
      status: task.status === "done" ? "todo" : "done",
    });
  };

  const handleSaveIssueText = (
    title: string,
    description: string,
    attachments: Attachment[],
  ) => {
    wsClient.updateTask(task.serverId, task.id, {
      title,
      description,
      attachments,
    });
    setEditVisible(false);
  };

  const handleDeleteIssue = () => {
    if (deletingIssue) {
      return;
    }

    Alert.alert("Delete issue?", `Delete ${issueId} permanently?`, [
      { text: "Cancel", style: "cancel" },
      {
        text: "Delete",
        style: "destructive",
        onPress: () => {
          void (async () => {
            setDeletingIssue(true);
            try {
              await wsClient.deleteTask(task.serverId, task.id);
              setEditVisible(false);
              router.back();
            } catch (error: any) {
              Alert.alert(
                "Could not delete issue",
                error?.message || "Try again.",
              );
            } finally {
              setDeletingIssue(false);
            }
          })();
        },
      },
    ]);
  };

  const handleSelectExistingProject = (selectedProjectId: string) => {
    const draft = loadProjectDraft(projects, selectedProjectId);
    setProjectDraftId(draft.projectId);
    setProjectNameDraft(draft.name);
    setProjectKeyDraft(draft.key);
    setProjectKeyDirty(false);
    setProjectRepoRootDraft(draft.repoRoot);
    setProjectWorktreeRootDraft(draft.worktreeRoot);
    setProjectBaseBranchDraft(draft.baseBranch);
  };

  const handleStartNewProject = () => {
    setProjectDraftId(NEW_PROJECT_ID);
    setProjectNameDraft("");
    setProjectKeyDraft(deriveProjectIssuePrefix(""));
    setProjectKeyDirty(false);
    setProjectRepoRootDraft("");
    setProjectWorktreeRootDraft("");
    setProjectBaseBranchDraft("");
  };

  const handleClearProject = () => {
    setProjectDraftId("");
    setProjectNameDraft("");
    setProjectKeyDraft(deriveProjectIssuePrefix(""));
    setProjectKeyDirty(false);
    setProjectRepoRootDraft("");
    setProjectWorktreeRootDraft("");
    setProjectBaseBranchDraft("");
  };

  const handleProjectNameChange = (value: string) => {
    const currentDerived = deriveProjectIssuePrefix(projectNameDraft);
    const nextDerived = deriveProjectIssuePrefix(value);
    setProjectNameDraft(value);
    if (
      projectDraftId === NEW_PROJECT_ID &&
      (!projectKeyDirty || projectKeyDraft === currentDerived)
    ) {
      setProjectKeyDraft(nextDerived);
    }
  };

  const handleProjectKeyChange = (value: string) => {
    const sanitized = sanitizeIssuePrefixInput(value);
    if (sanitized) {
      setProjectKeyDraft(sanitized);
      setProjectKeyDirty(true);
      return;
    }
    setProjectKeyDraft(deriveProjectIssuePrefix(projectNameDraft));
    setProjectKeyDirty(false);
  };

  const handleSaveProject = async () => {
    if (projectSaving) {
      return;
    }

    if (!projectDraftId) {
      wsClient.updateTask(task.serverId, task.id, { projectId: "" });
      setProjectVisible(false);
      return;
    }

    const trimmedName = projectNameDraft.trim();
    if (!trimmedName) {
      Alert.alert("Project name required", "Add a project name before saving.");
      return;
    }

    setProjectSaving(true);
    try {
      let savedProject: Project | any;
      if (projectDraftId === NEW_PROJECT_ID) {
        savedProject = await wsClient.createProject(task.serverId, {
          name: trimmedName,
          issuePrefix: projectKeyDraft.trim(),
          repoRoot: projectRepoRootDraft.trim(),
          worktreeRoot: projectWorktreeRootDraft.trim(),
          baseBranch: projectBaseBranchDraft.trim(),
        });
      } else {
        savedProject = await wsClient.updateProject(task.serverId, {
          projectId: projectDraftId,
          name: trimmedName,
          repoRoot: projectRepoRootDraft.trim(),
          worktreeRoot: projectWorktreeRootDraft.trim(),
          baseBranch: projectBaseBranchDraft.trim(),
        });
      }

      wsClient.updateTask(task.serverId, task.id, {
        projectId: savedProject.id,
      });
      setProjectVisible(false);
    } catch (error: any) {
      Alert.alert("Could not save project", error?.message || "Try again.");
    } finally {
      setProjectSaving(false);
    }
  };

  const handleAssign = async (preset: AssignAgentPreset) => {
    if (assigning) {
      return;
    }

    if (!projectReady) {
      setAssignVisible(false);
      setProjectVisible(true);
      return;
    }

    setAssigning(true);
    try {
      const created = await wsClient.createRun(task.serverId, {
        taskId: task.id,
        executionMode: "spawn_new_session",
        agentCmd: preset.command,
      });

      setAssignVisible(false);

      const createdSessionId =
        created.run?.agent_session_id || created.run?.agentSessionId;
      if (createdSessionId) {
        openSession(createdSessionId);
      }
    } catch (error: any) {
      Alert.alert("Could not start run", error?.message || "Try again.");
    } finally {
      setAssigning(false);
    }
  };

  const handleOpenCommentComposer = (replyTo?: TaskComment) => {
    if (!replyTo) return;
    setReplyTargetId(replyTo.id);
    setCommentDraft("");
    setCommentAttachments([]);
    setCommentSelection({ start: 0, end: 0 });
    setCommentVisible(true);
  };

  const handleAddCommentAttachment = async () => {
    if (uploadingCommentAttachment || commentSaving) {
      return;
    }

    setUploadingCommentAttachment(true);
    try {
      const attachment = await uploadDocumentForServer(task.serverId);
      if (!attachment) {
        return;
      }

      setCommentAttachments((current) => {
        if (current.some((existing) => existing.path === attachment.path)) {
          return current;
        }
        return [...current, attachment];
      });
    } catch (error: any) {
      Alert.alert("Could not attach file", error?.message || "Try again.");
    } finally {
      setUploadingCommentAttachment(false);
    }
  };

  const handleInsertMention = (handle: string) => {
    if (!activeMention) {
      return;
    }

    const replacement = `@${handle} `;
    const nextValue =
      commentDraft.slice(0, activeMention.start) +
      replacement +
      commentDraft.slice(activeMention.end);
    const cursor = activeMention.start + replacement.length;
    setCommentDraft(nextValue);
    setCommentSelection({ start: cursor, end: cursor });
  };

  const handleSaveComment = async () => {
    if (commentSaving) {
      return;
    }

    const targetAgent = findSupportedAgentMention(commentDraft);
    const cleanedBody = targetAgent
      ? stripSupportedAgentMentions(commentDraft)
      : commentDraft.trim();

    if (!cleanedBody) {
      Alert.alert(
        "Comment required",
        targetAgent
          ? "Add a message after the mention."
          : "Add a comment before sending.",
      );
      return;
    }

    setCommentSaving(true);
    try {
      await wsClient.addTaskComment(task.serverId, {
        taskId: task.id,
        body: cleanedBody,
        attachments: commentAttachments,
        parentCommentId: replyTargetId || undefined,
        deliveryMode: targetAgent ? "spawn_new_session" : "comment",
        agentCmd: targetAgent?.command,
      });
      setCommentVisible(false);
      setReplyTargetId("");
      setCommentDraft("");
      setCommentAttachments([]);
      setCommentSelection({ start: 0, end: 0 });
    } catch (error: any) {
      Alert.alert("Could not save comment", error?.message || "Try again.");
    } finally {
      setCommentSaving(false);
    }
  };

  return (
    <SafeAreaView style={styles.container} edges={["top", "bottom"]}>
      <View style={styles.header}>
        <TouchableOpacity
          style={styles.iconButton}
          onPress={() => router.back()}
          activeOpacity={0.82}
        >
          <Ionicons name="arrow-back" size={18} color={Colors.textPrimary} />
        </TouchableOpacity>

        <View style={styles.headerCopy}>
          <Text style={styles.headerKey}>{issueId}</Text>
        </View>

        <TouchableOpacity
          style={styles.headerAction}
          onPress={() => setEditVisible(true)}
          activeOpacity={0.82}
        >
          <Text style={styles.headerActionText}>Edit</Text>
        </TouchableOpacity>
      </View>

      <ScrollView
        style={styles.scroll}
        contentContainerStyle={{
          paddingHorizontal: 20,
          paddingTop: 8,
          paddingBottom: insets.bottom + 28,
          alignItems: pageMaxWidth ? "center" : "stretch",
        }}
        showsVerticalScrollIndicator={false}
      >
        <View
          style={[
            styles.page,
            pageMaxWidth ? { maxWidth: pageMaxWidth } : null,
          ]}
        >
          <View style={styles.hero}>
            {showExecutionBadge ? (
              <View style={styles.heroMetaRow}>
                <View style={styles.heroBadge}>
                  <View
                    style={[
                      styles.heroBadgeDot,
                      { backgroundColor: statusPresentation.tone },
                    ]}
                  />
                  <Text
                    style={[
                      styles.heroBadgeText,
                      { color: statusPresentation.tone },
                    ]}
                  >
                    {statusPresentation.label}
                  </Text>
                </View>
              </View>
            ) : null}

            <Text style={styles.title}>{task.title}</Text>
            {task.description.trim() ? (
              <Text style={styles.description}>{task.description.trim()}</Text>
            ) : null}
            {task.attachments.length > 0 ? (
              <View style={styles.heroAttachmentSurface}>
                <Text style={styles.heroAttachmentLabel}>Files</Text>
                <AttachmentStack attachments={task.attachments} compact />
              </View>
            ) : null}
          </View>

          <SectionHeader title="Details" />
          <View style={styles.surface}>
            <PropertyRow
              icon="ellipse-outline"
              label="Status"
              value={TASK_STATUS_LABEL[task.status] || task.status}
              valueTone={issueStatusColor(task.status)}
              onPress={() => setStatusVisible(true)}
            />
            <Divider />
            <PropertyRow
              icon="flag-outline"
              label="Priority"
              value={PRIORITY_LABEL[task.priority]}
              valueTone={
                task.priority > 0
                  ? priorityColor(task.priority)
                  : Colors.textSecondary
              }
              onPress={() => setPriorityVisible(true)}
            />
            <Divider />
            <PropertyRow
              icon="hardware-chip-outline"
              label="Agent"
              value={
                currentAgent
                  ? formatAgentLabel(currentAgent)
                  : currentRun?.agentSessionId
                    ? "Linked session"
                    : "Assign agent"
              }
              valueTone={currentAgent ? Colors.statusRunning : undefined}
              muted={!currentAgent && !currentRun?.agentSessionId}
              onPress={() => setAssignVisible(true)}
            />
            {currentAgent ? (
              <>
                <Divider />
                <PropertyRow
                  icon="terminal-outline"
                  label="Session"
                  value="Open running session"
                  valueTone={Colors.accent}
                  onPress={() => openSession(currentAgent.id)}
                />
              </>
            ) : null}
            {workspaceValue ? (
              <>
                <Divider />
                <PropertyRow
                  icon="folder-open-outline"
                  label="Workspace"
                  value={workspaceValue}
                  onPress={() => setProjectVisible(true)}
                />
              </>
            ) : null}
            <Divider />
            <PropertyRow
              icon="archive-outline"
              label="Project"
              value={project?.name || "No project"}
              muted={!project}
              onPress={() => setProjectVisible(true)}
            />
            <Divider />
            <PropertyRow
              icon="calendar-outline"
              label="Due date"
              value={
                task.dueDate ? formatDueDateShort(task.dueDate) : "No date"
              }
              valueTone={
                dueDateState?.isOverdue ? Colors.statusFailed : undefined
              }
              muted={!task.dueDate}
              onPress={() => setDueDateVisible(true)}
            />
          </View>

          <SectionHeader title="Comments" />
          <View style={styles.surface}>
            {commentThreads.length > 0 ? (
              <View style={styles.noteList}>
                {commentThreads.map((comment, index) => (
                  <React.Fragment key={comment.id}>
                    {index > 0 ? <Divider /> : null}
                    <CommentThread
                      comment={comment}
                      onReply={handleOpenCommentComposer}
                      onOpenSession={openSession}
                    />
                  </React.Fragment>
                ))}
              </View>
            ) : null}
            <Divider />
            <View style={styles.inlineComposer}>
              <TextInput
                style={styles.inlineComposerInput}
                value={commentDraft}
                onChangeText={setCommentDraft}
                onFocus={() => {
                  setReplyTargetId("");
                  setCommentAttachments([]);
                }}
                placeholder="Add a comment..."
                placeholderTextColor="rgba(255,255,255,0.25)"
                multiline
                textAlignVertical="top"
                selectionColor={Colors.accent}
              />
              {commentDraft.trim() ? (
                <TouchableOpacity
                  style={[
                    styles.inlineComposerSend,
                    commentSaving && styles.inlineComposerSendBusy,
                  ]}
                  onPress={() => void handleSaveComment()}
                  disabled={commentSaving}
                  activeOpacity={0.82}
                >
                  <Ionicons
                    name="arrow-up"
                    size={14}
                    color={Colors.bgPrimary}
                  />
                </TouchableOpacity>
              ) : null}
            </View>
          </View>

          <SectionHeader title="Activity" />
          <View style={styles.surface}>
            {activityItems.map((item, index) => (
              <React.Fragment key={item.key}>
                {index > 0 ? <Divider /> : null}
                <ActivityRow item={item} onOpenSession={openSession} />
              </React.Fragment>
            ))}
          </View>
        </View>
      </ScrollView>

      <SelectionSheet
        visible={statusVisible}
        title="Status"
        onClose={() => setStatusVisible(false)}
        options={STATUS_OPTIONS.map((option) => ({
          key: option.key,
          label: option.label,
          tone: issueStatusColor(option.key),
          selected: option.key === task.status,
          icon: "ellipse-outline",
          onPress: () => handleUpdateStatus(option.key),
        }))}
      />

      <SheetScaffold
        visible={priorityVisible}
        title="Priority"
        onClose={() => setPriorityVisible(false)}
      >
        <View style={styles.sheetContent}>
          <PriorityPicker
            value={task.priority}
            onChange={handleUpdatePriority}
          />
        </View>
      </SheetScaffold>

      <SheetScaffold
        visible={dueDateVisible}
        title="Due date"
        onClose={() => setDueDateVisible(false)}
      >
        <View style={styles.sheetContent}>
          <Text style={styles.sheetValue}>
            {formatDueDateLong(task.dueDate)}
          </Text>
          <DueDatePicker value={task.dueDate} onChange={handleUpdateDueDate} />
        </View>
      </SheetScaffold>

      <ProjectSetupSheet
        visible={projectVisible}
        projects={projects}
        activeProjectId={task.projectId}
        draftProjectId={projectDraftId}
        projectName={projectNameDraft}
        issuePrefix={projectKeyDraft}
        repoRoot={projectRepoRootDraft}
        worktreeRoot={projectWorktreeRootDraft}
        baseBranch={projectBaseBranchDraft}
        busy={projectSaving}
        onClose={() => setProjectVisible(false)}
        onSelectExisting={handleSelectExistingProject}
        onStartNew={handleStartNewProject}
        onClearProject={handleClearProject}
        onChangeName={handleProjectNameChange}
        onChangeIssuePrefix={handleProjectKeyChange}
        onChangeBaseBranch={setProjectBaseBranchDraft}
        onOpenRepoPicker={() => setDirectoryTarget("repo")}
        onOpenWorktreePicker={() => setDirectoryTarget("worktree")}
        onSave={handleSaveProject}
      />

      <CommentComposerSheet
        visible={commentVisible}
        busy={commentSaving}
        replyTarget={replyTarget}
        value={commentDraft}
        selection={commentSelection}
        attachments={commentAttachments}
        attachmentBusy={uploadingCommentAttachment}
        mentionSuggestions={mentionSuggestions}
        onChangeText={setCommentDraft}
        onChangeSelection={setCommentSelection}
        onPickMention={handleInsertMention}
        onAddAttachment={() => {
          void handleAddCommentAttachment();
        }}
        onRemoveAttachment={(attachment) => {
          setCommentAttachments((current) =>
            current.filter((existing) => existing.path !== attachment.path),
          );
        }}
        onClose={() => {
          setCommentVisible(false);
          setReplyTargetId("");
          setCommentDraft("");
          setCommentAttachments([]);
          setCommentSelection({ start: 0, end: 0 });
        }}
        onSave={() => {
          void handleSaveComment();
        }}
      />

      <EditIssueSheet
        visible={editVisible}
        serverId={task.serverId}
        title={task.title}
        description={task.description}
        attachments={task.attachments}
        deleting={deletingIssue}
        onClose={() => setEditVisible(false)}
        onSave={handleSaveIssueText}
        onDelete={handleDeleteIssue}
      />

      <AssignIssueSheet
        visible={assignVisible}
        busy={assigning}
        presets={ASSIGN_PRESETS}
        projectName={project?.name}
        workspaceCwd={task.cwd}
        repoRoot={project?.repoRoot}
        worktreeRoot={project?.worktreeRoot}
        baseBranch={project?.baseBranch}
        currentSessionLabel={
          currentAgent ? formatAgentLabel(currentAgent) : undefined
        }
        currentSessionStatus={currentAgent?.status}
        currentSessionSubtitle={
          currentAgent
            ? collapseCopy(currentAgent.summary, 120) ||
              currentAgent.cwd?.trim() ||
              undefined
            : undefined
        }
        canAssign={projectReady}
        onClose={() => setAssignVisible(false)}
        onConfigureProject={() => {
          setAssignVisible(false);
          setProjectVisible(true);
        }}
        onOpenCurrentSession={
          currentAgent ? () => openSession(currentAgent.id) : undefined
        }
        onAssign={(preset) => {
          void handleAssign(preset);
        }}
      />

      <DirectoryPicker
        visible={directoryTarget !== null}
        serverId={task.serverId}
        initialPath={
          directoryTarget === "repo"
            ? projectRepoRootDraft
            : projectWorktreeRootDraft
        }
        onClose={() => setDirectoryTarget(null)}
        onSelect={(path) => {
          if (directoryTarget === "repo") {
            setProjectRepoRootDraft(path);
          } else if (directoryTarget === "worktree") {
            setProjectWorktreeRootDraft(path);
          }
          setDirectoryTarget(null);
        }}
      />
    </SafeAreaView>
  );
}

function SectionHeader({
  title,
  actionLabel,
  onAction,
}: {
  title: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <View style={styles.sectionHeader}>
      <Text style={styles.sectionTitle}>{title}</Text>
      {actionLabel && onAction ? (
        <TouchableOpacity onPress={onAction} activeOpacity={0.82}>
          <Text style={styles.sectionAction}>{actionLabel}</Text>
        </TouchableOpacity>
      ) : null}
    </View>
  );
}

function Divider() {
  return <View style={styles.divider} />;
}

function EmptyState({
  title,
  body,
  actionLabel,
  onAction,
}: {
  title: string;
  body: string;
  actionLabel?: string;
  onAction?: () => void;
}) {
  return (
    <View style={styles.emptyState}>
      <Text style={styles.emptyStateTitle}>{title}</Text>
      <Text style={styles.emptyStateBody}>{body}</Text>
      {actionLabel && onAction ? (
        <TouchableOpacity
          style={styles.emptyStateAction}
          onPress={onAction}
          activeOpacity={0.82}
        >
          <Text style={styles.emptyStateActionText}>{actionLabel}</Text>
        </TouchableOpacity>
      ) : null}
    </View>
  );
}

function PropertyRow({
  icon,
  label,
  value,
  onPress,
  muted = false,
  valueTone,
}: {
  icon: keyof typeof Ionicons.glyphMap;
  label: string;
  value: string;
  onPress?: () => void;
  muted?: boolean;
  valueTone?: string;
}) {
  const inner = (
    <>
      <View style={styles.propertyLabelWrap}>
        <Ionicons name={icon} size={16} color={Colors.textSecondary} />
        <Text style={styles.propertyLabel}>{label}</Text>
      </View>

      <View style={styles.propertyValueWrap}>
        <Text
          style={[
            styles.propertyValue,
            muted && styles.propertyValueMuted,
            valueTone ? { color: valueTone } : null,
          ]}
          numberOfLines={1}
        >
          {value}
        </Text>
        {onPress ? (
          <Ionicons
            name="chevron-forward"
            size={15}
            color={Colors.textSecondary}
          />
        ) : null}
      </View>
    </>
  );

  if (!onPress) {
    return <View style={styles.propertyRow}>{inner}</View>;
  }

  return (
    <TouchableOpacity
      style={styles.propertyRow}
      onPress={onPress}
      activeOpacity={0.82}
    >
      {inner}
    </TouchableOpacity>
  );
}

function DetailRow({
  icon,
  label,
  value,
  meta,
  muted = false,
  onPress,
}: {
  icon: keyof typeof Ionicons.glyphMap;
  label: string;
  value: string;
  meta?: string;
  muted?: boolean;
  onPress?: () => void;
}) {
  const content = (
    <>
      <View style={styles.detailIconWrap}>
        <Ionicons name={icon} size={15} color={Colors.textSecondary} />
      </View>
      <View style={styles.detailCopy}>
        <Text style={styles.detailLabel}>{label}</Text>
        <Text
          style={[styles.detailValue, muted && styles.propertyValueMuted]}
          numberOfLines={2}
        >
          {value}
        </Text>
        {meta ? <Text style={styles.detailMeta}>{meta}</Text> : null}
      </View>
      {onPress ? (
        <Ionicons
          name="chevron-forward"
          size={15}
          color={Colors.textSecondary}
        />
      ) : null}
    </>
  );

  if (!onPress) {
    return <View style={styles.detailRow}>{content}</View>;
  }

  return (
    <TouchableOpacity
      style={styles.detailRow}
      onPress={onPress}
      activeOpacity={0.82}
    >
      {content}
    </TouchableOpacity>
  );
}

function ActivityRow({
  item,
  onOpenSession,
}: {
  item: ActivityItem;
  onOpenSession: (sessionId: string) => void;
}) {
  return (
    <View style={styles.activityRow}>
      <View
        style={[
          styles.activityDotWrap,
          { backgroundColor: `${item.tone}18` },
        ]}
      >
        <View style={[styles.activityDot, { backgroundColor: item.tone }]} />
      </View>

      <View style={styles.activityCopy}>
        <View style={styles.activityTopRow}>
          <Text style={styles.activityTitle}>{item.title}</Text>
          <Text style={styles.activityTime}>
            {formatTimestamp(item.timestamp)}
          </Text>
        </View>
        {item.meta ? (
          <Text style={styles.activityMeta}>{item.meta}</Text>
        ) : null}
        {item.body ? (
          <Text style={styles.activityBody}>{item.body}</Text>
        ) : null}
        {item.sessionId && item.sessionLabel ? (
          item.sessionLive ? (
            <TouchableOpacity
              style={styles.inlineLink}
              onPress={() => onOpenSession(item.sessionId!)}
              activeOpacity={0.82}
            >
              <Ionicons
                name="terminal-outline"
                size={13}
                color={Colors.accent}
              />
              <Text style={styles.inlineLinkText}>{item.sessionLabel}</Text>
            </TouchableOpacity>
          ) : (
            <View style={styles.inlineLink}>
              <Ionicons
                name="link-outline"
                size={13}
                color={Colors.textSecondary}
              />
              <Text style={[styles.inlineLinkText, styles.inlineLinkMuted]}>
                {item.sessionLabel}
              </Text>
            </View>
          )
        ) : null}
      </View>
    </View>
  );
}

function CommentThread({
  comment,
  onReply,
  onOpenSession,
  depth = 0,
}: {
  comment: CommentNode;
  onReply: (comment?: TaskComment) => void;
  onOpenSession: (sessionId: string) => void;
  depth?: number;
}) {
  const routingLabel = getCommentRoutingLabel(comment);

  return (
    <View
      style={[styles.noteThread, depth > 0 ? styles.noteThreadNested : null]}
    >
      <View style={styles.noteCard}>
        <View style={styles.noteHeader}>
          <Text style={styles.noteAuthor}>{comment.authorLabel || "You"}</Text>
          <Text style={styles.noteTime}>
            {formatTimestamp(comment.createdAt)}
          </Text>
        </View>

        {routingLabel ? (
          <View style={styles.commentMetaRow}>
            <View style={styles.commentMetaPill}>
              <Text style={styles.commentMetaPillText}>{routingLabel}</Text>
            </View>
            {comment.agentSessionId ? (
              <TouchableOpacity
                style={styles.inlineLink}
                onPress={() => onOpenSession(comment.agentSessionId!)}
                activeOpacity={0.82}
              >
                <Ionicons
                  name="terminal-outline"
                  size={13}
                  color={Colors.textSecondary}
                />
                <Text style={styles.inlineLinkText}>Open session</Text>
              </TouchableOpacity>
            ) : null}
          </View>
        ) : null}

        <Text style={styles.noteBody}>{comment.body}</Text>

        {comment.attachments.length > 0 ? (
          <View style={styles.commentAttachmentBlock}>
            <AttachmentStack attachments={comment.attachments} compact />
          </View>
        ) : null}

        <TouchableOpacity
          style={styles.noteReplyButton}
          onPress={() => onReply(comment)}
          activeOpacity={0.82}
        >
          <Ionicons
            name="arrow-undo-outline"
            size={13}
            color={Colors.textSecondary}
          />
          <Text style={styles.noteReplyText}>Reply</Text>
        </TouchableOpacity>
      </View>

      {comment.replies.length > 0 ? (
        <View style={styles.noteReplyList}>
          {comment.replies.map((reply) => (
            <CommentThread
              key={reply.id}
              comment={reply}
              onReply={onReply}
              onOpenSession={onOpenSession}
              depth={depth + 1}
            />
          ))}
        </View>
      ) : null}
    </View>
  );
}

function SheetScaffold({
  visible,
  title,
  onClose,
  children,
}: {
  visible: boolean;
  title: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  const insets = useSafeAreaInsets();

  return (
    <Modal
      visible={visible}
      transparent
      animationType="slide"
      presentationStyle="overFullScreen"
      onRequestClose={onClose}
    >
      <KeyboardAvoidingView
        style={styles.sheetRoot}
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        <Pressable style={styles.sheetBackdrop} onPress={onClose} />
        <View style={[styles.sheet, { paddingBottom: insets.bottom + 16 }]}>
          <View style={styles.sheetHandle} />
          <View style={styles.sheetHeader}>
            <Text style={styles.sheetTitle}>{title}</Text>
            <TouchableOpacity
              style={styles.iconButton}
              onPress={onClose}
              activeOpacity={0.82}
            >
              <Ionicons name="close" size={16} color={Colors.textPrimary} />
            </TouchableOpacity>
          </View>
          {children}
        </View>
      </KeyboardAvoidingView>
    </Modal>
  );
}

function SelectionSheet({
  visible,
  title,
  onClose,
  options,
}: {
  visible: boolean;
  title: string;
  onClose: () => void;
  options: {
    key: string;
    label: string;
    selected?: boolean;
    tone?: string;
    icon?: keyof typeof Ionicons.glyphMap;
    onPress: () => void;
  }[];
}) {
  return (
    <SheetScaffold visible={visible} title={title} onClose={onClose}>
      <ScrollView
        style={styles.selectionScroll}
        contentContainerStyle={styles.selectionContent}
      >
        {options.map((option) => (
          <TouchableOpacity
            key={option.key}
            style={[
              styles.selectionRow,
              option.selected && styles.selectionRowActive,
            ]}
            onPress={option.onPress}
            activeOpacity={0.82}
          >
            <View style={styles.selectionLabelWrap}>
              {option.icon ? (
                <Ionicons
                  name={option.icon}
                  size={15}
                  color={option.tone || Colors.textSecondary}
                />
              ) : null}
              <Text
                style={[
                  styles.selectionLabel,
                  option.tone ? { color: option.tone } : null,
                ]}
              >
                {option.label}
              </Text>
            </View>

            {option.selected ? (
              <Ionicons name="checkmark" size={16} color={Colors.accent} />
            ) : null}
          </TouchableOpacity>
        ))}
      </ScrollView>
    </SheetScaffold>
  );
}

function ProjectSetupSheet({
  visible,
  projects,
  activeProjectId,
  draftProjectId,
  projectName,
  issuePrefix,
  repoRoot,
  worktreeRoot,
  baseBranch,
  busy,
  onClose,
  onSelectExisting,
  onStartNew,
  onClearProject,
  onChangeName,
  onChangeIssuePrefix,
  onChangeBaseBranch,
  onOpenRepoPicker,
  onOpenWorktreePicker,
  onSave,
}: {
  visible: boolean;
  projects: Project[];
  activeProjectId?: string;
  draftProjectId: string;
  projectName: string;
  issuePrefix: string;
  repoRoot: string;
  worktreeRoot: string;
  baseBranch: string;
  busy?: boolean;
  onClose: () => void;
  onSelectExisting: (projectId: string) => void;
  onStartNew: () => void;
  onClearProject: () => void;
  onChangeName: (value: string) => void;
  onChangeIssuePrefix: (value: string) => void;
  onChangeBaseBranch: (value: string) => void;
  onOpenRepoPicker: () => void;
  onOpenWorktreePicker: () => void;
  onSave: () => void;
}) {
  const isNewProject = draftProjectId === NEW_PROJECT_ID;
  const showEditor = draftProjectId !== "";
  const selectedProject = projects.find((project) => project.id === draftProjectId) || null;
  const resolvedIssuePrefix = isNewProject
    ? issuePrefix
    : selectedProject?.key || issuePrefix;
  const primaryLabel = isNewProject
    ? "Create & use"
    : draftProjectId
      ? "Save project"
      : activeProjectId
        ? "Remove project"
        : "Done";

  return (
    <SheetScaffold visible={visible} title="Project" onClose={onClose}>
      <ScrollView
        style={styles.selectionScroll}
        contentContainerStyle={styles.projectSheetContent}
        keyboardShouldPersistTaps="handled"
        keyboardDismissMode={
          Platform.OS === "ios" ? "interactive" : "on-drag"
        }
        automaticallyAdjustKeyboardInsets={Platform.OS === "ios"}
      >
        <View style={styles.projectSelector}>
          <TouchableOpacity
            style={[
              styles.projectChip,
              !draftProjectId && styles.projectChipActive,
            ]}
            onPress={onClearProject}
            activeOpacity={0.82}
          >
            <Text
              style={[
                styles.projectChipText,
                !draftProjectId && styles.projectChipTextActive,
              ]}
            >
              No project
            </Text>
          </TouchableOpacity>

          {projects.map((project) => (
            <TouchableOpacity
              key={project.id}
              style={[
                styles.projectChip,
                draftProjectId === project.id && styles.projectChipActive,
              ]}
              onPress={() => onSelectExisting(project.id)}
              activeOpacity={0.82}
            >
              <Text
                style={[
                  styles.projectChipText,
                  draftProjectId === project.id && styles.projectChipTextActive,
                ]}
              >
                {project.name}
              </Text>
            </TouchableOpacity>
          ))}

          <TouchableOpacity
            style={[
              styles.projectChip,
              isNewProject && styles.projectChipActive,
            ]}
            onPress={onStartNew}
            activeOpacity={0.82}
          >
            <Ionicons
              name="add"
              size={14}
              color={isNewProject ? Colors.bgPrimary : Colors.textPrimary}
            />
            <Text
              style={[
                styles.projectChipText,
                isNewProject && styles.projectChipTextActive,
              ]}
            >
              New
            </Text>
          </TouchableOpacity>
        </View>

        {showEditor ? (
          <View style={styles.projectEditor}>
            <FieldLabel text="Project name" />
            <TextInput
              value={projectName}
              onChangeText={onChangeName}
              placeholder="Project name"
              placeholderTextColor={Colors.textSecondary}
              style={styles.editorTitleInput}
              autoCapitalize="words"
            />
            <FieldLabel text="Issue prefix" />
            {isNewProject ? (
              <TextInput
                value={resolvedIssuePrefix}
                onChangeText={onChangeIssuePrefix}
                placeholder={deriveProjectIssuePrefix(projectName)}
                placeholderTextColor={Colors.textSecondary}
                style={[styles.editorTitleInput, styles.projectPrefixInput]}
                autoCapitalize="characters"
                autoCorrect={false}
              />
            ) : (
              <View style={styles.projectPrefixRow}>
                <Text style={styles.projectPrefixLabel}>Issue prefix</Text>
                <Text style={styles.projectPrefixValue}>{resolvedIssuePrefix}</Text>
              </View>
            )}
            <Text style={styles.projectPrefixHint}>
              {isNewProject
                ? "Editable during creation. Duplicates must be unique."
                : "Stable once the project is created."}
            </Text>

            <FieldLabel text="Repo root" />
            <DirectoryField
              value={repoRoot}
              placeholder="Pick the repository root"
              onPress={onOpenRepoPicker}
            />

            <FieldLabel text="Worktree root" />
            <DirectoryField
              value={worktreeRoot}
              placeholder="Optional. Defaults to .zen/worktrees beside the repo"
              onPress={onOpenWorktreePicker}
            />

            <FieldLabel text="Base branch" />
            <TextInput
              value={baseBranch}
              onChangeText={onChangeBaseBranch}
              placeholder="Optional. Auto-detect if left empty"
              placeholderTextColor={Colors.textSecondary}
              style={styles.editorTitleInput}
              autoCapitalize="none"
              autoCorrect={false}
            />

            <Text style={styles.projectHint}>
              {repoRoot
                ? "Assign will use this repo context and create a dedicated issue worktree."
                : "Set a repo root to enable assign from this issue."}
            </Text>
          </View>
        ) : (
          <View style={styles.projectEmptyState}>
            <Text style={styles.projectHint}>
              Choose an existing project or start a new one to define repo
              context.
            </Text>
          </View>
        )}

        <View style={styles.projectActions}>
          <TouchableOpacity
            style={[
              styles.primarySheetButton,
              busy && styles.primarySheetButtonDisabled,
            ]}
            onPress={onSave}
            disabled={busy}
            activeOpacity={0.82}
          >
            <Text style={styles.primarySheetButtonText}>
              {busy ? "Saving..." : primaryLabel}
            </Text>
          </TouchableOpacity>
        </View>

        {activeProjectId &&
        draftProjectId &&
        activeProjectId !== draftProjectId ? (
          <Text style={styles.projectFootnote}>
            Saving also switches this issue to the selected project.
          </Text>
        ) : null}
      </ScrollView>
    </SheetScaffold>
  );
}

function FieldLabel({ text }: { text: string }) {
  return <Text style={styles.editorLabel}>{text}</Text>;
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
      <View style={styles.directoryFieldCopy}>
        <Text
          style={[
            styles.directoryFieldValue,
            !value && styles.directoryFieldPlaceholder,
          ]}
          numberOfLines={2}
        >
          {value || placeholder}
        </Text>
      </View>
      <Ionicons
        name="folder-open-outline"
        size={16}
        color={Colors.textSecondary}
      />
    </TouchableOpacity>
  );
}

function CommentComposerSheet({
  visible,
  busy,
  replyTarget,
  value,
  selection,
  attachments,
  attachmentBusy,
  mentionSuggestions,
  onChangeText,
  onChangeSelection,
  onPickMention,
  onAddAttachment,
  onRemoveAttachment,
  onClose,
  onSave,
}: {
  visible: boolean;
  busy?: boolean;
  replyTarget?: TaskComment | null;
  value: string;
  selection: ComposerSelection;
  attachments: Attachment[];
  attachmentBusy?: boolean;
  mentionSuggestions: typeof SUPPORTED_AGENT_TARGETS;
  onChangeText: (text: string) => void;
  onChangeSelection: (selection: ComposerSelection) => void;
  onPickMention: (handle: string) => void;
  onAddAttachment: () => void;
  onRemoveAttachment: (attachment: Attachment) => void;
  onClose: () => void;
  onSave: () => void;
}) {
  return (
    <SheetScaffold
      visible={visible}
      title={replyTarget ? "Reply" : "Comment"}
      onClose={onClose}
    >
      <View style={styles.editorContent}>
        {replyTarget ? (
          <View style={styles.replyContext}>
            <Text style={styles.replyContextLabel}>
              Replying to {replyTarget.authorLabel || "You"}
            </Text>
            <Text style={styles.replyContextBody} numberOfLines={2}>
              {collapseCopy(replyTarget.body, 120)}
            </Text>
          </View>
        ) : null}

        <TextInput
          value={value}
          onChangeText={onChangeText}
          onSelectionChange={(event) => {
            onChangeSelection(event.nativeEvent.selection);
          }}
          selection={selection}
          placeholder="Comment, or type @ to route work to an agent..."
          placeholderTextColor={Colors.textSecondary}
          style={styles.editorDescriptionInput}
          multiline
          autoFocus
          textAlignVertical="top"
        />

        {mentionSuggestions.length > 0 ? (
          <View style={styles.mentionPanel}>
            {mentionSuggestions.map((target) => (
              <TouchableOpacity
                key={target.id}
                style={styles.mentionRow}
                onPress={() => onPickMention(target.handle)}
                activeOpacity={0.82}
              >
                <View style={styles.mentionCopy}>
                  <View style={styles.mentionTopRow}>
                    <Text style={styles.mentionHandle}>@{target.handle}</Text>
                    <Text style={styles.mentionLabel}>{target.label}</Text>
                  </View>
                  <Text style={styles.mentionDescription}>
                    {target.description}
                  </Text>
                </View>
              </TouchableOpacity>
            ))}
          </View>
        ) : null}

        <View style={styles.attachmentEditor}>
          <Text style={styles.editorLabel}>Files</Text>
          <AttachmentStack
            attachments={attachments}
            emptyLabel="Optional screenshots, logs, or specs."
            addLabel={attachmentBusy ? "Uploading..." : "Attach file"}
            addDisabled={busy || attachmentBusy}
            onAdd={onAddAttachment}
            onRemove={onRemoveAttachment}
          />
        </View>

        <TouchableOpacity
          style={[
            styles.primarySheetButton,
            (!value.trim() || busy || attachmentBusy) &&
              styles.primarySheetButtonDisabled,
          ]}
          onPress={onSave}
          disabled={!value.trim() || busy || attachmentBusy}
          activeOpacity={0.82}
        >
          <Text style={styles.primarySheetButtonText}>
            {busy ? "Sending..." : "Send comment"}
          </Text>
        </TouchableOpacity>
      </View>
    </SheetScaffold>
  );
}

function EditIssueSheet({
  visible,
  serverId,
  title,
  description,
  attachments,
  deleting,
  onClose,
  onSave,
  onDelete,
}: {
  visible: boolean;
  serverId: string;
  title: string;
  description: string;
  attachments: Attachment[];
  deleting?: boolean;
  onClose: () => void;
  onSave: (
    title: string,
    description: string,
    attachments: Attachment[],
  ) => void;
  onDelete: () => void;
}) {
  const [nextTitle, setNextTitle] = useState(title);
  const [nextDescription, setNextDescription] = useState(description);
  const [nextAttachments, setNextAttachments] = useState(attachments);
  const [uploadingAttachment, setUploadingAttachment] = useState(false);

  useEffect(() => {
    if (visible) {
      setNextTitle(title);
      setNextDescription(description);
      setNextAttachments(attachments);
    }
  }, [attachments, description, title, visible]);

  const handleAddAttachment = async () => {
    if (uploadingAttachment) {
      return;
    }

    setUploadingAttachment(true);
    try {
      const attachment = await uploadDocumentForServer(serverId);
      if (!attachment) {
        return;
      }

      setNextAttachments((current) => {
        if (current.some((existing) => existing.path === attachment.path)) {
          return current;
        }
        return [...current, attachment];
      });
    } catch (error: any) {
      Alert.alert("Could not attach file", error?.message || "Try again.");
    } finally {
      setUploadingAttachment(false);
    }
  };

  return (
    <SheetScaffold visible={visible} title="Edit issue" onClose={onClose}>
      <View style={styles.editorContent}>
        <FieldLabel text="Title" />
        <TextInput
          value={nextTitle}
          onChangeText={setNextTitle}
          placeholder="Title"
          placeholderTextColor={Colors.textSecondary}
          style={styles.editorTitleInput}
        />

        <FieldLabel text="Description" />
        <TextInput
          value={nextDescription}
          onChangeText={setNextDescription}
          placeholder="Description"
          placeholderTextColor={Colors.textSecondary}
          style={styles.editorDescriptionInput}
          multiline
          textAlignVertical="top"
        />

        <View style={styles.attachmentEditor}>
          <FieldLabel text="Files" />
          <AttachmentStack
            attachments={nextAttachments}
            emptyLabel="Optional screenshots, logs, or specs."
            addLabel={uploadingAttachment ? "Uploading..." : "Attach file"}
            addDisabled={uploadingAttachment}
            onAdd={() => {
              void handleAddAttachment();
            }}
            onRemove={(attachment) => {
              setNextAttachments((current) =>
                current.filter((existing) => existing.path !== attachment.path),
              );
            }}
          />
        </View>

        <TouchableOpacity
          style={[
            styles.primarySheetButton,
            !nextTitle.trim() && styles.primarySheetButtonDisabled,
          ]}
          onPress={() =>
            onSave(nextTitle.trim(), nextDescription, nextAttachments)
          }
          disabled={!nextTitle.trim()}
          activeOpacity={0.82}
        >
          <Text style={styles.primarySheetButtonText}>Save</Text>
        </TouchableOpacity>

        <TouchableOpacity
          style={styles.deleteSheetButton}
          onPress={onDelete}
          disabled={!!deleting}
          activeOpacity={0.82}
        >
          <Text style={styles.deleteSheetButtonText}>
            {deleting ? "Deleting..." : "Delete issue"}
          </Text>
        </TouchableOpacity>
      </View>
    </SheetScaffold>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.bgPrimary,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 8,
  },
  iconButton: {
    width: 30,
    height: 30,
    borderRadius: 15,
    alignItems: "center",
    justifyContent: "center",
  },
  headerCopy: {
    flex: 1,
  },
  headerKey: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.terminalFont,
  },
  headerAction: {
    minHeight: 30,
    paddingHorizontal: 10,
    borderRadius: 6,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.05)",
  },
  headerActionText: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  missingTitle: {
    color: Colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
  },
  scroll: {
    flex: 1,
  },
  page: {
    width: "100%",
    gap: 20,
  },
  hero: {
    gap: 12,
    paddingTop: 4,
  },
  heroMetaRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 8,
    alignItems: "center",
  },
  heroBadge: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  heroBadgeSep: {
    color: Colors.textSecondary,
    fontSize: 12,
    opacity: 0.4,
  },
  heroBadgeDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  heroBadgeText: {
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  title: {
    color: Colors.textPrimary,
    fontSize: 20,
    lineHeight: 27,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: -0.2,
  },
  description: {
    color: Colors.textSecondary,
    fontSize: 14,
    lineHeight: 22,
    fontFamily: Typography.uiFont,
  },
  heroAttachmentSurface: {
    gap: 8,
    paddingHorizontal: 12,
    paddingVertical: 12,
    borderRadius: 12,
    backgroundColor: "rgba(255,255,255,0.03)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.07)",
  },
  heroAttachmentLabel: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
  },
  actionRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 8,
  },
  primaryAction: {
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
    minHeight: 36,
    paddingHorizontal: 14,
    borderRadius: 8,
    backgroundColor: Colors.accent,
  },
  primaryActionText: {
    color: Colors.bgPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  secondaryAction: {
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
    minHeight: 36,
    paddingHorizontal: 12,
    borderRadius: 8,
    backgroundColor: "rgba(255,255,255,0.05)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  secondaryActionText: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  sectionHeader: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
  },
  sectionTitle: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
    textTransform: "uppercase",
    letterSpacing: 0.5,
    opacity: 0.7,
  },
  sectionAction: {
    color: Colors.accent,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  surface: {
    backgroundColor: "rgba(255,255,255,0.025)",
    borderRadius: 14,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.07)",
    overflow: "hidden",
  },
  executionLead: {
    flexDirection: "row",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 14,
  },
  executionMarker: {
    width: 3,
    borderRadius: 2,
  },
  executionCopy: {
    flex: 1,
    gap: 4,
  },
  executionTitle: {
    color: Colors.textPrimary,
    fontSize: 15,
    fontFamily: Typography.uiFontMedium,
  },
  executionBody: {
    color: Colors.textSecondary,
    fontSize: 13,
    lineHeight: 21,
    fontFamily: Typography.uiFont,
  },
  divider: {
    height: StyleSheet.hairlineWidth,
    backgroundColor: "rgba(255,255,255,0.06)",
    marginHorizontal: 16,
  },
  propertyRow: {
    minHeight: 50,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 10,
  },
  propertyLabelWrap: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    minWidth: 88,
  },
  propertyLabel: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
  },
  propertyValueWrap: {
    flex: 1,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "flex-end",
    gap: 8,
  },
  propertyValue: {
    flexShrink: 1,
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
    textAlign: "right",
  },
  propertyValueMuted: {
    color: Colors.textSecondary,
  },
  detailRow: {
    minHeight: 56,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  detailIconWrap: {
    width: 24,
    alignItems: "center",
  },
  detailCopy: {
    flex: 1,
    gap: 3,
  },
  detailLabel: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  detailValue: {
    color: Colors.textPrimary,
    fontSize: 14,
    lineHeight: 20,
    fontFamily: Typography.uiFontMedium,
  },
  detailMeta: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  emptyState: {
    gap: 8,
    paddingHorizontal: 18,
    paddingVertical: 18,
  },
  emptyStateTitle: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  emptyStateBody: {
    color: Colors.textSecondary,
    fontSize: 13,
    lineHeight: 21,
    fontFamily: Typography.uiFont,
  },
  emptyStateAction: {
    alignSelf: "flex-start",
    minHeight: 34,
    paddingHorizontal: 12,
    borderRadius: 17,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.06)",
  },
  emptyStateActionText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  noteList: {
    paddingVertical: 4,
  },
  noteThread: {
    paddingHorizontal: 16,
    paddingVertical: 12,
    gap: 8,
  },
  noteThreadNested: {
    paddingLeft: 12,
    paddingRight: 0,
    paddingTop: 8,
    paddingBottom: 0,
  },
  noteCard: {
    gap: 8,
    padding: 12,
    borderRadius: 10,
    backgroundColor: "rgba(255,255,255,0.035)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.07)",
  },
  noteHeader: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
  },
  noteAuthor: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  noteTime: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  noteBody: {
    color: Colors.textPrimary,
    fontSize: 14,
    lineHeight: 22,
    fontFamily: Typography.uiFont,
  },
  commentMetaRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    alignItems: "center",
    gap: 8,
  },
  commentMetaPill: {
    minHeight: 22,
    paddingHorizontal: 8,
    borderRadius: 4,
    justifyContent: "center",
    backgroundColor: "rgba(91,157,255,0.08)",
  },
  commentMetaPillText: {
    color: Colors.accent,
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
  },
  commentAttachmentBlock: {
    paddingTop: 2,
  },
  noteReplyButton: {
    alignSelf: "flex-start",
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  noteReplyText: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  noteReplyList: {
    gap: 8,
  },
  activityRow: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  activityDotWrap: {
    width: 20,
    height: 20,
    borderRadius: 10,
    alignItems: "center",
    justifyContent: "center",
    marginTop: 2,
    backgroundColor: "rgba(255,255,255,0.04)",
  },
  activityDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  activityCopy: {
    flex: 1,
    gap: 4,
  },
  activityTopRow: {
    flexDirection: "row",
    justifyContent: "space-between",
    gap: 12,
  },
  activityTitle: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  activityTime: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  activityMeta: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  activityBody: {
    color: Colors.textPrimary,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
  },
  inlineComposer: {
    flexDirection: "row",
    alignItems: "flex-end",
    gap: 10,
    paddingHorizontal: 14,
    paddingVertical: 10,
  },
  inlineComposerInput: {
    flex: 1,
    minHeight: 44,
    maxHeight: 120,
    paddingTop: 10,
    paddingBottom: 10,
    color: Colors.textPrimary,
    fontSize: 14,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
  },
  inlineComposerSend: {
    width: 28,
    height: 28,
    borderRadius: 14,
    backgroundColor: Colors.accent,
    alignItems: "center",
    justifyContent: "center",
    marginBottom: 4,
  },
  inlineComposerSendBusy: {
    opacity: 0.5,
  },
  inlineLink: {
    marginTop: 2,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  inlineLinkText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  inlineLinkMuted: {
    color: Colors.textSecondary,
  },
  sheetRoot: {
    flex: 1,
    justifyContent: "flex-end",
  },
  sheetBackdrop: {
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
  sheetHandle: {
    alignSelf: "center",
    width: 44,
    height: 4,
    borderRadius: 2,
    backgroundColor: "rgba(255,255,255,0.14)",
    marginBottom: 10,
  },
  sheetHeader: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
    paddingHorizontal: 16,
    paddingBottom: 12,
  },
  sheetTitle: {
    color: Colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
  },
  sheetContent: {
    paddingHorizontal: 16,
    paddingBottom: 8,
    gap: 16,
  },
  sheetValue: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
  },
  selectionScroll: {
    maxHeight: 560,
  },
  selectionContent: {
    paddingHorizontal: 14,
    paddingBottom: 8,
    gap: 10,
  },
  selectionRow: {
    minHeight: 46,
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 10,
    backgroundColor: "rgba(255,255,255,0.03)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.07)",
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 10,
  },
  selectionRowActive: {
    borderColor: `${Colors.accent}44`,
    backgroundColor: "rgba(91,157,255,0.08)",
  },
  selectionLabelWrap: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
  },
  selectionLabel: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  projectSheetContent: {
    paddingHorizontal: 14,
    paddingBottom: 12,
    gap: 14,
  },
  projectSelector: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 10,
  },
  projectChip: {
    minHeight: 34,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    paddingHorizontal: 12,
    borderRadius: 17,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  projectChipActive: {
    backgroundColor: Colors.accent,
    borderColor: Colors.accent,
  },
  projectChipText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  projectChipTextActive: {
    color: Colors.bgPrimary,
  },
  projectEditor: {
    gap: 10,
  },
  projectEmptyState: {
    paddingHorizontal: 4,
    paddingVertical: 2,
  },
  editorContent: {
    paddingHorizontal: 16,
    paddingBottom: 8,
    gap: 12,
  },
  editorLabel: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  editorTitleInput: {
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
  projectPrefixInput: {
    fontFamily: Typography.terminalFont,
    textAlign: "center",
  },
  projectPrefixRow: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 4,
    marginTop: -2,
  },
  projectPrefixLabel: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  projectPrefixValue: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.terminalFont,
  },
  projectPrefixHint: {
    color: Colors.textSecondary,
    fontSize: 11,
    lineHeight: 17,
    fontFamily: Typography.uiFont,
    marginTop: -2,
  },
  editorDescriptionInput: {
    minHeight: 146,
    paddingHorizontal: 14,
    paddingTop: 14,
    paddingBottom: 14,
    borderRadius: 18,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    color: Colors.textPrimary,
    fontSize: 14,
    lineHeight: 22,
    fontFamily: Typography.uiFont,
  },
  directoryField: {
    minHeight: 54,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 14,
    borderRadius: 16,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  directoryFieldCopy: {
    flex: 1,
  },
  directoryFieldValue: {
    color: Colors.textPrimary,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
  },
  directoryFieldPlaceholder: {
    color: Colors.textSecondary,
  },
  projectHint: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
  },
  projectActions: {
    gap: 10,
  },
  projectFootnote: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
  },
  primarySheetButton: {
    minHeight: 46,
    borderRadius: 16,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: Colors.accent,
  },
  primarySheetButtonDisabled: {
    opacity: 0.45,
  },
  primarySheetButtonText: {
    color: Colors.bgPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  deleteSheetButton: {
    minHeight: 40,
    borderRadius: 14,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,82,82,0.1)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,82,82,0.22)",
  },
  deleteSheetButtonText: {
    color: Colors.statusFailed,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  replyContext: {
    gap: 4,
    paddingHorizontal: 14,
    paddingVertical: 12,
    borderRadius: 16,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  replyContextLabel: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  replyContextBody: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
  },
  mentionPanel: {
    gap: 8,
    padding: 12,
    borderRadius: 16,
    backgroundColor: "rgba(255,255,255,0.03)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  mentionRow: {
    paddingHorizontal: 2,
    paddingVertical: 2,
  },
  mentionCopy: {
    gap: 4,
  },
  mentionTopRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    alignItems: "center",
    gap: 8,
  },
  mentionHandle: {
    color: Colors.accent,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  mentionLabel: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  mentionDescription: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  attachmentEditor: {
    gap: 10,
  },
});
