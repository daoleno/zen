import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  ActivityIndicator,
  type LayoutChangeEvent,
  Modal,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import Animated, {
  FadeIn,
  FadeOut,
  LinearTransition,
  useReducedMotion,
} from "react-native-reanimated";
import Svg, { Circle, Path } from "react-native-svg";
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
  Radii,
  TypeScale,
  UiTextMetrics,
  useAppTheme,
} from "../../constants/tokens";
import type { ResolvedZenTheme } from "../../theme";
import { surfacesFromTheme } from "../../constants/themedSurfaces";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import {
  buildWorkRelationshipGraphProjection,
  layoutWorkRelationshipGraph,
  resolveWorkGraphSelection,
  workGraphOpenSessionAccessibilityLabel,
  type WorkGraphEdgeKind,
  type WorkGraphNode,
  type WorkGraphNodeLayout,
  type WorkGraphOwner,
  type WorkGraphSelection,
  type WorkGraphSessionTarget,
  type WorkGraphState,
  type WorkRelationshipGraphLayout,
  type WorkRelationshipGraphModel,
} from "./workRelationshipGraphModel";
import {
  resolveWorkObservatoryMotion,
  WORK_GRAPH_CONTROL_TOUCH_REGIONS,
} from "./workSignalObservatoryInteraction";

type WorkSignalObservatoryProps = {
  visible: boolean;
  aliases: StoredAgentAliases;
  onClose(): void;
  onOpenSession(agent: Agent): void;
};

type GraphFrame = { width: number; height: number };

const EMPTY_FRAME: GraphFrame = { width: 0, height: 0 };

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
  const [page, setPage] = useState(0);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [graphFrame, setGraphFrame] = useState<GraphFrame>(EMPTY_FRAME);
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
  const brainHydrated = Boolean(brain?.hydrated);
  const graphScopeKey = `${serverId ?? ""}\u0000${connectionGeneration}`;

  const currentAgents = useMemo(
    () => agentState.agents.filter((agent) => agent.serverId === serverId),
    [agentState.agents, serverId],
  );
  const agentById = useMemo(
    () => new Map(currentAgents.map((agent) => [agent.id, agent] as const)),
    [currentAgents],
  );
  const owners = useMemo<WorkGraphOwner[]>(
    () =>
      currentAgents.map((agent) => ({
        sessionId: agent.id,
        label: presentAgent(agent, aliases[agent.key]).title,
        status: agent.status,
        delegated: agent.delegated === true,
        needsAttention: agent.needs_attention === true,
        updatedAt: agent.updated_at,
      })),
    [aliases, currentAgents],
  );
  const projection = useMemo(
    () =>
      buildWorkRelationshipGraphProjection({
        currentServerHydrated,
        hasCurrentServer: Boolean(currentServer),
        brainHydrated,
        agentListFresh,
        work: brain?.active_work ?? [],
        owners,
        page,
      }),
    [
      agentListFresh,
      brain?.active_work,
      brainHydrated,
      currentServer,
      currentServerHydrated,
      owners,
      page,
    ],
  );
  const model = projection.state === "ready" ? projection.model : null;
  const layout = useMemo(
    () =>
      model
        ? layoutWorkRelationshipGraph(model, graphFrame)
        : emptyGraphLayout(graphFrame),
    [graphFrame, model],
  );
  const selection = useMemo(
    () => (model ? resolveWorkGraphSelection(model, selectedNodeId) : null),
    [model, selectedNodeId],
  );

  useEffect(() => {
    if (!visible) return;
    setPage(0);
    setSelectedNodeId(null);
  }, [graphScopeKey, visible]);

  useEffect(() => {
    if (selectedNodeId && !selection) setSelectedNodeId(null);
  }, [selectedNodeId, selection]);

  const handleGraphLayout = useCallback((event: LayoutChangeEvent) => {
    const width = Math.round(event.nativeEvent.layout.width);
    const height = Math.round(event.nativeEvent.layout.height);
    setGraphFrame((current) =>
      current.width === width && current.height === height
        ? current
        : { width, height },
    );
  }, []);
  const handleNodePress = useCallback(
    (node: WorkGraphNode) => {
      if (node.kind === "aggregate") {
        setSelectedNodeId(null);
        setPage((current) =>
          node.pageCount > 1 ? (current + 1) % node.pageCount : 0,
        );
        return;
      }
      if (node.kind === "brain") return;
      setSelectedNodeId((current) => (current === node.id ? null : node.id));
    },
    [],
  );
  const openSelectedSession = useCallback(() => {
    const sessionId = selection?.detail.sessionTarget?.sessionId;
    const agent = sessionId ? agentById.get(sessionId) : undefined;
    if (!agent) return;
    onClose();
    onOpenSession(agent);
  }, [
    agentById,
    onClose,
    onOpenSession,
    selection?.detail.sessionTarget?.sessionId,
  ]);
  const emptyState = resolveEmptyState({
    currentServerHydrated,
    hasServer: Boolean(currentServer),
    projectionState: projection.state,
    connected,
  });
  const openSessionTarget =
    selection?.detail.sessionTarget &&
    agentById.has(selection.detail.sessionTarget.sessionId)
      ? selection.detail.sessionTarget
      : null;

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
              {currentServer?.name || "CURRENT SERVER"}
              {connected ? " · LIVE" : ""}
            </Text>
            <Text style={styles.title} maxFontSizeMultiplier={1.4}>Work</Text>
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

        <GraphLegend styles={styles} />

        <View
          style={styles.graphFrame}
          onLayout={handleGraphLayout}
          accessible={Boolean(model && model.totalWorkCount === 0)}
          accessibilityRole="summary"
          accessibilityLabel={model?.accessibilityLabel}
        >
          {model && model.totalWorkCount > 0 && layout.nodes.length > 0 ? (
            <RelationshipGraph
              model={model}
              layout={layout}
              selection={selection}
              animateGraph={motion.animateGraph}
              styles={styles}
              onNodePress={handleNodePress}
            />
          ) : (
            <ObservatoryState
              title={emptyState.title}
              detail={emptyState.detail}
              loading={emptyState.loading}
              styles={styles}
            />
          )}
        </View>

        <GraphDetailDock
          selection={selection}
          openSessionTarget={openSessionTarget}
          animateGraph={motion.animateGraph}
          onOpenSession={openSelectedSession}
          styles={styles}
        />
      </SafeAreaView>
    </Modal>
  );
}

function RelationshipGraph({
  model,
  layout,
  selection,
  animateGraph,
  styles,
  onNodePress,
}: {
  model: WorkRelationshipGraphModel;
  layout: WorkRelationshipGraphLayout;
  selection: WorkGraphSelection | null;
  animateGraph: boolean;
  styles: ReturnType<typeof createStyles>;
  onNodePress(node: WorkGraphNode): void;
}) {
  const selectedNodeIds = useMemo(
    () => new Set(selection?.selectedNodeIds ?? []),
    [selection?.selectedNodeIds],
  );
  const selectedEdgeIds = useMemo(
    () => new Set(selection?.selectedEdgeIds ?? []),
    [selection?.selectedEdgeIds],
  );

  return (
    <View style={styles.graphCanvas}>
      <Svg
        width={layout.viewport.width}
        height={layout.viewport.height}
        style={StyleSheet.absoluteFill}
        accessible={false}
        importantForAccessibility="no-hide-descendants"
      >
        {layout.edges.map((edge) => (
          <RelationshipEdge
            key={edge.id}
            edge={edge}
            highlighted={!selection || selectedEdgeIds.has(edge.id)}
            styles={styles}
          />
        ))}
      </Svg>
      {layout.nodes.map((nodeLayout) => (
        <GraphNodeView
          key={nodeLayout.node.id}
          layout={nodeLayout}
          selected={selectedNodeIds.has(nodeLayout.node.id)}
          dimmed={Boolean(selection && !selectedNodeIds.has(nodeLayout.node.id))}
          animateGraph={animateGraph}
          styles={styles}
          onPress={() => onNodePress(nodeLayout.node)}
        />
      ))}
      {model.pageCount > 1 ? (
        <View style={styles.pageBadge} pointerEvents="none">
          <Text style={styles.pageBadgeText} maxFontSizeMultiplier={1.2}>
            {model.page + 1}/{model.pageCount}
          </Text>
        </View>
      ) : null}
    </View>
  );
}

function RelationshipEdge({
  edge,
  highlighted,
  styles,
}: {
  edge: WorkRelationshipGraphLayout["edges"][number];
  highlighted: boolean;
  styles: ReturnType<typeof createStyles>;
}) {
  const presentation = edgePresentation(edge.kind, styles.colors);
  const opacity = highlighted ? presentation.opacity : 0.13;
  return (
    <>
      <Path
        d={edge.path}
        fill="none"
        stroke={presentation.stroke}
        strokeWidth={presentation.strokeWidth}
        strokeDasharray={presentation.dash}
        strokeLinecap="round"
        opacity={opacity}
      />
      {edge.kind === "review" ? (
        <Circle
          cx={(edge.startX + edge.endX) / 2}
          cy={(edge.startY + edge.endY) / 2}
          r={3.5}
          fill={styles.colors.bgPrimary}
          stroke={presentation.stroke}
          strokeWidth={1.8}
          opacity={opacity}
        />
      ) : null}
    </>
  );
}

function GraphNodeView({
  layout,
  selected,
  dimmed,
  animateGraph,
  styles,
  onPress,
}: {
  layout: WorkGraphNodeLayout;
  selected: boolean;
  dimmed: boolean;
  animateGraph: boolean;
  styles: ReturnType<typeof createStyles>;
  onPress(): void;
}) {
  const { node } = layout;
  const animation = animateGraph ? LinearTransition.duration(180) : undefined;
  const frameStyle = {
    left: layout.x,
    top: layout.y,
    width: layout.width,
    height: layout.height,
    opacity: dimmed ? 0.24 : 1,
  };
  const content = <GraphNodeContent node={node} selected={selected} styles={styles} />;

  return (
    <Animated.View
      entering={animateGraph ? FadeIn.duration(160) : undefined}
      exiting={animateGraph ? FadeOut.duration(120) : undefined}
      layout={animation}
      style={[styles.nodeFrame, frameStyle]}
    >
      {node.kind === "brain" ? (
        <View
          style={[styles.nodeFill, styles.brainNode, selected && styles.nodeSelected]}
          accessible
          accessibilityRole="text"
          accessibilityLabel={node.accessibilityLabel}
        >
          {content}
        </View>
      ) : (
        <AnimatedPressable
          style={[
            styles.nodeFill,
            node.kind === "aggregate"
              ? styles.aggregateNode
              : styles.relationshipNode,
            selected && styles.nodeSelected,
          ]}
          preset="press"
          scale={animateGraph ? 0.96 : 1}
          onPress={onPress}
          accessibilityRole="button"
          accessibilityLabel={node.accessibilityLabel}
          accessibilityState={{ selected }}
          accessibilityHint={
            node.kind === "aggregate"
              ? "Shows the next graph view"
              : "Shows this relationship"
          }
        >
          {content}
        </AnimatedPressable>
      )}
    </Animated.View>
  );
}

function GraphNodeContent({
  node,
  selected,
  styles,
}: {
  node: WorkGraphNode;
  selected: boolean;
  styles: ReturnType<typeof createStyles>;
}) {
  if (node.kind === "brain") {
    return (
      <>
        <View style={styles.brainMark}>
          <Ionicons name="git-network-outline" size={17} color={styles.colors.accentStrong} />
        </View>
        <Text style={styles.brainLabel} maxFontSizeMultiplier={1.25}>
          Brain
        </Text>
      </>
    );
  }
  if (node.kind === "aggregate") {
    return (
      <View style={styles.aggregateContent}>
        <Ionicons name="sync-outline" size={13} color={styles.colors.accentStrong} />
        <View style={styles.aggregateCopy}>
          <Text style={styles.aggregateTitle} numberOfLines={1} maxFontSizeMultiplier={1.3}>
            {node.title}
          </Text>
          <Text style={styles.aggregateHint} numberOfLines={1} maxFontSizeMultiplier={1.2}>
            Next view
          </Text>
        </View>
      </View>
    );
  }
  const stateColors = graphStateColors(node.state, styles.colors);
  if (node.kind === "endpoint") {
    return (
      <View style={styles.endpointContent}>
        <View style={[styles.endpointIcon, { backgroundColor: stateColors.soft }]}>
          <Ionicons
            name={endpointIcon(node.endpointKind)}
            size={12}
            color={stateColors.strong}
          />
        </View>
        <View style={styles.endpointCopy}>
          <Text
            style={styles.endpointTitle}
            numberOfLines={2}
            maxFontSizeMultiplier={1.35}
          >
            {node.title}
          </Text>
          <StateLabel
            state={node.state}
            label={node.stateLabel}
            compact
            styles={styles}
          />
        </View>
      </View>
    );
  }
  return (
    <View style={styles.workNodeContent}>
      <View style={styles.workStateRow}>
        <StateLabel
          state={node.state}
          label={node.stateLabel}
          compact
          styles={styles}
        />
        {selected ? (
          <Ionicons name="locate" size={10} color={stateColors.strong} />
        ) : null}
      </View>
      <Text
        style={styles.workNodeTitle}
        numberOfLines={2}
        ellipsizeMode="tail"
        maxFontSizeMultiplier={1.35}
      >
        {node.title}
      </Text>
    </View>
  );
}

function StateLabel({
  state,
  label,
  compact,
  styles,
}: {
  state: WorkGraphState;
  label: string;
  compact?: boolean;
  styles: ReturnType<typeof createStyles>;
}) {
  const colors = graphStateColors(state, styles.colors);
  return (
    <View style={[styles.stateLabel, compact && styles.stateLabelCompact]}>
      <View style={[styles.stateDot, { backgroundColor: colors.strong }]} />
      <Text
        style={[
          styles.stateLabelText,
          compact && styles.stateLabelTextCompact,
          { color: colors.strong },
        ]}
        numberOfLines={1}
        maxFontSizeMultiplier={1.3}
      >
        {label}
      </Text>
    </View>
  );
}

function GraphDetailDock({
  selection,
  openSessionTarget,
  animateGraph,
  onOpenSession,
  styles,
}: {
  selection: WorkGraphSelection | null;
  openSessionTarget: WorkGraphSessionTarget | null;
  animateGraph: boolean;
  onOpenSession(): void;
  styles: ReturnType<typeof createStyles>;
}) {
  return (
    <View style={styles.detailDock}>
      {selection ? (
        <Animated.View
          key={selection.detail.nodeId}
          entering={animateGraph ? FadeIn.duration(140) : undefined}
          style={styles.detailContent}
        >
          <View style={styles.detailHeading}>
            <Text
              style={styles.detailTitle}
              numberOfLines={2}
              maxFontSizeMultiplier={1.35}
            >
              {selection.detail.title}
            </Text>
            <StateLabel
              state={selection.detail.state}
              label={selection.detail.stateLabel}
              styles={styles}
            />
          </View>
          <View style={styles.detailBottomRow}>
            <Text
              style={styles.detailRelationship}
              numberOfLines={2}
              maxFontSizeMultiplier={1.35}
            >
              {selection.detail.relationshipLabel}
            </Text>
            {openSessionTarget ? (
              <AnimatedPressable
                style={styles.openSessionButton}
                preset="press"
                scale={0.95}
                onPress={onOpenSession}
                accessibilityRole="button"
                accessibilityLabel={workGraphOpenSessionAccessibilityLabel(
                  openSessionTarget,
                )}
              >
                <Ionicons
                  name="arrow-forward"
                  size={13}
                  color={styles.colors.textOnAccent}
                />
                <Text style={styles.openSessionText} maxFontSizeMultiplier={1.25}>
                  Open Session
                </Text>
              </AnimatedPressable>
            ) : null}
          </View>
        </Animated.View>
      ) : (
        <View
          style={styles.detailPrompt}
          accessible
          accessibilityRole="summary"
          accessibilityLabel="Tap a Work or Session node to inspect its relationship"
        >
          <Ionicons name="finger-print-outline" size={18} color={styles.colors.textTertiary} />
          <View style={styles.detailPromptCopy}>
            <Text style={styles.detailPromptTitle} maxFontSizeMultiplier={1.35}>
              Tap a node
            </Text>
            <Text style={styles.detailPromptHint} numberOfLines={1} maxFontSizeMultiplier={1.3}>
              See its owner or wait
            </Text>
          </View>
        </View>
      )}
    </View>
  );
}

function GraphLegend({ styles }: { styles: ReturnType<typeof createStyles> }) {
  return (
    <View style={styles.legend} accessible accessibilityRole="text">
      <LegendItem label="Owns" kind="ownership" styles={styles} />
      <LegendItem label="Waits" kind="wait" styles={styles} />
      <LegendItem label="Review" kind="review" styles={styles} />
    </View>
  );
}

function LegendItem({
  label,
  kind,
  styles,
}: {
  label: string;
  kind: WorkGraphEdgeKind;
  styles: ReturnType<typeof createStyles>;
}) {
  const presentation = edgePresentation(kind, styles.colors);
  return (
    <View style={styles.legendItem}>
      <View style={styles.legendLineWrap}>
        <View
          style={[
            styles.legendLine,
            {
              borderTopColor: presentation.stroke,
              borderTopWidth: presentation.strokeWidth,
              borderStyle: presentation.dash ? "dashed" : "solid",
            },
          ]}
        />
        {kind === "review" ? (
          <View style={[styles.legendReviewDot, { borderColor: presentation.stroke }]} />
        ) : null}
      </View>
      <Text style={styles.legendLabel} maxFontSizeMultiplier={1.25}>
        {label}
      </Text>
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
        <View style={styles.emptyMark}>
          <Ionicons name="git-network-outline" size={20} color={styles.colors.success} />
        </View>
      )}
      <Text style={styles.emptyTitle} maxFontSizeMultiplier={1.4}>
        {title}
      </Text>
      <Text style={styles.emptyDetail} maxFontSizeMultiplier={1.4}>
        {detail}
      </Text>
    </View>
  );
}

function emptyGraphLayout(frame: GraphFrame): WorkRelationshipGraphLayout {
  return { viewport: frame, nodes: [], edges: [] };
}

function resolveEmptyState({
  currentServerHydrated,
  hasServer,
  projectionState,
  connected,
}: {
  currentServerHydrated: boolean;
  hasServer: boolean;
  projectionState: "updating" | "unavailable" | "ready";
  connected: boolean;
}) {
  if (!currentServerHydrated) {
    return { title: "Finding current server", detail: "Just a moment", loading: true };
  }
  if (!hasServer || projectionState === "unavailable") {
    return { title: "Work unavailable", detail: "Connect a current server", loading: false };
  }
  if (projectionState === "updating") {
    return connected
      ? { title: "Updating", detail: "Reading the latest relationships", loading: true }
      : { title: "Work unavailable", detail: "Reconnect to update", loading: false };
  }
  return {
    title: "All clear",
    detail: connected ? "Nothing in progress" : "Reconnect to update",
    loading: false,
  };
}

function endpointIcon(
  kind: "agent" | "wake" | "placeholder",
): React.ComponentProps<typeof Ionicons>["name"] {
  if (kind === "agent") return "person-outline";
  if (kind === "wake") return "notifications-outline";
  return "alert-outline";
}

function graphStateColors(
  state: WorkGraphState,
  colors: ResolvedZenTheme["colors"],
) {
  if (state === "running") {
    return { strong: colors.statusRunning, soft: colors.successSoft };
  }
  if (state === "waiting") {
    return { strong: colors.statusBlocked, soft: colors.warningSoft };
  }
  if (state === "review") {
    return { strong: colors.accentStrong, soft: colors.accentSoft };
  }
  return { strong: colors.dangerText, soft: colors.dangerSoft };
}

function edgePresentation(
  kind: WorkGraphEdgeKind,
  colors: ResolvedZenTheme["colors"],
): {
  stroke: string;
  strokeWidth: number;
  dash?: string;
  opacity: number;
} {
  if (kind === "ownership") {
    return { stroke: colors.textSecondary, strokeWidth: 1.8, opacity: 0.7 };
  }
  if (kind === "wait") {
    return {
      stroke: colors.statusBlocked,
      strokeWidth: 2.1,
      dash: "5 5",
      opacity: 0.9,
    };
  }
  if (kind === "review") {
    return { stroke: colors.accentStrong, strokeWidth: 2.6, opacity: 0.92 };
  }
  if (kind === "blocked") {
    return {
      stroke: colors.dangerText,
      strokeWidth: 2,
      dash: "2 4",
      opacity: 0.82,
    };
  }
  return { stroke: colors.borderStrong, strokeWidth: 1.25, opacity: 0.62 };
}

function createStyles(theme: ResolvedZenTheme) {
  const colors = theme.colors;
  const surfaces = surfacesFromTheme(theme);
  return Object.assign(
    StyleSheet.create({
      root: { flex: 1, backgroundColor: colors.bgPrimary },
      topBar: {
        minHeight: 60,
        paddingHorizontal: 16,
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "space-between",
      },
      titleCopy: { flex: 1, minWidth: 0 },
      eyebrow: {
        ...UiTextMetrics,
        ...TypeScale.micro,
        color: colors.textTertiary,
        letterSpacing: 0.7,
        textTransform: "uppercase",
      },
      title: {
        ...UiTextMetrics,
        ...TypeScale.title,
        color: colors.textPrimary,
        marginTop: 1,
      },
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
      legend: {
        minHeight: 28,
        paddingHorizontal: 18,
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "center",
        gap: 18,
      },
      legendItem: { flexDirection: "row", alignItems: "center", gap: 5 },
      legendLineWrap: {
        width: 22,
        height: 10,
        alignItems: "center",
        justifyContent: "center",
      },
      legendLine: { width: 22, height: 1 },
      legendReviewDot: {
        position: "absolute",
        width: 6,
        height: 6,
        borderRadius: 3,
        borderWidth: 1.3,
        backgroundColor: colors.bgPrimary,
      },
      legendLabel: { ...UiTextMetrics, ...TypeScale.micro, color: colors.textTertiary },
      graphFrame: {
        flex: 1,
        minHeight: 260,
        marginHorizontal: 8,
        marginTop: 4,
        borderRadius: Radii.lg,
        borderWidth: StyleSheet.hairlineWidth,
        borderColor: colors.borderSubtle,
        backgroundColor: surfaces.subtle,
        overflow: "hidden",
      },
      graphCanvas: { flex: 1 },
      nodeFrame: { position: "absolute" },
      nodeFill: {
        flex: 1,
        borderRadius: Radii.sm,
        borderWidth: StyleSheet.hairlineWidth,
        borderColor: surfaces.border,
        backgroundColor: surfaces.surface,
        overflow: "hidden",
      },
      relationshipNode: { justifyContent: "center" },
      nodeSelected: { borderWidth: 1.5, borderColor: colors.focusRing },
      brainNode: { alignItems: "center", justifyContent: "center", gap: 1 },
      brainMark: {
        width: 25,
        height: 24,
        borderRadius: 12,
        alignItems: "center",
        justifyContent: "center",
        backgroundColor: colors.accentSoft,
      },
      brainLabel: { ...UiTextMetrics, ...TypeScale.micro, color: colors.textPrimary },
      workNodeContent: { flex: 1, paddingHorizontal: 8, paddingVertical: 5 },
      workStateRow: {
        minHeight: 13,
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "space-between",
      },
      workNodeTitle: {
        ...UiTextMetrics,
        fontFamily: TypeScale.micro.fontFamily,
        fontSize: 11,
        lineHeight: 14,
        color: colors.textPrimary,
        marginTop: 1,
      },
      endpointContent: {
        flex: 1,
        paddingHorizontal: 7,
        flexDirection: "row",
        alignItems: "center",
        gap: 6,
      },
      endpointIcon: {
        width: 23,
        height: 23,
        borderRadius: 12,
        alignItems: "center",
        justifyContent: "center",
      },
      endpointCopy: { flex: 1, minWidth: 0 },
      endpointTitle: {
        ...UiTextMetrics,
        fontFamily: TypeScale.micro.fontFamily,
        fontSize: 10.5,
        lineHeight: 14,
        color: colors.textPrimary,
      },
      stateLabel: {
        minHeight: 17,
        paddingHorizontal: 7,
        borderRadius: 9,
        flexDirection: "row",
        alignItems: "center",
        gap: 4,
        backgroundColor: colors.surfaceSubtle,
      },
      stateLabelCompact: {
        minHeight: 12,
        paddingHorizontal: 0,
        backgroundColor: "transparent",
      },
      stateDot: { width: 5, height: 5, borderRadius: 3 },
      stateLabelText: {
        ...UiTextMetrics,
        fontFamily: TypeScale.micro.fontFamily,
        fontSize: 10,
        lineHeight: 13,
      },
      stateLabelTextCompact: { fontSize: 8.5, lineHeight: 11 },
      aggregateNode: {
        justifyContent: "center",
        borderColor: colors.accentStrong,
        backgroundColor: colors.accentSoft,
      },
      aggregateContent: {
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "center",
        gap: 6,
      },
      aggregateCopy: { minWidth: 0 },
      aggregateTitle: {
        ...UiTextMetrics,
        fontFamily: TypeScale.label.fontFamily,
        fontSize: 11,
        lineHeight: 14,
        color: colors.accentStrong,
      },
      aggregateHint: {
        ...UiTextMetrics,
        fontFamily: TypeScale.micro.fontFamily,
        fontSize: 8.5,
        lineHeight: 11,
        color: colors.textTertiary,
      },
      pageBadge: {
        position: "absolute",
        top: 7,
        right: 8,
        minWidth: 30,
        height: 18,
        paddingHorizontal: 6,
        borderRadius: 9,
        alignItems: "center",
        justifyContent: "center",
        backgroundColor: colors.modalSurfaceAlt,
        borderWidth: StyleSheet.hairlineWidth,
        borderColor: colors.borderSubtle,
      },
      pageBadgeText: {
        ...UiTextMetrics,
        fontFamily: TypeScale.micro.fontFamily,
        fontSize: 8.5,
        lineHeight: 11,
        color: colors.textTertiary,
      },
      detailDock: {
        height: 124,
        marginHorizontal: 10,
        marginTop: 8,
        marginBottom: 8,
        paddingHorizontal: 12,
        borderRadius: Radii.lg,
        borderWidth: StyleSheet.hairlineWidth,
        borderColor: surfaces.border,
        backgroundColor: surfaces.surface,
        justifyContent: "center",
        overflow: "hidden",
      },
      detailContent: { flex: 1, paddingVertical: 10, justifyContent: "space-between" },
      detailHeading: {
        flexDirection: "row",
        alignItems: "flex-start",
        gap: 10,
      },
      detailTitle: {
        ...UiTextMetrics,
        ...TypeScale.label,
        color: colors.textPrimary,
        flex: 1,
        minWidth: 0,
      },
      detailBottomRow: {
        minHeight: 34,
        flexDirection: "row",
        alignItems: "center",
        gap: 10,
      },
      detailRelationship: {
        ...UiTextMetrics,
        ...TypeScale.caption,
        color: colors.textSecondary,
        flex: 1,
        minWidth: 0,
      },
      openSessionButton: {
        minHeight: WORK_GRAPH_CONTROL_TOUCH_REGIONS.openSessionMinHeight,
        paddingHorizontal: 10,
        borderRadius: 16,
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "center",
        gap: 5,
        backgroundColor: colors.accentStrong,
      },
      openSessionText: {
        ...UiTextMetrics,
        fontFamily: TypeScale.micro.fontFamily,
        fontSize: 10,
        lineHeight: 13,
        color: colors.textOnAccent,
      },
      detailPrompt: {
        flexDirection: "row",
        alignItems: "center",
        justifyContent: "center",
        gap: 10,
      },
      detailPromptCopy: { minWidth: 0 },
      detailPromptTitle: { ...UiTextMetrics, ...TypeScale.label, color: colors.textSecondary },
      detailPromptHint: { ...UiTextMetrics, ...TypeScale.caption, color: colors.textTertiary },
      emptyState: {
        flex: 1,
        alignItems: "center",
        justifyContent: "center",
        paddingHorizontal: 28,
      },
      emptyMark: {
        width: 42,
        height: 42,
        borderRadius: 21,
        alignItems: "center",
        justifyContent: "center",
        backgroundColor: colors.successSoft,
      },
      emptyTitle: {
        ...UiTextMetrics,
        ...TypeScale.heading,
        color: colors.textPrimary,
        marginTop: 12,
      },
      emptyDetail: {
        ...UiTextMetrics,
        ...TypeScale.caption,
        color: colors.textTertiary,
        marginTop: 3,
        textAlign: "center",
      },
    }),
    { colors },
  );
}
