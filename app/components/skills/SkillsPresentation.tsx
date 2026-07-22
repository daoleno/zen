import React from "react";
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import {
  Radii,
  TypeScale,
  Typography,
  useAppColors,
} from "../../constants/tokens";
import type { AgentKind } from "../../services/agentPresentation";
import type {
  CatalogSkill,
  InstalledSkill,
  ManagedSkillAgent,
  RankedCatalogSkill,
  SkillsCatalogResult,
  SkillsLeaderboard,
  SkillsLeaderboards,
  SkillsRequestState,
} from "../../services/skillsManagement";
import { skillAgentLabel, scopeLabel } from "../../services/skillsManagement";
import {
  MANAGED_SKILL_AGENTS,
  skillsEmptyLeaderboardCopy,
  skillsLeaderboardLabel,
  skillsRemovalPlanForAgent,
  type SkillsAgentCounts,
  type SkillsLeaderboardView,
} from "../../services/skillsScreenModel";
import { AgentKindIcon } from "../terminal/AgentKindIcon";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";

export type SkillsMode = "installed" | "discover";

export interface SkillsPresentationProps {
  mode: SkillsMode;
  selectedAgent: ManagedSkillAgent;
  agentCounts: SkillsAgentCounts;
  inventoryState: SkillsRequestState<unknown>;
  installedSkills: InstalledSkill[];
  inventoryWarnings: string[];
  catalogState: SkillsRequestState<SkillsLeaderboards>;
  leaderboard?: SkillsLeaderboard;
  searchState: SkillsRequestState<SkillsCatalogResult>;
  searchResult?: SkillsCatalogResult;
  query: string;
  submittedQuery: string;
  leaderboardView: SkillsLeaderboardView;
  preparingMutation: string;
  creatingTerminal: boolean;
  currentServerAvailable: boolean;
  onSelectMode(mode: SkillsMode): void;
  onSelectAgent(agent: ManagedSkillAgent): void;
  onOpenSettings(): void;
  onRefreshInventory(): void;
  onRemove(skill: InstalledSkill): void;
  onChangeQuery(value: string): void;
  onSubmitSearch(): void;
  onClearSearch(): void;
  onSelectLeaderboard(view: SkillsLeaderboardView): void;
  onRetryCatalog(): void;
  onRetrySearch(): void;
  onInstall(skill: CatalogSkill | RankedCatalogSkill): void;
}

export function SkillsPresentation({
  mode,
  selectedAgent,
  agentCounts,
  inventoryState,
  installedSkills,
  inventoryWarnings,
  catalogState,
  leaderboard,
  searchState,
  searchResult,
  query,
  submittedQuery,
  leaderboardView,
  preparingMutation,
  creatingTerminal,
  currentServerAvailable,
  onSelectMode,
  onSelectAgent,
  onOpenSettings,
  onRefreshInventory,
  onRemove,
  onChangeQuery,
  onSubmitSearch,
  onClearSearch,
  onSelectLeaderboard,
  onRetryCatalog,
  onRetrySearch,
  onInstall,
}: SkillsPresentationProps) {
  const colors = useAppColors();

  return (
    <SafeAreaView
      style={[styles.root, { backgroundColor: colors.bgPrimary }]}
      edges={["bottom"]}
    >
      <View style={styles.chrome}>
        <AgentSelector
          selectedAgent={selectedAgent}
          counts={agentCounts}
          onSelect={onSelectAgent}
        />
        <ModeSwitch mode={mode} onSelect={onSelectMode} />
      </View>

      {mode === "installed" ? (
        <InstalledSkillsList
          selectedAgent={selectedAgent}
          state={inventoryState}
          skills={installedSkills}
          warnings={inventoryWarnings}
          currentServerAvailable={currentServerAvailable}
          refreshing={inventoryState.status === "loading"}
          preparingMutation={preparingMutation}
          onOpenSettings={onOpenSettings}
          onRefresh={onRefreshInventory}
          onRemove={onRemove}
        />
      ) : (
        <DiscoverSkillsList
          selectedAgent={selectedAgent}
          query={query}
          submittedQuery={submittedQuery}
          view={leaderboardView}
          catalogState={catalogState}
          leaderboard={leaderboard}
          searchState={searchState}
          searchResult={searchResult}
          currentServerAvailable={currentServerAvailable}
          preparingMutation={preparingMutation}
          onOpenSettings={onOpenSettings}
          onChangeQuery={onChangeQuery}
          onSubmitSearch={onSubmitSearch}
          onClearSearch={onClearSearch}
          onSelectView={onSelectLeaderboard}
          onRetryCatalog={onRetryCatalog}
          onRetrySearch={onRetrySearch}
          onInstall={onInstall}
        />
      )}

      {creatingTerminal ? (
        <View
          accessibilityRole="progressbar"
          accessibilityLabel="Opening Terminal"
          style={[
            styles.handoffStatus,
            {
              backgroundColor: colors.bgElevated,
              borderColor: colors.borderSubtle,
            },
          ]}
        >
          <ActivityIndicator size="small" color={colors.accent} />
          <Text style={[styles.handoffText, { color: colors.textSecondary }]}>
            Opening Terminal…
          </Text>
        </View>
      ) : null}
    </SafeAreaView>
  );
}

function AgentSelector({
  selectedAgent,
  counts,
  onSelect,
}: {
  selectedAgent: ManagedSkillAgent;
  counts: SkillsAgentCounts;
  onSelect(agent: ManagedSkillAgent): void;
}) {
  const colors = useAppColors();
  return (
    <View
      accessibilityRole="tablist"
      accessibilityLabel="Managed Agent"
      style={styles.agentRow}
    >
      {MANAGED_SKILL_AGENTS.map((agent) => {
        const selected = agent === selectedAgent;
        const label = skillAgentLabel(agent);
        return (
          <Pressable
            key={agent}
            accessibilityRole="tab"
            accessibilityLabel={`${label}, ${counts[agent]} installed`}
            accessibilityState={{ selected }}
            onPress={() => onSelect(agent)}
            style={[
              styles.agentChip,
              {
                backgroundColor: selected
                  ? colors.surfaceSubtle
                  : "transparent",
                borderColor: selected
                  ? colors.borderStrong
                  : colors.borderSubtle,
              },
            ]}
          >
            <AgentKindIcon
              kind={managedAgentKind(agent)}
              size={22}
              variant="compact"
            />
            <View style={styles.agentCopy}>
              <Text
                numberOfLines={1}
                maxFontSizeMultiplier={1.35}
                style={[
                  styles.agentName,
                  {
                    color: selected ? colors.textPrimary : colors.textSecondary,
                    fontFamily: selected
                      ? Typography.uiFontMedium
                      : Typography.uiFont,
                  },
                ]}
              >
                {label}
              </Text>
              <Text
                maxFontSizeMultiplier={1.35}
                style={[
                  styles.agentCount,
                  {
                    color: selected
                      ? colors.textSecondary
                      : colors.textTertiary,
                  },
                ]}
              >
                {counts[agent]}
              </Text>
            </View>
          </Pressable>
        );
      })}
    </View>
  );
}

function ModeSwitch({
  mode,
  onSelect,
}: {
  mode: SkillsMode;
  onSelect(mode: SkillsMode): void;
}) {
  const colors = useAppColors();
  return (
    <View
      accessibilityRole="tablist"
      style={[styles.modeSwitch, { backgroundColor: colors.surfaceSubtle }]}
    >
      {(["installed", "discover"] as SkillsMode[]).map((value) => {
        const selected = value === mode;
        return (
          <Pressable
            key={value}
            accessibilityRole="tab"
            accessibilityState={{ selected }}
            onPress={() => onSelect(value)}
            style={[
              styles.modeButton,
              selected ? { backgroundColor: colors.bgElevated } : null,
            ]}
          >
            <Text
              numberOfLines={1}
              maxFontSizeMultiplier={1.35}
              style={[
                styles.modeLabel,
                {
                  color: selected ? colors.textPrimary : colors.textTertiary,
                  fontFamily: selected
                    ? Typography.uiFontMedium
                    : Typography.uiFont,
                },
              ]}
            >
              {value === "installed" ? "Installed" : "Discover"}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

function InstalledSkillsList({
  selectedAgent,
  state,
  skills,
  warnings,
  currentServerAvailable,
  refreshing,
  preparingMutation,
  onOpenSettings,
  onRefresh,
  onRemove,
}: {
  selectedAgent: ManagedSkillAgent;
  state: SkillsRequestState<unknown>;
  skills: InstalledSkill[];
  warnings: string[];
  currentServerAvailable: boolean;
  refreshing: boolean;
  preparingMutation: string;
  onOpenSettings(): void;
  onRefresh(): void;
  onRemove(skill: InstalledSkill): void;
}) {
  const colors = useAppColors();
  const agentLabel = skillAgentLabel(selectedAgent);
  const hasInventory = state.status === "ready" || state.status === "empty";
  return (
    <FlatList
      style={styles.list}
      data={skills}
      keyExtractor={(skill) => skill.id}
      keyboardShouldPersistTaps="handled"
      contentContainerStyle={styles.listContent}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={onRefresh}
          colors={[colors.accent]}
          tintColor={colors.accent}
        />
      }
      ItemSeparatorComponent={() => (
        <View
          style={[styles.separator, { backgroundColor: colors.borderSubtle }]}
        />
      )}
      ListHeaderComponent={
        <View style={styles.listHeader}>
          <View style={styles.inventoryToolbar}>
            <View style={styles.toolbarSpacer} />
            <Pressable
              accessibilityRole="button"
              accessibilityLabel="Refresh installed Skills"
              accessibilityState={{ busy: refreshing }}
              hitSlop={6}
              onPress={onRefresh}
              style={styles.iconButton}
            >
              {refreshing ? (
                <ActivityIndicator size="small" color={colors.accent} />
              ) : (
                <Ionicons
                  name="refresh"
                  size={19}
                  color={colors.textSecondary}
                />
              )}
            </Pressable>
          </View>
          {state.status === "loading" && !hasInventory ? (
            <RequestState title="Loading installed Skills…" busy />
          ) : null}
          {state.status === "error" ? (
            <RequestState
              title="Installed Skills unavailable"
              detail={state.error}
              action={currentServerAvailable ? "Retry" : "Settings"}
              onAction={currentServerAvailable ? onRefresh : onOpenSettings}
            />
          ) : null}
          {hasInventory && skills.length === 0 ? (
            <RequestState
              title={`No Skills for ${agentLabel}`}
              detail={`Switch to Discover to install one for ${agentLabel}.`}
            />
          ) : null}
          {warnings.slice(0, 1).map((warning) => (
            <Text
              key={warning}
              numberOfLines={2}
              style={[styles.warning, { color: colors.warning }]}
            >
              {warning}
            </Text>
          ))}
        </View>
      }
      renderItem={({ item }) => (
        <InstalledSkillRow
          skill={item}
          selectedAgent={selectedAgent}
          busy={preparingMutation === `remove:${item.id}`}
          onRemove={onRemove}
        />
      )}
    />
  );
}

function InstalledSkillRow({
  skill,
  selectedAgent,
  busy,
  onRemove,
}: {
  skill: InstalledSkill;
  selectedAgent: ManagedSkillAgent;
  busy: boolean;
  onRemove(skill: InstalledSkill): void;
}) {
  const colors = useAppColors();
  const removalPlan = skillsRemovalPlanForAgent(skill, selectedAgent);
  const secondary = installedSkillSecondaryLine(
    skill,
    removalPlan?.affectedAgents,
  );
  return (
    <View style={styles.skillRow}>
      <View style={styles.skillCopy}>
        <Text
          numberOfLines={1}
          style={[styles.skillName, { color: colors.textPrimary }]}
        >
          {skill.name}
        </Text>
        {skill.description ? (
          <Text
            numberOfLines={2}
            style={[styles.description, { color: colors.textSecondary }]}
          >
            {skill.description}
          </Text>
        ) : null}
        <Text
          numberOfLines={1}
          style={[styles.metadata, { color: colors.textTertiary }]}
        >
          {secondary}
        </Text>
      </View>
      {removalPlan ? (
        <SmallAction
          label="Remove"
          accessibilityLabel={`Remove ${skill.name} from ${removalPlan.affectedAgents
            .map(skillAgentLabel)
            .join(", ")}`}
          destructive
          busy={busy}
          onPress={() => onRemove(skill)}
        />
      ) : null}
    </View>
  );
}

function DiscoverSkillsList({
  selectedAgent,
  query,
  submittedQuery,
  view,
  catalogState,
  leaderboard,
  searchState,
  searchResult,
  currentServerAvailable,
  preparingMutation,
  onOpenSettings,
  onChangeQuery,
  onSubmitSearch,
  onClearSearch,
  onSelectView,
  onRetryCatalog,
  onRetrySearch,
  onInstall,
}: {
  selectedAgent: ManagedSkillAgent;
  query: string;
  submittedQuery: string;
  view: SkillsLeaderboardView;
  catalogState: SkillsRequestState<SkillsLeaderboards>;
  leaderboard?: SkillsLeaderboard;
  searchState: SkillsRequestState<SkillsCatalogResult>;
  searchResult?: SkillsCatalogResult;
  currentServerAvailable: boolean;
  preparingMutation: string;
  onOpenSettings(): void;
  onChangeQuery(value: string): void;
  onSubmitSearch(): void;
  onClearSearch(): void;
  onSelectView(view: SkillsLeaderboardView): void;
  onRetryCatalog(): void;
  onRetrySearch(): void;
  onInstall(skill: CatalogSkill | RankedCatalogSkill): void;
}) {
  const colors = useAppColors();
  const shortQuery = query.trim().length < 2;
  const showingSearch = Boolean(submittedQuery);
  const data: Array<CatalogSkill | RankedCatalogSkill> = showingSearch
    ? (searchResult?.skills ?? [])
    : (leaderboard?.skills ?? []);
  const trailing = (
    <View style={styles.searchAccessory}>
      {searchState.status === "loading" ? (
        <ActivityIndicator size="small" color={colors.accent} />
      ) : query ? (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Clear search"
          hitSlop={8}
          onPress={onClearSearch}
          style={styles.searchClearButton}
        >
          <Ionicons name="close-circle" size={19} color={colors.textTertiary} />
        </Pressable>
      ) : null}
    </View>
  );
  return (
    <FlatList
      style={styles.list}
      data={data}
      keyExtractor={(skill) => skill.id}
      keyboardShouldPersistTaps="handled"
      contentContainerStyle={styles.listContent}
      ItemSeparatorComponent={() => (
        <View
          style={[styles.separator, { backgroundColor: colors.borderSubtle }]}
        />
      )}
      ListHeaderComponent={
        <View style={styles.discoverHeader}>
          <MobileSingleLineInput
            value={query}
            onChangeText={onChangeQuery}
            placeholder="Search skills.sh"
            placeholderTextColor={colors.textTertiary}
            accessibilityLabel="Search skills.sh"
            accessibilityHint="Type a query, then press Search"
            autoCapitalize="none"
            autoCorrect={false}
            returnKeyType="search"
            onSubmitEditing={onSubmitSearch}
            leading={
              <Ionicons name="search" size={18} color={colors.textTertiary} />
            }
            trailing={trailing}
            containerStyle={[
              styles.searchBox,
              {
                backgroundColor: colors.inputBackground,
                borderColor: colors.borderSubtle,
              },
            ]}
            inputStyle={{ color: colors.textPrimary }}
          />
          {query.length > 0 && shortQuery ? (
            <Text style={[styles.searchHint, { color: colors.textTertiary }]}>
              Enter at least 2 characters, then Search
            </Text>
          ) : null}
          {!showingSearch ? (
            <LeaderboardTabs view={view} onSelect={onSelectView} />
          ) : (
            <Text
              numberOfLines={1}
              style={[styles.searchContext, { color: colors.textSecondary }]}
            >
              Results for “{submittedQuery}”
            </Text>
          )}
          {showingSearch && searchState.status === "loading" ? (
            <RequestState title={`Searching for “${submittedQuery}”…`} busy />
          ) : null}
          {showingSearch && searchState.status === "error" ? (
            <RequestState
              title="Search unavailable"
              detail={searchState.error}
              action={currentServerAvailable ? "Retry" : "Settings"}
              onAction={currentServerAvailable ? onRetrySearch : onOpenSettings}
            />
          ) : null}
          {showingSearch && searchState.status === "empty" ? (
            <RequestState
              title="No results"
              detail={`Nothing matched “${searchResult?.query || submittedQuery}”. Clear search to return to ${skillsLeaderboardLabel(view)}.`}
            />
          ) : null}
          {!showingSearch && catalogState.status === "loading" ? (
            <RequestState title="Loading skills.sh rankings…" busy />
          ) : null}
          {!showingSearch && catalogState.status === "error" ? (
            <RequestState
              title="Rankings unavailable"
              detail={catalogState.error}
              action={currentServerAvailable ? "Retry" : "Settings"}
              onAction={
                currentServerAvailable ? onRetryCatalog : onOpenSettings
              }
            />
          ) : null}
          {!showingSearch &&
          (catalogState.status === "ready" ||
            catalogState.status === "empty") &&
          leaderboard?.skills.length === 0 ? (
            <RequestState {...skillsEmptyLeaderboardCopy(view)} />
          ) : null}
        </View>
      }
      renderItem={({ item: skill }) => {
        const ranked = isRankedCatalogSkill(skill);
        return (
          <View style={styles.catalogRow}>
            {ranked ? (
              <Text
                accessibilityLabel={`Rank ${skill.rank}`}
                style={[styles.catalogRank, { color: colors.textTertiary }]}
              >
                {skill.rank}
              </Text>
            ) : null}
            <View style={styles.catalogCopy}>
              <Text
                numberOfLines={1}
                style={[styles.skillName, { color: colors.textPrimary }]}
              >
                {skill.name}
              </Text>
              <Text
                numberOfLines={ranked && view === "hot" ? 2 : 1}
                style={[styles.metadata, { color: colors.textTertiary }]}
              >
                {ranked
                  ? `${skill.source} · ${rankedMetric(skill, view)}`
                  : `${skill.source} · ${formatInstalls(skill.installs)} installs`}
              </Text>
            </View>
            {skill.installable ? (
              <SmallAction
                label="Install"
                accessibilityLabel={`Install ${skill.name} for ${skillAgentLabel(selectedAgent)}`}
                busy={preparingMutation === `install:${skill.id}`}
                onPress={() => onInstall(skill)}
              />
            ) : (
              <Text
                accessibilityLabel="Install unavailable for this source"
                style={[styles.viewOnly, { color: colors.textTertiary }]}
              >
                View only
              </Text>
            )}
          </View>
        );
      }}
    />
  );
}

function LeaderboardTabs({
  view,
  onSelect,
}: {
  view: SkillsLeaderboardView;
  onSelect(view: SkillsLeaderboardView): void;
}) {
  const colors = useAppColors();
  const tabs: Array<{ view: SkillsLeaderboardView; label: string }> = [
    { view: "all-time", label: "All Time" },
    { view: "trending", label: "Trending 24h" },
    { view: "hot", label: "Hot" },
  ];
  return (
    <View
      accessibilityRole="tablist"
      style={[styles.leaderboardTabs, { borderColor: colors.borderSubtle }]}
    >
      {tabs.map((tab) => {
        const selected = tab.view === view;
        return (
          <Pressable
            key={tab.view}
            accessibilityRole="tab"
            accessibilityState={{ selected }}
            onPress={() => onSelect(tab.view)}
            style={[
              styles.leaderboardTab,
              selected ? { backgroundColor: colors.surfaceSubtle } : null,
            ]}
          >
            <Text
              numberOfLines={1}
              maxFontSizeMultiplier={1.35}
              style={[
                styles.leaderboardTabText,
                {
                  color: selected ? colors.textPrimary : colors.textTertiary,
                  fontFamily: selected
                    ? Typography.uiFontMedium
                    : Typography.uiFont,
                },
              ]}
            >
              {tab.label}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

function RequestState({
  title,
  detail,
  busy,
  action,
  onAction,
}: {
  title: string;
  detail?: string;
  busy?: boolean;
  action?: string;
  onAction?(): void;
}) {
  const colors = useAppColors();
  return (
    <View style={styles.requestState}>
      {busy ? <ActivityIndicator size="small" color={colors.accent} /> : null}
      <View style={styles.requestCopy}>
        <Text style={[styles.requestTitle, { color: colors.textPrimary }]}>
          {title}
        </Text>
        {detail ? (
          <Text style={[styles.requestDetail, { color: colors.textTertiary }]}>
            {detail}
          </Text>
        ) : null}
      </View>
      {action && onAction ? (
        <SmallAction label={action} onPress={onAction} />
      ) : null}
    </View>
  );
}

function SmallAction({
  label,
  accessibilityLabel,
  busy,
  destructive,
  onPress,
}: {
  label: string;
  accessibilityLabel?: string;
  busy?: boolean;
  destructive?: boolean;
  onPress(): void;
}) {
  const colors = useAppColors();
  return (
    <AnimatedPressable
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel ?? label}
      accessibilityState={{ disabled: busy }}
      disabled={busy}
      onPress={onPress}
      style={[
        styles.smallAction,
        {
          backgroundColor: destructive
            ? colors.dangerSoft
            : colors.surfaceSubtle,
          borderColor: destructive ? colors.dangerText : colors.borderSubtle,
        },
      ]}
    >
      {busy ? (
        <ActivityIndicator size="small" color={colors.accent} />
      ) : (
        <Text
          style={[
            styles.smallActionText,
            { color: destructive ? colors.dangerText : colors.textSecondary },
          ]}
        >
          {label}
        </Text>
      )}
    </AnimatedPressable>
  );
}

function managedAgentKind(agent: ManagedSkillAgent): AgentKind {
  switch (agent) {
    case "codex":
      return "codex";
    case "claude-code":
      return "claude";
    case "cursor":
      return "cursor";
  }
}

function installedSkillSecondaryLine(
  skill: InstalledSkill,
  affectedAgents?: ManagedSkillAgent[],
): string {
  const scope = scopeLabel(skill.scope);
  const provenance = installedSkillProvenance(skill);
  if (affectedAgents && affectedAgents.length > 1) {
    return `${scope} · shared with ${affectedAgents
      .map(skillAgentLabel)
      .join(", ")}`;
  }
  return `${scope} · ${provenance}`;
}

function installedSkillProvenance(skill: InstalledSkill): string {
  switch (skill.manager) {
    case "skills-cli":
      return skill.source || "skills CLI";
    case "plugin":
      return skill.plugin ? `Plugin · ${skill.plugin}` : "Plugin";
    case "builtin":
      return "Built in";
    default:
      return skill.provenance;
  }
}

function formatInstalls(value: number): string {
  return Intl.NumberFormat(undefined, {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(value);
}

function isRankedCatalogSkill(
  skill: CatalogSkill | RankedCatalogSkill,
): skill is RankedCatalogSkill {
  return "rank" in skill;
}

function rankedMetric(
  skill: RankedCatalogSkill,
  view: SkillsLeaderboardView,
): string {
  switch (view) {
    case "all-time":
      return `${formatInstalls(skill.totalInstalls!)} total`;
    case "trending":
      return `${formatInstalls(skill.installs24h!)} · 24h`;
    case "hot": {
      const change = skill.change!;
      const changeLabel =
        change > 0 ? `+${formatInstalls(change)}` : formatInstalls(change);
      return `${formatInstalls(skill.currentInstalls!)} now · ${changeLabel}`;
    }
  }
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  chrome: {
    width: "100%",
    maxWidth: 720,
    alignSelf: "center",
    paddingHorizontal: 16,
    paddingTop: 8,
    gap: 10,
    paddingBottom: 4,
  },
  agentRow: {
    flexDirection: "row",
    gap: 8,
  },
  agentChip: {
    flex: 1,
    minWidth: 0,
    minHeight: 56,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: Radii.sm,
    paddingHorizontal: 10,
    paddingVertical: 8,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  agentCopy: {
    flex: 1,
    minWidth: 0,
    gap: 1,
  },
  agentName: {
    ...TypeScale.label,
  },
  agentCount: {
    ...TypeScale.micro,
    fontVariant: ["tabular-nums"],
  },
  modeSwitch: {
    flexDirection: "row",
    padding: 3,
    borderRadius: Radii.sm,
  },
  modeButton: {
    flex: 1,
    minWidth: 0,
    minHeight: 40,
    borderRadius: 10,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 8,
  },
  modeLabel: {
    ...TypeScale.compact,
  },
  list: { flex: 1 },
  listContent: {
    width: "100%",
    maxWidth: 720,
    alignSelf: "center",
    paddingHorizontal: 16,
    paddingTop: 4,
    paddingBottom: 40,
  },
  listHeader: { gap: 8, paddingBottom: 2 },
  inventoryToolbar: {
    minHeight: 40,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "flex-end",
  },
  toolbarSpacer: { flex: 1 },
  iconButton: {
    width: 44,
    height: 44,
    alignItems: "center",
    justifyContent: "center",
  },
  separator: { height: StyleSheet.hairlineWidth },
  skillRow: {
    minHeight: 72,
    paddingVertical: 12,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  skillCopy: { flex: 1, minWidth: 0, gap: 3 },
  skillName: {
    ...TypeScale.body,
    fontFamily: Typography.uiFontMedium,
  },
  description: { ...TypeScale.compact },
  metadata: { ...TypeScale.caption },
  warning: { ...TypeScale.caption, lineHeight: 17 },
  discoverHeader: { gap: 10, paddingTop: 4, paddingBottom: 6 },
  searchBox: {
    borderRadius: Radii.sm,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  searchAccessory: {
    width: "100%",
    height: "100%",
    alignItems: "center",
    justifyContent: "center",
  },
  searchClearButton: {
    width: 44,
    height: 44,
    alignItems: "center",
    justifyContent: "center",
  },
  searchHint: { ...TypeScale.caption, paddingHorizontal: 2 },
  searchContext: { ...TypeScale.caption, paddingHorizontal: 2 },
  leaderboardTabs: {
    minHeight: 40,
    flexDirection: "row",
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  leaderboardTab: {
    flex: 1,
    minWidth: 0,
    minHeight: 40,
    paddingHorizontal: 6,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: Radii.xs,
  },
  leaderboardTabText: { ...TypeScale.caption },
  catalogRow: {
    minHeight: 72,
    paddingVertical: 12,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  catalogRank: {
    ...TypeScale.caption,
    width: 24,
    textAlign: "right",
    fontVariant: ["tabular-nums"],
  },
  catalogCopy: { flex: 1, minWidth: 0, gap: 3 },
  viewOnly: { ...TypeScale.caption, width: 70, textAlign: "center" },
  requestState: {
    minHeight: 64,
    paddingVertical: 10,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  requestCopy: { flex: 1, minWidth: 0, gap: 2 },
  requestTitle: {
    ...TypeScale.compact,
    fontFamily: Typography.uiFontMedium,
  },
  requestDetail: { ...TypeScale.caption, lineHeight: 18 },
  smallAction: {
    minWidth: 70,
    minHeight: 44,
    paddingHorizontal: 12,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: Radii.xs,
    alignItems: "center",
    justifyContent: "center",
  },
  smallActionText: {
    ...TypeScale.label,
    fontFamily: Typography.uiFontMedium,
  },
  handoffStatus: {
    position: "absolute",
    left: 16,
    right: 16,
    bottom: 16,
    minHeight: 48,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: Radii.sm,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
  },
  handoffText: { ...TypeScale.compact },
});
