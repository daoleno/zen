import React from "react";
import {
  ActivityIndicator,
  Alert,
  FlatList,
  Pressable,
  RefreshControl,
  ScrollView,
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
  SkillMutationOperation,
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
import {
  type CacheFallbackPlugin,
  type PluginExpansionState,
  type PluginSectionView,
} from "../../services/pluginsScreenModel";
import {
  type AvailablePlugin,
  type InstalledPluginRow,
  type PluginInventory,
} from "../../services/pluginsManagement";
import type {
  SkillsSurfaceSection,
} from "../../services/skillsSurfaceModel";
import { AgentKindIcon } from "../terminal/AgentKindIcon";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";

export type SkillsMode = "installed" | "discover";
export type PluginsMode = "installed" | "explore";

export type StatusTone =
  | "installed"
  | "builtin"
  | "unmanaged"
  | "unavailable";

export interface SkillsPresentationProps {
  section: SkillsSurfaceSection;
  mode: SkillsMode;
  pluginsMode: PluginsMode;
  selectedAgent: ManagedSkillAgent;
  agentCounts: SkillsAgentCounts;
  inventoryState: SkillsRequestState<unknown>;
  installedSkills: InstalledSkill[];
  pluginsState: SkillsRequestState<PluginInventory>;
  pluginSection: PluginSectionView;
  pluginsFallback: boolean;
  fallbackPlugins: CacheFallbackPlugin[];
  pluginExpansion: PluginExpansionState;
  inventoryWarnings: string[];
  catalogState: SkillsRequestState<SkillsLeaderboards>;
  leaderboard?: SkillsLeaderboard;
  searchState: SkillsRequestState<SkillsCatalogResult>;
  searchResult?: SkillsCatalogResult;
  query: string;
  submittedQuery: string;
  leaderboardView: SkillsLeaderboardView;
  mutationOperations: readonly SkillMutationOperation[];
  hasProjectCwd: boolean;
  preparingMutation: string;
  creatingTerminal: boolean;
  currentServerAvailable: boolean;
  onSelectSection(section: SkillsSurfaceSection): void;
  onSelectMode(mode: SkillsMode): void;
  onSelectPluginsMode(mode: PluginsMode): void;
  onSelectAgent(agent: ManagedSkillAgent): void;
  onTogglePlugin(pluginId: string): void;
  onOpenSettings(): void;
  onRefreshInventory(): void;
  onRetryPlugins(): void;
  onRemove(skill: InstalledSkill): void;
  onUpdateSkills(scope: "project" | "global"): void;
  onInstallPlugin(entry: AvailablePlugin): void;
  onUpdatePlugin(row: InstalledPluginRow): void;
  onUninstallPlugin(row: InstalledPluginRow): void;
  onChangeQuery(value: string): void;
  onSubmitSearch(): void;
  onClearSearch(): void;
  onSelectLeaderboard(view: SkillsLeaderboardView): void;
  onRetryCatalog(): void;
  onRetrySearch(): void;
  onInstall(skill: CatalogSkill | RankedCatalogSkill): void;
}

export function SkillsPresentation({
  section,
  mode,
  pluginsMode,
  selectedAgent,
  agentCounts,
  inventoryState,
  installedSkills,
  pluginsState,
  pluginSection,
  pluginsFallback,
  fallbackPlugins,
  pluginExpansion,
  inventoryWarnings,
  catalogState,
  leaderboard,
  searchState,
  searchResult,
  query,
  submittedQuery,
  leaderboardView,
  mutationOperations,
  hasProjectCwd,
  preparingMutation,
  creatingTerminal,
  currentServerAvailable,
  onSelectSection,
  onSelectMode,
  onSelectPluginsMode,
  onSelectAgent,
  onTogglePlugin,
  onOpenSettings,
  onRefreshInventory,
  onRetryPlugins,
  onRemove,
  onUpdateSkills,
  onInstallPlugin,
  onUpdatePlugin,
  onUninstallPlugin,
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
        <SurfaceTabs section={section} onSelect={onSelectSection} />
        {section === "plugins" ? (
          <PluginsSection
            pluginsMode={pluginsMode}
            state={pluginsState}
            view={pluginSection}
            fallback={pluginsFallback}
            fallbackPlugins={fallbackPlugins}
            expansion={pluginExpansion}
            currentServerAvailable={currentServerAvailable}
            refreshing={
              pluginsState.status === "loading" && !pluginsFallback
            }
            preparingMutation={preparingMutation}
            onSelectMode={onSelectPluginsMode}
            onOpenSettings={onOpenSettings}
            onRetry={onRetryPlugins}
            onTogglePlugin={onTogglePlugin}
            onInstallPlugin={onInstallPlugin}
            onUpdatePlugin={onUpdatePlugin}
            onUninstallPlugin={onUninstallPlugin}
          />
        ) : (
          <>
            <AgentSelector
              selectedAgent={selectedAgent}
              counts={agentCounts}
              onSelect={onSelectAgent}
            />
            <ModeSwitch mode={mode} onSelect={onSelectMode} />
            {mode === "installed" ? (
              <InstalledSkillsList
                selectedAgent={selectedAgent}
                state={inventoryState}
                skills={installedSkills}
                warnings={inventoryWarnings}
                currentServerAvailable={currentServerAvailable}
                refreshing={inventoryState.status === "loading"}
                preparingMutation={preparingMutation}
                mutationOperations={mutationOperations}
                hasProjectCwd={hasProjectCwd}
                onOpenSettings={onOpenSettings}
                onRefresh={onRefreshInventory}
                onRemove={onRemove}
                onUpdateSkills={onUpdateSkills}
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
          </>
        )}
      </View>

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

function SurfaceTabs({
  section,
  onSelect,
}: {
  section: SkillsSurfaceSection;
  onSelect(section: SkillsSurfaceSection): void;
}) {
  const colors = useAppColors();
  const tabs: Array<{
    section: SkillsSurfaceSection;
    label: string;
    icon: "apps" | "sparkles";
  }> = [
    { section: "plugins", label: "Plugins", icon: "apps" },
    { section: "skills", label: "Skills", icon: "sparkles" },
  ];
  return (
    <View
      accessibilityRole="tablist"
      accessibilityLabel="Plugins and Skills"
      style={[styles.surfaceTabs, { backgroundColor: colors.surfaceSubtle }]}
    >
      {tabs.map((tab) => {
        const selected = tab.section === section;
        return (
          <Pressable
            key={tab.section}
            accessibilityRole="tab"
            accessibilityState={{ selected }}
            onPress={() => onSelect(tab.section)}
            style={[
              styles.surfaceTab,
              selected ? { backgroundColor: colors.bgElevated } : null,
            ]}
          >
            <Ionicons
              name={tab.icon}
              size={16}
              color={selected ? colors.textPrimary : colors.textTertiary}
            />
            <Text
              numberOfLines={1}
              maxFontSizeMultiplier={1.35}
              style={[
                styles.surfaceTabLabel,
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
    <ScrollView
      accessibilityRole="tablist"
      accessibilityLabel="Managed Agent"
      horizontal
      showsHorizontalScrollIndicator={false}
      contentContainerStyle={styles.agentRow}
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
    </ScrollView>
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

function PluginsModeSwitch({
  mode,
  onSelect,
}: {
  mode: PluginsMode;
  onSelect(mode: PluginsMode): void;
}) {
  const colors = useAppColors();
  return (
    <View
      accessibilityRole="tablist"
      style={[styles.modeSwitch, { backgroundColor: colors.surfaceSubtle }]}
    >
      {(["installed", "explore"] as PluginsMode[]).map((value) => {
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
              {value === "installed" ? "Installed" : "Explore"}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

function PluginsSection({
  pluginsMode,
  state,
  view,
  fallback,
  fallbackPlugins,
  expansion,
  currentServerAvailable,
  refreshing,
  preparingMutation,
  onSelectMode,
  onOpenSettings,
  onRetry,
  onTogglePlugin,
  onInstallPlugin,
  onUpdatePlugin,
  onUninstallPlugin,
}: {
  pluginsMode: PluginsMode;
  state: SkillsRequestState<PluginInventory>;
  view: PluginSectionView;
  fallback: boolean;
  fallbackPlugins: CacheFallbackPlugin[];
  expansion: PluginExpansionState;
  currentServerAvailable: boolean;
  refreshing: boolean;
  preparingMutation: string;
  onSelectMode(mode: PluginsMode): void;
  onOpenSettings(): void;
  onRetry(): void;
  onTogglePlugin(pluginId: string): void;
  onInstallPlugin(entry: AvailablePlugin): void;
  onUpdatePlugin(row: InstalledPluginRow): void;
  onUninstallPlugin(row: InstalledPluginRow): void;
}) {
  if (fallback) {
    return (
      <FallbackPluginsList
        plugins={fallbackPlugins}
        refreshing={refreshing}
        expansion={expansion}
        onTogglePlugin={onTogglePlugin}
      />
    );
  }
  return (
    <>
      <PluginsModeSwitch mode={pluginsMode} onSelect={onSelectMode} />
      {pluginsMode === "installed" ? (
        <InstalledPluginsList
          state={state}
          rows={view.installed}
          catalogReady={view.catalogReady}
          currentServerAvailable={currentServerAvailable}
          refreshing={refreshing}
          preparingMutation={preparingMutation}
          expansion={expansion}
          onOpenSettings={onOpenSettings}
          onRetry={onRetry}
          onTogglePlugin={onTogglePlugin}
          onUpdatePlugin={onUpdatePlugin}
          onUninstallPlugin={onUninstallPlugin}
        />
      ) : (
        <ExplorePluginsList
          state={state}
          entries={view.explore}
          currentServerAvailable={currentServerAvailable}
          refreshing={refreshing}
          preparingMutation={preparingMutation}
          onOpenSettings={onOpenSettings}
          onRetry={onRetry}
          onInstallPlugin={onInstallPlugin}
        />
      )}
    </>
  );
}

function InstalledPluginsList({
  state,
  rows,
  catalogReady,
  currentServerAvailable,
  refreshing,
  preparingMutation,
  expansion,
  onOpenSettings,
  onRetry,
  onTogglePlugin,
  onUpdatePlugin,
  onUninstallPlugin,
}: {
  state: SkillsRequestState<PluginInventory>;
  rows: InstalledPluginRow[];
  catalogReady: boolean;
  currentServerAvailable: boolean;
  refreshing: boolean;
  preparingMutation: string;
  expansion: PluginExpansionState;
  onOpenSettings(): void;
  onRetry(): void;
  onTogglePlugin(pluginId: string): void;
  onUpdatePlugin(row: InstalledPluginRow): void;
  onUninstallPlugin(row: InstalledPluginRow): void;
}) {
  const colors = useAppColors();
  const hasData = state.status === "ready" || state.status === "empty";
  return (
    <FlatList
      style={styles.list}
      data={rows}
      keyExtractor={(row) => row.id}
      contentContainerStyle={styles.listContent}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={onRetry}
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
          {state.status === "loading" && !hasData ? (
            <RequestState title="Loading plugins…" busy />
          ) : null}
          {state.status === "error" ? (
            <RequestState
              title="Plugins unavailable"
              detail={state.error}
              action={currentServerAvailable ? "Retry" : "Settings"}
              onAction={currentServerAvailable ? onRetry : onOpenSettings}
            />
          ) : null}
          {hasData && rows.length === 0 ? (
            <RequestState title="No plugins installed" />
          ) : null}
        </View>
      }
      renderItem={({ item }) => (
        <InstalledPluginRow
          row={item}
          expanded={expansion.expanded.includes(item.id)}
          preparingMutation={preparingMutation}
          onToggle={() => onTogglePlugin(item.id)}
          onUpdate={() => onUpdatePlugin(item)}
          onUninstall={() => onUninstallPlugin(item)}
        />
      )}
    />
  );
}

function InstalledPluginRow({
  row,
  expanded,
  preparingMutation,
  onToggle,
  onUpdate,
  onUninstall,
}: {
  row: InstalledPluginRow;
  expanded: boolean;
  preparingMutation: string;
  onToggle(): void;
  onUpdate(): void;
  onUninstall(): void;
}) {
  const colors = useAppColors();
  const manageable = row.mutable && row.host === "claude" && row.source === "catalog";
  const openActions = () => {
    Alert.alert(row.name, undefined, [
      { text: "Update", onPress: onUpdate },
      {
        text: "Uninstall",
        style: "destructive",
        onPress: onUninstall,
      },
      { text: "Cancel", style: "cancel" },
    ]);
  };
  return (
    <View style={styles.pluginCard}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${row.name}, version ${row.version}, ${row.skillCount} ${
          row.skillCount === 1 ? "Skill" : "Skills"
        }`}
        accessibilityState={{ expanded }}
        onPress={onToggle}
        style={({ pressed }) => [
          styles.pluginHeader,
          pressed ? { backgroundColor: colors.surfacePressed } : null,
        ]}
      >
        <View
          style={[styles.pluginIconTile, { backgroundColor: colors.surfaceSubtle }]}
        >
          <Ionicons name="apps" size={20} color={colors.textSecondary} />
        </View>
        <View style={styles.pluginCopy}>
          <Text
            numberOfLines={1}
            style={[styles.skillName, { color: colors.textPrimary }]}
          >
            {row.name}
          </Text>
          <View style={styles.pluginMetaRow}>
            <AgentKindIcon
              kind={pluginHostKind(row.host)}
              size={14}
              variant="compact"
            />
            <Text
              maxFontSizeMultiplier={1.35}
              numberOfLines={1}
              style={[styles.metadata, { color: colors.textTertiary }]}
            >
              @{row.marketplace} · v{row.version} · {row.skillCount}{" "}
              {row.skillCount === 1 ? "Skill" : "Skills"}
            </Text>
          </View>
        </View>
        <StatusBadge label={pluginStatusLabel(row)} tone={pluginStatusTone(row)} />
        {manageable ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={`${row.name} actions`}
            accessibilityState={{ busy: preparingMutation !== "" }}
            disabled={preparingMutation !== ""}
            hitSlop={8}
            onPress={openActions}
            style={({ pressed }) => [
              styles.iconAction,
              {
                backgroundColor: pressed
                  ? colors.surfacePressed
                  : colors.surfaceSubtle,
              },
            ]}
          >
            <Ionicons
              name="ellipsis-horizontal"
              size={18}
              color={colors.textSecondary}
            />
          </Pressable>
        ) : null}
        <Ionicons
          name="chevron-down"
          size={16}
          color={colors.textTertiary}
          style={expanded ? styles.chevronExpanded : styles.chevronCollapsed}
        />
      </Pressable>
      {expanded ? (
        <View style={styles.pluginSkills}>
          {row.skills.length === 0 ? (
            <Text
              style={[
                styles.hostedSkillName,
                { color: colors.textTertiary },
              ]}
            >
              No hosted Skills
            </Text>
          ) : null}
          {row.skills.map((skill) => (
            <View
              key={skill.canonicalPath}
              style={[
                styles.hostedSkillRow,
                { borderTopColor: colors.borderSubtle },
              ]}
            >
              <View style={styles.hostedSkillCopy}>
                <Text
                  numberOfLines={1}
                  style={[styles.hostedSkillName, { color: colors.textPrimary }]}
                >
                  {skill.name}
                </Text>
              </View>
              <StatusBadge label="Plugin" tone="builtin" />
            </View>
          ))}
        </View>
      ) : null}
    </View>
  );
}

function pluginStatusLabel(row: InstalledPluginRow): string {
  if (row.host === "codex") {
    return "Read-only";
  }
  return row.source === "catalog" ? "Installed" : "Read-only";
}

function pluginStatusTone(row: InstalledPluginRow): StatusTone {
  return row.source === "catalog" && row.host === "claude"
    ? "installed"
    : "unmanaged";
}

function ExplorePluginsList({
  state,
  entries,
  currentServerAvailable,
  refreshing,
  preparingMutation,
  onOpenSettings,
  onRetry,
  onInstallPlugin,
}: {
  state: SkillsRequestState<PluginInventory>;
  entries: AvailablePlugin[];
  currentServerAvailable: boolean;
  refreshing: boolean;
  preparingMutation: string;
  onOpenSettings(): void;
  onRetry(): void;
  onInstallPlugin(entry: AvailablePlugin): void;
}) {
  const colors = useAppColors();
  const hasData = state.status === "ready" || state.status === "empty";
  const catalogReady =
    (state.status === "ready" || state.status === "empty") &&
    state.data.catalog.status === "ready";
  return (
    <FlatList
      style={styles.list}
      data={entries}
      keyExtractor={(entry) => entry.pluginId}
      contentContainerStyle={styles.listContent}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={onRetry}
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
          {state.status === "loading" && !hasData ? (
            <RequestState title="Loading plugin catalog…" busy />
          ) : null}
          {state.status === "error" ? (
            <RequestState
              title="Plugin catalog unavailable"
              detail={state.error}
              action={currentServerAvailable ? "Retry" : "Settings"}
              onAction={currentServerAvailable ? onRetry : onOpenSettings}
            />
          ) : null}
          {hasData && !catalogReady ? (
            <RequestState
              title="Plugin catalog unavailable"
              action={currentServerAvailable ? "Retry" : "Settings"}
              onAction={currentServerAvailable ? onRetry : onOpenSettings}
            />
          ) : null}
          {hasData && catalogReady && entries.length === 0 ? (
            <RequestState title="No plugins available" />
          ) : null}
        </View>
      }
      renderItem={({ item }) => (
        <View style={styles.catalogRow}>
          <View style={styles.catalogCopy}>
            <Text
              numberOfLines={1}
              style={[styles.skillName, { color: colors.textPrimary }]}
            >
              {item.name}
            </Text>
            <Text
              numberOfLines={1}
              style={[styles.metadata, { color: colors.textTertiary }]}
            >
              @{item.marketplaceName}
              {item.sourceRef ? ` · ${item.sourceRef}` : ""}
            </Text>
          </View>
          {item.installable ? (
            <SmallAction
              label="Install"
              accessibilityLabel={`Install ${item.name}`}
              busy={preparingMutation === `plugin:install:${item.pluginId}`}
              onPress={() => onInstallPlugin(item)}
            />
          ) : (
            <StatusBadge label="Installed" tone="installed" />
          )}
        </View>
      )}
    />
  );
}

function FallbackPluginsList({
  plugins,
  refreshing,
  expansion,
  onTogglePlugin,
}: {
  plugins: CacheFallbackPlugin[];
  refreshing: boolean;
  expansion: PluginExpansionState;
  onTogglePlugin(pluginId: string): void;
}) {
  const colors = useAppColors();
  return (
    <FlatList
      style={styles.list}
      data={plugins}
      keyExtractor={(plugin) => plugin.id}
      contentContainerStyle={styles.listContent}
      refreshControl={
        <RefreshControl
          refreshing={refreshing}
          onRefresh={() => undefined}
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
          {plugins.length === 0 ? (
            <RequestState title="No plugins installed" />
          ) : null}
        </View>
      }
      renderItem={({ item }) => (
        <FallbackPluginRow
          plugin={item}
          expanded={expansion.expanded.includes(item.id)}
          onToggle={() => onTogglePlugin(item.id)}
        />
      )}
    />
  );
}

function FallbackPluginRow({
  plugin,
  expanded,
  onToggle,
}: {
  plugin: CacheFallbackPlugin;
  expanded: boolean;
  onToggle(): void;
}) {
  const colors = useAppColors();
  return (
    <View style={styles.pluginCard}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${plugin.name}, ${plugin.skillCount} ${
          plugin.skillCount === 1 ? "Skill" : "Skills"
        }`}
        accessibilityState={{ expanded }}
        onPress={onToggle}
        style={({ pressed }) => [
          styles.pluginHeader,
          pressed ? { backgroundColor: colors.surfacePressed } : null,
        ]}
      >
        <View
          style={[styles.pluginIconTile, { backgroundColor: colors.surfaceSubtle }]}
        >
          <Ionicons name="apps" size={20} color={colors.textSecondary} />
        </View>
        <View style={styles.pluginCopy}>
          <Text
            numberOfLines={1}
            style={[styles.skillName, { color: colors.textPrimary }]}
          >
            {plugin.name}
          </Text>
          <Text
            maxFontSizeMultiplier={1.35}
            numberOfLines={1}
            style={[styles.metadata, { color: colors.textTertiary }]}
          >
            {plugin.skillCount} {plugin.skillCount === 1 ? "Skill" : "Skills"}
          </Text>
        </View>
        <StatusBadge label="Read-only" tone="unmanaged" />
        <Ionicons
          name="chevron-down"
          size={16}
          color={colors.textTertiary}
          style={expanded ? styles.chevronExpanded : styles.chevronCollapsed}
        />
      </Pressable>
      {expanded ? (
        <View style={styles.pluginSkills}>
          {plugin.skills.map((skill) => (
            <View
              key={skill.canonicalPath}
              style={[
                styles.hostedSkillRow,
                { borderTopColor: colors.borderSubtle },
              ]}
            >
              <View style={styles.hostedSkillCopy}>
                <Text
                  numberOfLines={1}
                  style={[styles.hostedSkillName, { color: colors.textPrimary }]}
                >
                  {skill.name}
                </Text>
              </View>
              <StatusBadge label={scopeLabel(skill.scope)} tone="builtin" />
            </View>
          ))}
        </View>
      ) : null}
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
  mutationOperations,
  hasProjectCwd,
  onOpenSettings,
  onRefresh,
  onRemove,
  onUpdateSkills,
}: {
  selectedAgent: ManagedSkillAgent;
  state: SkillsRequestState<unknown>;
  skills: InstalledSkill[];
  warnings: string[];
  currentServerAvailable: boolean;
  refreshing: boolean;
  preparingMutation: string;
  mutationOperations: readonly SkillMutationOperation[];
  hasProjectCwd: boolean;
  onOpenSettings(): void;
  onRefresh(): void;
  onRemove(skill: InstalledSkill): void;
  onUpdateSkills(scope: "project" | "global"): void;
}) {
  const colors = useAppColors();
  const agentLabel = skillAgentLabel(selectedAgent);
  const hasInventory = state.status === "ready" || state.status === "empty";
  const updateSupported = mutationOperations.includes("update");
  return (
    <FlatList
      style={styles.list}
      data={skills}
      keyExtractor={(skill) => skill.id}
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
            {updateSupported ? (
              <>
                <ToolbarAction
                  label="Update global"
                  accessibilityLabel="Update global Skills to their latest versions"
                  busy={preparingMutation === "update:global"}
                  onPress={() => onUpdateSkills("global")}
                />
                {hasProjectCwd ? (
                  <ToolbarAction
                    label="Update project"
                    accessibilityLabel="Update project Skills to their latest versions"
                    busy={preparingMutation === "update:project"}
                    onPress={() => onUpdateSkills("project")}
                  />
                ) : null}
              </>
            ) : null}
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
            <RequestState title={`No Skills for ${agentLabel}`} />
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
  const badge = installedSkillBadge(skill);
  return (
    <View style={styles.skillRow}>
      <View style={styles.skillCopy}>
        <Text
          numberOfLines={1}
          style={[styles.skillName, { color: colors.textPrimary }]}
        >
          {skill.name}
        </Text>
        <View style={styles.badgeRow}>
          <StatusBadge label={badge.label} tone={badge.tone} />
          {installedSkillCaption(skill, removalPlan?.affectedAgents) ? (
            <Text
              numberOfLines={1}
              maxFontSizeMultiplier={1.35}
              style={[styles.metadata, { color: colors.textTertiary }]}
            >
              {installedSkillCaption(skill, removalPlan?.affectedAgents)}
            </Text>
          ) : null}
        </View>
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
          {!showingSearch ? (
            <LeaderboardTabs view={view} onSelect={onSelectView} />
          ) : null}
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
            <RequestState title="No results" />
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
            <RequestState title={skillsEmptyLeaderboardCopy(view).title} />
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
              <StatusBadge label="Unavailable" tone="unavailable" />
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

function StatusBadge({
  label,
  tone,
}: {
  label: string;
  tone: StatusTone;
}) {
  const colors = useAppColors();
  const palette = statusPalette(tone, colors);
  return (
    <View
      accessibilityLabel={label}
      style={[
        styles.statusBadge,
        { backgroundColor: palette.background, borderColor: palette.border },
      ]}
    >
      <Text
        maxFontSizeMultiplier={1.35}
        style={[styles.statusBadgeText, { color: palette.text }]}
      >
        {label}
      </Text>
    </View>
  );
}

function statusPalette(
  tone: StatusTone,
  colors: ReturnType<typeof useAppColors>,
): { background: string; border: string; text: string } {
  switch (tone) {
    case "installed":
      return {
        background: colors.successSoft,
        border: "transparent",
        text: colors.success,
      };
    case "builtin":
    case "unavailable":
      return {
        background: colors.surfaceSubtle,
        border: colors.borderSubtle,
        text: colors.textTertiary,
      };
    case "unmanaged":
      return {
        background: colors.warningSoft,
        border: "transparent",
        text: colors.warning,
      };
  }
}

function installedSkillBadge(
  skill: InstalledSkill,
): { label: string; tone: StatusTone } {
  switch (skill.manager) {
    case "skills-cli":
      return skill.capability.canRemove
        ? { label: "Installed", tone: "installed" }
        : { label: "Unmanaged", tone: "unmanaged" };
    case "builtin":
      return { label: "Builtin", tone: "builtin" };
    case "plugin":
      return { label: "Plugin", tone: "builtin" };
    default:
      return { label: "Unmanaged", tone: "unmanaged" };
  }
}

/**
 * One compact judgment-critical caption per row: remove impact when a removal
 * affects other Agents, otherwise the installed source provenance.
 */
function installedSkillCaption(
  skill: InstalledSkill,
  affectedAgents?: ManagedSkillAgent[],
): string | undefined {
  if (affectedAgents && affectedAgents.length > 1) {
    return `shared with ${affectedAgents.map(skillAgentLabel).join(", ")}`;
  }
  return installedSkillProvenance(skill);
}

function installedSkillProvenance(skill: InstalledSkill): string | undefined {
  switch (skill.manager) {
    case "skills-cli":
      return skill.source || undefined;
    case "plugin":
      return skill.plugin || undefined;
    default:
      return undefined;
  }
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


function ToolbarAction({
  label,
  accessibilityLabel,
  busy,
  onPress,
}: {
  label: string;
  accessibilityLabel: string;
  busy?: boolean;
  onPress(): void;
}) {
  const colors = useAppColors();
  return (
    <AnimatedPressable
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      accessibilityState={{ disabled: busy }}
      disabled={busy}
      onPress={onPress}
      style={[
        styles.toolbarAction,
        { backgroundColor: colors.surfaceSubtle },
      ]}
    >
      {busy ? (
        <ActivityIndicator size="small" color={colors.accent} />
      ) : (
        <Text
          maxFontSizeMultiplier={1.35}
          style={[styles.toolbarActionText, { color: colors.textSecondary }]}
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
    case "opencode":
      return "opencode";
    case "pi":
      return "pi";
  }
}

function pluginHostKind(host: "claude" | "codex"): AgentKind {
  return host === "claude" ? "claude" : "codex";
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
    flex: 1,
  },
  surfaceTabs: {
    flexDirection: "row",
    padding: 3,
    borderRadius: Radii.sm,
  },
  surfaceTab: {
    flex: 1,
    minWidth: 0,
    minHeight: 44,
    borderRadius: 10,
    alignItems: "center",
    justifyContent: "center",
    flexDirection: "row",
    gap: 6,
    paddingHorizontal: 8,
  },
  surfaceTabLabel: {
    ...TypeScale.label,
  },
  agentRow: {
    flexDirection: "row",
    gap: 8,
    paddingHorizontal: 2,
    paddingVertical: 2,
  },
  agentChip: {
    minWidth: 108,
    minHeight: 56,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: Radii.sm,
    paddingHorizontal: 12,
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
    gap: 8,
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
  skillCopy: { flex: 1, minWidth: 0, gap: 4 },
  badgeRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    minHeight: 22,
  },
  skillName: {
    ...TypeScale.body,
    fontFamily: Typography.uiFontMedium,
  },
  metadata: { ...TypeScale.caption },
  warning: { ...TypeScale.caption, lineHeight: 17 },
  pluginCard: {
    borderRadius: Radii.sm,
    overflow: "hidden",
  },
  pluginHeader: {
    minHeight: 68,
    paddingVertical: 10,
    paddingHorizontal: 4,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
  },
  pluginIconTile: {
    width: 40,
    height: 40,
    borderRadius: Radii.sm,
    alignItems: "center",
    justifyContent: "center",
  },
  pluginCopy: {
    flex: 1,
    minWidth: 0,
    gap: 4,
  },
  pluginMetaRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  pluginSkills: {
    paddingBottom: 4,
  },
  hostedSkillRow: {
    minHeight: 44,
    paddingVertical: 8,
    paddingLeft: 52,
    paddingRight: 4,
    borderTopWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
  },
  hostedSkillCopy: {
    flex: 1,
    minWidth: 0,
  },
  hostedSkillName: {
    ...TypeScale.compact,
  },
  chevronCollapsed: {
    transform: [{ rotate: "-90deg" }],
  },
  chevronExpanded: {
    transform: [{ rotate: "0deg" }],
  },
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
  statusBadge: {
    minHeight: 22,
    paddingHorizontal: 8,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: Radii.pill,
    alignItems: "center",
    justifyContent: "center",
  },
  statusBadgeText: {
    ...TypeScale.micro,
  },
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
  iconAction: {
    width: 40,
    height: 40,
    borderRadius: Radii.xs,
    alignItems: "center",
    justifyContent: "center",
  },
  toolbarAction: {
    minHeight: 36,
    paddingHorizontal: 12,
    borderRadius: Radii.xs,
    alignItems: "center",
    justifyContent: "center",
  },
  toolbarActionText: {
    ...TypeScale.caption,
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
