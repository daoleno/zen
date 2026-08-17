import React, {
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
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
import { useLocalSearchParams, useNavigation, useRouter } from "expo-router";
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
  selectCurrentServerCalendar,
  selectCurrentServerCalendarItems,
  useCalendar,
  type CalendarItem,
  type CalendarKind,
  type CalendarRecurrence,
  type ServerCalendarItem,
} from "../store/calendar";
import { useBrain } from "../store/brain";
import { useCurrentServer } from "../store/currentServer";

type ServerItem = ServerCalendarItem;
const CALENDAR_FONT_SCALE_MAX = 1.25;
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
  notificationStateOverride?: CalendarNotificationState;
  initialError?: string;
  loading?: boolean;
  serverIdOverride?: string;
};

export default function CalendarScreen(props: CalendarScreenProps = {}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const router = useRouter();
  const navigation = useNavigation();
  const params = useLocalSearchParams<{ id?: string; serverId?: string }>();
  const state = useCalendar();
  const { currentServerId } = useCurrentServer();
  const scopedServerId = props.serverIdOverride ?? currentServerId;
  const viewerZone = useMemo(() => viewerTimezone(), []);
  const now = new Date();
  const todayKey = calendarDateKey(now, viewerZone);
  const [monthExpanded, setMonthExpanded] = useState(false);
  const [month, setMonth] = useState(() => new Date());
  const [selectedDate, setSelectedDate] = useState<string | null>(null);
  const [selected, setSelected] = useState<ServerItem | null>(null);
  const [editing, setEditing] = useState<ServerItem | "new" | null>(null);
  const [error, setError] = useState(props.initialError ?? "");
  const [notificationState, setNotificationState] =
    useState<CalendarNotificationState>(
      props.notificationStateOverride ?? "undetermined",
    );
  const serverScopeRef = useRef(scopedServerId);
  const items = useMemo(
    () => selectCurrentServerCalendarItems(state, scopedServerId),
    [scopedServerId, state],
  );
  const activeServer = selectCurrentServerCalendar(state, scopedServerId);
  useEffect(() => {
    if (serverScopeRef.current === scopedServerId) return;
    serverScopeRef.current = scopedServerId;
    setSelected(null);
    setEditing(null);
    setError("");
  }, [scopedServerId]);
  useLayoutEffect(() => {
    navigation.setOptions({
      headerShown: true,
      headerTitle: () => (
        <Text
          maxFontSizeMultiplier={1.15}
          numberOfLines={1}
          style={styles.headerTitle}
        >
          Calendar
        </Text>
      ),
      headerLeft: () => (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Back"
          hitSlop={8}
          onPress={() => {
            if (navigation.canGoBack()) {
              router.back();
              return;
            }
            router.replace("/");
          }}
          style={styles.calendarHeaderAction}
        >
          <Ionicons name="chevron-back" size={23} color={colors.textPrimary} />
        </Pressable>
      ),
      headerRight: () => (
        <View style={styles.calendarHeaderActions}>
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={
              monthExpanded ? "Collapse month calendar" : "Expand month calendar"
            }
            accessibilityState={{ expanded: monthExpanded }}
            hitSlop={8}
            onPress={() => {
              setMonthExpanded((expanded) => {
                if (expanded) setSelectedDate(null);
                return !expanded;
              });
            }}
            style={styles.calendarHeaderAction}
          >
            <Ionicons
              name={monthExpanded ? "calendar" : "calendar-outline"}
              size={21}
              color={colors.textPrimary}
            />
          </Pressable>
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Add calendar item"
            accessibilityState={{ disabled: !activeServer }}
            disabled={!activeServer}
            hitSlop={8}
            onPress={() => setEditing("new")}
            style={styles.calendarHeaderAction}
          >
            <Ionicons
              name="add"
              size={24}
              color={activeServer ? colors.textPrimary : colors.disabledText}
            />
          </Pressable>
        </View>
      ),
    });
  }, [activeServer, colors, monthExpanded, navigation, router, styles]);
  useEffect(() => {
    if (props.notificationStateOverride) return;
    let active = true;
    const refreshPermission = async () => {
      const next = await calendarNotificationState();
      if (!active) return;
      setNotificationState(next);
      if (next === "granted" && activeServer) {
        await syncCalendarNotifications(
          activeServer.serverId,
          activeServer.items,
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
  }, [activeServer, props.notificationStateOverride]);
  useEffect(() => {
    if (!params.id) return;
    const found = items.find(
      (item) =>
        item.id === params.id &&
        (!params.serverId || item.serverId === params.serverId),
    );
    if (found) setSelected(found);
  }, [items, params.id, params.serverId]);
  const visible = items.filter((item) => item.status !== "cancelled");
  const agendaItems = selectedDate
    ? visible.filter(
        (item) =>
          calendarDateKey(itemInstant(item), viewerZone) === selectedDate,
      )
    : visible;
  const sections = selectedDate
    ? [{ title: selectedDate, data: agendaItems }]
    : groupAgenda(agendaItems, now, viewerZone);
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
      if (activeServer) {
        await syncCalendarNotifications(
          activeServer.serverId,
          activeServer.items,
        );
      }
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
      <ScrollView contentContainerStyle={styles.content}>
        {monthExpanded ? (
          <MonthNavigator
            items={visible}
            month={month}
            selectedDate={selectedDate}
            timeZone={viewerZone}
            todayKey={todayKey}
            onChangeMonth={setMonth}
            onSelectDate={(date, key) => {
              setSelectedDate(key);
              if (
                date.getMonth() !== month.getMonth() ||
                date.getFullYear() !== month.getFullYear()
              ) {
                setMonth(new Date(date.getFullYear(), date.getMonth(), 1));
              }
            }}
          />
        ) : null}
        <AgendaHeading
          now={now}
          selectedDate={selectedDate}
          timeZone={viewerZone}
          onToday={() => {
            setSelectedDate(null);
            setMonth(new Date());
          }}
        />
        {props.loading ? (
          <View
            accessibilityRole="progressbar"
            accessibilityLabel="Loading calendar"
            style={styles.agendaStatus}
          >
            <Ionicons name="sync-outline" size={20} color={colors.accent} />
            <Text style={styles.emptyTitle}>Loading calendar…</Text>
          </View>
        ) : sections.length ? (
          sections.map((section) => (
            <View key={section.title} style={styles.section}>
              {selectedDate || section.title === "Today" ? null : (
                <Text style={styles.sectionTitle}>{section.title}</Text>
              )}
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
            dateFiltered={selectedDate !== null}
          />
        )}
      </ScrollView>
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

function AgendaHeading({
  now,
  selectedDate,
  timeZone,
  onToday,
}: {
  now: Date;
  selectedDate: string | null;
  timeZone: string;
  onToday(): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const dateFormatter = new Intl.DateTimeFormat(undefined, {
    weekday: "long",
    month: "long",
    day: "numeric",
    timeZone,
  });
  const date = dateFormatter.format(
    selectedDate ? dateFromKey(selectedDate) : now,
  );
  const time = new Intl.DateTimeFormat(undefined, {
    hour: "numeric",
    minute: "2-digit",
    timeZone,
    timeZoneName: "short",
  }).format(now);
  const isToday =
    selectedDate === null || selectedDate === calendarDateKey(now, timeZone);
  return (
    <View
      accessibilityRole="header"
      accessibilityLabel={isToday ? `Today, ${date}, ${time}` : date}
      style={styles.agendaHeading}
    >
      <View style={styles.agendaTitleRow}>
        <Text
          maxFontSizeMultiplier={CALENDAR_FONT_SCALE_MAX}
          numberOfLines={2}
          style={styles.agendaTitle}
        >
          {isToday ? "Today" : date}
        </Text>
        {!isToday ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Return to today"
            onPress={onToday}
            style={styles.todayButton}
          >
            <Text style={styles.todayButtonText}>Today</Text>
          </Pressable>
        ) : null}
      </View>
      {isToday ? (
        <Text
          maxFontSizeMultiplier={CALENDAR_FONT_SCALE_MAX}
          numberOfLines={2}
          style={styles.agendaAnchor}
        >
          {date} · {time}
        </Text>
      ) : null}
    </View>
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
function MonthNavigator({
  items,
  month,
  selectedDate,
  timeZone,
  todayKey,
  onChangeMonth,
  onSelectDate,
}: {
  items: ServerItem[];
  month: Date;
  selectedDate: string | null;
  timeZone: string;
  todayKey: string;
  onChangeMonth(date: Date): void;
  onSelectDate(date: Date, key: string): void;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const counts = useMemo(() => {
    const next = new Map<string, number>();
    for (const item of items) {
      const key = calendarDateKey(itemInstant(item), timeZone);
      next.set(key, (next.get(key) ?? 0) + 1);
    }
    return next;
  }, [items, timeZone]);
  return (
    <View
      accessibilityLabel="Month date navigator"
      style={styles.monthNavigator}
    >
      <MonthHeader month={month} onChange={onChangeMonth} />
      <View style={styles.weekdays}>
        {["S", "M", "T", "W", "T", "F", "S"].map((label, index) => (
          <Text key={`${label}${index}`} style={styles.weekday}>
            {label}
          </Text>
        ))}
      </View>
      <View style={styles.monthGrid}>
        {monthGrid(month).map((date) => {
          const key = dayKey(date);
          const count = counts.get(key) ?? 0;
          const inMonth = date.getMonth() === month.getMonth();
          const selected = key === selectedDate;
          const today = key === todayKey;
          return (
            <Pressable
              key={key}
              accessibilityRole="button"
              accessibilityLabel={`${date.toLocaleDateString()}, ${count} items`}
              accessibilityState={{ selected }}
              onPress={() => onSelectDate(date, key)}
              style={[
                styles.dayCell,
                today && styles.dayToday,
                selected && styles.daySelected,
              ]}
            >
              <Text
                maxFontSizeMultiplier={CALENDAR_FONT_SCALE_MAX}
                style={[
                  styles.dayNumber,
                  !inMonth && styles.dayMuted,
                  selected && styles.dayNumberSelected,
                ]}
              >
                {date.getDate()}
              </Text>
              {count ? <View style={styles.dayDot} /> : null}
            </Pressable>
          );
        })}
      </View>
    </View>
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
        accessibilityRole="button"
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
        accessibilityRole="button"
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
  dateFiltered,
}: {
  connected: boolean;
  dateFiltered: boolean;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  return (
    <View style={styles.empty}>
      <Ionicons
        name="calendar-clear-outline"
        size={19}
        color={colors.textTertiary}
      />
      <Text
        maxFontSizeMultiplier={CALENDAR_FONT_SCALE_MAX}
        style={styles.emptyTitle}
      >
        {connected
          ? dateFiltered
            ? "Nothing planned for this date"
            : "Nothing planned"
          : "Calendar offline"}
      </Text>
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
  const { state: brainState } = useBrain();
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
      const sourceThreadId = existing?.source_thread_id ??
        (serverId ? brainState.byServer[serverId]?.chat_thread_id : undefined);
      if (kind === "scheduled_action" && !sourceThreadId) {
        throw new Error("Scheduled Work requires an active Brain conversation on this server.");
      }
      const payload: any = {
        ...(existing ?? {}),
        title: title.trim(),
        kind,
        timezone: timezone.trim(),
        recurrence,
        notes: notes.trim(),
        action_instruction:
          kind === "scheduled_action" ? instruction.trim() : undefined,
        source_thread_id:
          kind === "scheduled_action" ? sourceThreadId : undefined,
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
function dateFromKey(key: string) {
  return new Date(`${key}T12:00:00`);
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
    calendarHeaderAction: {
      width: 44,
      height: 44,
      alignItems: "center",
      justifyContent: "center",
    },
    calendarHeaderActions: { flexDirection: "row", alignItems: "center" },
    headerTitle: {
      ...TypeScale.title,
      color: colors.textPrimary,
    },
    permission: {
      marginHorizontal: Spacing.lg,
      marginTop: Spacing.sm,
      marginBottom: 0,
      padding: 12,
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
      flexDirection: "row",
      alignItems: "flex-start",
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
      marginHorizontal: Spacing.lg,
      marginTop: Spacing.sm,
      marginBottom: 0,
      padding: 10,
      borderRadius: Radii.xs,
      backgroundColor: colors.surfaceSubtle,
    },
    errorText: { ...TypeScale.compact, color: colors.statusFailed },
    content: {
      paddingHorizontal: Spacing.lg,
      paddingTop: Spacing.md,
      paddingBottom: 40,
      gap: Spacing.md,
    },
    agendaHeading: { gap: 2 },
    agendaTitleRow: {
      minHeight: 44,
      flexDirection: "row",
      alignItems: "center",
      gap: Spacing.sm,
    },
    agendaTitle: { ...TypeScale.heading, flex: 1, color: colors.textPrimary },
    agendaAnchor: { ...TypeScale.caption, color: colors.textSecondary },
    todayButton: {
      minWidth: 56,
      minHeight: 44,
      paddingHorizontal: Spacing.sm,
      alignItems: "center",
      justifyContent: "center",
      borderRadius: Radii.sm,
      backgroundColor: colors.surfaceSubtle,
    },
    todayButtonText: { ...TypeScale.label, color: colors.accent },
    agendaStatus: {
      minHeight: 64,
      paddingVertical: Spacing.md,
      flexDirection: "row",
      alignItems: "center",
      gap: Spacing.sm,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
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
      minHeight: 52,
      paddingVertical: Spacing.sm,
      flexDirection: "row",
      alignItems: "center",
      gap: Spacing.sm,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    emptyTitle: { ...TypeScale.compact, color: colors.textSecondary },
    emptyBody: {
      ...TypeScale.body,
      color: colors.textTertiary,
      textAlign: "center",
    },
    primaryButtonText: {
      ...TypeScale.label,
      color: colors.textOnAccent,
    },
    monthHeader: {
      height: 48,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
    },
    monthNavigator: {
      paddingBottom: Spacing.sm,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
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
    dayToday: {
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.accent,
    },
    dayNumber: { ...TypeScale.compact, color: colors.textPrimary },
    dayNumberSelected: { color: colors.accent },
    dayMuted: { color: colors.textTertiary },
    dayDot: {
      width: 4,
      height: 4,
      borderRadius: 2,
      backgroundColor: colors.accent,
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
