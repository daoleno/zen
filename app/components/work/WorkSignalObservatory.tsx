import React, {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  ActivityIndicator,
  FlatList,
  type ListRenderItem,
  Modal,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import Animated, { FadeIn, useReducedMotion } from "react-native-reanimated";
import { SafeAreaView } from "react-native-safe-area-context";
import {
  isAgentSessionListFreshForConnection,
  useAgents,
  type Agent,
} from "../../store/agents";
import { useBrain } from "../../store/brain";
import { useCurrentServer } from "../../store/currentServer";
import type { StoredAgentAliases } from "../../services/storage";
import { presentAgent } from "../../services/agentPresentation";
import {
  acceptSessionResourceSnapshotResponse,
} from "../../services/sessionResourceSnapshot";
import { wsClient } from "../../services/websocket";
import {
  Radii,
  TypeScale,
  UiTextMetrics,
  useAppTheme,
} from "../../constants/tokens";
import type { ResolvedZenTheme } from "../../theme";
import { surfacesFromTheme } from "../../constants/themedSurfaces";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import {
  buildWorkResourcePresentation,
  buildWorkSignalObservatoryProjection,
  projectWorkResourceRequest,
  reconcileStableWorkSignalItems,
  workResourceRequestIdentity,
  type WorkResourcePresentation,
  type WorkResourceRequestState,
  type WorkSignalItem,
  type WorkSignalOwner,
  type WorkSignalTone,
} from "./workSignalObservatoryModel";
import { resolveWorkObservatoryMotion } from "./workSignalObservatoryInteraction";

type WorkSignalObservatoryProps = {
  visible: boolean;
  aliases: StoredAgentAliases;
  onClose(): void;
  onOpenSession(agent: Agent): void;
};

const MAINTAIN_VISIBLE_POSITION = { minIndexForVisible: 0 } as const;
const EMPTY_WORK_SIGNAL_ITEMS: readonly WorkSignalItem[] = [];

export function WorkSignalObservatory({
  visible,
  aliases,
  onClose,
  onOpenSession,
}: WorkSignalObservatoryProps) {
  const { theme } = useAppTheme();
  const reducedMotion = useReducedMotion();
  const motion = resolveWorkObservatoryMotion(reducedMotion);
  const styles = useMemo(() => createStyles(theme), [theme]);
  const { state: agentState } = useAgents();
  const { state: brainState } = useBrain();
  const {
    currentServer,
    currentServerId,
    hydrated: currentServerHydrated,
  } = useCurrentServer();
  const serverId = currentServerId;
  const brain = serverId ? brainState.byServer[serverId] : undefined;
  const connectionState = serverId
    ? agentState.serverConnections[serverId] ?? "offline"
    : "offline";
  const connected = connectionState === "connected";
  const connectionGeneration = serverId
    ? agentState.connectionGenerationByServer[serverId] ?? 0
    : 0;
  const agentListFresh = serverId
    ? isAgentSessionListFreshForConnection(agentState, serverId)
    : false;
  const brainHydrated = currentServerHydrated && Boolean(brain?.hydrated);

  const currentAgents = useMemo(
    () => agentState.agents.filter((agent) => agent.serverId === serverId),
    [agentState.agents, serverId],
  );
  const agentById = useMemo(
    () => new Map(currentAgents.map((agent) => [agent.id, agent] as const)),
    [currentAgents],
  );
  const owners = useMemo<WorkSignalOwner[]>(
    () =>
      currentAgents.map((agent) => ({
        sessionId: agent.id,
        label: presentAgent(agent, aliases[agent.key]).title,
        status: agent.status,
        delegated: agent.delegated === true,
        needsAttention: agent.needs_attention === true,
      })),
    [aliases, currentAgents],
  );
  const projection = useMemo(
    () =>
      buildWorkSignalObservatoryProjection({
        brainHydrated,
        agentListFresh,
        work: brain?.active_work ?? [],
        owners,
      }),
    [agentListFresh, brain?.active_work, brainHydrated, owners],
  );
  const model = projection.state === "ready" ? projection.model : null;
  const projectionScopeKey = `${serverId ?? ""}\u0000${connectionGeneration}`;
  const items = useStableWorkSignalItems(
    projectionScopeKey,
    model?.items ?? EMPTY_WORK_SIGNAL_ITEMS,
  );
  const resourceSessionId = useMemo(
    () =>
      items.find(
        (item) => item.stage === "owned" && item.targetSessionId,
      )?.targetSessionId ?? null,
    [items],
  );
  const resource = useWorkResourceSnapshot({
    serverId,
    sessionId: resourceSessionId,
    connected: visible && connected,
    connectionGeneration,
  });
  const resourcePresentation: WorkResourcePresentation = model
    ? buildWorkResourcePresentation({
        activeCount: model.activeCount,
        ownerCount: resourceSessionId ? model.ownerCount : 0,
        connected,
        loading: resource.status === "loading",
        snapshot: resource.snapshot,
        failed: resource.status === "failed",
      })
    : {
        state: connected ? "loading" : "unavailable",
        label: connected ? "Updating" : "Resources paused",
      };

  const openSession = useCallback(
    (agent: Agent) => {
      onClose();
      onOpenSession(agent);
    },
    [onClose, onOpenSession],
  );
  const renderItem = useCallback<ListRenderItem<WorkSignalItem>>(
    ({ item }) => {
      const target = item.targetSessionId
        ? agentById.get(item.targetSessionId)
        : undefined;
      return (
        <WorkSignalRow
          item={item}
          onPress={target ? () => openSession(target) : undefined}
          animateRows={motion.animateRows}
          styles={styles}
        />
      );
    },
    [agentById, motion.animateRows, openSession, styles],
  );
  const emptyState = resolveEmptyState({
    currentServerHydrated,
    hasServer: Boolean(currentServer),
    projectionReady: model !== null,
    connected,
  });
  const liveLabel = connected ? "live" : "last known";
  const summary = model?.summaryLabel ?? "Updating";

  return (
    <Modal
      visible={visible}
      animationType={motion.modalAnimationType}
      presentationStyle="fullScreen"
      onRequestClose={onClose}
    >
      <SafeAreaView style={styles.root} edges={["top", "bottom"]}>
        <View style={styles.topBar}>
          <View style={styles.titleCopy}>
            <Text style={styles.eyebrow} numberOfLines={1}>
              {connected ? "CURRENT SERVER · LIVE" : "CURRENT SERVER"}
            </Text>
            <Text style={styles.title}>Work</Text>
          </View>
          <AnimatedPressable
            style={styles.closeButton}
            preset="press"
            scale={0.94}
            onPress={onClose}
            accessibilityRole="button"
            accessibilityLabel="Close Work"
            hitSlop={8}
          >
            <Ionicons name="close" size={20} color={theme.colors.textPrimary} />
          </AnimatedPressable>
        </View>

        <View
          style={styles.summaryCard}
          accessible
          accessibilityRole="summary"
          accessibilityLabel={`Work, ${liveLabel}, ${summary}, ${resourcePresentation.label}`}
        >
          <SignalGlyph
            tone={headerTone(
              model?.failureCount ?? 0,
              model?.attentionCount ?? 0,
            )}
            styles={styles}
          />
          <View style={styles.summaryCopy}>
            <Text style={styles.summaryLabel} numberOfLines={1}>
              {summary}
            </Text>
            <Text style={styles.summaryDetail} numberOfLines={1}>
              {currentServer?.name || "No current server"}
            </Text>
          </View>
          <ResourceIndicator presentation={resourcePresentation} styles={styles} />
        </View>

        <FlatList
          data={items}
          keyExtractor={workKeyExtractor}
          renderItem={renderItem}
          ItemSeparatorComponent={RowSeparator}
          ListEmptyComponent={
            <ObservatoryState
              title={emptyState.title}
              detail={emptyState.detail}
              loading={emptyState.loading}
              styles={styles}
            />
          }
          contentContainerStyle={[
            styles.listContent,
            (!model || items.length === 0) && styles.emptyListContent,
          ]}
          initialNumToRender={10}
          maxToRenderPerBatch={8}
          windowSize={5}
          removeClippedSubviews={false}
          maintainVisibleContentPosition={MAINTAIN_VISIBLE_POSITION}
          showsVerticalScrollIndicator={false}
        />
      </SafeAreaView>
    </Modal>
  );
}

function WorkSignalRow({
  item,
  animateRows,
  styles,
  onPress,
}: {
  item: WorkSignalItem;
  animateRows: boolean;
  styles: ReturnType<typeof createStyles>;
  onPress?: () => void;
}) {
  const colors = toneColors(item.tone, styles.colors);
  const content = (
    <Animated.View
      key={item.transitionKey}
      entering={animateRows ? FadeIn.duration(180) : undefined}
      style={styles.rowContent}
    >
      <View style={styles.rail} pointerEvents="none">
        <View style={[styles.railLine, { backgroundColor: colors.soft }]} />
        <View style={[styles.signalHalo, { backgroundColor: colors.soft }]}>
          <View style={[styles.signalDot, { backgroundColor: colors.strong }]} />
        </View>
      </View>
      <View style={styles.rowCopy}>
        <Text style={styles.workTitle} numberOfLines={1}>
          {item.title}
        </Text>
        <View style={styles.nextLine}>
          <Ionicons name={signalIcon(item)} size={14} color={colors.strong} />
          <Text style={[styles.nextLabel, { color: colors.strong }]} numberOfLines={1}>
            {item.signalLabel}
          </Text>
          {item.detail ? (
            <Text style={styles.nextDetail} numberOfLines={1}>
              · {item.detail}
            </Text>
          ) : null}
        </View>
      </View>
      {onPress ? (
        <Ionicons name="chevron-forward" size={16} color={styles.colors.textTertiary} />
      ) : null}
    </Animated.View>
  );

  return onPress ? (
    <AnimatedPressable
      style={styles.row}
      preset="press"
      scale={0.985}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={item.accessibilityLabel}
      accessibilityHint="Opens this Session"
    >
      {content}
    </AnimatedPressable>
  ) : (
    <View
      style={styles.row}
      accessible
      accessibilityRole="text"
      accessibilityLabel={item.accessibilityLabel}
    >
      {content}
    </View>
  );
}

function ObservatoryState({
  title,
  detail,
  loading,
  styles,
}: {
  title: string;
  detail: string;
  loading: boolean;
  styles: ReturnType<typeof createStyles>;
}) {
  return (
    <View
      style={styles.emptyState}
      accessible
      accessibilityRole="summary"
      accessibilityLabel={`${title}, ${detail}`}
    >
      {loading ? (
        <ActivityIndicator color={styles.colors.accentStrong} />
      ) : (
        <SignalGlyph tone="complete" styles={styles} />
      )}
      <Text style={styles.emptyTitle}>{title}</Text>
      <Text style={styles.emptyDetail}>{detail}</Text>
    </View>
  );
}

function SignalGlyph({
  tone,
  styles,
}: {
  tone: WorkSignalTone;
  styles: ReturnType<typeof createStyles>;
}) {
  const colors = toneColors(tone, styles.colors);
  return (
    <View style={styles.glyph} pointerEvents="none">
      <View style={[styles.glyphLine, { backgroundColor: colors.soft }]} />
      <View style={[styles.glyphSmall, { backgroundColor: colors.strong }]} />
      <View style={[styles.glyphCore, { backgroundColor: colors.soft, borderColor: colors.strong }]}>
        <View style={[styles.glyphSmall, { backgroundColor: colors.strong }]} />
      </View>
      <View style={[styles.glyphSmall, { backgroundColor: colors.strong }]} />
    </View>
  );
}

function ResourceIndicator({
  presentation,
  styles,
}: {
  presentation: WorkResourcePresentation;
  styles: ReturnType<typeof createStyles>;
}) {
  const tone =
    presentation.state === "pressure"
      ? "failed"
      : presentation.state === "steady"
        ? "complete"
        : presentation.state === "loading"
          ? "active"
          : "muted";
  const colors = toneColors(tone, styles.colors);
  return (
    <View style={styles.resource}>
      <View style={styles.resourceLabelRow}>
        {presentation.state === "loading" ? (
          <ActivityIndicator size="small" color={colors.strong} style={styles.resourceSpinner} />
        ) : (
          <View style={[styles.resourceDot, { backgroundColor: colors.strong }]} />
        )}
        <Text style={styles.resourceLabel} numberOfLines={1}>
          {presentation.label}
        </Text>
      </View>
      {presentation.level != null ? (
        <View style={styles.resourceTrack}>
          <View
            style={[
              styles.resourceFill,
              { backgroundColor: colors.strong, width: `${presentation.level * 100}%` },
            ]}
          />
        </View>
      ) : null}
    </View>
  );
}

function useStableWorkSignalItems(
  scopeKey: string,
  incoming: readonly WorkSignalItem[],
): readonly WorkSignalItem[] {
  const [committed, setCommitted] = useState<{
    scopeKey: string;
    items: readonly WorkSignalItem[];
  }>(() => ({ scopeKey, items: incoming }));
  const stableItems = useMemo(
    () =>
      committed.scopeKey === scopeKey
        ? reconcileStableWorkSignalItems(committed.items, incoming)
        : incoming,
    [committed, incoming, scopeKey],
  );

  useLayoutEffect(() => {
    setCommitted((current) =>
      current.scopeKey === scopeKey && current.items === stableItems
        ? current
        : { scopeKey, items: stableItems },
    );
  }, [scopeKey, stableItems]);

  return stableItems;
}

function useWorkResourceSnapshot({
  serverId,
  sessionId,
  connected,
  connectionGeneration,
}: {
  serverId: string | null;
  sessionId: string | null;
  connected: boolean;
  connectionGeneration: number;
}) {
  const requestSeqRef = useRef(0);
  const identity = workResourceRequestIdentity(
    serverId,
    sessionId,
    connected,
    connectionGeneration,
  );
  const [request, setRequest] = useState<WorkResourceRequestState | null>(null);
  const projection = projectWorkResourceRequest(identity, request);

  useEffect(() => {
    const requestSeq = requestSeqRef.current + 1;
    requestSeqRef.current = requestSeq;
    if (!identity || !serverId || !sessionId) {
      setRequest(null);
      return;
    }

    setRequest({ identity, status: "loading" });
    void wsClient
      .getSessionResourceSnapshot(serverId, sessionId)
      .then((next) => {
        if (
          !acceptSessionResourceSnapshotResponse({
            requestSeq,
            currentSeq: requestSeqRef.current,
            snapshotAgentId: next.agent_id,
            expectedAgentId: sessionId,
          })
        ) {
          return;
        }
        setRequest({ identity, status: "ready", snapshot: next });
      })
      .catch(() => {
        if (requestSeqRef.current === requestSeq) {
          setRequest({ identity, status: "failed" });
        }
      });

    return () => {
      if (requestSeqRef.current === requestSeq) {
        requestSeqRef.current += 1;
      }
    };
  }, [identity, serverId, sessionId]);

  return projection;
}

function resolveEmptyState({
  currentServerHydrated,
  hasServer,
  projectionReady,
  connected,
}: {
  currentServerHydrated: boolean;
  hasServer: boolean;
  projectionReady: boolean;
  connected: boolean;
}) {
  if (!currentServerHydrated) {
    return { title: "Finding current server", detail: "Just a moment", loading: true };
  }
  if (!hasServer) {
    return { title: "Work unavailable", detail: "Connect a current server", loading: false };
  }
  if (!projectionReady) {
    return connected
      ? { title: "Updating", detail: "Reading the latest Work", loading: true }
      : { title: "Work unavailable", detail: "Reconnect to update", loading: false };
  }
  return {
    title: "All clear",
    detail: connected ? "Nothing in progress" : "Last known state",
    loading: false,
  };
}

function signalIcon(item: WorkSignalItem): React.ComponentProps<typeof Ionicons>["name"] {
  if (item.tone === "failed") return "alert-circle-outline";
  if (item.stage === "owned") return "person-outline";
  if (item.stage === "waiting") return "time-outline";
  if (item.stage === "ready") return "arrow-forward-circle-outline";
  if (item.stage === "completed") return "checkmark-circle-outline";
  return "remove-circle-outline";
}

function headerTone(failures: number, attention: number): WorkSignalTone {
  if (failures > 0) return "failed";
  return attention > 0 ? "attention" : "active";
}

function toneColors(
  tone: WorkSignalTone,
  colors: ResolvedZenTheme["colors"],
) {
  if (tone === "active") return { strong: colors.statusRunning, soft: colors.accentSoft };
  if (tone === "waiting") return { strong: colors.statusBlocked, soft: colors.warningSoft };
  if (tone === "attention") return { strong: colors.accentStrong, soft: colors.accentSoft };
  if (tone === "failed") return { strong: colors.dangerText, soft: colors.dangerSoft };
  if (tone === "complete") return { strong: colors.success, soft: colors.successSoft };
  return { strong: colors.textTertiary, soft: colors.surfaceSubtle };
}

const workKeyExtractor = (item: WorkSignalItem) => item.id;
const RowSeparator = () => <View style={rowSeparatorStyle} />;
const rowSeparatorStyle = { height: StyleSheet.hairlineWidth };

function createStyles(theme: ResolvedZenTheme) {
  const colors = theme.colors;
  const surfaces = surfacesFromTheme(theme);
  return Object.assign(
    StyleSheet.create({
      root: { flex: 1, backgroundColor: colors.bgPrimary },
      topBar: {
        minHeight: 64,
        paddingHorizontal: 18,
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "space-between",
      },
      titleCopy: { flex: 1, minWidth: 0 },
      eyebrow: { ...UiTextMetrics, ...TypeScale.micro, color: colors.textTertiary, letterSpacing: 0.8 },
      title: { ...UiTextMetrics, ...TypeScale.title, color: colors.textPrimary, marginTop: 1 },
      closeButton: {
        width: 40,
        height: 40,
        borderRadius: 20,
        alignItems: "center",
        justifyContent: "center",
        backgroundColor: surfaces.subtle,
        borderWidth: StyleSheet.hairlineWidth,
        borderColor: surfaces.border,
      },
      summaryCard: {
        minHeight: 76,
        marginHorizontal: 16,
        marginBottom: 12,
        paddingHorizontal: 14,
        borderRadius: Radii.lg,
        borderWidth: StyleSheet.hairlineWidth,
        borderColor: surfaces.border,
        backgroundColor: surfaces.subtle,
        flexDirection: "row",
        alignItems: "center",
        gap: 11,
      },
      glyph: { width: 42, height: 34, flexDirection: "row", alignItems: "center", justifyContent: "space-between" },
      glyphLine: { position: "absolute", left: 7, right: 7, height: 2, borderRadius: 1 },
      glyphSmall: { width: 6, height: 6, borderRadius: 3 },
      glyphCore: { width: 22, height: 22, borderRadius: 11, borderWidth: 1, alignItems: "center", justifyContent: "center" },
      summaryCopy: { flex: 1, minWidth: 0 },
      summaryLabel: { ...UiTextMetrics, ...TypeScale.label, color: colors.textPrimary },
      summaryDetail: { ...UiTextMetrics, ...TypeScale.caption, color: colors.textTertiary, marginTop: 2 },
      resource: { maxWidth: 120, alignItems: "flex-end", gap: 5 },
      resourceLabelRow: { minHeight: 16, flexDirection: "row", alignItems: "center", justifyContent: "flex-end", gap: 5 },
      resourceLabel: { ...UiTextMetrics, ...TypeScale.micro, color: colors.textSecondary, flexShrink: 1 },
      resourceDot: { width: 5, height: 5, borderRadius: 3 },
      resourceSpinner: { width: 8, height: 8, transform: [{ scale: 0.4 }] },
      resourceTrack: { width: 52, height: 3, borderRadius: 2, overflow: "hidden", backgroundColor: colors.borderSubtle },
      resourceFill: { height: 3, borderRadius: 2 },
      listContent: { paddingHorizontal: 16, paddingBottom: 24 },
      emptyListContent: { flexGrow: 1 },
      row: {
        height: 76,
        justifyContent: "center",
        borderRadius: Radii.md,
        backgroundColor: surfaces.surface,
        overflow: "hidden",
      },
      rowContent: { height: 76, paddingRight: 14, flexDirection: "row", alignItems: "center", gap: 10 },
      rail: { width: 36, height: 76, alignItems: "center", justifyContent: "center" },
      railLine: { position: "absolute", top: 0, bottom: 0, width: StyleSheet.hairlineWidth },
      signalHalo: { width: 18, height: 18, borderRadius: 9, alignItems: "center", justifyContent: "center" },
      signalDot: { width: 7, height: 7, borderRadius: 4 },
      rowCopy: { flex: 1, minWidth: 0 },
      workTitle: { ...UiTextMetrics, ...TypeScale.compact, color: colors.textPrimary },
      nextLine: { minHeight: 20, marginTop: 3, flexDirection: "row", alignItems: "center", gap: 5, minWidth: 0 },
      nextLabel: { ...UiTextMetrics, ...TypeScale.caption, flexShrink: 1, minWidth: 0 },
      nextDetail: { ...UiTextMetrics, ...TypeScale.caption, color: colors.textTertiary, flex: 1, minWidth: 0 },
      emptyState: { flex: 1, alignItems: "center", justifyContent: "center", paddingHorizontal: 32, paddingBottom: 64 },
      emptyTitle: { ...UiTextMetrics, ...TypeScale.title, color: colors.textPrimary, marginTop: 18 },
      emptyDetail: { ...UiTextMetrics, ...TypeScale.body, color: colors.textTertiary, marginTop: 4, textAlign: "center" },
    }),
    { colors },
  );
}
