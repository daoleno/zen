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
import { useLocalSearchParams, useRouter } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { SafeAreaView, useSafeAreaInsets } from "react-native-safe-area-context";
import { Colors, Radii, Spacing, Typography, useAppColors, shadow } from "../../constants/tokens";
import { useWork, type WorkItem } from "../../store/work";
import {
  WorkEditor,
} from "../../components/work/WorkEditor";
import { MarkdownView } from "../../components/work/MarkdownView";
import {
  relativeTime,
  workItemStatus,
  workItemTitle,
} from "../../components/work/WorkRow";
import { wsClient } from "../../services/websocket";
import { AnimatedPressable } from "../../components/ui/AnimatedPressable";
import { RisingSheet } from "../../components/ui/RisingSheet";

const AUTOSAVE_DELAY_MS = 600;
type IconName = React.ComponentProps<typeof Ionicons>["name"];

function workItemKey(serverId: string, id: string) {
  return `${serverId}:${id}`;
}

export default function WorkDetailScreen() {
  const params = useLocalSearchParams<{ id?: string; serverId?: string }>();
  const router = useRouter();
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const insets = useSafeAreaInsets();
  const { state } = useWork();

  const itemId = typeof params.id === "string" ? params.id : "";
  const serverId = typeof params.serverId === "string" ? params.serverId : "";
  const item = state.byKey[workItemKey(serverId, itemId)] as WorkItem | undefined;

  const [draftBody, setDraftBody] = useState(item?.body ?? "");
  const [baseMtime, setBaseMtime] = useState(item?.mtime ?? "");
  const [dirty, setDirty] = useState(false);
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
    if (!dirty) {
      setDraftBody(item.body);
      setBaseMtime(item.mtime);
      setRemoteBanner(false);
      return;
    }
    if (item.mtime !== baseMtime && item.body !== draftBody) {
      setRemoteBanner(true);
    }
  }, [baseMtime, dirty, draftBody, item]);

  const saveWorkItem = async (frontmatter = item?.frontmatter) => {
    if (!item || !serverId || !frontmatter) {
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
    Alert.alert("Delete work item", "Remove this Markdown work file?", [
      { text: "Cancel", style: "cancel" },
      {
        text: "Delete",
        style: "destructive",
        onPress: () => {
          void (async () => {
            try {
              await wsClient.deleteWorkItem(serverId, item.id);
              router.back();
            } catch (error: any) {
              Alert.alert(
                "Delete failed",
                error?.message || "Could not delete work item.",
              );
            }
          })();
        },
      },
    ]);
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
        <Text style={styles.emptyTitle}>Brain item not found</Text>
      </SafeAreaView>
    );
  }

  const brainLog = item.frontmatter.kind === "brain_log";
  const done = !brainLog && !!item.frontmatter.done;
  const draftTitle = workItemTitle(item) || titleFromMarkdown(draftBody) || "Untitled work";
  const status = workStatusInfo(item, colors);
  const updatedLabel = relativeTime(item.mtime || item.frontmatter.created);
  const previewBody = stripLeadingTitle(draftBody);
  const headerTitle = brainLog ? "Brain" : item.project;
  const contextLabel = brainLog
    ? [item.serverName, updatedLabel].filter(Boolean).join(" · ")
    : `${item.serverName ? `${item.serverName} · ${item.project}` : item.project}${
        updatedLabel ? ` · ${updatedLabel}` : ""
      }`;

  return (
    <SafeAreaView style={styles.screen} edges={["top"]}>
      <KeyboardAvoidingView
        style={styles.kav}
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        <View style={styles.header}>
          <AnimatedPressable
            onPress={() => {
              Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
              router.back();
            }}
            preset="press"
            scale={0.88}
            style={styles.iconButton}
          >
            <Ionicons name="chevron-back" size={22} color={colors.textPrimary} />
          </AnimatedPressable>

          <View style={styles.headerCenter}>
            <Text style={styles.headerTitle} numberOfLines={1}>
              {headerTitle}
            </Text>
          </View>

          <AnimatedPressable
            onPress={() => void toggleEditing()}
            preset="press"
            scale={0.88}
            style={styles.iconButton}
          >
            <Ionicons
              name={editing ? "eye-outline" : "create-outline"}
              size={19}
              color={colors.textPrimary}
            />
          </AnimatedPressable>

          <AnimatedPressable
            onPress={() => {
              Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
              setMenuOpen(true);
            }}
            preset="press"
            scale={0.88}
            style={styles.iconButton}
          >
            <Ionicons name="ellipsis-horizontal" size={20} color={colors.textPrimary} />
          </AnimatedPressable>
        </View>

        <View style={styles.context}>
          <View style={styles.statusRow}>
            {brainLog ? null : (
              <StatusPill
                icon={status.icon}
                label={status.label}
                color={status.color}
              />
            )}
            <Text style={styles.contextPath} numberOfLines={1}>
              {contextLabel}
            </Text>
          </View>

          <Text style={styles.workTitle} numberOfLines={2}>
            {draftTitle}
          </Text>
        </View>

        {remoteBanner ? (
          <AnimatedPressable
            onPress={() => {
              Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
              setDraftBody(item.body);
              setBaseMtime(item.mtime);
              setDirty(false);
              setRemoteBanner(false);
            }}
            preset="press"
            scale={0.98}
            style={styles.banner}
          >
            <Ionicons
              name="cloud-download-outline"
              size={14}
              color={colors.textPrimary}
            />
            <Text style={styles.bannerText}>
              Remote changes — tap to load.
            </Text>
          </AnimatedPressable>
        ) : null}

        <View style={styles.contentShell}>
          {editing ? (
            <WorkEditor
              value={draftBody}
              onChange={(next) => {
                setDraftBody(next);
                setDirty(true);
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
            {brainLog ? null : (
              <AnimatedPressable
                onPress={() => {
                  Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                  void handleToggleDone();
                }}
                disabled={saving}
                preset="press"
                scale={0.94}
                style={[styles.secondaryButton, saving && styles.primaryButtonDisabled]}
              >
                <Ionicons
                  name={done ? "return-up-back-outline" : "checkmark"}
                  size={14}
                  color={colors.textPrimary}
                />
                <Text style={styles.secondaryButtonText}>{done ? "Reopen" : "Done"}</Text>
              </AnimatedPressable>
            )}
          </View>
        </View>
      </KeyboardAvoidingView>

      <OverflowMenu
        visible={menuOpen}
        onClose={() => setMenuOpen(false)}
        done={done}
        showDone={!brainLog}
        onToggleDone={() => {
          setMenuOpen(false);
          void handleToggleDone();
        }}
        onDelete={() => {
          setMenuOpen(false);
          handleDelete();
        }}
      />
    </SafeAreaView>
  );
}

function StatusPill({
  icon,
  label,
  color,
}: {
  icon: IconName;
  label: string;
  color: string;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);

  return (
    <View style={styles.statusPill}>
      <Ionicons name={icon} size={13} color={color} />
      <Text style={[styles.statusPillText, { color }]}>{label}</Text>
    </View>
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

function workStatusInfo(item: WorkItem, colors: typeof Colors = Colors): {
  icon: IconName;
  label: string;
  color: string;
} {
  switch (workItemStatus(item)) {
    case "failed":
      return { icon: "close-circle", label: "Failed", color: colors.statusFailed };
    case "blocked":
      return { icon: "alert-circle", label: "Blocked", color: colors.statusBlocked };
    case "done":
      return { icon: "checkmark-circle", label: "Done", color: colors.statusDone };
    case "removed":
      return { icon: "archive", label: "Removed", color: colors.statusDone };
    case "running":
      return { icon: "play-circle", label: "Running", color: colors.statusRunning };
    case "unknown":
      return { icon: "help-circle", label: "Unknown", color: colors.statusUnknown };
    case "queued":
      return { icon: "ellipse", label: "Queued", color: colors.accent };
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

function OverflowMenu({
  visible,
  onClose,
  done,
  showDone,
  onToggleDone,
  onDelete,
}: {
  visible: boolean;
  onClose: () => void;
  done: boolean;
  showDone: boolean;
  onToggleDone: () => void;
  onDelete: () => void;
}) {
  const colors = useAppColors();
  const menuStyles = useMemo(() => createMenuStyles(colors), [colors]);

  return (
    <RisingSheet visible={visible} onClose={onClose} align="bottom" cardStyle={menuStyles.card}>
      {showDone ? (
        <>
          <AnimatedPressable
            onPress={() => {
              Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
              onToggleDone();
            }}
            preset="press"
            scale={0.98}
            style={menuStyles.item}
          >
            <Ionicons
              name={done ? "refresh-outline" : "checkmark-circle-outline"}
              size={18}
              color={colors.textPrimary}
            />
            <Text style={menuStyles.itemText}>{done ? "Reopen" : "Mark done"}</Text>
          </AnimatedPressable>
          <View style={menuStyles.divider} />
        </>
      ) : null}
      <AnimatedPressable
        onPress={() => {
          Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
          onDelete();
        }}
        preset="press"
        scale={0.98}
        style={menuStyles.item}
      >
        <Ionicons name="trash-outline" size={18} color={colors.statusFailed} />
        <Text style={[menuStyles.itemText, menuStyles.itemTextDestructive]}>
          Delete
        </Text>
      </AnimatedPressable>
    </RisingSheet>
  );
}

function createStyles(colors: typeof Colors) {
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
  emptyTitle: {
    color: colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 17,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 10,
    paddingTop: 3,
    paddingBottom: 5,
  },
  iconButton: {
    width: 36,
    height: 36,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: Radii.pill,
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
  statusPill: {
    height: 22,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    paddingHorizontal: 8,
    borderRadius: Radii.pill,
    backgroundColor: colors.surfaceSubtle,
  },
  statusPillText: {
    fontFamily: Typography.terminalFont,
    fontSize: 10,
    lineHeight: 13,
    textTransform: "uppercase",
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
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.sm,
    marginHorizontal: Spacing.md,
    marginBottom: Spacing.sm,
    paddingHorizontal: Spacing.md,
    paddingVertical: Spacing.sm,
    borderRadius: Radii.md,
    backgroundColor: colors.warningSoft,
  },
  bannerText: {
    color: colors.textPrimary,
    fontFamily: Typography.uiFont,
    fontSize: 13,
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
  secondaryButton: {
    height: 34,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
    paddingHorizontal: 12,
    borderRadius: Radii.pill,
    backgroundColor: colors.surfacePressed,
  },
  secondaryButtonText: {
    color: colors.textPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 12,
    lineHeight: 16,
  },
  primaryButton: {
    height: 34,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    paddingHorizontal: 12,
    borderRadius: Radii.pill,
    backgroundColor: colors.accent,
  },
  primaryButtonPressed: {
    opacity: 0.85,
  },
  primaryButtonDisabled: {
    opacity: 0.5,
  },
  primaryButtonText: {
    color: colors.textOnAccent,
    fontFamily: Typography.uiFontMedium,
    fontSize: 13,
    lineHeight: 17,
  },
  });
}

function createMenuStyles(colors: typeof Colors) {
  return StyleSheet.create({
  card: {
    marginHorizontal: Spacing.md,
    marginBottom: Spacing.xl,
    borderRadius: Radii.lg,
    backgroundColor: colors.modalSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
    overflow: "hidden",
    paddingVertical: 6,
    ...shadow("float", colors.shadowColor),
  },
  item: {
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.md,
    paddingHorizontal: Spacing.lg,
    paddingVertical: Spacing.lg,
  },
  divider: {
    height: StyleSheet.hairlineWidth,
    backgroundColor: colors.borderSubtle,
    marginHorizontal: Spacing.lg,
  },
  itemText: {
    color: colors.textPrimary,
    fontFamily: Typography.uiFont,
    fontSize: 15,
  },
  itemTextDestructive: {
    color: colors.statusFailed,
  },
  });
}
