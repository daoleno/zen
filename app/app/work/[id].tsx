import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  Alert,
  KeyboardAvoidingView,
  Platform,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { useIsFocused, useLocalSearchParams, useRouter } from "expo-router";
import { useCurrentServer } from "../../store/currentServer";
import * as Haptics from "expo-haptics";
import { SafeAreaView, useSafeAreaInsets } from "react-native-safe-area-context";
import { Radii, Spacing, Typography, useAppColors, shadow, type AppColors } from "../../constants/tokens";
import { useWork, type WorkItem } from "../../store/work";
import {
  WorkEditor,
} from "../../components/work/WorkEditor";
import { MarkdownView } from "../../components/work/MarkdownView";
import { wsClient } from "../../services/websocket";
import {
  ActionMenu,
  Button,
  EmptyState,
  IconButton,
  InlineNotice,
  StatusPill,
  confirmDestructive,
} from "../../components/ui";
import type { StatusTone } from "../../components/ui/StatusPill";

const AUTOSAVE_DELAY_MS = 600;

function workItemKey(serverId: string, id: string) {
  return `${serverId}:${id}`;
}

export default function WorkDetailScreen() {
  const { hydrated, currentServerId, isCurrentServer } = useCurrentServer();
  const focused = useIsFocused();
  const router = useRouter();
  const params = useLocalSearchParams<{ id?: string; serverId?: string }>();
  useEffect(() => {
    if (focused && hydrated && !isCurrentServer(params.serverId)) router.replace("/(primary)/list");
  }, [focused, hydrated, currentServerId, isCurrentServer, params.serverId, router]);
  if (!hydrated || !isCurrentServer(params.serverId)) return null;
  return <CurrentWorkDetail key={`${params.serverId}:${params.id}`} />;
}

function CurrentWorkDetail() {
  const params = useLocalSearchParams<{ id?: string; serverId?: string }>();
  const { isCurrentServer } = useCurrentServer();
  const router = useRouter();
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const insets = useSafeAreaInsets();
  const { state, dispatch } = useWork();

  const itemId = typeof params.id === "string" ? params.id : "";
  const serverId = typeof params.serverId === "string" ? params.serverId : "";
  const item = state.byKey[workItemKey(serverId, itemId)] as WorkItem | undefined;
  const storedDraft = state.draftsByKey[workItemKey(serverId, itemId)];

  const [draftBody, setDraftBody] = useState(storedDraft?.body ?? item?.body ?? "");
  const [baseMtime, setBaseMtime] = useState(storedDraft?.baseMtime ?? item?.mtime ?? "");
  const [dirty, setDirty] = useState(Boolean(storedDraft));
  const [remoteBanner, setRemoteBanner] = useState(false);
  const [saving, setSaving] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const savingRef = useRef(false);
  const draftBodyRef = useRef(draftBody);
  useEffect(() => {
    draftBodyRef.current = draftBody;
  }, [draftBody]);

  useEffect(() => {
    if (!item) {
      return;
    }
    if (!dirty || item.body === draftBody) {
      if (dirty) {
        dispatch({ type: "WORK_DRAFT_SAVED", serverId, id: itemId, body: draftBody, baseMtime, mtime: item.mtime });
        setDirty(false);
      }
      setDraftBody(item.body);
      setBaseMtime(item.mtime);
      setRemoteBanner(false);
      return;
    }
    if (item.mtime !== baseMtime && item.body !== draftBody) {
      setRemoteBanner(true);
    }
  }, [baseMtime, dirty, draftBody, item, dispatch, serverId, itemId]);

  const saveWorkItem = async (frontmatter = item?.frontmatter) => {
    if (!item || !isCurrentServer(serverId) || !frontmatter) {
      return null;
    }
    if (savingRef.current) {
      return null;
    }
    savingRef.current = true;
    setSaving(true);
    const bodyAtSave = draftBodyRef.current;
    try {
      const written = await wsClient.writeWorkItem(serverId, {
        id: item.id,
        project: item.project,
        path: item.path,
        body: bodyAtSave,
        frontmatter,
        baseMtime,
      });
      dispatch({ type: "WORK_DRAFT_SAVED", serverId, id: itemId, body: bodyAtSave, baseMtime, mtime: written.mtime });
      if (!isCurrentServer(serverId)) return written;
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
      if (!isCurrentServer(serverId)) return null;
      if (error?.code === "conflict" && error?.current) {
        setRemoteBanner(true);
        setBaseMtime(error.current.mtime || baseMtime);
      } else {
        Alert.alert("Save failed", error?.message || "Could not save work item.");
      }
      return null;
    } finally {
      savingRef.current = false;
      setSaving(false);
    }
  };

  // Debounced autosave whenever the body changes.
  useEffect(() => {
    if (!dirty || remoteBanner || !item) {
      return;
    }
    const timer = setTimeout(() => {
      void saveWorkItem();
    }, AUTOSAVE_DELAY_MS);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dirty, draftBody, remoteBanner]);

  const handleToggleDone = async () => {
    if (!item) {
      return;
    }
    const nextFrontmatter = {
      ...item.frontmatter,
      done: item.frontmatter.done ? null : new Date().toISOString(),
    };
    await saveWorkItem(nextFrontmatter);
  };

  const handleDelete = () => {
    if (!item || !serverId) {
      return;
    }
    confirmDestructive({
      title: "Delete work item?",
      message: "This removes the Markdown work file from the server.",
      confirmLabel: "Delete",
      onConfirm: async () => {
        if (!isCurrentServer(serverId)) return;
        try {
          await wsClient.deleteWorkItem(serverId, item.id);
          if (!isCurrentServer(serverId)) return;
          router.back();
        } catch (error: any) {
          if (!isCurrentServer(serverId)) return;
          Alert.alert(
            "Delete failed",
            error?.message || "Could not delete work item.",
          );
        }
      },
    });
  };

  const toggleEditing = async () => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    if (editing && dirty && !remoteBanner) {
      await saveWorkItem();
    }
    setEditing((prev) => !prev);
  };

  if (!item) {
    return (
      <SafeAreaView style={styles.emptyScreen} edges={["top"]}>
        <EmptyState
          icon="document-text-outline"
          title="Work item not found"
          detail="It may have been deleted or belongs to another server."
          action={{ label: "Go back", icon: "chevron-back", onPress: () => router.back() }}
        />
      </SafeAreaView>
    );
  }

  const done = !!item.frontmatter.done;
  const draftTitle = workItemTitle(item) || titleFromMarkdown(draftBody) || "Untitled work";
  const status = workStatusInfo(item);
  const updatedLabel = relativeTime(item.mtime || item.frontmatter.created);
  const previewBody = stripLeadingTitle(draftBody);
  const headerTitle = item.project;
  const contextLabel = `${item.serverName ? `${item.serverName} · ${item.project}` : item.project}${
    updatedLabel ? ` · ${updatedLabel}` : ""
  }`;

  return (
    <SafeAreaView style={styles.screen} edges={["top"]}>
      <KeyboardAvoidingView
        style={styles.kav}
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        <View style={styles.header}>
          <IconButton
            icon="chevron-back"
            iconSize={21}
            size={40}
            accessibilityLabel="Back"
            onPress={() => router.back()}
          />

          <View style={styles.headerCenter}>
            <Text style={styles.headerTitle} numberOfLines={1}>
              {headerTitle}
            </Text>
          </View>

          <IconButton
            icon={editing ? "eye-outline" : "create-outline"}
            size={40}
            accessibilityLabel={editing ? "Preview" : "Edit"}
            onPress={() => void toggleEditing()}
          />

          <IconButton
            icon="ellipsis-horizontal"
            size={40}
            accessibilityLabel="Work actions"
            onPress={() => setMenuOpen(true)}
          />
        </View>

        <View style={styles.context}>
          <View style={styles.statusRow}>
            <StatusPill
              label={status.label}
              tone={status.tone}
              live={status.tone === "accent"}
            />
            <Text style={styles.contextPath} numberOfLines={1}>
              {contextLabel}
            </Text>
          </View>

          <Text style={styles.workTitle} numberOfLines={2}>
            {draftTitle}
          </Text>
        </View>

        {remoteBanner ? (
          <InlineNotice
            tone="accent"
            icon="cloud-download-outline"
            title="Newer version on the server"
            detail={dirty ? "Loading it discards your unsaved edits." : null}
            style={styles.banner}
            action={{
              label: "Load remote changes",
              onPress: () => {
                setDraftBody(item.body);
                setBaseMtime(item.mtime);
                setDirty(false);
                setRemoteBanner(false);
                dispatch({ type: "WORK_DRAFT_DISCARDED", serverId, id: itemId });
              },
            }}
          />
        ) : null}

        <View style={styles.contentShell}>
          {editing ? (
            <WorkEditor
              value={draftBody}
              onChange={(next) => {
                draftBodyRef.current = next;
                setDraftBody(next);
                setDirty(true);
                dispatch({ type: "WORK_DRAFT_CHANGED", serverId, id: itemId, body: next, baseMtime });
              }}
              onBlur={() => {
                if (dirty && !remoteBanner) {
                  void saveWorkItem();
                }
              }}
            />
          ) : (
            <ScrollView
              style={styles.previewScroll}
              contentContainerStyle={styles.previewContent}
              showsVerticalScrollIndicator={false}
            >
              <MarkdownView value={previewBody} />
            </ScrollView>
          )}
        </View>

        <View style={[styles.footer, { paddingBottom: Math.max(insets.bottom, Spacing.md) }]}>
          <SaveState saving={saving} dirty={dirty} />
          <View style={styles.footerActions}>
            <Button
              label={done ? "Reopen" : "Mark done"}
              icon={done ? "return-up-back-outline" : "checkmark"}
              variant={done ? "plain" : "tinted"}
              size="sm"
              loading={saving}
              onPress={() => void handleToggleDone()}
            />
          </View>
        </View>
      </KeyboardAvoidingView>

      <ActionMenu
        visible={menuOpen}
        title={draftTitle}
        onClose={() => setMenuOpen(false)}
        items={[
          {
            key: "done",
            label: done ? "Reopen" : "Mark done",
            icon: done ? "refresh-outline" : "checkmark-circle-outline",
            onPress: () => void handleToggleDone(),
          },
          {
            key: "delete",
            label: "Delete",
            icon: "trash-outline",
            destructive: true,
            onPress: handleDelete,
          },
        ]}
      />
    </SafeAreaView>
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

function stripLeadingTitle(value: string): string {
  return value.replace(/^\s*#\s+.+?(?:\r?\n){1,2}/, "");
}

type WorkStatus = "queued" | "running" | "blocked" | "done" | "failed" | "removed" | "unknown";

function workItemStatus(item: WorkItem): WorkStatus {
  const raw =
    typeof item.frontmatter.status === "string"
      ? item.frontmatter.status.trim().toLowerCase()
      : "";
  switch (raw) {
    case "failed":
    case "blocked":
    case "done":
    case "removed":
    case "running":
    case "unknown":
      return raw;
    default:
      break;
  }
  if (item.frontmatter.done) {
    return "done";
  }
  if (item.frontmatter.started) {
    return "running";
  }
  return "queued";
}

function relativeTime(iso: string): string {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) {
    return "";
  }
  const diff = Math.floor((Date.now() - then) / 1000);
  if (diff < 60) return "now";
  if (diff < 3600) return `${Math.floor(diff / 60)}m`;
  if (diff < 86_400) return `${Math.floor(diff / 3600)}h`;
  if (diff < 86_400 * 30) return `${Math.floor(diff / 86_400)}d`;
  return `${Math.floor(diff / (86_400 * 30))}mo`;
}

function workItemTitle(item: WorkItem): string {
  return usefulInlineText(item.frontmatter.title?.trim() || item.title);
}

function usefulInlineText(value?: string): string {
  return (value || "")
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1")
    .replace(/\*\*([^*]+)\*\*/g, "$1")
    .replace(/`([^`]+)`/g, "$1")
    .trim();
}

function workStatusInfo(item: WorkItem): {
  label: string;
  tone: StatusTone;
} {
  switch (workItemStatus(item)) {
    case "failed":
      return { label: "Failed", tone: "danger" };
    case "blocked":
      return { label: "Blocked", tone: "warning" };
    case "done":
      return { label: "Done", tone: "success" };
    case "removed":
      return { label: "Removed", tone: "neutral" };
    case "running":
      return { label: "Running", tone: "accent" };
    case "unknown":
      return { label: "Unknown", tone: "neutral" };
    case "queued":
      return { label: "Queued", tone: "neutral" };
  }
}

function SaveState({ saving, dirty }: { saving: boolean; dirty: boolean }) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
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

function createStyles(colors: AppColors) {
  return StyleSheet.create({
  screen: {
    flex: 1,
    backgroundColor: colors.bgPrimary,
  },
  kav: {
    flex: 1,
  },
  emptyScreen: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: colors.bgPrimary,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 12,
    paddingTop: 4,
    paddingBottom: 8,
  },
  headerCenter: {
    flex: 1,
    alignItems: "center",
    minWidth: 0,
  },
  headerTitle: {
    color: colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 15,
    lineHeight: 18,
  },
  context: {
    paddingHorizontal: 16,
    paddingTop: 6,
    paddingBottom: 11,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.borderSubtle,
  },
  statusRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.sm,
  },
  contextPath: {
    flex: 1,
    color: colors.textTertiary,
    fontFamily: Typography.uiFont,
    fontSize: 11,
    lineHeight: 15,
  },
  workTitle: {
    marginTop: 9,
    color: colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 22,
    lineHeight: 28,
    letterSpacing: -0.3,
  },
  banner: {
    marginHorizontal: 16,
    marginBottom: 8,
  },
  contentShell: {
    flex: 1,
    marginHorizontal: 10,
    marginTop: 8,
    marginBottom: 8,
    borderRadius: Radii.md,
    overflow: "hidden",
    backgroundColor: colors.bgSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.borderSubtle,
    ...shadow("card", colors.shadowColor),
  },
  previewScroll: {
    flex: 1,
  },
  previewContent: {
    paddingBottom: Spacing.lg,
  },
  footer: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: Spacing.sm,
    paddingHorizontal: 14,
    paddingTop: 8,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.borderSubtle,
    backgroundColor: colors.bgPrimary,
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
    backgroundColor: colors.statusUnknown,
  },
  unsavedDot: {
    backgroundColor: colors.statusUnknown,
  },
  savedDot: {
    backgroundColor: colors.statusDone,
  },
  savingText: {
    color: colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
  },
  savedText: {
    color: colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
    opacity: 0.46,
  },
  });
}
