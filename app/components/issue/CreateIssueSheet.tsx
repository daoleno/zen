import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  Alert,
  Keyboard,
  KeyboardAvoidingView,
  Modal,
  Platform,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  useWindowDimensions,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { Colors, Typography, priorityColor } from "../../constants/tokens";
import type { IssuePriority } from "../../constants/tokens";
import { DueDatePicker } from "./DueDatePicker";
import { AttachmentStack } from "./AttachmentStack";
import { formatDueDateShort } from "../../services/dueDate";
import {
  deriveProjectIssuePrefix,
  sanitizeIssuePrefixInput,
} from "../../services/taskIdentity";
import { uploadDocumentForServer } from "../../services/uploads";
import { useTasks } from "../../store/tasks";
import type { Attachment } from "../../store/tasks";
import { wsClient } from "../../services/websocket";

type CreateAction = {
  key: string;
  label: string;
  primary?: boolean;
  icon?: keyof typeof Ionicons.glyphMap;
  afterCreate?: (serverId: string, task: any) => Promise<void> | void;
};

type ExpandedSection = "project" | "priority" | "dueDate" | "files" | null;

const PRIORITY_OPTIONS: { value: IssuePriority; label: string }[] = [
  { value: 0, label: "No priority" },
  { value: 4, label: "Low" },
  { value: 3, label: "Medium" },
  { value: 2, label: "High" },
  { value: 1, label: "Urgent" },
];

function clamp(value: number, min: number, max: number) {
  return Math.min(Math.max(value, min), max);
}

interface Props {
  visible: boolean;
  serverOptions: { id: string; name: string }[];
  selectedServerId: string | null;
  onSelectServer: (id: string) => void;
  onClose: () => void;
  initialProjectId?: string;
  initialTitle?: string;
  initialDescription?: string;
  actions?: CreateAction[];
  onCreated?: (serverId: string, taskId: string) => void;
}

export function CreateIssueSheet({
  visible,
  serverOptions,
  selectedServerId,
  onSelectServer,
  onClose,
  initialProjectId,
  initialTitle,
  initialDescription,
  actions,
  onCreated,
}: Props) {
  const { state: taskState } = useTasks();
  const insets = useSafeAreaInsets();
  const {
    width: windowWidth,
    height: windowHeight,
    fontScale,
  } = useWindowDimensions();

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [priority, setPriority] = useState<IssuePriority>(0);
  const [projectId, setProjectId] = useState("");
  const [dueDate, setDueDate] = useState<string | null>(null);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [expandedSection, setExpandedSection] = useState<ExpandedSection>(null);
  const [submitting, setSubmitting] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");
  const [newProjectIssuePrefix, setNewProjectIssuePrefix] = useState(
    deriveProjectIssuePrefix(""),
  );
  const [newProjectIssuePrefixDirty, setNewProjectIssuePrefixDirty] =
    useState(false);
  const [creatingProject, setCreatingProject] = useState(false);
  const [uploadingAttachment, setUploadingAttachment] = useState(false);
  const [keyboardHeight, setKeyboardHeight] = useState(0);
  const didInitializeRef = useRef(false);

  const projectOptions = useMemo(() => {
    if (!selectedServerId) {
      return [];
    }

    return taskState.projects
      .filter((project) => project.serverId === selectedServerId)
      .sort((left, right) => left.name.localeCompare(right.name));
  }, [selectedServerId, taskState.projects]);

  const selectedProject = useMemo(
    () => projectOptions.find((project) => project.id === projectId) || null,
    [projectId, projectOptions],
  );

  const selectedPriority = useMemo(
    () =>
      PRIORITY_OPTIONS.find((option) => option.value === priority) ||
      PRIORITY_OPTIONS[0],
    [priority],
  );

  const resolvedActions: CreateAction[] =
    actions && actions.length > 0
      ? actions
      : [{ key: "create", label: "Create issue", primary: true }];
  const defaultServerId = serverOptions[0]?.id || null;

  const normalizedFontScale = clamp(fontScale || 1, 1, 1.25);
  const isLandscape = windowWidth > windowHeight;
  const isTabletLike = windowWidth >= 768;
  const isNarrowPhone = windowWidth < 360;
  const isCompactHeight =
    windowHeight < 760 || (isLandscape && windowHeight < 620);
  const stackComposer = isNarrowPhone || normalizedFontScale > 1.15;
  const stackActions =
    resolvedActions.length > 1 && (isNarrowPhone || normalizedFontScale > 1.1);
  const shouldInsetSheet = isTabletLike || isLandscape;

  const keyboardInset = Math.max(0, keyboardHeight - insets.bottom);
  const keyboardOpen = keyboardInset > 0;
  const sheetHorizontalPadding = isTabletLike ? 24 : isNarrowPhone ? 16 : 20;
  const sheetRadius = isTabletLike ? 32 : 30;
  const sheetWidth = shouldInsetSheet
    ? Math.min(windowWidth - 16, 720)
    : windowWidth;
  const sheetMaxHeight = Math.min(
    windowHeight - insets.top - (shouldInsetSheet ? 10 : 14),
    windowHeight * (isLandscape ? 0.96 : 0.9),
  );
  const sheetMinHeight = Math.min(
    sheetMaxHeight,
    windowHeight * (isCompactHeight ? 0.82 : isTabletLike ? 0.76 : 0.72),
  );
  const footerInset = insets.bottom + (isCompactHeight ? 14 : 18);
  const headerTopPadding = isCompactHeight ? 4 : 6;
  const headerBottomPadding = isCompactHeight ? 10 : 14;
  const contentTopPadding = isCompactHeight ? 2 : 6;
  const contentBottomPadding = keyboardOpen
    ? isCompactHeight
      ? 96
      : 112
    : isCompactHeight
      ? 16
      : 20;
  const titleFontSize = isTabletLike ? 30 : isCompactHeight ? 24 : 28;
  const titleLineHeight = Math.round(titleFontSize * 1.18);
  const titleVerticalPadding =
    Platform.OS === "android"
      ? isCompactHeight
        ? 8
        : 10
      : isCompactHeight
        ? 6
        : 8;
  const titleMinHeight =
    Platform.OS === "android"
      ? isCompactHeight
        ? 48
        : 52
      : isCompactHeight
        ? 44
        : 48;
  const descriptionMinHeight = isTabletLike ? 80 : isCompactHeight ? 56 : 64;
  const cardRadius = isTabletLike ? 20 : 18;
  const groupRadius = isTabletLike ? 20 : 18;
  const rowMinHeight = isTabletLike ? 60 : isCompactHeight ? 54 : 58;
  const panelVerticalPadding = isCompactHeight ? 12 : 14;
  const sectionGap = isCompactHeight ? 8 : 10;
  const footerTopPadding = isCompactHeight ? 10 : 12;
  const actionButtonHeight = isCompactHeight ? 46 : 48;
  const actionButtonRadius = isCompactHeight ? 14 : 16;

  useEffect(() => {
    if (!visible || selectedServerId || !defaultServerId) {
      return;
    }

    onSelectServer(defaultServerId);
  }, [defaultServerId, onSelectServer, selectedServerId, visible]);

  useEffect(() => {
    if (!visible) {
      didInitializeRef.current = false;
      setKeyboardHeight(0);
      return;
    }

    if (didInitializeRef.current) {
      return;
    }

    didInitializeRef.current = true;
    setTitle(initialTitle || "");
    setDescription(initialDescription || "");
    setPriority(0);
    setProjectId(initialProjectId || "");
    setDueDate(null);
    setAttachments([]);
    setExpandedSection(null);
    setNewProjectName("");
    setNewProjectIssuePrefix(deriveProjectIssuePrefix(""));
    setNewProjectIssuePrefixDirty(false);
    setKeyboardHeight(0);
  }, [
    initialDescription,
    initialProjectId,
    initialTitle,
    visible,
  ]);

  useEffect(() => {
    if (!projectId) {
      return;
    }

    if (!projectOptions.some((project) => project.id === projectId)) {
      setProjectId("");
    }
  }, [projectId, projectOptions]);

  useEffect(() => {
    if (!visible) {
      return;
    }

    const showEvent =
      Platform.OS === "ios" ? "keyboardWillShow" : "keyboardDidShow";
    const hideEvent =
      Platform.OS === "ios" ? "keyboardWillHide" : "keyboardDidHide";

    const showSubscription = Keyboard.addListener(showEvent, (event) => {
      setKeyboardHeight(event.endCoordinates.height);
    });

    const hideSubscription = Keyboard.addListener(hideEvent, () => {
      setKeyboardHeight(0);
    });

    return () => {
      showSubscription.remove();
      hideSubscription.remove();
    };
  }, [visible]);

  useEffect(() => {
    if (!visible || !selectedServerId) {
      return;
    }

    wsClient.listProjects(selectedServerId);
  }, [selectedServerId, visible]);

  const handleClose = (force = false) => {
    if ((submitting || creatingProject) && !force) {
      return;
    }

    Keyboard.dismiss();
    setExpandedSection(null);
    onClose();
  };

  const handleCreate = async (action: CreateAction) => {
    const trimmed = title.trim();
    if (!trimmed || !selectedServerId) {
      return;
    }

    Keyboard.dismiss();
    setSubmitting(true);
    try {
      const task = await wsClient.createTask(selectedServerId, {
        title: trimmed,
        description: description.trim(),
        attachments,
        priority,
        projectId: projectId || undefined,
        dueDate: dueDate || undefined,
      });

      if (action.afterCreate) {
        try {
          await action.afterCreate(selectedServerId, task);
        } catch (error: any) {
          Alert.alert(
            "Issue created, follow-up failed",
            error?.message || "You can retry from the issue detail screen.",
          );
        }
      }

      onCreated?.(selectedServerId, task.id);
      handleClose(true);
    } catch (error: any) {
      Alert.alert("Could not create issue", error?.message || "Try again.");
    } finally {
      setSubmitting(false);
    }
  };

  const handleAddAttachment = async () => {
    if (
      !selectedServerId ||
      submitting ||
      creatingProject ||
      uploadingAttachment
    ) {
      return;
    }

    setUploadingAttachment(true);
    try {
      const attachment = await uploadDocumentForServer(selectedServerId);
      if (!attachment) {
        return;
      }

      setAttachments((current) => {
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

  const handleRemoveAttachment = (attachment: Attachment) => {
    setAttachments((current) =>
      current.filter((existing) => existing.path !== attachment.path),
    );
  };

  const handleCreateProject = async () => {
    const trimmed = newProjectName.trim();
    if (!trimmed || !selectedServerId || creatingProject || submitting) {
      return;
    }

    Keyboard.dismiss();
    setCreatingProject(true);
    try {
      const project = await wsClient.createProject(selectedServerId, {
        name: trimmed,
        issuePrefix: newProjectIssuePrefix.trim(),
      });
      setProjectId(project.id);
      setNewProjectName("");
      setNewProjectIssuePrefix(deriveProjectIssuePrefix(""));
      setNewProjectIssuePrefixDirty(false);
      setExpandedSection(null);
    } catch (error: any) {
      Alert.alert("Could not create project", error?.message || "Try again.");
    } finally {
      setCreatingProject(false);
    }
  };

  const selectProject = (nextProjectId: string) => {
    setProjectId(nextProjectId);
    setExpandedSection(null);
  };

  const handleNewProjectNameChange = (value: string) => {
    const currentDerived = deriveProjectIssuePrefix(newProjectName);
    const nextDerived = deriveProjectIssuePrefix(value);
    setNewProjectName(value);
    if (
      !newProjectIssuePrefixDirty ||
      newProjectIssuePrefix === currentDerived
    ) {
      setNewProjectIssuePrefix(nextDerived);
    }
  };

  const handleNewProjectIssuePrefixChange = (value: string) => {
    const sanitized = sanitizeIssuePrefixInput(value);
    if (sanitized) {
      setNewProjectIssuePrefix(sanitized);
      setNewProjectIssuePrefixDirty(true);
      return;
    }
    setNewProjectIssuePrefix(deriveProjectIssuePrefix(newProjectName));
    setNewProjectIssuePrefixDirty(false);
  };

  const selectPriority = (nextPriority: IssuePriority) => {
    setPriority(nextPriority);
    setExpandedSection(null);
  };

  const selectDueDate = (nextDueDate: string | null) => {
    setDueDate(nextDueDate);
    setExpandedSection(null);
  };

  const toggleSection = (section: ExpandedSection) => {
    if (submitting || creatingProject) {
      return;
    }

    Keyboard.dismiss();
    setExpandedSection((current) => (current === section ? null : section));
  };

  const renderProjectValue = () => {
    if (selectedProject) {
      return selectedProject.name;
    }

    return "No project";
  };

  const renderAttributeRow = ({
    section,
    icon,
    label,
    value,
    valueMuted = false,
    trailing,
    children,
  }: {
    section: Exclude<ExpandedSection, null>;
    icon: keyof typeof Ionicons.glyphMap;
    label: string;
    value: string;
    valueMuted?: boolean;
    trailing?: React.ReactNode;
    children?: React.ReactNode;
  }) => {
    const active = expandedSection === section;

    return (
      <View style={[styles.attributeGroup, { borderRadius: groupRadius }]}>
        <TouchableOpacity
          style={[
            styles.attributeRow,
            { minHeight: rowMinHeight },
            active && styles.attributeRowActive,
          ]}
          onPress={() => toggleSection(section)}
          activeOpacity={0.82}
          disabled={submitting || creatingProject}
        >
          <View style={styles.attributeLeft}>
            <Ionicons name={icon} size={17} color={Colors.textSecondary} />
            <Text style={styles.attributeLabel} maxFontSizeMultiplier={1.1}>
              {label}
            </Text>
          </View>

          <View style={styles.attributeRight}>
            {trailing}
            <Text
              maxFontSizeMultiplier={1.1}
              style={[
                styles.attributeValue,
                valueMuted
                  ? styles.attributeValueMuted
                  : styles.attributeValueActive,
              ]}
              numberOfLines={1}
            >
              {value}
            </Text>
            <Ionicons
              name={active ? "chevron-up" : "chevron-down"}
              size={15}
              color={Colors.textSecondary}
            />
          </View>
        </TouchableOpacity>

        {active && children ? (
          <View style={styles.inlinePanel}>
            <View
              style={[
                styles.panelContent,
                { paddingVertical: panelVerticalPadding },
              ]}
            >
              {children}
            </View>
          </View>
        ) : null}
      </View>
    );
  };

  return (
    <Modal
      visible={visible}
      transparent
      animationType="slide"
      presentationStyle="overFullScreen"
      onRequestClose={() => handleClose()}
    >
      <KeyboardAvoidingView
        style={styles.root}
        // Android already resizes this modal for the keyboard. Applying
        // "height" here makes the sheet fight that resize and flicker.
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        <TouchableOpacity
          style={styles.backdrop}
          activeOpacity={1}
          onPress={() => handleClose()}
        />

        <View
          style={[
            styles.sheet,
            {
              width: sheetWidth,
              minHeight: sheetMinHeight,
              maxHeight: sheetMaxHeight,
              borderTopLeftRadius: sheetRadius,
              borderTopRightRadius: sheetRadius,
            },
          ]}
        >
          <View
            style={[
              styles.handle,
              {
                marginTop: isCompactHeight ? 8 : 10,
                marginBottom: isCompactHeight ? 6 : 8,
              },
            ]}
          />

          <View
            style={[
              styles.header,
              {
                paddingHorizontal: sheetHorizontalPadding,
                paddingTop: headerTopPadding,
                paddingBottom: headerBottomPadding,
              },
            ]}
          >
            <Text style={styles.headerTitle} maxFontSizeMultiplier={1.12}>
              New issue
            </Text>

            <View style={styles.headerActions}>
              {keyboardOpen ? (
                <TouchableOpacity
                  style={styles.secondaryHeaderButton}
                  onPress={() => Keyboard.dismiss()}
                  activeOpacity={0.82}
                  disabled={submitting || creatingProject}
                >
                  <Ionicons
                    name="chevron-down"
                    size={18}
                    color={Colors.textSecondary}
                  />
                </TouchableOpacity>
              ) : null}

              <TouchableOpacity
                style={styles.closeButton}
                onPress={() => handleClose()}
                activeOpacity={0.82}
                disabled={submitting || creatingProject}
              >
                <Ionicons name="close" size={18} color={Colors.textSecondary} />
              </TouchableOpacity>
            </View>
          </View>

          <ScrollView
            style={styles.content}
            contentContainerStyle={[
              styles.contentContainer,
              {
                paddingHorizontal: sheetHorizontalPadding,
                paddingTop: contentTopPadding,
                paddingBottom: contentBottomPadding,
              },
            ]}
            showsVerticalScrollIndicator={false}
            keyboardShouldPersistTaps="handled"
            keyboardDismissMode={
              Platform.OS === "ios" ? "interactive" : "on-drag"
            }
            automaticallyAdjustKeyboardInsets={Platform.OS === "ios"}
          >
            <TextInput
              style={[
                styles.titleInput,
                {
                  minHeight: titleMinHeight,
                  paddingTop: titleVerticalPadding,
                  paddingBottom: titleVerticalPadding,
                  fontSize: titleFontSize,
                  lineHeight: titleLineHeight,
                },
              ]}
              value={title}
              onChangeText={setTitle}
              onFocus={() => setExpandedSection(null)}
              placeholder="Title"
              placeholderTextColor="rgba(255,255,255,0.24)"
              autoCapitalize="sentences"
              autoCorrect={false}
              editable={!submitting && !creatingProject}
              selectionColor={Colors.accent}
              multiline={false}
              autoFocus
              maxFontSizeMultiplier={1.12}
            />

            <View
              style={[styles.descriptionCard, { borderRadius: cardRadius }]}
            >
              <TextInput
                style={[
                  styles.descriptionInput,
                  { minHeight: descriptionMinHeight },
                ]}
                value={description}
                onChangeText={setDescription}
                onFocus={() => setExpandedSection(null)}
                placeholder="Add description..."
                placeholderTextColor="rgba(255,255,255,0.18)"
                autoCapitalize="sentences"
                editable={!submitting && !creatingProject}
                selectionColor={Colors.accent}
                multiline
                numberOfLines={5}
                textAlignVertical="top"
                maxFontSizeMultiplier={1.15}
              />
            </View>

            <View style={[styles.attributeList, { gap: sectionGap }]}>
              {renderAttributeRow({
                section: "project",
                icon: "folder-open-outline",
                label: "Project",
                value: renderProjectValue(),
                valueMuted: !selectedProject,
                children: (
                  <>
                    <View style={styles.optionList}>
                      <TouchableOpacity
                        style={styles.optionRow}
                        onPress={() => selectProject("")}
                        activeOpacity={0.82}
                      >
                        <View style={styles.optionMain}>
                          <Ionicons
                            name="remove-circle-outline"
                            size={16}
                            color={Colors.textSecondary}
                          />
                          <Text
                            style={styles.optionText}
                            maxFontSizeMultiplier={1.1}
                          >
                            No project
                          </Text>
                        </View>

                        {!projectId ? (
                          <Ionicons
                            name="checkmark"
                            size={16}
                            color={Colors.accent}
                          />
                        ) : null}
                      </TouchableOpacity>

                      {projectOptions.map((project) => {
                        const active = project.id === projectId;

                        return (
                          <TouchableOpacity
                            key={project.id}
                            style={styles.optionRow}
                            onPress={() => selectProject(project.id)}
                            activeOpacity={0.82}
                          >
                            <View style={styles.optionMain}>
                              <Ionicons
                                name="folder-open-outline"
                                size={16}
                                color={Colors.textSecondary}
                              />
                              <Text
                                style={styles.optionText}
                                maxFontSizeMultiplier={1.1}
                              >
                                {project.name}
                              </Text>
                            </View>

                            {active ? (
                              <Ionicons
                                name="checkmark"
                                size={16}
                                color={Colors.accent}
                              />
                            ) : null}
                          </TouchableOpacity>
                        );
                      })}
                    </View>

                    {projectOptions.length === 0 ? (
                      <View style={styles.emptyState}>
                        <Text
                          style={styles.emptyStateTitle}
                          maxFontSizeMultiplier={1.1}
                        >
                          No projects yet
                        </Text>
                      </View>
                    ) : null}

                    <View style={styles.composer}>
                      <Text
                        style={styles.composerLabel}
                        maxFontSizeMultiplier={1.05}
                      >
                        Create project
                      </Text>
                      <View style={styles.composerMetaRow}>
                        <Text
                          style={styles.composerMetaLabel}
                          maxFontSizeMultiplier={1.05}
                        >
                          Issue prefix
                        </Text>
                        <TextInput
                          style={styles.composerPrefixInput}
                          value={newProjectIssuePrefix}
                          onChangeText={handleNewProjectIssuePrefixChange}
                          placeholder={deriveProjectIssuePrefix(newProjectName)}
                          placeholderTextColor="rgba(255,255,255,0.22)"
                          autoCapitalize="characters"
                          autoCorrect={false}
                          editable={!creatingProject && !submitting}
                          maxFontSizeMultiplier={1.05}
                        />
                      </View>
                      <Text
                        style={styles.composerHint}
                        maxFontSizeMultiplier={1.05}
                      >
                        Used for new issue IDs in this project.
                      </Text>
                      <View
                        style={[
                          styles.composerRow,
                          stackComposer && styles.composerRowStacked,
                        ]}
                      >
                        <TextInput
                          style={[
                            styles.composerInput,
                            stackComposer && styles.composerInputStacked,
                          ]}
                          value={newProjectName}
                          onChangeText={handleNewProjectNameChange}
                          placeholder="Project name"
                          placeholderTextColor="rgba(255,255,255,0.22)"
                          autoCapitalize="words"
                          autoCorrect={false}
                          editable={!creatingProject && !submitting}
                          returnKeyType="done"
                          onSubmitEditing={() => {
                            void handleCreateProject();
                          }}
                          maxFontSizeMultiplier={1.1}
                        />

                        <TouchableOpacity
                          style={[
                            styles.composerButton,
                            stackComposer && styles.composerButtonStacked,
                            (!newProjectName.trim() ||
                              creatingProject ||
                              submitting) &&
                              styles.composerButtonDisabled,
                          ]}
                          onPress={() => {
                            void handleCreateProject();
                          }}
                          activeOpacity={0.82}
                          disabled={
                            !newProjectName.trim() ||
                            creatingProject ||
                            submitting
                          }
                        >
                          <Text
                            style={styles.composerButtonText}
                            maxFontSizeMultiplier={1.05}
                          >
                            {creatingProject ? "Creating..." : "Create"}
                          </Text>
                        </TouchableOpacity>
                      </View>
                    </View>
                  </>
                ),
              })}

              {renderAttributeRow({
                section: "priority",
                icon: "flag-outline",
                label: "Priority",
                value: selectedPriority.label,
                valueMuted: priority === 0,
                trailing: (
                  <View
                    style={[
                      styles.priorityDot,
                      {
                        backgroundColor:
                          priority === 0
                            ? "rgba(255,255,255,0.12)"
                            : priorityColor(priority),
                      },
                    ]}
                  />
                ),
                children: (
                  <>
                    <View style={styles.optionList}>
                      {PRIORITY_OPTIONS.map((option) => {
                        const active = option.value === priority;
                        const tint =
                          option.value === 0
                            ? "rgba(255,255,255,0.18)"
                            : priorityColor(option.value);

                        return (
                          <TouchableOpacity
                            key={option.value}
                            style={styles.optionRow}
                            onPress={() => selectPriority(option.value)}
                            activeOpacity={0.82}
                          >
                            <View style={styles.optionMain}>
                              <View
                                style={[
                                  styles.priorityDot,
                                  styles.optionPriorityDot,
                                  { backgroundColor: tint },
                                ]}
                              />
                              <Text
                                style={styles.optionText}
                                maxFontSizeMultiplier={1.1}
                              >
                                {option.label}
                              </Text>
                            </View>

                            {active ? (
                              <Ionicons
                                name="checkmark"
                                size={16}
                                color={Colors.accent}
                              />
                            ) : null}
                          </TouchableOpacity>
                        );
                      })}
                    </View>
                  </>
                ),
              })}

              {renderAttributeRow({
                section: "dueDate",
                icon: "calendar-outline",
                label: "Due date",
                value: formatDueDateShort(dueDate),
                valueMuted: !dueDate,
                children: (
                  <DueDatePicker value={dueDate} onChange={selectDueDate} />
                ),
              })}

              {renderAttributeRow({
                section: "files",
                icon: "attach-outline",
                label: "Files",
                value: attachments.length > 0
                  ? `${attachments.length} file${attachments.length !== 1 ? "s" : ""}`
                  : "None",
                valueMuted: attachments.length === 0,
                children: (
                  <AttachmentStack
                    attachments={attachments}
                    emptyLabel="Optional files, screenshots, or specs."
                    addLabel={uploadingAttachment ? "Uploading..." : "Attach file"}
                    addDisabled={
                      !selectedServerId ||
                      submitting ||
                      creatingProject ||
                      uploadingAttachment
                    }
                    onAdd={() => {
                      void handleAddAttachment();
                    }}
                    onRemove={handleRemoveAttachment}
                  />
                ),
              })}
            </View>
          </ScrollView>

          <View
            style={[
              styles.footer,
              {
                paddingHorizontal: sheetHorizontalPadding,
                paddingTop: footerTopPadding,
                paddingBottom: footerInset,
              },
            ]}
          >
            <View
              style={[
                styles.actionRow,
                resolvedActions.length === 1 && styles.actionRowSingle,
                stackActions && styles.actionRowStacked,
              ]}
            >
              {resolvedActions.map((action) => {
                const disabled = !title.trim() || submitting || creatingProject;
                const singlePrimary =
                  resolvedActions.length === 1 && !!action.primary;

                return (
                  <TouchableOpacity
                    key={action.key}
                    style={[
                      styles.actionButton,
                      {
                        minHeight: actionButtonHeight,
                        borderRadius: actionButtonRadius,
                      },
                      action.primary
                        ? styles.actionButtonPrimary
                        : styles.actionButtonSecondary,
                      singlePrimary && styles.actionButtonPrimarySingle,
                      stackActions && styles.actionButtonStacked,
                      disabled && styles.actionButtonDisabled,
                    ]}
                    onPress={() => {
                      void handleCreate(action);
                    }}
                    disabled={disabled}
                    activeOpacity={0.82}
                  >
                    {action.icon ? (
                      <Ionicons
                        name={action.icon}
                        size={15}
                        color={
                          action.primary ? Colors.bgPrimary : Colors.textPrimary
                        }
                      />
                    ) : null}
                    <Text
                      style={[
                        styles.actionText,
                        action.primary && styles.actionTextPrimary,
                      ]}
                      maxFontSizeMultiplier={1.08}
                    >
                      {submitting ? "Creating..." : action.label}
                    </Text>
                  </TouchableOpacity>
                );
              })}
            </View>
          </View>
        </View>
      </KeyboardAvoidingView>
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
    backgroundColor: "rgba(4,6,10,0.66)",
  },
  sheet: {
    alignSelf: "center",
    backgroundColor: Colors.bgSurface,
    borderTopLeftRadius: 30,
    borderTopRightRadius: 30,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderLeftWidth: StyleSheet.hairlineWidth,
    borderRightWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.07)",
  },
  handle: {
    width: 40,
    height: 4,
    borderRadius: 999,
    backgroundColor: "rgba(255,255,255,0.14)",
    alignSelf: "center",
    marginTop: 10,
    marginBottom: 8,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 16,
  },
  headerActions: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  headerTitle: {
    color: Colors.textPrimary,
    fontSize: 20,
    lineHeight: 26,
    fontFamily: Typography.uiFontMedium,
    flex: 1,
  },
  closeButton: {
    width: 34,
    height: 34,
    borderRadius: 17,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.04)",
  },
  secondaryHeaderButton: {
    width: 34,
    height: 34,
    borderRadius: 17,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(255,255,255,0.04)",
  },
  content: {
    flex: 1,
  },
  contentContainer: {},
  titleInput: {
    color: Colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    textAlignVertical: "center",
    includeFontPadding: false,
  },
  descriptionCard: {
    marginTop: 4,
    borderRadius: 18,
    backgroundColor: "rgba(255,255,255,0.035)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.06)",
    paddingHorizontal: 14,
    paddingVertical: 4,
  },
  descriptionInput: {
    minHeight: 108,
    paddingTop: 8,
    paddingBottom: 12,
    color: Colors.textPrimary,
    fontSize: 15,
    lineHeight: 23,
    fontFamily: Typography.uiFont,
  },
  attributeList: {
    marginTop: 16,
  },
  attributeGroup: {
    borderRadius: 18,
    backgroundColor: "rgba(255,255,255,0.03)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.06)",
    overflow: "hidden",
  },
  attributeRow: {
    paddingHorizontal: 14,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 14,
  },
  attributeRowActive: {
    backgroundColor: "rgba(255,255,255,0.028)",
  },
  attributeLeft: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    flex: 1,
  },
  attributeLabel: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  attributeRight: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    maxWidth: "58%",
  },
  attributeValue: {
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  attributeValueActive: {
    color: Colors.textPrimary,
  },
  attributeValueMuted: {
    color: Colors.textSecondary,
  },
  inlinePanel: {
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: "rgba(255,255,255,0.06)",
    backgroundColor: "rgba(255,255,255,0.02)",
  },
  panelContent: {
    paddingHorizontal: 14,
    gap: 12,
  },
  optionList: {
    gap: 2,
  },
  optionRow: {
    minHeight: 46,
    paddingHorizontal: 2,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
  },
  optionMain: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    flex: 1,
  },
  optionText: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  priorityDot: {
    width: 8,
    height: 8,
    borderRadius: 999,
  },
  optionPriorityDot: {
    width: 10,
    height: 10,
  },
  emptyState: {
    paddingHorizontal: 2,
  },
  emptyStateTitle: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  composer: {
    paddingTop: 4,
    gap: 8,
  },
  composerLabel: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  composerHint: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFont,
    opacity: 0.78,
  },
  composerMetaRow: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
  },
  composerMetaLabel: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFont,
  },
  composerPrefixInput: {
    minWidth: 108,
    minHeight: 36,
    borderRadius: 12,
    paddingHorizontal: 12,
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.terminalFont,
    backgroundColor: "rgba(255,255,255,0.05)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    textAlign: "center",
  },
  composerRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
  },
  composerRowStacked: {
    alignItems: "stretch",
    flexDirection: "column",
  },
  composerInput: {
    flex: 1,
    minHeight: 44,
    borderRadius: 14,
    paddingHorizontal: 14,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFont,
    backgroundColor: "rgba(255,255,255,0.05)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
  },
  composerInputStacked: {
    width: "100%",
  },
  composerButton: {
    minWidth: 84,
    minHeight: 44,
    paddingHorizontal: 16,
    borderRadius: 14,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: Colors.accent,
  },
  composerButtonStacked: {
    width: "100%",
  },
  composerButtonDisabled: {
    opacity: 0.45,
  },
  composerButtonText: {
    color: Colors.bgPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  footer: {
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: "rgba(255,255,255,0.06)",
  },
  actionRow: {
    flexDirection: "row",
    gap: 10,
  },
  actionRowStacked: {
    flexDirection: "column",
  },
  actionRowSingle: {
    width: "100%",
  },
  actionButton: {
    paddingHorizontal: 16,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
    flex: 1,
  },
  actionButtonStacked: {
    width: "100%",
  },
  actionButtonPrimary: {
    backgroundColor: Colors.accent,
  },
  actionButtonPrimarySingle: {
    width: "100%",
  },
  actionButtonSecondary: {
    backgroundColor: "rgba(255,255,255,0.05)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.09)",
  },
  actionButtonDisabled: {
    opacity: 0.4,
  },
  actionText: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  actionTextPrimary: {
    color: Colors.bgPrimary,
  },
});
