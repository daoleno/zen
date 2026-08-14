import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  FlatList,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useLocalSearchParams, useRouter } from "expo-router";
import { useWindowDimensions } from "react-native";
import {
  SafeAreaView,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
import { AgentListRowContainer } from "../components/agents/AgentListRowContainer";
import {
  PrimaryDrawerShell,
  resolvePrimaryAppBarGeometry,
} from "../components/navigation/PrimaryDrawerShell";
import { ChatCanvas } from "../components/terminal/ChatCanvas";
import { InterfaceChatComposer } from "../components/terminal/InterfaceChatComposer";
import { InterfaceChatKeyboardFrame } from "../components/terminal/InterfaceChatKeyboardFrame";
import { InterfaceTimelineView } from "../components/terminal/InterfaceTimelineView";
import type { ZenTimelineItem } from "../components/terminal/InterfaceTimelineItemView";
import {
  patchDisplayPath,
  truncateRunes,
} from "../components/terminal/InterfaceTimelineModel";
import { useInterfaceTimelineItems } from "../components/terminal/useInterfaceTimelineItems";
import { TerminalTopBar } from "../components/terminal/TerminalTopBar";
import {
  CHAT_HEADER_HEIGHT,
  CHAT_HEADER_OUTER_GAP,
} from "../components/terminal/chatChromeMetrics";
import {
  TypeScale,
  UiTextMetrics,
  useAppColors,
  useAppTheme,
} from "../constants/tokens";
import { buildChatChrome } from "../theme";
import {
  SCREENSHOT_BRAIN_EVENTS,
  SCREENSHOT_CHAT_EVENTS,
  SCREENSHOT_SESSION_AGENTS,
  SCREENSHOT_STATS_FIXTURE,
  screenshotChatPendingUserMessages,
} from "../services/screenshotDemoFixtures";
import {
  resolveScreenshotChatPendingFixture,
  resolveScreenshotDemoState,
  resolveScreenshotProvidersDemo,
  screenshotDemoEnabled,
  screenshotDemoRouteOptedIn,
} from "../services/screenshotDemo";
import { InterfaceDevicePerformanceDemoGate } from "../components/terminal/InterfaceDevicePerformanceDemo";
import { ProvidersPresentation } from "../components/providers/ProvidersPresentation";
import { SessionModelSheet } from "../components/providers/SessionModelSheet";
import {
  resolveComposerModelControl,
  sessionModelSheetRows,
} from "../services/providers/sessionModelHelpers";
import type { ProvidersSnapshot } from "../services/providers/types";
import { StatsScreenshotDemo, type StatsPayload } from "./stats";
import CalendarScreen from "./calendar";
import { useCalendarDispatch, type CalendarItem } from "../store/calendar";

const NOOP = () => undefined;
const loadNoDemoAsset = async () => null;

export default function ScreenshotDemoRoute() {
  const router = useRouter();
  const params = useLocalSearchParams<{
    demo?: string | string[];
    state?: string | string[];
  }>();
  const state = resolveScreenshotDemoState(
    params.state ?? process.env.EXPO_PUBLIC_ZEN_SCREENSHOT_DEMO_STATE,
  );
  const enabled = screenshotDemoEnabled();
  const explicitlyRequested = screenshotDemoRouteOptedIn(params.demo);
  const available = enabled && explicitlyRequested;

  useEffect(() => {
    if (!available) router.replace("/");
  }, [available, router]);

  if (!available) return null;

  switch (state) {
    case "sessions":
      return <SessionsDemo />;
    case "brain":
      return <BrainDemo />;
    case "stats":
      return <StatsDemo />;
    case "calendar":
      return <CalendarDemo />;
    case "providers":
      return <ProvidersDemo />;
    case "profile":
      return <InterfaceDevicePerformanceDemoGate />;
    case "composer":
      return <ComposerStatesDemo />;
    case "chat":
    default:
      return <ChatDemo />;
  }
}

function CalendarDemo() {
  const dispatch = useCalendarDispatch();
  const params = useLocalSearchParams<{
    view?: string | string[];
    fixture?: string | string[];
    notification?: string | string[];
  }>();
  const fixture = Array.isArray(params.fixture)
    ? params.fixture[0]
    : params.fixture;
  const notification = Array.isArray(params.notification)
    ? params.notification[0]
    : params.notification;
  const notificationState =
    notification === "granted" ||
    notification === "undetermined" ||
    notification === "unavailable"
      ? notification
      : "denied";

  useEffect(() => {
    dispatch({
      type: "CALENDAR_SNAPSHOT",
      serverId: "screenshot-calendar",
      serverName: "Studio Mac",
      serverUrl: "https://calendar.example.invalid",
      items:
        fixture === "empty" || fixture === "loading" ? [] : calendarFixtures(),
    });
  }, [dispatch, fixture]);

  return (
    <CalendarScreen
      notificationStateOverride={notificationState}
      initialError={
        fixture === "error"
          ? "Calendar sync failed. Check the daemon connection and retry."
          : ""
      }
      loading={fixture === "loading"}
    />
  );
}

function calendarFixtures(): CalendarItem[] {
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const now = new Date();
  const at = (dayOffset: number, hour: number, minute = 0) => {
    const value = new Date(now);
    value.setDate(value.getDate() + dayOffset);
    value.setHours(hour, minute, 0, 0);
    return value.toISOString();
  };
  const base = {
    timezone,
    recurrence: "none" as const,
    created_at: at(-2, 9),
    updated_at: at(-1, 9),
    revision: 1,
  };
  return [
    {
      ...base,
      id: "demo-reminder",
      title: "Review the launch checklist before the customer call",
      kind: "reminder",
      status: "scheduled",
      notify_at: at(0, 10, 30),
      next_at: at(0, 10, 30),
      notes: "Bring the accessibility notes.",
    },
    {
      ...base,
      id: "demo-action",
      title: "整理发布说明并运行移动端回归检查",
      kind: "scheduled_action",
      status: "running",
      due_at: at(0, 14),
      next_at: at(0, 14),
      recurrence: "weekdays",
      action_instruction:
        "Update the visible Work item and run the mobile checks.",
      linked_work_id: "calendar-demo-work",
    },
    {
      ...base,
      id: "demo-event",
      title: "Design review: Calendar and Work lifecycle",
      kind: "event",
      status: "scheduled",
      start_at: at(1, 9),
      end_at: at(1, 10),
      next_at: at(1, 9),
      source_thread_id: "brain-calendar-review",
    },
    {
      ...base,
      id: "demo-deadline",
      title:
        "Submit the deliberately very long localization and release-readiness report",
      kind: "deadline",
      status: "failed",
      due_at: at(3, 17),
      next_at: at(3, 17),
      failure_reason: "The linked agent stopped before the report was written.",
    },
  ];
}

function ChatDemo() {
  const params = useLocalSearchParams<{
    attachment?: string | string[];
    draft?: string | string[];
    long?: string | string[];
    pending?: string | string[];
    working?: string | string[];
  }>();
  const { theme: zenTheme } = useAppTheme();
  const { chrome, theme } = useMemo(
    () => buildChatChrome(zenTheme),
    [zenTheme],
  );
  const menuAnchorRef = useRef<View>(null);
  const scrollRef = useRef<FlatList<ZenTimelineItem>>(null);
  const inputRef = useRef<TextInput>(null);
  const insets = useSafeAreaInsets();
  const requestedDraft = Array.isArray(params.draft)
    ? params.draft[0]
    : params.draft;
  const [draft, setDraft] = useState(
    requestedDraft === "multiline"
      ? "Queue a careful review of the keyboard overlay.\nPreserve the visible message anchor.\nVerify every narrow width."
      : requestedDraft === "text"
        ? "Queue another check"
        : "",
  );
  const [focused, setFocused] = useState(false);
  const [providerActivityStartedAt] = useState(() =>
    new Date(Date.now() - 43_000).toISOString(),
  );
  const longTimeline =
    (Array.isArray(params.long) ? params.long[0] : params.long) === "1";
  const working =
    (Array.isArray(params.working) ? params.working[0] : params.working) ===
    "1";
  const showAttachment =
    (Array.isArray(params.attachment)
      ? params.attachment[0]
      : params.attachment) === "1";
  const pendingFixture = resolveScreenshotChatPendingFixture(params.pending);
  const timelineEvents = useMemo(() => {
    const transcriptEvents = SCREENSHOT_CHAT_EVENTS.filter(
      (event) =>
        event.kind === "user_message" ||
        event.kind === "assistant_message" ||
        event.kind === "plan",
    );
    if (!longTimeline) {
      return transcriptEvents;
    }
    return Array.from({ length: 4 }, (_, batch) =>
      transcriptEvents.map((event) => ({
        ...event,
        id: `history:${batch}:${event.id}`,
        seq: batch * 1000 + event.seq,
      })),
    ).flat();
  }, [longTimeline]);
  const pendingUserMessages = useMemo(
    () => screenshotChatPendingUserMessages(pendingFixture),
    [pendingFixture],
  );
  const runningActivity = useMemo(
    () =>
      working
        ? {
            id: "demo-working-turn",
            status: "running" as const,
            started_at: providerActivityStartedAt,
          }
        : undefined,
    [working, providerActivityStartedAt],
  );
  const timeline = useInterfaceTimelineItems({
    events: timelineEvents,
    pendingUserMessages,
    runningActivity,
    onRetryPendingUserMessage: NOOP,
  });
  const attachments = showAttachment
    ? [
        {
          id: "demo-attachment",
          name: "keyboard-geometry-notes.md",
          path: "/demo/keyboard-geometry-notes.md",
          mimeType: "text/markdown",
        },
      ]
    : [];
  const hasContent = draft.trim().length > 0 || attachments.length > 0;
  const topChromeInset = CHAT_HEADER_HEIGHT + CHAT_HEADER_OUTER_GAP * 2;

  return (
    <SafeAreaView
      style={[styles.flex, { backgroundColor: chrome.appBackground }]}
      edges={["top", "bottom"]}
    >
      <View style={styles.flex}>
        <View pointerEvents="box-none" style={styles.demoHeaderOverlay}>
          <TerminalTopBar
            title="Mobile handoff"
            subtitle="atlas-notes · Chat"
            kind="codex"
            backgroundColor={chrome.appBackground}
            chrome={chrome}
            menuAnchorRef={menuAnchorRef}
            interfaceRenderMode="chat"
            gitDiffDisabled={false}
            gitDiffPresentation={{
              accessibilityLabel: "Open changes",
              backgroundColor: chrome.accentSoft,
              iconColor: chrome.accent,
              additionsText: "+42",
              deletionsText: "−9",
            }}
            isStructuredChatAgent
            onBack={NOOP}
            onOpenSessionDetails={NOOP}
            onOpenGitDiff={NOOP}
            onOpenMenu={NOOP}
            onToggleInterfaceRenderMode={NOOP}
          />
        </View>
        <InterfaceChatKeyboardFrame
          enabled
          keyboardVerticalOffset={0}
          chrome={chrome}
          topChromeInset={topChromeInset}
          composer={
            <InterfaceChatComposer
              inputRef={inputRef}
              draft={draft}
              placeholder="Message the agent"
              editable
              focused={focused}
              canAttach
              uploading={false}
              activeUpload={null}
              sendEnabled={hasContent}
              sending={false}
              sendLabel={working ? "Queue message" : "Send message"}
              showStopButton={working && !hasContent}
              stopEnabled={working}
              stopLabel="Stop current turn"
              stopLoading={false}
              providerActivityStartedAt={
                working ? providerActivityStartedAt : undefined
              }
              bottomPadding={Math.max(insets.bottom, 8)}
              showActionMenuButton
              actionMenuIcon="add"
              composerLayout="telegram"
              showAttachmentRail
              showCommandMenu={false}
              showCommandList={false}
              showComposerActions={false}
              composerActionButtonEnabled
              commandQuery=""
              commands={[]}
              attachments={attachments}
              chrome={chrome}
              theme={theme}
              onSelectCommand={NOOP}
              onToggleActionMenu={NOOP}
              onDismissActionMenu={NOOP}
              onRemoveAttachment={NOOP}
              onDraftChange={setDraft}
              onUploadPress={NOOP}
              onCancelUpload={NOOP}
              onInputFocus={() => setFocused(true)}
              onInputBlur={() => setFocused(false)}
              onSendPress={() => setDraft("")}
              onStopPress={NOOP}
            />
          }
          renderTimeline={(extraContentPadding, keyboardLifecycleGate) => (
            <InterfaceTimelineView
              scrollRef={scrollRef}
              nativeFollowSuspended={false}
              items={timeline}
              loading={false}
              emptyStateSuppressed={false}
              unavailable={false}
              syncing={false}
              textSelectable={false}
              extraContentPadding={extraContentPadding}
              keyboardLifecycleGate={keyboardLifecycleGate}
              topChromeInset={topChromeInset}
              chrome={chrome}
              theme={theme}
              onLayout={NOOP}
              onScroll={NOOP}
              onScrollBeginDrag={NOOP}
              onScrollEndDrag={NOOP}
              onMomentumScrollBegin={NOOP}
              onMomentumScrollEnd={NOOP}
              onContentSizeChange={NOOP}
              onTextSelectionGestureStart={NOOP}
              onTextSelectionGestureEnd={NOOP}
              loadAssetPreview={loadNoDemoAsset}
              formatPatchPath={patchDisplayPath}
              truncateBody={truncateRunes}
            />
          )}
        />
      </View>
    </SafeAreaView>
  );
}

function BrainDemo() {
  const { theme: zenTheme } = useAppTheme();
  const insets = useSafeAreaInsets();
  const { chrome, theme } = useMemo(
    () => buildChatChrome(zenTheme),
    [zenTheme],
  );
  const scrollRef = useRef<FlatList<ZenTimelineItem>>(null);
  const inputRef = useRef<TextInput>(null);
  const [draft, setDraft] = useState("");
  const [focused, setFocused] = useState(false);
  const [providerActivityStartedAt] = useState(() =>
    new Date(Date.now() - 43_000).toISOString(),
  );
  const emptyPending = useMemo(() => [], []);
  const runningActivity = useMemo(
    () => ({
      id: "demo-brain-working-turn",
      status: "running" as const,
      started_at: providerActivityStartedAt,
    }),
    [providerActivityStartedAt],
  );
  const timeline = useInterfaceTimelineItems({
    events: SCREENSHOT_BRAIN_EVENTS,
    pendingUserMessages: emptyPending,
    runningActivity,
    onRetryPendingUserMessage: NOOP,
  });
  const hasContent = draft.trim().length > 0;
  const topChromeInset = resolvePrimaryAppBarGeometry(insets.top).contentInset;

  return (
    <PrimaryDrawerShell activePrimaryRoute="brain" onSelectPrimaryRoute={NOOP}>
      <ChatCanvas chrome={chrome}>
        <InterfaceChatKeyboardFrame
          enabled
          keyboardVerticalOffset={0}
          chrome={chrome}
          topChromeInset={topChromeInset}
          composer={
            <InterfaceChatComposer
              inputRef={inputRef}
              draft={draft}
              placeholder="Ask Brain"
              editable
              focused={focused}
              canAttach
              uploading={false}
              activeUpload={null}
              sendEnabled={hasContent}
              sending={false}
              sendLabel="Queue message"
              showStopButton={!hasContent}
              stopEnabled
              stopLabel="Stop current turn"
              stopLoading={false}
              providerActivityStartedAt={providerActivityStartedAt}
              bottomPadding={Math.max(insets.bottom, 8)}
              showActionMenuButton
              actionMenuIcon="add"
              composerLayout="telegram"
              showAttachmentRail
              showCommandMenu={false}
              showCommandList={false}
              showComposerActions={false}
              composerActionButtonEnabled
              commandQuery=""
              commands={[]}
              attachments={[]}
              chrome={chrome}
              theme={theme}
              onSelectCommand={NOOP}
              onToggleActionMenu={NOOP}
              onDismissActionMenu={NOOP}
              onRemoveAttachment={NOOP}
              onDraftChange={setDraft}
              onUploadPress={NOOP}
              onCancelUpload={NOOP}
              onInputFocus={() => setFocused(true)}
              onInputBlur={() => setFocused(false)}
              onSendPress={() => setDraft("")}
              onStopPress={NOOP}
            />
          }
          renderTimeline={(extraContentPadding, keyboardLifecycleGate) => (
            <InterfaceTimelineView
              scrollRef={scrollRef}
              nativeFollowSuspended={false}
              items={timeline}
              loading={false}
              emptyStateSuppressed={false}
              unavailable={false}
              syncing={false}
              textSelectable={false}
              extraContentPadding={extraContentPadding}
              keyboardLifecycleGate={keyboardLifecycleGate}
              topChromeInset={topChromeInset}
              chrome={chrome}
              theme={theme}
              onLayout={NOOP}
              onScroll={NOOP}
              onScrollBeginDrag={NOOP}
              onScrollEndDrag={NOOP}
              onMomentumScrollBegin={NOOP}
              onMomentumScrollEnd={NOOP}
              onContentSizeChange={NOOP}
              onTextSelectionGestureStart={NOOP}
              onTextSelectionGestureEnd={NOOP}
              loadAssetPreview={loadNoDemoAsset}
              formatPatchPath={patchDisplayPath}
              truncateBody={truncateRunes}
            />
          )}
        />
      </ChatCanvas>
    </PrimaryDrawerShell>
  );
}

/**
 * Composer motion fixture: both capsule states at a representative narrow
 * width (320), with and without the active-model control, draft, attachment,
 * running Stop timer, and a long model label proving deterministic
 * one-line truncation (the full label stays in the chip accessibility label).
 * Composer text is capped at COMPOSER_MAX_FONT_SCALE so the geometry never
 * clips at large system font scales.
 */
function ComposerStatesDemo() {
  const params = useLocalSearchParams<{ autoModel?: string | string[] }>();
  const autoModel = Array.isArray(params.autoModel)
    ? params.autoModel[0]
    : params.autoModel;
  const { theme: zenTheme } = useAppTheme();
  const insets = useSafeAreaInsets();
  const { chrome, theme } = useMemo(
    () => buildChatChrome(zenTheme),
    [zenTheme],
  );
  const compactRef = useRef<TextInput>(null);
  const draftRef = useRef<TextInput>(null);
  const expandedRef = useRef<TextInput>(null);
  const runningRef = useRef<TextInput>(null);
  const longLabelRef = useRef<TextInput>(null);
  const routedRef = useRef<TextInput>(null);
  const [compactDraft, setCompactDraft] = useState("");
  const [expandedDraft, setExpandedDraft] = useState("");
  const [runningDraft, setRunningDraft] = useState("");
  const [providerActivityStartedAt] = useState(() =>
    new Date(Date.now() - 43_000).toISOString(),
  );
  const modelControl = {
    label: "claude-sonnet-4-5",
    accessibilityLabel: "Open model selection, claude-sonnet-4-5",
    modelRequired: false,
    preferredConnectionId: "c2",
  };
  const longModelControl = {
    label: "gpt-5.1-codex-max-longhaul-8k-context",
    accessibilityLabel:
      "Open model selection, gpt-5.1-codex-max-longhaul-8k-context",
    modelRequired: false,
    preferredConnectionId: "c1",
  };
  const [demoSheet, setDemoSheet] = useState<null | "routed">(null);
  const [demoModelId, setDemoModelId] = useState("deepseek-chat");
  const demoSelection = {
    session_id: "tmux:@demo",
    client: "codex" as const,
    connection_id: "c1",
    connection_name: "DeepSeek",
    model_id: demoModelId,
    credential_ready: true,
    hot_switchable: true,
  };
  useEffect(() => {
    if (autoModel !== "1") return;
    setDemoSheet("routed");
  }, [autoModel]);

  const routedModelControl = resolveComposerModelControl({
    capabilities: {
      structured_events: true,
      model_profile_managed: true,
      model_profile_active_switch: true,
    },
    connectionConnected: true,
    selection: demoSelection,
    refreshRequired: false,
    preferredConnectionId: "c1",
  });
  const demoCatalog: ProvidersSnapshot = {
    revision: 1,
    connections: [
      {
        id: "c1",
        name: "DeepSeek",
        clients: ["codex"],
        credential_ready: true,
        advanced: false,
        preset_id: "deepseek",
      },
      {
        id: "c2",
        name: "Claude Gateway",
        clients: ["claude"],
        credential_ready: true,
        advanced: true,
      },
    ],
    defaults: {
      codex: { connection_id: "c1", model_id: demoModelId },
    },
    presets: [],
    models: {
      c1: [
        { id: "deepseek-chat", available: true, source: "bundled" },
        { id: "deepseek-reasoner", available: true, source: "bundled" },
      ],
      c2: [{ id: "claude-sonnet-4-5", available: true, source: "bundled" }],
    },
  };
  const demoRows = sessionModelSheetRows({
    snapshot: demoCatalog,
    selection: demoSelection,
  });
  const narrowRow = (
    label: string,
    composer: React.ReactElement<typeof InterfaceChatComposer>,
  ) => (
    <View style={styles.composerStateColumn}>
      <Text style={[styles.composerStateLabel, { color: chrome.textMuted }]}>
        {label}
      </Text>
      <View style={styles.composerStateRow}>{composer}</View>
    </View>
  );
  const runningAttachment = [
    {
      id: "demo-running-attachment",
      name: "keyboard-geometry-notes.md",
      path: "/demo/keyboard-geometry-notes.md",
      mimeType: "text/markdown",
    },
  ];

  return (
    <SafeAreaView
      style={[styles.flex, { backgroundColor: chrome.appBackground }]}
      edges={["top", "bottom"]}
    >
      <ScrollView
        contentContainerStyle={[
          styles.composerDemoBody,
          { paddingBottom: Math.max(insets.bottom, 16) },
        ]}
        showsVerticalScrollIndicator={false}
      >
        {narrowRow(
          "Compact · empty · no model control",
          <InterfaceChatComposer
            inputRef={compactRef}
            draft={compactDraft}
            placeholder="Message the agent"
            editable
            focused={false}
            canAttach
            uploading={false}
            activeUpload={null}
            sendEnabled={false}
            sending={false}
            sendLabel="Send message"
            showStopButton={false}
            stopEnabled={false}
            stopLabel="Stop current turn"
            stopLoading={false}
            bottomPadding={8}
            showActionMenuButton
            actionMenuIcon="add"
            composerLayout="telegram"
            showAttachmentRail
            showCommandMenu={false}
            showCommandList={false}
            showComposerActions={false}
            composerActionButtonEnabled
            commandQuery=""
            commands={[]}
            attachments={[]}
            chrome={chrome}
            theme={theme}
            modelControl={null}
            onSelectCommand={NOOP}
            onToggleActionMenu={NOOP}
            onDismissActionMenu={NOOP}
            onRemoveAttachment={NOOP}
            onDraftChange={setCompactDraft}
            onUploadPress={NOOP}
            onCancelUpload={NOOP}
            onInputFocus={NOOP}
            onInputBlur={NOOP}
            onSendPress={NOOP}
            onStopPress={NOOP}
          />,
        )}
        {narrowRow(
          "Compact · draft · no model control",
          <InterfaceChatComposer
            inputRef={draftRef}
            draft="Queue a careful review of the keyboard overlay."
            placeholder="Message the agent"
            editable
            focused={false}
            canAttach
            uploading={false}
            activeUpload={null}
            sendEnabled
            sending={false}
            sendLabel="Send message"
            showStopButton={false}
            stopEnabled={false}
            stopLabel="Stop current turn"
            stopLoading={false}
            bottomPadding={8}
            showActionMenuButton
            actionMenuIcon="add"
            composerLayout="telegram"
            showAttachmentRail
            showCommandMenu={false}
            showCommandList={false}
            showComposerActions={false}
            composerActionButtonEnabled
            commandQuery=""
            commands={[]}
            attachments={[]}
            chrome={chrome}
            theme={theme}
            modelControl={null}
            onSelectCommand={NOOP}
            onToggleActionMenu={NOOP}
            onDismissActionMenu={NOOP}
            onRemoveAttachment={NOOP}
            onDraftChange={NOOP}
            onUploadPress={NOOP}
            onCancelUpload={NOOP}
            onInputFocus={NOOP}
            onInputBlur={NOOP}
            onSendPress={NOOP}
            onStopPress={NOOP}
          />,
        )}
        {narrowRow(
          "Expanded · focused · model control",
          <InterfaceChatComposer
            inputRef={expandedRef}
            draft={expandedDraft}
            placeholder="Message the agent"
            editable
            focused
            canAttach
            uploading={false}
            activeUpload={null}
            sendEnabled={false}
            sending={false}
            sendLabel="Send message"
            showStopButton={false}
            stopEnabled={false}
            stopLabel="Stop current turn"
            stopLoading={false}
            bottomPadding={8}
            showActionMenuButton
            actionMenuIcon="add"
            composerLayout="telegram"
            showAttachmentRail
            showCommandMenu={false}
            showCommandList={false}
            showComposerActions={false}
            composerActionButtonEnabled
            commandQuery=""
            commands={[]}
            attachments={[]}
            chrome={chrome}
            theme={theme}
            modelControl={modelControl}
            onSelectCommand={NOOP}
            onToggleActionMenu={NOOP}
            onDismissActionMenu={NOOP}
            onRemoveAttachment={NOOP}
            onDraftChange={setExpandedDraft}
            onUploadPress={NOOP}
            onCancelUpload={NOOP}
            onInputFocus={NOOP}
            onInputBlur={NOOP}
            onSendPress={NOOP}
            onStopPress={NOOP}
          />,
        )}
        {narrowRow(
          "Expanded · multiline · attachment · running Stop",
          <InterfaceChatComposer
            inputRef={runningRef}
            draft="Queue a careful review of the keyboard overlay.\nPreserve the visible message anchor.\nVerify every narrow width."
            placeholder="Message the agent"
            editable
            focused
            canAttach
            uploading={false}
            activeUpload={null}
            sendEnabled={false}
            sending={false}
            sendLabel="Queue message"
            showStopButton
            stopEnabled
            stopLabel="Stop current turn"
            stopLoading={false}
            providerActivityStartedAt={providerActivityStartedAt}
            bottomPadding={8}
            showActionMenuButton
            actionMenuIcon="add"
            composerLayout="telegram"
            showAttachmentRail
            showCommandMenu={false}
            showCommandList={false}
            showComposerActions={false}
            composerActionButtonEnabled
            commandQuery=""
            commands={[]}
            attachments={runningAttachment}
            chrome={chrome}
            theme={theme}
            modelControl={modelControl}
            onSelectCommand={NOOP}
            onToggleActionMenu={NOOP}
            onDismissActionMenu={NOOP}
            onRemoveAttachment={NOOP}
            onDraftChange={setRunningDraft}
            onUploadPress={NOOP}
            onCancelUpload={NOOP}
            onInputFocus={NOOP}
            onInputBlur={NOOP}
            onSendPress={NOOP}
            onStopPress={NOOP}
          />,
        )}
        {narrowRow(
          "Expanded · long model label truncates · full label accessible",
          <InterfaceChatComposer
            inputRef={longLabelRef}
            draft=""
            placeholder="Message the agent"
            editable
            focused
            canAttach
            uploading={false}
            activeUpload={null}
            sendEnabled={false}
            sending={false}
            sendLabel="Send message"
            showStopButton={false}
            stopEnabled={false}
            stopLabel="Stop current turn"
            stopLoading={false}
            bottomPadding={8}
            showActionMenuButton
            actionMenuIcon="add"
            composerLayout="telegram"
            showAttachmentRail
            showCommandMenu={false}
            showCommandList={false}
            showComposerActions={false}
            composerActionButtonEnabled
            commandQuery=""
            commands={[]}
            attachments={[]}
            chrome={chrome}
            theme={theme}
            modelControl={longModelControl}
            onSelectCommand={NOOP}
            onToggleActionMenu={NOOP}
            onDismissActionMenu={NOOP}
            onRemoveAttachment={NOOP}
            onDraftChange={NOOP}
            onUploadPress={NOOP}
            onCancelUpload={NOOP}
            onInputFocus={NOOP}
            onInputBlur={NOOP}
            onSendPress={NOOP}
            onStopPress={NOOP}
          />,
        )}
        {narrowRow(
          "Expanded · routed Provider session · switchable",
          <InterfaceChatComposer
            inputRef={routedRef}
            draft=""
            placeholder="Message the agent"
            editable
            focused
            canAttach
            uploading={false}
            activeUpload={null}
            sendEnabled={false}
            sending={false}
            sendLabel="Send message"
            showStopButton={false}
            stopEnabled={false}
            stopLabel="Stop current turn"
            stopLoading={false}
            bottomPadding={8}
            showActionMenuButton
            actionMenuIcon="add"
            composerLayout="telegram"
            showAttachmentRail
            showCommandMenu={false}
            showCommandList={false}
            showComposerActions={false}
            composerActionButtonEnabled
            commandQuery=""
            commands={[]}
            attachments={[]}
            chrome={chrome}
            theme={theme}
            modelControl={routedModelControl}
            onSelectCommand={NOOP}
            onToggleActionMenu={NOOP}
            onDismissActionMenu={NOOP}
            onRemoveAttachment={NOOP}
            onDraftChange={NOOP}
            onUploadPress={NOOP}
            onCancelUpload={NOOP}
            onInputFocus={NOOP}
            onInputBlur={NOOP}
            onSendPress={NOOP}
            onStopPress={NOOP}
            onModelControlPress={() => {
              // Open the native bottom sheet, as the real control does.
              setDemoSheet("routed");
            }}
          />,
        )}
      </ScrollView>

      <SessionModelSheet
        visible={demoSheet !== null}
        loading={false}
        activating={false}
        error={null}
        selection={demoSelection}
        rows={demoRows}
        modelRequired={false}
        effortContract={null}
        effortChoice=""
        onEffortChange={NOOP}
        chrome={chrome}
        onClose={() => setDemoSheet(null)}
        onRetry={NOOP}
        onActivate={(choice) => {
          // Fixture-only same-Session switch: update the current model label
          // and close; no daemon, no Provider mutation.
          setDemoModelId(choice.modelId);
          setDemoSheet(null);
        }}
      />
    </SafeAreaView>
  );
}

function SessionsDemo() {
  const colors = useAppColors();
  const insets = useSafeAreaInsets();
  const topChromeInset = resolvePrimaryAppBarGeometry(insets.top).contentInset;  return (
    <PrimaryDrawerShell activePrimaryRoute="list" onSelectPrimaryRoute={NOOP}>
      <View
        style={[
          styles.flex,
          { backgroundColor: colors.bgPrimary, marginTop: topChromeInset },
        ]}
      >
        <View style={styles.sessionsHeader}>
          <View>
            <Text
              style={[styles.sectionEyebrow, { color: colors.textTertiary }]}
            >
              STUDIO MAC
            </Text>
            <Text
              style={[styles.sectionHeading, { color: colors.textPrimary }]}
            >
              Running on your computer
            </Text>
          </View>
          <View
            style={[
              styles.connectedBadge,
              { backgroundColor: colors.successSoft },
            ]}
          >
            <View
              style={[styles.connectedDot, { backgroundColor: colors.success }]}
            />
            <Text style={[styles.connectedText, { color: colors.success }]}>
              Connected
            </Text>
          </View>
        </View>
        <ScrollView
          contentContainerStyle={{ paddingBottom: Math.max(insets.bottom, 16) }}
          showsVerticalScrollIndicator={false}
        >
          {SCREENSHOT_SESSION_AGENTS.map((agent) => (
            <AgentListRowContainer
              key={agent.key}
              agent={agent}
              showServerName={false}
              onOpenAgent={NOOP}
              onOpenContextMenu={NOOP}
            />
          ))}
        </ScrollView>
      </View>
    </PrimaryDrawerShell>
  );
}

function StatsDemo() {
  const colors = useAppColors();
  return (
    <SafeAreaView
      style={[styles.flex, { backgroundColor: colors.bgPrimary }]}
      edges={["top", "bottom"]}
    >
      <View
        style={[styles.statsHeader, { borderBottomColor: colors.borderSubtle }]}
      >
        <Ionicons name="stats-chart" size={20} color={colors.accent} />
        <View style={styles.flex}>
          <Text style={[styles.statsTitle, { color: colors.textPrimary }]}>
            Stats
          </Text>
          <Text style={[styles.statsSubtitle, { color: colors.textTertiary }]}>
            All activity · fictional demo
          </Text>
        </View>
      </View>
      <StatsScreenshotDemo
        statsData={SCREENSHOT_STATS_FIXTURE as unknown as StatsPayload}
      />
    </SafeAreaView>
  );
}

function ProvidersDemo() {
  const params = useLocalSearchParams<{
    fixture?: string | string[];
    editor?: string | string[];
    keyboard?: string | string[];
    picker?: string | string[];
  }>();
  const demo = useMemo(() => resolveScreenshotProvidersDemo(params), [params]);
  const modelPicker = useMemo(() => {
    const raw = Array.isArray(params.picker) ? params.picker[0] : params.picker;
    if (raw !== "1") return null;
    const codex = demo.catalog.connections
      .filter(
        (candidate) =>
          candidate.clients.includes("codex") && candidate.credential_ready,
      )
      .sort(
        (left, right) =>
          (demo.catalog.models[right.id]?.length ?? 0) -
          (demo.catalog.models[left.id]?.length ?? 0),
      )[0];
    if (!codex) return null;
    return {
      client: "codex" as const,
      connection: codex,
      models: demo.catalog.models[codex.id] ?? [],
    };
  }, [demo.catalog, params]);
  return (
    <SafeAreaView style={styles.flex} edges={["top"]}>
      <ProvidersPresentation
        catalog={demo.catalog}
        loading={false}
        refreshing={false}
        error={null}
        offline={false}
        unavailable={false}
        currentServerAvailable
        editor={demo.editor}
        mutating={false}
        apiKeyAutoFocus={demo.apiKeyAutoFocus}
        onRefresh={NOOP}
        onOpenSettings={NOOP}
        onOpenEditor={NOOP}
        onCloseEditor={NOOP}
        onDelete={NOOP}
        onUseDirect={NOOP}
        onSetDefault={NOOP}
        onDiscover={NOOP}
        modelPicker={modelPicker}
        onCloseModelPicker={NOOP}
        onSelectModel={NOOP}
        onTestConnection={async () => ({
          client: "codex",
          modelCount: 3,
          latencyMs: 128,
        })}
        onTestConnectionById={async () => ({
          client: "codex",
          modelCount: 3,
          latencyMs: 128,
        })}
        onSaveProvider={() => ({ status: "saved" })}
      />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  demoHeaderOverlay: {
    position: "absolute",
    top: 0,
    right: 0,
    left: 0,
    zIndex: 20,
  },
  sessionsHeader: {
    minHeight: 74,
    paddingHorizontal: 16,
    paddingVertical: 12,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    gap: 12,
  },
  sectionEyebrow: {
    ...UiTextMetrics,
    ...TypeScale.micro,
    letterSpacing: 0.8,
  },
  sectionHeading: {
    ...UiTextMetrics,
    ...TypeScale.body,
    marginTop: 2,
  },
  connectedBadge: {
    minHeight: 28,
    paddingHorizontal: 9,
    borderRadius: 14,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
  },
  connectedDot: { width: 6, height: 6, borderRadius: 3 },
  connectedText: { ...UiTextMetrics, ...TypeScale.micro },
  statsHeader: {
    minHeight: 58,
    paddingHorizontal: 16,
    flexDirection: "row",
    alignItems: "center",
    gap: 11,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  statsTitle: { ...UiTextMetrics, ...TypeScale.body },
  statsSubtitle: { ...UiTextMetrics, ...TypeScale.micro, marginTop: 1 },
  composerDemoBody: {
    paddingTop: 16,
    paddingHorizontal: 12,
    gap: 18,
  },
  composerStateColumn: {
    gap: 6,
  },
  composerStateLabel: {
    ...UiTextMetrics,
    ...TypeScale.micro,
    marginLeft: 2,
  },
  composerStateRow: {
    width: 320,
    alignSelf: "center",
  },
});
