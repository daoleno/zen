import React, { useEffect, useMemo, useState } from "react";
import {
  Alert,
  AppState,
  KeyboardAvoidingView,
  Linking,
  Modal,
  Platform,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useLocalSearchParams, useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import {
  Radii,
  Spacing,
  TypeScale,
  Typography,
  useAppColors,
} from "../constants/tokens";
import {
  calendarDateKey,
  executes,
  formatCalendarTime,
  groupAgenda,
  itemInstant,
  kindLabel,
  viewerTimezone,
} from "../services/calendarPresentation";
import {
  formatResolvedInstant,
  localFieldsFromInstant,
  resolveLocalDateTime,
  type LocalDateTimeResolution,
} from "../services/calendarTime";
import {
  calendarNotificationState,
  requestCalendarNotifications,
  syncCalendarNotifications,
  type CalendarNotificationState,
} from "../services/calendarNotifications";
import { wsClient } from "../services/websocket";
import {
  useCalendar,
  type CalendarItem,
  type CalendarKind,
  type CalendarRecurrence,
} from "../store/calendar";

type Mode = "agenda" | "month" | "day";
type ServerItem = CalendarItem & { serverId: string; serverName: string };
const statuses: Record<CalendarItem["status"], string> = {
  scheduled: "Scheduled",
  waiting: "Waiting",
  running: "Running",
  completed: "Completed",
  failed: "Failed",
  cancelled: "Cancelled",
};
const statusIcons: Record<
  CalendarItem["status"],
  React.ComponentProps<typeof Ionicons>["name"]
> = {
  scheduled: "time-outline",
  waiting: "notifications-outline",
  running: "play-circle-outline",
  completed: "checkmark-circle-outline",
  failed: "alert-circle-outline",
  cancelled: "close-circle-outline",
};

type CalendarScreenProps = {
  initialMode?: Mode;
  notificationStateOverride?: CalendarNotificationState;
  initialError?: string;
  loading?: boolean;
};

export default function CalendarScreen(props: CalendarScreenProps = {}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const router = useRouter();
  const params = useLocalSearchParams<{ id?: string; serverId?: string }>();
  const state = useCalendar();
  const [mode, setMode] = useState<Mode>(props.initialMode ?? "agenda");
  const viewerZone = useMemo(() => viewerTimezone(), []);
  const [month, setMonth] = useState(() => new Date());
  const [selectedDay, setSelectedDay] = useState(() =>
    calendarDateKey(new Date(), viewerZone),
  );
  const [selected, setSelected] = useState<ServerItem | null>(null);
  const [editing, setEditing] = useState<ServerItem | "new" | null>(null);
  const [error, setError] = useState(props.initialError ?? "");
  const [notificationState, setNotificationState] =
    useState<CalendarNotificationState>(
      props.notificationStateOverride ?? "undetermined",
    );
  const items = useMemo(
    () =>
      Object.values(state.byServer)
        .flatMap((server) =>
          server.items.map((item) => ({
            ...item,
            serverId: server.serverId,
            serverName: server.serverName,
          })),
        )
        .sort((a, b) => Date.parse(a.next_at) - Date.parse(b.next_at)),
    [state],
  );
  const activeServer =
    Object.values(state.byServer).find((server) => server.hydrated) ?? null;
  useEffect(() => {
    if (props.notificationStateOverride) return;
    let active = true;
    const refreshPermission = async () => {
      const next = await calendarNotificationState();
      if (!active) return;
      setNotificationState(next);
      if (next === "granted") {
        await Promise.all(
          Object.values(state.byServer).map((server) =>
            syncCalendarNotifications(server.serverId, server.items),
          ),
        );
      }
    };
    void refreshPermission();
    const subscription = AppState.addEventListener("change", (next) => {
      if (next === "active") void refreshPermission();
    });
    return () => {
      active = false;
      subscription.remove();
    };
  }, [props.notificationStateOverride, state.byServer]);
  useEffect(() => {
    if (!params.id) return;
    const found = items.find(
      (item) =>
        item.id === params.id &&
        (!params.serverId || item.serverId === params.serverId),
    );
    if (found) setSelected(found);
  }, [items, params.id, params.serverId]);
  const visible = items.filter(
    (item) => item.status !== "cancelled" || mode !== "agenda",
  );
  const sections = groupAgenda(visible, new Date(), viewerZone);
  const dayItems = visible.filter(
    (item) => calendarDateKey(itemInstant(item), viewerZone) === selectedDay,
  );
  const monthDays = monthGrid(month);
  const openItem = (item: ServerItem) => {
    setError("");
    setSelected(item);
  };
  const actionError = (value: unknown) =>
    setError(
      value instanceof Error ? value.message : "Calendar action failed.",
    );
  const enableNotifications = async () => {
    if (notificationState === "denied") {
      try {
        await Linking.openSettings();
      } catch (value) {
        actionError(value);
      }
      return;
    }
    if (notificationState === "unavailable") return;
    const next = await requestCalendarNotifications();
    if (next !== "granted") {
      setNotificationState(next);
      return;
    }
    try {
      await Promise.all(
        Object.values(state.byServer).map((server) =>
          syncCalendarNotifications(server.serverId, server.items),
        ),
      );
      setNotificationState("granted");
    } catch (value) {
      setNotificationState("unavailable");
      actionError(value);
    }
  };
  const cancel = async (item: ServerItem) => {
    try {
      await wsClient.cancelCalendarItem(item.serverId, item.id, item.revision);
      setSelected(null);
    } catch (e) {
      actionError(e);
    }
  };
  const runNow = async (item: ServerItem) => {
    try {
      await wsClient.runCalendarItem(item.serverId, item.id);
    } catch (e) {
      actionError(e);
    }
  };
  return (
    <SafeAreaView edges={["bottom"]} style={styles.screen}>
      <View style={styles.toolbar}>
        <View style={styles.segment}>
          {(["agenda", "month", "day"] as Mode[]).map((value) => (
            <Pressable
              key={value}
              accessibilityRole="button"
              accessibilityState={{ selected: mode === value }}
              onPress={() => setMode(value)}
              style={[
                styles.segmentButton,
                mode === value && styles.segmentActive,
              ]}
            >
              <Text
                style={[
                  styles.segmentText,
                  mode === value && styles.segmentTextActive,
                ]}
              >
                {value[0].toUpperCase() + value.slice(1)}
              </Text>
            </Pressable>
          ))}
        </View>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Create calendar item"
          disabled={!activeServer}
          onPress={() => setEditing("new")}
          style={styles.addButton}
        >
          <Ionicons
            name="add"
            size={25}
            color={activeServer ? colors.textPrimary : colors.disabledText}
          />
        </Pressable>
      </View>
      {notificationState !== "granted" ? (
        <View style={styles.permission}>
          <Ionicons
            name="notifications-off-outline"
            size={18}
            color={colors.statusBlocked}
          />
          <View style={{ flex: 1 }}>
            <Text style={styles.permissionTitle}>
              {notificationState === "denied"
                ? "Reminder notifications are disabled"
                : notificationState === "unavailable"
                  ? "Reminder notifications are unavailable"
                  : "Reminder notifications are not enabled"}
            </Text>
          </View>
          {notificationState !== "unavailable" ? (
            <Pressable
              accessibilityRole="button"
              onPress={() => void enableNotifications()}
              style={styles.permissionAction}
            >
              <Text style={styles.permissionActionText}>
                {notificationState === "denied" ? "Open settings" : "Enable"}
              </Text>
            </Pressable>
          ) : null}
        </View>
      ) : null}
      {error ? (
        <View style={styles.errorBanner}>
          <Text style={styles.errorText}>{error}</Text>
        </View>
      ) : null}
      {mode === "agenda" ? (
        <ScrollView contentContainerStyle={styles.content}>
          {props.loading ? (
            <View
              accessibilityRole="progressbar"
              accessibilityLabel="Loading calendar commitments"
              style={styles.empty}
            >
              <Ionicons name="sync-outline" size={34} color={colors.accent} />
              <Text style={styles.emptyTitle}>Loading commitments…</Text>
            </View>
          ) : sections.length ? (
            sections.map((section) => (
              <View key={section.title} style={styles.section}>
                <Text style={styles.sectionTitle}>{section.title}</Text>
                {section.data.map((raw) => (
                  <CalendarRow
                    key={`${(raw as ServerItem).serverId}:${raw.id}`}
                    item={raw as ServerItem}
                    onPress={() => openItem(raw as ServerItem)}
                  />
                ))}
              </View>
            ))
          ) : (
            <EmptyState
              connected={Boolean(activeServer)}
              onCreate={() => setEditing("new")}
            />
          )}
        </ScrollView>
      ) : null}
      {mode === "month" ? (
        <ScrollView contentContainerStyle={styles.content}>
          <MonthHeader month={month} onChange={setMonth} />
          <View style={styles.weekdays}>
            {["S", "M", "T", "W", "T", "F", "S"].map((label, index) => (
              <Text key={`${label}${index}`} style={styles.weekday}>
                {label}
              </Text>
            ))}
          </View>
          <View style={styles.monthGrid}>
            {monthDays.map((date) => {
              const key = dayKey(date);
              const inMonth = date.getMonth() === month.getMonth();
              const count = visible.filter(
                (item) =>
                  calendarDateKey(itemInstant(item), viewerZone) === key,
              ).length;
              return (
                <Pressable
                  key={key}
                  accessibilityRole="button"
                  accessibilityLabel={`${date.toDateString()}, ${count} items`}
                  onPress={() => {
                    setSelectedDay(key);
                    setMode("day");
                  }}
                  style={[
                    styles.dayCell,
                    key === selectedDay && styles.daySelected,
                  ]}
                >
                  <Text style={[styles.dayNumber, !inMonth && styles.dayMuted]}>
                    {date.getDate()}
                  </Text>
                  {count ? <View style={styles.dayDot} /> : null}
                </Pressable>
              );
            })}
          </View>
        </ScrollView>
      ) : null}
      {mode === "day" ? (
        <ScrollView contentContainerStyle={styles.content}>
          <MonthHeader
            month={new Date(`${selectedDay}T12:00:00`)}
            onChange={(date) => setSelectedDay(dayKey(date))}
          />
          <Text style={styles.dayHeading}>
            {new Date(`${selectedDay}T12:00:00`).toLocaleDateString(undefined, {
              weekday: "long",
              month: "long",
              day: "numeric",
            })}
          </Text>
          {dayItems.length ? (
            dayItems.map((item) => (
              <CalendarRow
                key={`${item.serverId}:${item.id}`}
                item={item}
                onPress={() => openItem(item)}
              />
            ))
          ) : (
            <Text style={styles.emptyBody}>
              Nothing committed for this day.
            </Text>
          )}
        </ScrollView>
      ) : null}
      <DetailModal
        item={selected}
        error={error}
        onClose={() => {
          setSelected(null);
          setError("");
        }}
        onEdit={(item) => {
          setSelected(null);
          setEditing(item);
        }}
        onCancel={cancel}
        onRun={runNow}
        onOpenWork={(item) =>
          router.push({
            pathname: "/work/[id]",
            params: { id: item.linked_work_id!, serverId: item.serverId },
          })
        }
      />
      <EditorModal
        value={editing}
        serverId={
          editing === "new"
            ? activeServer?.serverId
            : (editing as ServerItem | null)?.serverId
        }
        onClose={() => setEditing(null)}
        onSaved={() => setEditing(null)}
        onError={actionError}
      />
    </SafeAreaView>
  );
}

function CalendarRow({ item, onPress }: { item: ServerItem; onPress(): void }) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${item.title}, ${kindLabel[item.kind]}, ${statuses[item.status]}`}
      onPress={onPress}
      style={({ pressed }) => [styles.row, pressed && styles.rowPressed]}
    >
      <View style={styles.timeRail}>
        <Text style={styles.rowTime}>
          {new Date(itemInstant(item)).toLocaleTimeString(undefined, {
            hour: "numeric",
            minute: "2-digit",
            timeZone: item.timezone,
          })}
        </Text>
        <View
          style={[
            styles.kindRail,
            {
              backgroundColor:
                item.status === "failed"
                  ? colors.statusFailed
                  : item.status === "running"
                    ? colors.statusRunning
                    : colors.accent,
            },
          ]}
        />
      </View>
      <View style={styles.rowMain}>
        <Text style={styles.rowTitle} numberOfLines={2}>
          {item.title}
        </Text>
        <View style={styles.meta}>
          <Text style={styles.metaText}>{kindLabel[item.kind]}</Text>
          <Text style={styles.metaDot}>·</Text>
          <Ionicons
            name={statusIcons[item.status]}
            size={13}
            color={
              item.status === "failed"
                ? colors.statusFailed
                : colors.textTertiary
            }
          />
          <Text style={styles.metaText}>{statuses[item.status]}</Text>
          {executes(item) ? (
            <>
              <Text style={styles.metaDot}>·</Text>
              <Text style={styles.executeText}>Zen executes</Text>
            </>
          ) : null}
        </View>
      </View>
      <Ionicons name="chevron-forward" size={17} color={colors.textTertiary} />
    </Pressable>
  );
}
function MonthHeader({
  month,
  onChange,
}: {
  month: Date;
  onChange(date: Date): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  return (
    <View style={styles.monthHeader}>
      <Pressable
        accessibilityLabel="Previous month"
        onPress={() =>
          onChange(new Date(month.getFullYear(), month.getMonth() - 1, 1))
        }
        style={styles.iconButton}
      >
        <Ionicons name="chevron-back" size={20} color={colors.textPrimary} />
      </Pressable>
      <Text style={styles.monthTitle}>
        {month.toLocaleDateString(undefined, {
          month: "long",
          year: "numeric",
        })}
      </Text>
      <Pressable
        accessibilityLabel="Next month"
        onPress={() =>
          onChange(new Date(month.getFullYear(), month.getMonth() + 1, 1))
        }
        style={styles.iconButton}
      >
        <Ionicons name="chevron-forward" size={20} color={colors.textPrimary} />
      </Pressable>
    </View>
  );
}
function EmptyState({
  connected,
  onCreate,
}: {
  connected: boolean;
  onCreate(): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  return (
    <View style={styles.empty}>
      <Ionicons
        name="calendar-clear-outline"
        size={38}
        color={colors.textTertiary}
      />
      <Text style={styles.emptyTitle}>
        {connected ? "Your time is clear" : "Calendar is offline"}
      </Text>
      <Text style={styles.emptyBody}>
        {connected
          ? "Commitments created by you or Brain will appear here."
          : "Pair or reconnect a Zen daemon to sync commitments."}
      </Text>
      {connected ? (
        <Pressable onPress={onCreate} style={styles.primaryButton}>
          <Text style={styles.primaryButtonText}>Create item</Text>
        </Pressable>
      ) : null}
    </View>
  );
}

function DetailModal({
  item,
  error,
  onClose,
  onEdit,
  onCancel,
  onRun,
  onOpenWork,
}: {
  item: ServerItem | null;
  error: string;
  onClose(): void;
  onEdit(item: ServerItem): void;
  onCancel(item: ServerItem): void;
  onRun(item: ServerItem): void;
  onOpenWork(item: ServerItem): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  if (!item) return null;
  const canRun =
    item.kind === "scheduled_action" &&
    item.status !== "running" &&
    item.status !== "cancelled";
  return (
    <Modal visible transparent animationType="slide" onRequestClose={onClose}>
      <View style={styles.modalBackdrop}>
        <SafeAreaView edges={["bottom"]} style={styles.sheet}>
          <View style={styles.sheetHandle} />
          <View style={styles.detailHeader}>
            <View style={{ flex: 1 }}>
              <Text style={styles.detailKind}>
                {kindLabel[item.kind]} · {statuses[item.status]}
              </Text>
              <Text style={styles.detailTitle}>{item.title}</Text>
            </View>
            <Pressable
              accessibilityLabel="Close"
              onPress={onClose}
              style={styles.iconButton}
            >
              <Ionicons name="close" size={23} color={colors.textPrimary} />
            </Pressable>
          </View>
          <ScrollView contentContainerStyle={styles.detailContent}>
            <Info
              label={
                item.kind === "reminder"
                  ? "Notify"
                  : item.kind === "event"
                    ? "Time"
                    : "Due"
              }
              value={formatCalendarTime(item)}
            />
            {item.end_at ? (
              <Info
                label="Ends"
                value={new Intl.DateTimeFormat(undefined, {
                  dateStyle: "medium",
                  timeStyle: "short",
                  timeZone: item.timezone,
                }).format(new Date(item.end_at))}
              />
            ) : null}
            <Info label="Timezone" value={item.timezone} />
            <Info label="Recurrence" value={item.recurrence} />
            {item.notes ? <Info label="Notes" value={item.notes} /> : null}
            {item.action_instruction ? (
              <Info label="Zen action" value={item.action_instruction} />
            ) : null}
            {item.source_thread_id ? (
              <Info label="Source Brain thread" value={item.source_thread_id} />
            ) : null}
            {item.linked_work_id ? (
              <Pressable
                onPress={() => onOpenWork(item)}
                style={styles.linkRow}
              >
                <Text style={styles.infoLabel}>Linked Work</Text>
                <Text style={styles.linkText}>{item.linked_work_id} ›</Text>
              </Pressable>
            ) : null}
            {item.failure_reason ? (
              <View style={styles.failure}>
                <Text style={styles.failureTitle}>Why it failed</Text>
                <Text style={styles.failureBody}>{item.failure_reason}</Text>
              </View>
            ) : null}
            {error ? <Text style={styles.errorText}>{error}</Text> : null}
          </ScrollView>
          <View style={styles.actions}>
            {canRun ? (
              <Pressable
                onPress={() => void onRun(item)}
                style={styles.primaryAction}
              >
                <Text style={styles.primaryButtonText}>
                  {item.status === "failed" ? "Retry now" : "Run now"}
                </Text>
              </Pressable>
            ) : null}
            <Pressable
              disabled={item.status === "running"}
              onPress={() => onEdit(item)}
              style={styles.secondaryAction}
            >
              <Text style={styles.secondaryText}>Edit</Text>
            </Pressable>
            <Pressable
              disabled={
                item.status === "running" || item.status === "cancelled"
              }
              onPress={() =>
                Alert.alert(
                  "Cancel calendar item?",
                  "It will remain in Calendar as cancelled.",
                  [
                    { text: "Keep", style: "cancel" },
                    {
                      text: "Cancel item",
                      style: "destructive",
                      onPress: () => void onCancel(item),
                    },
                  ],
                )
              }
              style={styles.secondaryAction}
            >
              <Text style={styles.cancelText}>Cancel</Text>
            </Pressable>
          </View>
        </SafeAreaView>
      </View>
    </Modal>
  );
}
function Info({ label, value }: { label: string; value: string }) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  return (
    <View style={styles.info}>
      <Text style={styles.infoLabel}>{label}</Text>
      <Text style={styles.infoValue}>{value}</Text>
    </View>
  );
}

function EditorModal({
  value,
  serverId,
  onClose,
  onSaved,
  onError,
}: {
  value: ServerItem | "new" | null;
  serverId?: string;
  onClose(): void;
  onSaved(): void;
  onError(error: unknown): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const existing = value && value !== "new" ? value : null;
  const [title, setTitle] = useState("");
  const [kind, setKind] = useState<CalendarKind>("reminder");
  const [atDate, setAtDate] = useState("");
  const [atTime, setAtTime] = useState("");
  const [endDate, setEndDate] = useState("");
  const [endTime, setEndTime] = useState("");
  const [atOccurrence, setAtOccurrence] = useState<0 | 1 | null>(null);
  const [endOccurrence, setEndOccurrence] = useState<0 | 1 | null>(null);
  const [timezone, setTimezone] = useState(
    Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  );
  const [recurrence, setRecurrence] = useState<CalendarRecurrence>("none");
  const [notes, setNotes] = useState("");
  const [instruction, setInstruction] = useState("");
  const [saving, setSaving] = useState(false);
  const editorSessionId =
    value === "new"
      ? "new"
      : value
        ? `${value.serverId}:${value.id}`
        : null;
  useEffect(() => {
    if (!value) return;
    const initial = existing
      ? itemInstant(existing)
      : new Date(Date.now() + 3600000).toISOString();
    setTitle(existing?.title ?? "");
    setKind(existing?.kind ?? "reminder");
    const zone =
      existing?.timezone ??
      Intl.DateTimeFormat().resolvedOptions().timeZone ??
      "UTC";
    const atFields = localFieldsFromInstant(initial, zone);
    const endFields = localFieldsFromInstant(
      existing?.end_at ?? new Date(Date.parse(initial) + 3600000).toISOString(),
      zone,
    );
    setAtDate(atFields.date);
    setAtTime(atFields.time);
    setEndDate(endFields.date);
    setEndTime(endFields.time);
    setAtOccurrence(null);
    setEndOccurrence(null);
    setTimezone(zone);
    setRecurrence(existing?.recurrence ?? "none");
    setNotes(existing?.notes ?? "");
    setInstruction(existing?.action_instruction ?? "");
  }, [editorSessionId]);
  const atResolution = useMemo(
    () => resolveLocalDateTime(atDate, atTime, timezone),
    [atDate, atTime, timezone],
  );
  const endResolution = useMemo(
    () => resolveLocalDateTime(endDate, endTime, timezone),
    [endDate, endTime, timezone],
  );
  if (!value) return null;
  const atInstant = chosenInstant(atResolution, atOccurrence);
  const endInstant = chosenInstant(endResolution, endOccurrence);
  const timeValidation =
    atResolution.status === "resolved" ||
    (atResolution.status === "ambiguous" && atOccurrence !== null)
      ? kind !== "event" ||
        endResolution.status === "resolved" ||
        (endResolution.status === "ambiguous" && endOccurrence !== null)
        ? kind !== "event" ||
          !atInstant ||
          !endInstant ||
          endInstant > atInstant
          ? ""
          : "Event end must be after its start."
        : endResolution.message
      : atResolution.message;
  const save = async () => {
    if (!serverId) return;
    setSaving(true);
    try {
      if (!title.trim()) throw new Error("Title is required.");
      if (!atInstant) throw new Error(resolutionMessage(atResolution));
      if (kind === "event" && !endInstant)
        throw new Error(resolutionMessage(endResolution));
      if (kind === "event" && endInstant! <= atInstant)
        throw new Error("Event end must be after its start.");
      const payload: any = {
        ...(existing ?? {}),
        title: title.trim(),
        kind,
        timezone: timezone.trim(),
        recurrence,
        notes: notes.trim(),
        action_instruction:
          kind === "scheduled_action" ? instruction.trim() : undefined,
      };
      delete payload.start_at;
      delete payload.end_at;
      delete payload.notify_at;
      delete payload.due_at;
      if (kind === "event") {
        payload.start_at = atInstant;
        payload.end_at = endInstant;
      } else if (kind === "reminder") payload.notify_at = atInstant;
      else payload.due_at = atInstant;
      if (existing) await wsClient.updateCalendarItem(serverId, payload);
      else await wsClient.createCalendarItem(serverId, payload);
      onSaved();
    } catch (e) {
      onError(e);
    } finally {
      setSaving(false);
    }
  };
  return (
    <Modal visible transparent animationType="slide" onRequestClose={onClose}>
      <KeyboardAvoidingView
        behavior={Platform.OS === "ios" ? "padding" : "height"}
        style={styles.modalBackdrop}
      >
        <SafeAreaView edges={["bottom"]} style={styles.editor}>
          <View style={styles.editorHeader}>
            <Pressable onPress={onClose} style={styles.headerAction}>
              <Text style={styles.secondaryText}>Cancel</Text>
            </Pressable>
            <Text style={styles.editorTitle}>
              {existing ? "Edit item" : "New item"}
            </Text>
            <Pressable
              disabled={saving || !serverId}
              onPress={() => void save()}
              style={styles.headerAction}
            >
              <Text style={styles.saveText}>{saving ? "Saving…" : "Save"}</Text>
            </Pressable>
          </View>
          <ScrollView
            style={styles.editorScroll}
            keyboardShouldPersistTaps="handled"
            keyboardDismissMode="on-drag"
            contentContainerStyle={styles.form}
          >
            <Field
              label="Title"
              value={title}
              onChangeText={setTitle}
              placeholder="What is the commitment?"
            />
            <Text style={styles.fieldLabel}>Kind</Text>
            <View style={styles.chips}>
              {(
                [
                  "event",
                  "reminder",
                  "deadline",
                  "scheduled_action",
                ] as CalendarKind[]
              ).map((option) => (
                <Chip
                  key={option}
                  selected={kind === option}
                  label={kindLabel[option]}
                  onPress={() => setKind(option)}
                />
              ))}
            </View>
            <Text style={styles.fieldLabel}>
              {kind === "event"
                ? "Starts"
                : kind === "reminder"
                  ? "Notify at"
                  : "Due at"}
            </Text>
            <View style={styles.dateTimeRow}>
              <Field
                label="Local date"
                value={atDate}
                onChangeText={(text) => {
                  setAtDate(text);
                  setAtOccurrence(null);
                }}
                placeholder="2026-07-14"
                autoCapitalize="none"
                keyboardType="numbers-and-punctuation"
                containerStyle={styles.dateField}
              />
              <Field
                label="Local time"
                value={atTime}
                onChangeText={(text) => {
                  setAtTime(text);
                  setAtOccurrence(null);
                }}
                placeholder="18:20"
                autoCapitalize="none"
                keyboardType="numbers-and-punctuation"
                containerStyle={styles.timeField}
              />
            </View>
            <AmbiguityChoice
              resolution={atResolution}
              selected={atOccurrence}
              onSelect={setAtOccurrence}
              timezone={timezone}
            />
            {kind === "event" ? (
              <>
                <Text style={styles.fieldLabel}>Ends</Text>
                <View style={styles.dateTimeRow}>
                  <Field
                    label="Local date"
                    value={endDate}
                    onChangeText={(text) => {
                      setEndDate(text);
                      setEndOccurrence(null);
                    }}
                    placeholder="2026-07-14"
                    autoCapitalize="none"
                    keyboardType="numbers-and-punctuation"
                    containerStyle={styles.dateField}
                  />
                  <Field
                    label="Local time"
                    value={endTime}
                    onChangeText={(text) => {
                      setEndTime(text);
                      setEndOccurrence(null);
                    }}
                    placeholder="19:20"
                    autoCapitalize="none"
                    keyboardType="numbers-and-punctuation"
                    containerStyle={styles.timeField}
                  />
                </View>
                <AmbiguityChoice
                  resolution={endResolution}
                  selected={endOccurrence}
                  onSelect={setEndOccurrence}
                  timezone={timezone}
                />
              </>
            ) : null}
            <Field
              label="Timezone"
              value={timezone}
              onChangeText={setTimezone}
              placeholder="Asia/Shanghai"
              autoCapitalize="none"
            />
            <Text style={styles.fieldLabel}>Repeats</Text>
            <View style={styles.chips}>
              {(
                ["none", "daily", "weekly", "weekdays"] as CalendarRecurrence[]
              ).map((option) => (
                <Chip
                  key={option}
                  selected={recurrence === option}
                  label={option[0].toUpperCase() + option.slice(1)}
                  onPress={() => setRecurrence(option)}
                />
              ))}
            </View>
            <Field
              label="Notes"
              value={notes}
              onChangeText={setNotes}
              multiline
              placeholder="Optional context"
            />
            {kind === "scheduled_action" ? (
              <Field
                label="Action instruction"
                value={instruction}
                onChangeText={setInstruction}
                multiline
                placeholder="What should Zen do when the time comes?"
              />
            ) : null}
            <View style={styles.resolvedCard}>
              <Text style={styles.resolvedLabel}>Resolved time</Text>
              <Text
                style={
                  timeValidation ? styles.validationError : styles.resolved
                }
              >
                {timeValidation ||
                  (atInstant
                    ? formatResolvedInstant(atInstant, timezone)
                    : resolutionMessage(atResolution))}
              </Text>
              {!timeValidation && kind === "event" && endInstant ? (
                <Text style={styles.resolved}>
                  Ends {formatResolvedInstant(endInstant, timezone)}
                </Text>
              ) : null}
            </View>
          </ScrollView>
        </SafeAreaView>
      </KeyboardAvoidingView>
    </Modal>
  );
}
const Field = React.memo(function Field(
  props: React.ComponentProps<typeof TextInput> & {
    label: string;
    containerStyle?: object;
  },
) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const { label, containerStyle, ...input } = props;
  return (
    <View style={[styles.field, containerStyle]}>
      <Text style={styles.fieldLabel}>{label}</Text>
      <TextInput
        {...input}
        accessibilityLabel={label}
        placeholderTextColor={colors.textTertiary}
        style={[
          styles.input,
          input.multiline ? styles.multiline : styles.singleLine,
        ]}
      />
    </View>
  );
});
function AmbiguityChoice({
  resolution,
  selected,
  onSelect,
  timezone,
}: {
  resolution: LocalDateTimeResolution;
  selected: 0 | 1 | null;
  onSelect(value: 0 | 1): void;
  timezone: string;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  if (resolution.status !== "ambiguous") return null;
  return (
    <View style={styles.ambiguity}>
      <Text style={styles.validationError}>{resolution.message}</Text>
      <View style={styles.chips}>
        {resolution.instants.map((instant, index) => (
          <Chip
            key={instant}
            selected={selected === index}
            label={`${index === 0 ? "First" : "Second"} · ${new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit", timeZone: timezone, timeZoneName: "short" }).format(new Date(instant))}`}
            onPress={() => onSelect(index as 0 | 1)}
          />
        ))}
      </View>
    </View>
  );
}
function Chip({
  selected,
  label,
  onPress,
}: {
  selected: boolean;
  label: string;
  onPress(): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  return (
    <Pressable
      onPress={onPress}
      style={[styles.chip, selected && styles.chipSelected]}
    >
      <Text style={[styles.chipText, selected && styles.chipTextSelected]}>
        {label}
      </Text>
    </Pressable>
  );
}
function chosenInstant(
  resolution: LocalDateTimeResolution,
  occurrence: 0 | 1 | null,
) {
  if (resolution.status === "resolved") return resolution.instant;
  if (resolution.status === "ambiguous" && occurrence !== null)
    return resolution.instants[occurrence];
  return null;
}
function resolutionMessage(resolution: LocalDateTimeResolution) {
  return resolution.status === "resolved"
    ? "Time resolved."
    : resolution.message;
}
function dayKey(date: Date) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}
function monthGrid(month: Date) {
  const first = new Date(month.getFullYear(), month.getMonth(), 1);
  const start = new Date(first);
  start.setDate(1 - first.getDay());
  return Array.from({ length: 42 }, (_, index) => {
    const d = new Date(start);
    d.setDate(start.getDate() + index);
    return d;
  });
}

function createStyles(colors: any) {
  return StyleSheet.create({
    screen: { flex: 1, backgroundColor: colors.bgPrimary },
    toolbar: {
      height: 58,
      paddingHorizontal: Spacing.lg,
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    segment: {
      flex: 1,
      flexDirection: "row",
      padding: 3,
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
    },
    segmentButton: {
      flex: 1,
      minHeight: 36,
      alignItems: "center",
      justifyContent: "center",
      borderRadius: Radii.xs,
    },
    segmentActive: { backgroundColor: colors.bgSurface },
    segmentText: { ...TypeScale.label, color: colors.textTertiary },
    segmentTextActive: { color: colors.textPrimary },
    addButton: {
      width: 44,
      height: 44,
      alignItems: "center",
      justifyContent: "center",
    },
    permission: {
      margin: 12,
      marginBottom: 0,
      padding: 12,
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
      flexDirection: "row",
      alignItems: "center",
      gap: 10,
    },
    permissionTitle: { ...TypeScale.label, color: colors.textPrimary },
    permissionAction: {
      minHeight: 40,
      paddingHorizontal: 12,
      alignItems: "center",
      justifyContent: "center",
      borderRadius: Radii.sm,
      backgroundColor: colors.bgSurface,
    },
    permissionActionText: { ...TypeScale.label, color: colors.accent },
    errorBanner: {
      margin: 12,
      padding: 10,
      borderRadius: Radii.xs,
      backgroundColor: colors.surfaceSubtle,
    },
    errorText: { ...TypeScale.compact, color: colors.statusFailed },
    content: { padding: Spacing.lg, paddingBottom: 40, gap: 20 },
    section: { gap: 4 },
    sectionTitle: {
      ...TypeScale.heading,
      color: colors.textPrimary,
      marginBottom: 8,
    },
    row: {
      minHeight: 72,
      paddingVertical: 10,
      paddingHorizontal: 8,
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    rowPressed: { backgroundColor: colors.surfacePressed },
    timeRail: {
      width: 68,
      alignSelf: "stretch",
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
    },
    rowTime: { ...TypeScale.caption, color: colors.textSecondary, width: 58 },
    kindRail: { width: 3, alignSelf: "stretch", borderRadius: 2 },
    rowMain: { flex: 1, gap: 5 },
    rowTitle: { ...TypeScale.body, color: colors.textPrimary },
    meta: {
      flexDirection: "row",
      alignItems: "center",
      flexWrap: "wrap",
      gap: 4,
    },
    metaText: { ...TypeScale.caption, color: colors.textTertiary },
    metaDot: { color: colors.textTertiary },
    executeText: { ...TypeScale.caption, color: colors.accent },
    empty: {
      alignItems: "center",
      paddingVertical: 70,
      paddingHorizontal: 30,
      gap: 10,
    },
    emptyTitle: { ...TypeScale.heading, color: colors.textPrimary },
    emptyBody: {
      ...TypeScale.body,
      color: colors.textTertiary,
      textAlign: "center",
    },
    primaryButton: {
      marginTop: 10,
      minHeight: 46,
      paddingHorizontal: 20,
      borderRadius: Radii.sm,
      backgroundColor: colors.accent,
      alignItems: "center",
      justifyContent: "center",
    },
    primaryButtonText: {
      ...TypeScale.label,
      color: colors.onAccent ?? colors.bgPrimary,
    },
    monthHeader: {
      height: 48,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
    },
    monthTitle: { ...TypeScale.heading, color: colors.textPrimary },
    iconButton: {
      width: 44,
      height: 44,
      alignItems: "center",
      justifyContent: "center",
    },
    weekdays: { flexDirection: "row" },
    weekday: {
      ...TypeScale.micro,
      color: colors.textTertiary,
      width: "14.285%",
      textAlign: "center",
    },
    monthGrid: { flexDirection: "row", flexWrap: "wrap" },
    dayCell: {
      width: "14.285%",
      height: 52,
      alignItems: "center",
      justifyContent: "center",
      borderRadius: Radii.sm,
      gap: 3,
    },
    daySelected: { backgroundColor: colors.surfaceSubtle },
    dayNumber: { ...TypeScale.compact, color: colors.textPrimary },
    dayMuted: { color: colors.textTertiary },
    dayDot: {
      width: 4,
      height: 4,
      borderRadius: 2,
      backgroundColor: colors.accent,
    },
    dayHeading: {
      ...TypeScale.title,
      color: colors.textPrimary,
      marginBottom: 8,
    },
    modalBackdrop: {
      flex: 1,
      justifyContent: "flex-end",
      backgroundColor: colors.modalBackdrop,
    },
    sheet: {
      maxHeight: "88%",
      backgroundColor: colors.bgSurface,
      borderTopLeftRadius: Radii.lg,
      borderTopRightRadius: Radii.lg,
    },
    sheetHandle: {
      width: 40,
      height: 4,
      borderRadius: 2,
      backgroundColor: colors.border,
      alignSelf: "center",
      marginTop: 8,
    },
    detailHeader: { padding: 16, flexDirection: "row", gap: 12 },
    detailKind: { ...TypeScale.label, color: colors.accent, marginBottom: 5 },
    detailTitle: { ...TypeScale.title, color: colors.textPrimary },
    detailContent: { paddingHorizontal: 16, paddingBottom: 20, gap: 14 },
    info: { gap: 3 },
    infoLabel: {
      ...TypeScale.micro,
      color: colors.textTertiary,
      textTransform: "uppercase",
    },
    infoValue: { ...TypeScale.body, color: colors.textPrimary },
    linkRow: { gap: 3 },
    linkText: { ...TypeScale.body, color: colors.accent },
    failure: {
      padding: 12,
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
      gap: 4,
    },
    failureTitle: { ...TypeScale.label, color: colors.statusFailed },
    failureBody: { ...TypeScale.compact, color: colors.textSecondary },
    actions: {
      padding: 16,
      flexDirection: "row",
      gap: 8,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    primaryAction: {
      flex: 1,
      minHeight: 46,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.accent,
      borderRadius: Radii.sm,
    },
    secondaryAction: {
      minWidth: 70,
      minHeight: 46,
      alignItems: "center",
      justifyContent: "center",
      paddingHorizontal: 12,
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
    },
    secondaryText: { ...TypeScale.label, color: colors.textPrimary },
    cancelText: { ...TypeScale.label, color: colors.statusFailed },
    editor: {
      flex: 1,
      marginTop: Spacing.xl,
      backgroundColor: colors.bgSurface,
      borderTopLeftRadius: Radii.lg,
      borderTopRightRadius: Radii.lg,
      overflow: "hidden",
    },
    editorHeader: {
      height: 58,
      paddingHorizontal: 8,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    editorTitle: { ...TypeScale.heading, color: colors.textPrimary },
    headerAction: {
      minWidth: 70,
      minHeight: 44,
      alignItems: "center",
      justifyContent: "center",
    },
    saveText: { ...TypeScale.label, color: colors.accent },
    editorScroll: { flex: 1 },
    form: { padding: 16, paddingBottom: 50, gap: 14 },
    field: { gap: 6 },
    dateTimeRow: { flexDirection: "row", gap: 10 },
    dateField: { flex: 1.35 },
    timeField: { flex: 1 },
    fieldLabel: { ...TypeScale.label, color: colors.textSecondary },
    input: {
      ...TypeScale.body,
      color: colors.textPrimary,
      borderWidth: 1,
      borderColor: colors.borderSubtle,
      borderRadius: Radii.sm,
      paddingHorizontal: 12,
      backgroundColor: colors.bgPrimary,
    },
    singleLine: {
      height: 48,
      paddingVertical: 0,
      textAlignVertical: "center",
    },
    multiline: {
      minHeight: 92,
      paddingVertical: 10,
      textAlignVertical: "top",
    },
    ambiguity: { gap: 8 },
    validationError: { ...TypeScale.compact, color: colors.statusFailed },
    resolvedCard: {
      gap: 4,
      padding: 12,
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
    },
    resolvedLabel: { ...TypeScale.label, color: colors.textPrimary },
    chips: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
    chip: {
      minHeight: 40,
      paddingHorizontal: 12,
      alignItems: "center",
      justifyContent: "center",
      borderRadius: Radii.pill,
      backgroundColor: colors.surfaceSubtle,
      borderWidth: 1,
      borderColor: "transparent",
    },
    chipSelected: { borderColor: colors.accent },
    chipText: { ...TypeScale.caption, color: colors.textSecondary },
    chipTextSelected: {
      color: colors.accent,
      fontFamily: Typography.uiFontMedium,
    },
    resolved: { ...TypeScale.caption, color: colors.textTertiary },
  });
}
