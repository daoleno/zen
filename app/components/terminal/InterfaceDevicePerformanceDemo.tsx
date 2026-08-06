import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  FlatList,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  View,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from "react-native";
import * as Clipboard from "expo-clipboard";
import { useLocalSearchParams } from "expo-router";
import {
  SafeAreaView,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
import { InterfaceChatComposer } from "./InterfaceChatComposer";
import { InterfaceChatKeyboardFrame } from "./InterfaceChatKeyboardFrame";
import { InterfaceTimelineView } from "./InterfaceTimelineView";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import {
  patchDisplayPath,
  truncateRunes,
} from "./InterfaceTimelineModel";
import { useInterfaceTimelineItems } from "./useInterfaceTimelineItems";
import { TerminalTopBar } from "./TerminalTopBar";
import {
  CHAT_HEADER_HEIGHT,
  CHAT_HEADER_OUTER_GAP,
} from "./chatChromeMetrics";
import { TypeScale, UiTextMetrics, useAppTheme } from "../../constants/tokens";
import { buildChatChrome } from "../../theme";
import {
  prepareInterfaceDevicePerfScenario,
  resolveInterfaceDevicePerfScenario,
  type InterfaceDevicePerfScenarioId,
  type InterfaceDevicePerfScenarioState,
} from "./interfaceDevicePerformanceScenarios";
import {
  INTERFACE_DEVICE_PERF_LAUNCH_HINTS,
  createInterfaceDevicePerfRunner,
  startJsFrameGapSampler,
} from "./interfaceDevicePerformanceHarness";
import { usePinnedTimeline } from "./InterfaceChatSurfaceHooks";
import { TIMELINE_BOTTOM_THRESHOLD } from "./timelineScrollPolicy";
import {
  disableTimelineProjectionPerf,
  enableTimelineProjectionPerf,
  formatTimelineProjectionPerfDeviceSummary,
  getTimelineProjectionPerfScenarioRevision,
  resetTimelineProjectionPerf,
  setTimelineProjectionPerfScenarioRevision,
} from "./timelineProjectionPerf";

const NOOP = () => undefined;
const loadNoDemoAsset = async () => null;

/**
 * Enables and clears the content-free collector before the measured timeline
 * can mount. A scenario change unmounts the old child before starting a new
 * collection.
 */
export function InterfaceDevicePerformanceDemoGate() {
  const params = useLocalSearchParams<{
    scenario?: string | string[];
  }>();
  const scenarioId = resolveInterfaceDevicePerfScenario(params.scenario);
  const [readyScenarioId, setReadyScenarioId] =
    useState<InterfaceDevicePerfScenarioId | null>(null);

  useEffect(() => {
    disableTimelineProjectionPerf();
    resetTimelineProjectionPerf();
    enableTimelineProjectionPerf();
    setReadyScenarioId(scenarioId);
    return () => {
      disableTimelineProjectionPerf();
      resetTimelineProjectionPerf();
    };
  }, [scenarioId]);

  if (readyScenarioId !== scenarioId) {
    return null;
  }
  return (
    <InterfaceDevicePerformanceDemo
      key={scenarioId}
      scenarioId={scenarioId}
    />
  );
}

/**
 * Development-only Interface profiling surface.
 * Reuses the canonical timeline + message body owners; no second list model.
 */
export function InterfaceDevicePerformanceDemo({
  scenarioId,
}: {
  scenarioId: InterfaceDevicePerfScenarioId;
}) {
  const { theme: zenTheme } = useAppTheme();
  const { chrome, theme } = useMemo(
    () => buildChatChrome(zenTheme),
    [zenTheme],
  );
  const menuAnchorRef = useRef<View>(null);
  const inputRef = useRef<TextInput>(null);
  const insets = useSafeAreaInsets();
  const emptyPending = useMemo(() => [], []);
  const detachedInitRef = useRef(false);

  const prepared = useMemo(
    () => prepareInterfaceDevicePerfScenario(scenarioId),
    [scenarioId],
  );

  const [scenarioState, setScenarioState] =
    useState<InterfaceDevicePerfScenarioState>(() => ({
      id: prepared.id,
      revision: 0,
      events: prepared.initialEvents,
      done: prepared.steps.length === 0,
      label: "initial",
      streamChars: 0,
    }));
  const [summaryText, setSummaryText] = useState("");
  const [summaryOpen, setSummaryOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  const timeline = useInterfaceTimelineItems({
    events: scenarioState.events,
    pendingUserMessages: emptyPending,
    onRetryPendingUserMessage: NOOP,
  });

  const topChromeInset = CHAT_HEADER_HEIGHT + CHAT_HEADER_OUTER_GAP * 2;
  const pinned = usePinnedTimeline(timeline.length, scenarioId, topChromeInset);

  useEffect(() => {
    const sampler = startJsFrameGapSampler({
      now: () =>
        (
          globalThis as { performance?: { now(): number } }
        ).performance?.now?.() ?? Date.now(),
      requestAnimationFrame: (cb) => requestAnimationFrame(cb),
      cancelAnimationFrame: (handle) => cancelAnimationFrame(handle),
      scenarioRevision: getTimelineProjectionPerfScenarioRevision,
    });
    return () => sampler.stop();
  }, [scenarioId]);

  useEffect(() => {
    if (!prepared.startsDetached || detachedInitRef.current) {
      return;
    }
    detachedInitRef.current = true;
    pinned.handleScrollBeginDrag();
    pinned.handleScrollEndDrag({
      nativeEvent: {
        contentOffset: { y: TIMELINE_BOTTOM_THRESHOLD + 500 },
        contentInset: { top: 0 },
        velocity: { y: 0 },
      },
    } as NativeSyntheticEvent<NativeScrollEvent>);
  }, [pinned, prepared.startsDetached]);

  useEffect(() => {
    setTimelineProjectionPerfScenarioRevision(0);
    detachedInitRef.current = false;

    const wallClock = {
      now() {
        return (
          (
            globalThis as { performance?: { now(): number } }
          ).performance?.now?.() ?? Date.now()
        );
      },
    };
    const startedAt = wallClock.now();
    const clock = {
      now() {
        return wallClock.now() - startedAt;
      },
    };
    const runner = createInterfaceDevicePerfRunner({
      prepared,
      clock,
      host: {
        publish: setScenarioState,
      },
      schedule(callback, delayMs) {
        const handle = setTimeout(callback, delayMs);
        return {
          cancel() {
            clearTimeout(handle);
          },
        };
      },
    });
    runner.start();
    return () => runner.stop();
  }, [prepared]);

  const refreshSummary = () => {
    setSummaryText(
      formatTimelineProjectionPerfDeviceSummary({
        scenarioId,
        scenarioRevision: getTimelineProjectionPerfScenarioRevision(),
        nativeFollowSuspended: pinned.nativeFollowSuspended,
      }),
    );
    setCopied(false);
  };

  return (
    <SafeAreaView
      style={[styles.flex, { backgroundColor: chrome.appBackground }]}
      edges={["top", "bottom"]}
    >
      <View style={styles.flex}>
        <View pointerEvents="box-none" style={styles.demoHeaderOverlay}>
          <TerminalTopBar
            title="Interface perf"
            subtitle={scenarioId}
            kind="codex"
            backgroundColor={chrome.appBackground}
            chrome={chrome}
            menuAnchorRef={menuAnchorRef}
            interfaceRenderMode="chat"
            gitDiffDisabled
            gitDiffPresentation={{
              accessibilityLabel: "Open changes",
              backgroundColor: chrome.accentSoft,
              iconColor: chrome.accent,
              additionsText: "+0",
              deletionsText: "−0",
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
              draft=""
              placeholder="Profiling harness (read-only)"
              editable={false}
              focused={false}
              canAttach={false}
              uploading={false}
              activeUpload={null}
              sendEnabled={false}
              sending={false}
              sendLabel="Send message"
              showStopButton={false}
              stopEnabled={false}
              stopLabel="Stop"
              stopLoading={false}
              bottomPadding={Math.max(insets.bottom, 8)}
              showActionMenuButton={false}
              actionMenuIcon="add"
              composerLayout="telegram"
              showAttachmentRail={false}
              showCommandMenu={false}
              showCommandList={false}
              showComposerActions={false}
              composerActionButtonEnabled={false}
              commandQuery=""
              commands={[]}
              attachments={[]}
              chrome={chrome}
              theme={theme}
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
            />
          }
          renderTimeline={(extraContentPadding, keyboardLifecycleGate) => (
            <InterfaceTimelineView
              scrollRef={pinned.scrollRef}
              nativeFollowSuspended={pinned.nativeFollowSuspended}
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
              onLayout={pinned.handleLayout}
              onScroll={pinned.handleScroll}
              onScrollBeginDrag={pinned.handleScrollBeginDrag}
              onScrollEndDrag={pinned.handleScrollEndDrag}
              onMomentumScrollBegin={pinned.handleMomentumScrollBegin}
              onMomentumScrollEnd={pinned.handleMomentumScrollEnd}
              onItemsMutated={pinned.handleTimelineItemsMutated}
              onContentSizeChange={pinned.handleContentSizeChange}
              onTextSelectionGestureStart={NOOP}
              onTextSelectionGestureEnd={NOOP}
              loadAssetPreview={loadNoDemoAsset}
              formatPatchPath={patchDisplayPath}
              truncateBody={truncateRunes}
            />
          )}
        />
        <Pressable
          accessibilityRole="button"
          onPress={() => {
            if (!summaryOpen) {
              refreshSummary();
            }
            setSummaryOpen((open) => !open);
          }}
          style={[
            styles.resultsToggle,
            { backgroundColor: chrome.accentSoft },
          ]}
        >
          <Text style={[styles.summaryButtonText, { color: chrome.accent }]}>
            {summaryOpen ? "Hide results" : "Results"}
          </Text>
        </Pressable>
        {summaryOpen ? (
          <View
            pointerEvents="box-none"
            style={[
              styles.summaryCard,
              {
                backgroundColor: chrome.appBackground,
                borderColor: chrome.border,
              },
            ]}
          >
            <Text style={[styles.summaryMeta, { color: chrome.textMuted }]}>
              {scenarioState.label}
              {scenarioState.streamChars > 0
                ? ` · streamChars=${scenarioState.streamChars}`
                : ""}
              {scenarioState.done ? " · done" : " · collecting"}
              {pinned.nativeFollowSuspended ? " · detached" : ""}
            </Text>
            <Text
              selectable
              style={[styles.summaryText, { color: chrome.text }]}
            >
              {summaryText || "No snapshot yet"}
            </Text>
            <View style={styles.summaryActions}>
              <Pressable
                accessibilityRole="button"
                onPress={refreshSummary}
                style={[
                  styles.summaryButton,
                  { backgroundColor: chrome.accentSoft },
                ]}
              >
                <Text
                  style={[styles.summaryButtonText, { color: chrome.accent }]}
                >
                  Refresh
                </Text>
              </Pressable>
              <Pressable
                accessibilityRole="button"
                onPress={() => {
                  void Clipboard.setStringAsync(summaryText).then(() => {
                    setCopied(true);
                  });
                }}
                style={[
                  styles.summaryButton,
                  { backgroundColor: chrome.accentSoft },
                ]}
              >
                <Text
                  style={[styles.summaryButtonText, { color: chrome.accent }]}
                >
                  {copied ? "Copied" : "Copy summary"}
                </Text>
              </Pressable>
            </View>
            <Text style={[styles.launchHint, { color: chrome.textSubtle }]}>
              {INTERFACE_DEVICE_PERF_LAUNCH_HINTS}
            </Text>
          </View>
        ) : null}
      </View>
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
  summaryCard: {
    position: "absolute",
    left: 10,
    right: 10,
    bottom: 10,
    maxHeight: "42%",
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 12,
    padding: 10,
    gap: 6,
    zIndex: 30,
  },
  resultsToggle: {
    position: "absolute",
    right: 10,
    bottom: 10,
    borderRadius: 8,
    paddingHorizontal: 10,
    paddingVertical: 6,
    zIndex: 31,
  },
  summaryMeta: {
    ...UiTextMetrics,
    ...TypeScale.micro,
  },
  summaryText: {
    ...UiTextMetrics,
    ...TypeScale.micro,
    fontFamily: "monospace",
  },
  summaryActions: {
    flexDirection: "row",
    gap: 8,
  },
  summaryButton: {
    paddingHorizontal: 10,
    paddingVertical: 6,
    borderRadius: 8,
  },
  summaryButtonText: {
    ...UiTextMetrics,
    ...TypeScale.micro,
  },
  launchHint: {
    ...UiTextMetrics,
    ...TypeScale.micro,
  },
});
