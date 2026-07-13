import React, { useEffect, useMemo, useRef } from "react";
import {
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useLocalSearchParams, useRouter } from "expo-router";
import { SafeAreaView, useSafeAreaInsets } from "react-native-safe-area-context";
import { AgentListRowContainer } from "../components/agents/AgentListRowContainer";
import { PrimaryDrawerShell } from "../components/navigation/PrimaryDrawerShell";
import { ChatCanvas } from "../components/terminal/ChatCanvas";
import {
  ZenTimelineItemView,
  type ZenTimelineItem,
} from "../components/terminal/CodexTimelineItemView";
import {
  buildZenTimeline,
  patchDisplayPath,
  truncateRunes,
} from "../components/terminal/CodexTimelineModel";
import { TerminalTopBar } from "../components/terminal/TerminalTopBar";
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
} from "../services/screenshotDemoFixtures";
import {
  resolveScreenshotDemoState,
  screenshotDemoEnabled,
} from "../services/screenshotDemo";
import {
  StatsScreenshotDemo,
  type StatsPayload,
} from "./stats";

const NOOP = () => undefined;
const loadNoDemoAsset = async () => null;

export default function ScreenshotDemoRoute() {
  const router = useRouter();
  const params = useLocalSearchParams<{ state?: string | string[] }>();
  const state = resolveScreenshotDemoState(params.state);
  const enabled = screenshotDemoEnabled();

  useEffect(() => {
    if (!enabled) router.replace("/onboarding");
  }, [enabled, router]);

  if (!enabled) return null;

  switch (state) {
    case "sessions":
      return <SessionsDemo />;
    case "brain":
      return <BrainDemo />;
    case "stats":
      return <StatsDemo />;
    case "chat":
    default:
      return <ChatDemo />;
  }
}

function ChatDemo() {
  const { theme: zenTheme } = useAppTheme();
  const { chrome, theme } = useMemo(
    () => buildChatChrome(zenTheme),
    [zenTheme],
  );
  const menuAnchorRef = useRef<View>(null);
  const timeline = useMemo(
    () => buildZenTimeline(SCREENSHOT_CHAT_EVENTS),
    [],
  );

  return (
    <SafeAreaView
      style={[styles.flex, { backgroundColor: chrome.appBackground }]}
      edges={["top", "bottom"]}
    >
      <TerminalTopBar
        title="Mobile handoff"
        subtitle="atlas-notes · Chat"
        kind="codex"
        backgroundColor={chrome.appBackground}
        chrome={chrome}
        menuAnchorRef={menuAnchorRef}
        codexRenderMode="chat"
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
        onOpenPicker={NOOP}
        onOpenGitDiff={NOOP}
        onOpenMenu={NOOP}
        onToggleCodexRenderMode={NOOP}
      />
      <DemoTimeline
        timeline={timeline}
        chrome={chrome}
        theme={theme}
        progressLabel="Focused checks passed · 320 px review in progress"
      />
      <DemoComposer chrome={chrome} placeholder="Message the agent" />
    </SafeAreaView>
  );
}

function BrainDemo() {
  const { theme: zenTheme } = useAppTheme();
  const { chrome, theme } = useMemo(
    () => buildChatChrome(zenTheme),
    [zenTheme],
  );
  const timeline = useMemo(
    () => buildZenTimeline(SCREENSHOT_BRAIN_EVENTS),
    [],
  );

  return (
    <PrimaryDrawerShell activePrimaryRoute="brain" onSelectPrimaryRoute={NOOP}>
      <ChatCanvas chrome={chrome}>
        <DemoTimeline
          timeline={timeline}
          chrome={chrome}
          theme={theme}
          progressLabel="Mobile QA agent · accessibility checks in progress"
        />
        <DemoComposer chrome={chrome} placeholder="Ask Brain" />
      </ChatCanvas>
    </PrimaryDrawerShell>
  );
}

function SessionsDemo() {
  const colors = useAppColors();
  const insets = useSafeAreaInsets();
  return (
    <PrimaryDrawerShell activePrimaryRoute="list" onSelectPrimaryRoute={NOOP}>
      <View style={[styles.flex, { backgroundColor: colors.bgPrimary }]}>
        <View style={styles.sessionsHeader}>
          <View>
            <Text style={[styles.sectionEyebrow, { color: colors.textTertiary }]}>STUDIO MAC</Text>
            <Text style={[styles.sectionHeading, { color: colors.textPrimary }]}>Running on your computer</Text>
          </View>
          <View style={[styles.connectedBadge, { backgroundColor: colors.successSoft }]}>
            <View style={[styles.connectedDot, { backgroundColor: colors.success }]} />
            <Text style={[styles.connectedText, { color: colors.success }]}>Connected</Text>
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
      <View style={[styles.statsHeader, { borderBottomColor: colors.borderSubtle }]}>
        <Ionicons name="stats-chart" size={20} color={colors.accent} />
        <View style={styles.flex}>
          <Text style={[styles.statsTitle, { color: colors.textPrimary }]}>Stats</Text>
          <Text style={[styles.statsSubtitle, { color: colors.textTertiary }]}>All activity · fictional demo</Text>
        </View>
      </View>
      <StatsScreenshotDemo
        statsData={SCREENSHOT_STATS_FIXTURE as unknown as StatsPayload}
      />
    </SafeAreaView>
  );
}

function DemoTimeline({
  timeline,
  chrome,
  theme,
  progressLabel,
}: {
  timeline: ZenTimelineItem[];
  chrome: ReturnType<typeof buildChatChrome>["chrome"];
  theme: ReturnType<typeof buildChatChrome>["theme"];
  progressLabel: string;
}) {
  const stableTimeline = timeline.filter((item) => item.type !== "activity");
  return (
    <ScrollView
      style={styles.flex}
      contentContainerStyle={styles.timelineContent}
      showsVerticalScrollIndicator={false}
    >
      {stableTimeline.map((item, index) => (
        <React.Fragment key={item.id}>
          <ZenTimelineItemView
            item={item}
            chrome={chrome}
            theme={theme}
            loadAssetPreview={loadNoDemoAsset}
            formatPatchPath={patchDisplayPath}
            truncateBody={truncateRunes}
          />
          {index === stableTimeline.length - 2 ? (
            <View style={styles.progressRow}>
              <Ionicons name="sync" size={14} color={chrome.accent} />
              <Text style={[styles.progressText, { color: chrome.textMuted }]}>
                {progressLabel}
              </Text>
            </View>
          ) : null}
        </React.Fragment>
      ))}
    </ScrollView>
  );
}

function DemoComposer({
  chrome,
  placeholder,
}: {
  chrome: ReturnType<typeof buildChatChrome>["chrome"];
  placeholder: string;
}) {
  return (
    <View style={[styles.demoComposerWrap, { backgroundColor: chrome.appBackground }]}>
      <View
        style={[
          styles.demoComposer,
          { backgroundColor: chrome.composerInput, borderColor: chrome.border },
        ]}
      >
        <Ionicons name="add" size={20} color={chrome.textSubtle} />
        <Text style={[styles.demoComposerPlaceholder, { color: chrome.textSubtle }]}>
          {placeholder}
        </Text>
        <View style={[styles.demoSend, { backgroundColor: chrome.accentSoft }]}>
          <Ionicons name="arrow-up" size={16} color={chrome.accent} />
        </View>
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  flex: { flex: 1 },
  timelineContent: {
    paddingHorizontal: 14,
    paddingTop: 12,
    paddingBottom: 8,
  },
  progressRow: {
    minHeight: 34,
    marginBottom: 8,
    paddingHorizontal: 2,
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
  },
  progressText: {
    ...UiTextMetrics,
    ...TypeScale.caption,
    flex: 1,
  },
  demoComposerWrap: {
    paddingHorizontal: 12,
    paddingTop: 8,
    paddingBottom: 8,
  },
  demoComposer: {
    minHeight: 48,
    borderRadius: 24,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 12,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  demoComposerPlaceholder: {
    ...UiTextMetrics,
    ...TypeScale.body,
    flex: 1,
  },
  demoSend: {
    width: 32,
    height: 32,
    borderRadius: 16,
    alignItems: "center",
    justifyContent: "center",
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
});
