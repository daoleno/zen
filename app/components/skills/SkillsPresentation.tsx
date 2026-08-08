import React, { useEffect, useMemo, useState } from "react";
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
import type { PluginSectionView } from "../../services/pluginsScreenModel";
import {
  PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER,
  PLUGINS_SKILLS_TOUCH_TARGET,
  compactSkillTargets,
  filterAvailablePlugins,
  filterInstalledPlugins,
  filterInstalledSkills,
  installedPluginMetadata,
  installedPluginOwnership,
  installedSkillMetadata,
  installedSkillOwnership,
  type PluginsSkillsMode,
} from "../../services/pluginsSkillsSurfaceModel";
import type {
  AvailablePlugin,
  InstalledPluginRow,
  PluginInventory,
} from "../../services/pluginsManagement";
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
import {
  skillAgentLabel,
  skillsRequestData,
} from "../../services/skillsManagement";
import {
  skillsEmptyLeaderboardCopy,
  skillsLeaderboardLabel,
  type SkillsAgentCounts,
  type SkillsLeaderboardView,
} from "../../services/skillsScreenModel";
import type { SkillsSurfaceSection } from "../../services/skillsSurfaceModel";
import { AgentKindIcon } from "../terminal/AgentKindIcon";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";

export type SkillsMode = PluginsSkillsMode;
export type PluginsMode = PluginsSkillsMode;

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

type SurfaceSheet =
  | { kind: "mode" }
  | { kind: "target" }
  | { kind: "ranking" }
  | { kind: "skills-update" }
  | { kind: "skill-details"; skill: InstalledSkill }
  | { kind: "plugin-details"; plugin: InstalledPluginRow }
  | null;

export function SkillsPresentation(props: SkillsPresentationProps) {
  const {
    section,
    mode,
    pluginsMode,
    selectedAgent,
    agentCounts,
    inventoryState,
    installedSkills,
    pluginsState,
    pluginSection,
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
  } = props;
  const colors = useAppColors();
  const [localQuery, setLocalQuery] = useState("");
  const [sheet, setSheet] = useState<SurfaceSheet>(null);
  const activeMode = section === "plugins" ? pluginsMode : mode;
  const showingSkillsSearch =
    section === "skills" && activeMode === "discover";

  useEffect(() => {
    setLocalQuery("");
    setSheet(null);
  }, [activeMode, section, selectedAgent]);

  const refresh = () => {
    if (section === "plugins") {
      onRetryPlugins();
      return;
    }
    if (mode === "installed") {
      onRefreshInventory();
      return;
    }
    if (submittedQuery) {
      onRetrySearch();
      return;
    }
    onRetryCatalog();
  };
  const refreshing =
    section === "plugins"
      ? pluginsState.status === "loading"
      : mode === "installed"
        ? inventoryState.status === "loading"
        : submittedQuery
          ? searchState.status === "loading"
          : catalogState.status === "loading";
  const searchValue = showingSkillsSearch ? query : localQuery;
  const clearSearch = showingSkillsSearch
    ? onClearSearch
    : () => setLocalQuery("");
  const updateSupported = mutationOperations.includes("update");

  return (
    <SafeAreaView
      style={[styles.root, { backgroundColor: colors.bgPrimary }]}
      edges={["bottom"]}
    >
      <View style={styles.frame}>
        <SurfaceTabs section={section} onSelect={onSelectSection} />
        <View style={styles.tools}>
          <CompactToolbar
            section={section}
            mode={activeMode}
            selectedAgent={selectedAgent}
            leaderboardView={leaderboardView}
            showingLeaderboard={showingSkillsSearch && !submittedQuery}
            updateSupported={
              section === "skills" && mode === "installed" && updateSupported
            }
            refreshing={refreshing}
            onOpenMode={() => setSheet({ kind: "mode" })}
            onOpenTarget={() => setSheet({ kind: "target" })}
            onOpenRanking={() => setSheet({ kind: "ranking" })}
            onOpenUpdate={() => setSheet({ kind: "skills-update" })}
            onRefresh={refresh}
          />
          <SurfaceSearch
            value={searchValue}
            remote={showingSkillsSearch}
            loading={showingSkillsSearch && searchState.status === "loading"}
            placeholder={searchPlaceholder(section, activeMode)}
            onChange={showingSkillsSearch ? onChangeQuery : setLocalQuery}
            onSubmit={showingSkillsSearch ? onSubmitSearch : undefined}
            onClear={clearSearch}
          />
        </View>

        {section === "plugins" ? (
          <PluginsSection
            mode={pluginsMode}
            query={localQuery}
            state={pluginsState}
            view={pluginSection}
            currentServerAvailable={currentServerAvailable}
            refreshing={refreshing}
            preparingMutation={preparingMutation}
            onOpenSettings={onOpenSettings}
            onRetry={onRetryPlugins}
            onInstall={onInstallPlugin}
            onInspectPlugin={(plugin) =>
              setSheet({ kind: "plugin-details", plugin })
            }
          />
        ) : mode === "installed" ? (
          <InstalledSkillsList
            selectedAgent={selectedAgent}
            query={localQuery}
            state={inventoryState}
            skills={installedSkills}
            currentServerAvailable={currentServerAvailable}
            refreshing={refreshing}
            preparingMutation={preparingMutation}
            onOpenSettings={onOpenSettings}
            onRefresh={onRefreshInventory}
            onRemove={onRemove}
            onInspect={(skill) => setSheet({ kind: "skill-details", skill })}
          />
        ) : (
          <DiscoverSkillsList
            selectedAgent={selectedAgent}
            submittedQuery={submittedQuery}
            view={leaderboardView}
            catalogState={catalogState}
            leaderboard={leaderboard}
            searchState={searchState}
            searchResult={searchResult}
            currentServerAvailable={currentServerAvailable}
            preparingMutation={preparingMutation}
            refreshing={refreshing}
            onOpenSettings={onOpenSettings}
            onRefresh={refresh}
            onRetryCatalog={onRetryCatalog}
            onRetrySearch={onRetrySearch}
            onInstall={onInstall}
          />
        )}
      </View>

      <SurfaceSheet
        sheet={sheet}
        section={section}
        mode={activeMode}
        selectedAgent={selectedAgent}
        agentCounts={agentCounts}
        leaderboardView={leaderboardView}
        hasProjectCwd={hasProjectCwd}
        preparingMutation={preparingMutation}
        onClose={() => setSheet(null)}
        onSelectMode={(value) => {
          if (section === "plugins") onSelectPluginsMode(value);
          else onSelectMode(value);
          setSheet(null);
        }}
        onSelectAgent={(agent) => {
          onSelectAgent(agent);
          setSheet(null);
        }}
        onSelectLeaderboard={(value) => {
          onSelectLeaderboard(value);
          setSheet(null);
        }}
        onUpdateSkills={(scope) => {
          setSheet(null);
          onUpdateSkills(scope);
        }}
        onUpdatePlugin={(plugin) => {
          setSheet(null);
          onUpdatePlugin(plugin);
        }}
        onUninstallPlugin={(plugin) => {
          setSheet(null);
          onUninstallPlugin(plugin);
        }}
      />

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
          <Text
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.handoffText, { color: colors.textSecondary }]}
          >
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
    icon: "extension-puzzle-outline" | "sparkles-outline";
  }> = [
    {
      section: "plugins",
      label: "Plugins",
      icon: "extension-puzzle-outline",
    },
    { section: "skills", label: "Skills", icon: "sparkles-outline" },
  ];
  return (
    <View
      accessibilityRole="tablist"
      accessibilityLabel="Plugins and Skills"
      style={[styles.surfaceTabs, { borderBottomColor: colors.borderSubtle }]}
    >
      {tabs.map((tab) => {
        const selected = tab.section === section;
        return (
          <Pressable
            key={tab.section}
            accessibilityRole="tab"
            accessibilityLabel={tab.label}
            accessibilityState={{ selected }}
            onPress={() => onSelect(tab.section)}
            style={({ pressed }) => [
              styles.surfaceTab,
              pressed ? { backgroundColor: colors.surfacePressed } : null,
            ]}
          >
            <Ionicons
              accessible={false}
              name={tab.icon}
              size={18}
              color={selected ? colors.accent : colors.textTertiary}
            />
            <Text
              numberOfLines={1}
              maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
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
            {selected ? (
              <View
                style={[styles.selectedTabLine, { backgroundColor: colors.accent }]}
              />
            ) : null}
          </Pressable>
        );
      })}
    </View>
  );
}

function CompactToolbar({
  section,
  mode,
  selectedAgent,
  leaderboardView,
  showingLeaderboard,
  updateSupported,
  refreshing,
  onOpenMode,
  onOpenTarget,
  onOpenRanking,
  onOpenUpdate,
  onRefresh,
}: {
  section: SkillsSurfaceSection;
  mode: PluginsSkillsMode;
  selectedAgent: ManagedSkillAgent;
  leaderboardView: SkillsLeaderboardView;
  showingLeaderboard: boolean;
  updateSupported: boolean;
  refreshing: boolean;
  onOpenMode(): void;
  onOpenTarget(): void;
  onOpenRanking(): void;
  onOpenUpdate(): void;
  onRefresh(): void;
}) {
  const colors = useAppColors();
  return (
    <View style={styles.toolbar}>
      <ToolButton
        accessibilityLabel={`View ${mode === "installed" ? "Installed" : "Discover"}`}
        icon={mode === "installed" ? "download-outline" : "compass-outline"}
        label={mode === "installed" ? "Installed" : "Discover"}
        onPress={onOpenMode}
      />
      {section === "skills" ? (
        <ToolButton
          accessibilityLabel={`Target ${skillAgentLabel(selectedAgent)}`}
          label={skillAgentLabel(selectedAgent)}
          agent={selectedAgent}
          onPress={onOpenTarget}
        />
      ) : null}
      {showingLeaderboard ? (
        <ToolButton
          accessibilityLabel={`Ranking ${skillsLeaderboardLabel(leaderboardView)}`}
          icon="options-outline"
          label={skillsLeaderboardLabel(leaderboardView)}
          onPress={onOpenRanking}
        />
      ) : null}
      {updateSupported ? (
        <ToolButton
          accessibilityLabel="Update installed Skills"
          icon="arrow-up-circle-outline"
          label="Update"
          onPress={onOpenUpdate}
        />
      ) : null}
      <View style={styles.toolbarSpacer} />
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Refresh"
        accessibilityState={{ busy: refreshing }}
        disabled={refreshing}
        onPress={onRefresh}
        style={({ pressed }) => [
          styles.iconButton,
          pressed ? { backgroundColor: colors.surfacePressed } : null,
        ]}
      >
        {refreshing ? (
          <ActivityIndicator size="small" color={colors.accent} />
        ) : (
          <Ionicons
            accessible={false}
            name="refresh"
            size={20}
            color={colors.textSecondary}
          />
        )}
      </Pressable>
    </View>
  );
}

function ToolButton({
  accessibilityLabel,
  icon,
  label,
  agent,
  onPress,
}: {
  accessibilityLabel: string;
  icon?: React.ComponentProps<typeof Ionicons>["name"];
  label: string;
  agent?: ManagedSkillAgent;
  onPress(): void;
}) {
  const colors = useAppColors();
  return (
    <AnimatedPressable
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      onPress={onPress}
      style={[
        styles.toolButton,
        {
          backgroundColor: colors.surfaceSubtle,
          borderColor: colors.borderSubtle,
        },
      ]}
    >
      <View
        accessible={false}
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
      >
        {agent ? (
          <AgentKindIcon kind={managedAgentKind(agent)} size={18} variant="compact" />
        ) : icon ? (
          <Ionicons name={icon} size={18} color={colors.textSecondary} />
        ) : null}
      </View>
      <Text
        maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
        style={[styles.toolButtonText, { color: colors.textSecondary }]}
      >
        {label}
      </Text>
      <Ionicons
        accessible={false}
        name="chevron-down"
        size={14}
        color={colors.textTertiary}
      />
    </AnimatedPressable>
  );
}

function SurfaceSearch({
  value,
  remote,
  loading,
  placeholder,
  onChange,
  onSubmit,
  onClear,
}: {
  value: string;
  remote: boolean;
  loading: boolean;
  placeholder: string;
  onChange(value: string): void;
  onSubmit?(): void;
  onClear(): void;
}) {
  const colors = useAppColors();
  const trailing = loading ? (
    <ActivityIndicator size="small" color={colors.accent} />
  ) : value ? (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel="Clear search"
      onPress={onClear}
      style={styles.searchClearButton}
    >
      <Ionicons
        accessible={false}
        name="close-circle"
        size={20}
        color={colors.textTertiary}
      />
    </Pressable>
  ) : null;
  return (
    <MobileSingleLineInput
      value={value}
      onChangeText={onChange}
      placeholder={placeholder}
      placeholderTextColor={colors.textTertiary}
      accessibilityLabel={placeholder}
      accessibilityHint={remote ? "Type a query, then press Search" : undefined}
      autoCapitalize="none"
      autoCorrect={false}
      returnKeyType="search"
      onSubmitEditing={onSubmit}
      leading={
        <Ionicons
          accessible={false}
          name="search"
          size={18}
          color={colors.textTertiary}
        />
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
  );
}

function PluginsSection({
  mode,
  query,
  state,
  view,
  currentServerAvailable,
  refreshing,
  preparingMutation,
  onOpenSettings,
  onRetry,
  onInstall,
  onInspectPlugin,
}: {
  mode: PluginsMode;
  query: string;
  state: SkillsRequestState<PluginInventory>;
  view: PluginSectionView;
  currentServerAvailable: boolean;
  refreshing: boolean;
  preparingMutation: string;
  onOpenSettings(): void;
  onRetry(): void;
  onInstall(plugin: AvailablePlugin): void;
  onInspectPlugin(plugin: InstalledPluginRow): void;
}) {
  if (mode === "installed") {
    return (
      <InstalledPluginsList
        query={query}
        state={state}
        rows={view.installed}
        currentServerAvailable={currentServerAvailable}
        refreshing={refreshing}
        onOpenSettings={onOpenSettings}
        onRetry={onRetry}
        onInspect={onInspectPlugin}
      />
    );
  }
  return (
    <DiscoverPluginsList
      query={query}
      state={state}
      entries={view.explore}
      catalogReady={view.catalogReady}
      currentServerAvailable={currentServerAvailable}
      refreshing={refreshing}
      preparingMutation={preparingMutation}
      onOpenSettings={onOpenSettings}
      onRetry={onRetry}
      onInstall={onInstall}
    />
  );
}

function InstalledPluginsList({
  query,
  state,
  rows,
  currentServerAvailable,
  refreshing,
  onOpenSettings,
  onRetry,
  onInspect,
}: {
  query: string;
  state: SkillsRequestState<PluginInventory>;
  rows: InstalledPluginRow[];
  currentServerAvailable: boolean;
  refreshing: boolean;
  onOpenSettings(): void;
  onRetry(): void;
  onInspect(plugin: InstalledPluginRow): void;
}) {
  const colors = useAppColors();
  const visibleRows = useMemo(
    () => filterInstalledPlugins(rows, query),
    [query, rows],
  );
  const hasData = skillsRequestData(state) !== undefined;
  return (
    <FlatList
      style={styles.list}
      data={visibleRows}
      keyExtractor={(row) => row.id}
      keyboardShouldPersistTaps="handled"
      contentContainerStyle={styles.listContent}
      refreshControl={surfaceRefreshControl(refreshing, onRetry, colors.accent)}
      ItemSeparatorComponent={() => <Separator />}
      ListHeaderComponent={
        <ListStateHeader
          loading={state.status === "loading" && !hasData}
          loadingTitle="Loading plugins…"
          error={state.status === "error" ? state.error : undefined}
          errorTitle="Plugins unavailable"
          empty={hasData && rows.length === 0}
          emptyTitle="No plugins installed"
          noMatches={hasData && rows.length > 0 && visibleRows.length === 0}
          currentServerAvailable={currentServerAvailable}
          onOpenSettings={onOpenSettings}
          onRetry={onRetry}
        />
      }
      renderItem={({ item }) => (
        <InstalledPluginItem plugin={item} onInspect={() => onInspect(item)} />
      )}
    />
  );
}

function InstalledPluginItem({
  plugin,
  onInspect,
}: {
  plugin: InstalledPluginRow;
  onInspect(): void;
}) {
  const colors = useAppColors();
  const ownership = installedPluginOwnership(plugin);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${plugin.name}, ${installedPluginMetadata(plugin)}`}
      accessibilityHint={
        ownership.manageable ? "Open plugin actions" : "Show plugin ownership"
      }
      onPress={onInspect}
      style={({ pressed }) => [
        styles.itemRow,
        pressed ? { backgroundColor: colors.surfacePressed } : null,
      ]}
    >
      <View
        accessible={false}
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
        style={[styles.itemIcon, { backgroundColor: colors.surfaceSubtle }]}
      >
        <AgentKindIcon kind={pluginHostKind(plugin.host)} size={22} variant="compact" />
      </View>
      <View style={styles.itemCopy}>
        <Text
          numberOfLines={2}
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.itemName, { color: colors.textPrimary }]}
        >
          {plugin.name}
        </Text>
        <Text
          numberOfLines={2}
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.itemMetadata, { color: colors.textTertiary }]}
        >
          {installedPluginMetadata(plugin)}
        </Text>
      </View>
      <ItemActionIndicator
        icon={ownership.manageable ? "ellipsis-horizontal" : "information-circle-outline"}
      />
    </Pressable>
  );
}

function DiscoverPluginsList({
  query,
  state,
  entries,
  catalogReady,
  currentServerAvailable,
  refreshing,
  preparingMutation,
  onOpenSettings,
  onRetry,
  onInstall,
}: {
  query: string;
  state: SkillsRequestState<PluginInventory>;
  entries: AvailablePlugin[];
  catalogReady: boolean;
  currentServerAvailable: boolean;
  refreshing: boolean;
  preparingMutation: string;
  onOpenSettings(): void;
  onRetry(): void;
  onInstall(plugin: AvailablePlugin): void;
}) {
  const colors = useAppColors();
  const visibleEntries = useMemo(
    () => filterAvailablePlugins(entries, query),
    [entries, query],
  );
  const hasData = skillsRequestData(state) !== undefined;
  return (
    <FlatList
      style={styles.list}
      data={visibleEntries}
      keyExtractor={(entry) => entry.pluginId}
      keyboardShouldPersistTaps="handled"
      contentContainerStyle={styles.listContent}
      refreshControl={surfaceRefreshControl(refreshing, onRetry, colors.accent)}
      ItemSeparatorComponent={() => <Separator />}
      ListHeaderComponent={
        <ListStateHeader
          loading={state.status === "loading" && !hasData}
          loadingTitle="Loading plugin catalog…"
          error={state.status === "error" ? state.error : undefined}
          errorTitle="Plugin catalog unavailable"
          empty={hasData && catalogReady && entries.length === 0}
          emptyTitle="No plugins available"
          noMatches={
            hasData && catalogReady && entries.length > 0 && visibleEntries.length === 0
          }
          capabilityUnavailable={hasData && !catalogReady}
          currentServerAvailable={currentServerAvailable}
          onOpenSettings={onOpenSettings}
          onRetry={onRetry}
        />
      }
      renderItem={({ item }) => (
        <CatalogRow
          icon={<AgentKindIcon kind="claude" size={22} variant="compact" />}
          name={item.name}
          metadata={
            item.description ||
            [`@${item.marketplaceName}`, item.sourceRef].filter(Boolean).join(" · ")
          }
          trailing={
            item.installable ? (
              <SmallAction
                label="Install"
                accessibilityLabel={`Install ${item.name}`}
                busy={preparingMutation === `plugin:install:${item.pluginId}`}
                onPress={() => onInstall(item)}
              />
            ) : (
              <InstalledCheck />
            )
          }
        />
      )}
    />
  );
}

function InstalledSkillsList({
  selectedAgent,
  query,
  state,
  skills,
  currentServerAvailable,
  refreshing,
  preparingMutation,
  onOpenSettings,
  onRefresh,
  onRemove,
  onInspect,
}: {
  selectedAgent: ManagedSkillAgent;
  query: string;
  state: SkillsRequestState<unknown>;
  skills: InstalledSkill[];
  currentServerAvailable: boolean;
  refreshing: boolean;
  preparingMutation: string;
  onOpenSettings(): void;
  onRefresh(): void;
  onRemove(skill: InstalledSkill): void;
  onInspect(skill: InstalledSkill): void;
}) {
  const colors = useAppColors();
  const visibleSkills = useMemo(
    () => filterInstalledSkills(skills, query),
    [query, skills],
  );
  const hasInventory = skillsRequestData(state) !== undefined;
  return (
    <FlatList
      style={styles.list}
      data={visibleSkills}
      keyExtractor={(skill) => skill.id}
      keyboardShouldPersistTaps="handled"
      contentContainerStyle={styles.listContent}
      refreshControl={surfaceRefreshControl(refreshing, onRefresh, colors.accent)}
      ItemSeparatorComponent={() => <Separator />}
      ListHeaderComponent={
        <ListStateHeader
          loading={state.status === "loading" && !hasInventory}
          loadingTitle="Loading installed Skills…"
          error={state.status === "error" ? state.error : undefined}
          errorTitle="Installed Skills unavailable"
          empty={hasInventory && skills.length === 0}
          emptyTitle={`No Skills for ${skillAgentLabel(selectedAgent)}`}
          noMatches={hasInventory && skills.length > 0 && visibleSkills.length === 0}
          currentServerAvailable={currentServerAvailable}
          onOpenSettings={onOpenSettings}
          onRetry={onRefresh}
        />
      }
      renderItem={({ item }) => (
        <InstalledSkillItem
          skill={item}
          selectedAgent={selectedAgent}
          busy={preparingMutation === `remove:${item.id}`}
          onRemove={() => onRemove(item)}
          onInspect={() => onInspect(item)}
        />
      )}
    />
  );
}

function InstalledSkillItem({
  skill,
  selectedAgent,
  busy,
  onRemove,
  onInspect,
}: {
  skill: InstalledSkill;
  selectedAgent: ManagedSkillAgent;
  busy: boolean;
  onRemove(): void;
  onInspect(): void;
}) {
  const colors = useAppColors();
  const ownership = installedSkillOwnership(skill, selectedAgent);
  return (
    <View style={styles.itemRow}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${skill.name}, ${installedSkillMetadata(skill)}`}
        accessibilityHint="Show Skill details"
        onPress={onInspect}
        style={({ pressed }) => [
          styles.itemMain,
          pressed ? { backgroundColor: colors.surfacePressed } : null,
        ]}
      >
        <View
          accessible={false}
          accessibilityElementsHidden
          importantForAccessibility="no-hide-descendants"
          style={[styles.itemIcon, { backgroundColor: colors.surfaceSubtle }]}
        >
          <Ionicons name="sparkles-outline" size={21} color={colors.textSecondary} />
        </View>
        <View style={styles.itemCopy}>
          <Text
            numberOfLines={2}
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.itemName, { color: colors.textPrimary }]}
          >
            {skill.name}
          </Text>
          <Text
            numberOfLines={2}
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.itemMetadata, { color: colors.textTertiary }]}
          >
            {installedSkillMetadata(skill)}
          </Text>
        </View>
      </Pressable>
      {ownership.manageable ? (
        <SmallAction
          label="Remove"
          accessibilityLabel={`Remove ${skill.name}`}
          destructive
          busy={busy}
          onPress={onRemove}
        />
      ) : (
        <ItemIconAction
          label={`About ${skill.name}`}
          icon="information-circle-outline"
          onPress={onInspect}
        />
      )}
    </View>
  );
}

function DiscoverSkillsList({
  selectedAgent,
  submittedQuery,
  view,
  catalogState,
  leaderboard,
  searchState,
  searchResult,
  currentServerAvailable,
  preparingMutation,
  refreshing,
  onOpenSettings,
  onRefresh,
  onRetryCatalog,
  onRetrySearch,
  onInstall,
}: {
  selectedAgent: ManagedSkillAgent;
  submittedQuery: string;
  view: SkillsLeaderboardView;
  catalogState: SkillsRequestState<SkillsLeaderboards>;
  leaderboard?: SkillsLeaderboard;
  searchState: SkillsRequestState<SkillsCatalogResult>;
  searchResult?: SkillsCatalogResult;
  currentServerAvailable: boolean;
  preparingMutation: string;
  refreshing: boolean;
  onOpenSettings(): void;
  onRefresh(): void;
  onRetryCatalog(): void;
  onRetrySearch(): void;
  onInstall(skill: CatalogSkill | RankedCatalogSkill): void;
}) {
  const colors = useAppColors();
  const showingSearch = Boolean(submittedQuery);
  const hasCatalog = skillsRequestData(catalogState) !== undefined;
  const data: Array<CatalogSkill | RankedCatalogSkill> = showingSearch
    ? (searchResult?.skills ?? [])
    : (leaderboard?.skills ?? []);
  return (
    <FlatList
      style={styles.list}
      data={data}
      keyExtractor={(skill) => skill.id}
      keyboardShouldPersistTaps="handled"
      contentContainerStyle={styles.listContent}
      refreshControl={surfaceRefreshControl(refreshing, onRefresh, colors.accent)}
      ItemSeparatorComponent={() => <Separator />}
      ListHeaderComponent={
        showingSearch ? (
          <ListStateHeader
            loading={searchState.status === "loading"}
            loadingTitle={`Searching for “${submittedQuery}”…`}
            error={searchState.status === "error" ? searchState.error : undefined}
            errorTitle="Search unavailable"
            empty={searchState.status === "empty"}
            emptyTitle="No results"
            currentServerAvailable={currentServerAvailable}
            onOpenSettings={onOpenSettings}
            onRetry={onRetrySearch}
          />
        ) : (
          <ListStateHeader
            loading={catalogState.status === "loading" && !hasCatalog}
            loadingTitle="Loading skills.sh rankings…"
            error={catalogState.status === "error" ? catalogState.error : undefined}
            errorTitle="Rankings unavailable"
            empty={
              hasCatalog && leaderboard?.skills.length === 0
            }
            emptyTitle={skillsEmptyLeaderboardCopy(view).title}
            currentServerAvailable={currentServerAvailable}
            onOpenSettings={onOpenSettings}
            onRetry={onRetryCatalog}
          />
        )
      }
      renderItem={({ item: skill }) => {
        const ranked = isRankedCatalogSkill(skill);
        return (
          <CatalogRow
            prefix={ranked ? String(skill.rank) : undefined}
            icon={
              <Ionicons name="sparkles-outline" size={21} color={colors.textSecondary} />
            }
            name={skill.name}
            metadata={
              ranked
                ? `${skill.source} · ${rankedMetric(skill, view)}`
                : `${skill.source} · ${formatInstalls(skill.installs)} installs`
            }
            trailing={
              skill.installable ? (
                <SmallAction
                  label="Install"
                  accessibilityLabel={`Install ${skill.name} for ${skillAgentLabel(selectedAgent)}`}
                  busy={preparingMutation === `install:${skill.id}`}
                  onPress={() => onInstall(skill)}
                />
              ) : (
                <ItemIconAction
                  label={`Why ${skill.name} is unavailable`}
                  icon="information-circle-outline"
                  onPress={() =>
                    Alert.alert(
                      skill.name,
                      "This catalog entry does not expose an installable npx skills identity.",
                    )
                  }
                />
              )
            }
          />
        );
      }}
    />
  );
}

function CatalogRow({
  prefix,
  icon,
  name,
  metadata,
  trailing,
}: {
  prefix?: string;
  icon: React.ReactNode;
  name: string;
  metadata: string;
  trailing: React.ReactNode;
}) {
  const colors = useAppColors();
  return (
    <View style={styles.itemRow}>
      {prefix ? (
        <Text
          accessibilityLabel={`Rank ${prefix}`}
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.catalogRank, { color: colors.textTertiary }]}
        >
          {prefix}
        </Text>
      ) : null}
      <View
        accessible={false}
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
        style={[styles.itemIcon, { backgroundColor: colors.surfaceSubtle }]}
      >
        {icon}
      </View>
      <View style={styles.itemCopy}>
        <Text
          numberOfLines={2}
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.itemName, { color: colors.textPrimary }]}
        >
          {name}
        </Text>
        <Text
          numberOfLines={2}
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.itemMetadata, { color: colors.textTertiary }]}
        >
          {metadata}
        </Text>
      </View>
      {trailing}
    </View>
  );
}

function SurfaceSheet({
  sheet,
  section,
  mode,
  selectedAgent,
  agentCounts,
  leaderboardView,
  hasProjectCwd,
  preparingMutation,
  onClose,
  onSelectMode,
  onSelectAgent,
  onSelectLeaderboard,
  onUpdateSkills,
  onUpdatePlugin,
  onUninstallPlugin,
}: {
  sheet: SurfaceSheet;
  section: SkillsSurfaceSection;
  mode: PluginsSkillsMode;
  selectedAgent: ManagedSkillAgent;
  agentCounts: SkillsAgentCounts;
  leaderboardView: SkillsLeaderboardView;
  hasProjectCwd: boolean;
  preparingMutation: string;
  onClose(): void;
  onSelectMode(mode: PluginsSkillsMode): void;
  onSelectAgent(agent: ManagedSkillAgent): void;
  onSelectLeaderboard(view: SkillsLeaderboardView): void;
  onUpdateSkills(scope: "project" | "global"): void;
  onUpdatePlugin(plugin: InstalledPluginRow): void;
  onUninstallPlugin(plugin: InstalledPluginRow): void;
}) {
  const colors = useAppColors();
  return (
    <BottomSheetFrame
      visible={sheet !== null}
      maxHeight="82%"
      dragToDismiss
      onClose={onClose}
    >
      <ScrollView
        contentContainerStyle={styles.sheetContent}
        showsVerticalScrollIndicator={false}
      >
        {sheet?.kind === "mode" ? (
          <>
            <SheetTitle>{section === "plugins" ? "Plugins" : "Skills"}</SheetTitle>
            <SheetOption
              icon="download-outline"
              label="Installed"
              detail={`Show installed ${section === "plugins" ? "plugins" : "Skills"}`}
              selected={mode === "installed"}
              onPress={() => onSelectMode("installed")}
            />
            <SheetOption
              icon="compass-outline"
              label="Discover"
              detail={`Browse ${section === "plugins" ? "the plugin catalog" : "skills.sh"}`}
              selected={mode === "discover"}
              onPress={() => onSelectMode("discover")}
            />
          </>
        ) : null}

        {sheet?.kind === "target" ? (
          <>
            <SheetTitle>Target</SheetTitle>
            {compactSkillTargets(agentCounts).map((target) => (
              <SheetOption
                key={target.agent}
                agent={target.agent}
                label={target.label}
                detail={`${target.count} installed`}
                selected={target.agent === selectedAgent}
                onPress={() => onSelectAgent(target.agent)}
              />
            ))}
          </>
        ) : null}

        {sheet?.kind === "ranking" ? (
          <>
            <SheetTitle>Ranking</SheetTitle>
            {(["all-time", "trending", "hot"] as SkillsLeaderboardView[]).map(
              (view) => (
                <SheetOption
                  key={view}
                  icon={view === "hot" ? "flame-outline" : "stats-chart-outline"}
                  label={skillsLeaderboardLabel(view)}
                  selected={view === leaderboardView}
                  onPress={() => onSelectLeaderboard(view)}
                />
              ),
            )}
          </>
        ) : null}

        {sheet?.kind === "skills-update" ? (
          <>
            <SheetTitle>Update Skills</SheetTitle>
            <SheetOption
              icon="globe-outline"
              label="Global Skills"
              detail="Update every global Skill with npx skills"
              busy={preparingMutation === "update:global"}
              onPress={() => onUpdateSkills("global")}
            />
            <SheetOption
              icon="folder-outline"
              label="Project Skills"
              detail={
                hasProjectCwd
                  ? "Update every Skill in the current project"
                  : "Open a project Session to enable this action"
              }
              disabled={!hasProjectCwd}
              busy={preparingMutation === "update:project"}
              onPress={() => onUpdateSkills("project")}
            />
          </>
        ) : null}

        {sheet?.kind === "skill-details" ? (
          <SkillDetailSheet
            skill={sheet.skill}
            selectedAgent={selectedAgent}
          />
        ) : null}

        {sheet?.kind === "plugin-details" ? (
          <PluginDetailSheet
            plugin={sheet.plugin}
            preparingMutation={preparingMutation}
            onUpdate={() => onUpdatePlugin(sheet.plugin)}
            onUninstall={() => onUninstallPlugin(sheet.plugin)}
          />
        ) : null}

      </ScrollView>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Close"
        onPress={onClose}
        style={({ pressed }) => [
          styles.sheetClose,
          { borderTopColor: colors.borderSubtle },
          pressed ? { backgroundColor: colors.surfacePressed } : null,
        ]}
      >
        <Text
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.sheetCloseText, { color: colors.textSecondary }]}
        >
          Close
        </Text>
      </Pressable>
    </BottomSheetFrame>
  );
}

function SkillDetailSheet({
  skill,
  selectedAgent,
}: {
  skill: InstalledSkill;
  selectedAgent: ManagedSkillAgent;
}) {
  const ownership = installedSkillOwnership(skill, selectedAgent);
  return (
    <>
      <SheetTitle>{skill.name}</SheetTitle>
      <SheetMetadata>{installedSkillMetadata(skill)}</SheetMetadata>
      {skill.description ? <SheetBody>{skill.description}</SheetBody> : null}
      <SheetSection title={ownership.summary}>{ownership.detail}</SheetSection>
    </>
  );
}

function PluginDetailSheet({
  plugin,
  preparingMutation,
  onUpdate,
  onUninstall,
}: {
  plugin: InstalledPluginRow;
  preparingMutation: string;
  onUpdate(): void;
  onUninstall(): void;
}) {
  const ownership = installedPluginOwnership(plugin);
  return (
    <>
      <SheetTitle>{plugin.name}</SheetTitle>
      <SheetMetadata>{installedPluginMetadata(plugin)}</SheetMetadata>
      <SheetSection title={ownership.summary}>{ownership.detail}</SheetSection>
      {plugin.skills.length > 0 ? (
        <SheetSkillList skills={plugin.skills.map((skill) => skill.name)} />
      ) : null}
      {ownership.manageable ? (
        <View style={styles.sheetActions}>
          <SheetAction
            icon="arrow-up-circle-outline"
            label="Update"
            busy={preparingMutation === `plugin:update:${plugin.id}`}
            onPress={onUpdate}
          />
          <SheetAction
            icon="trash-outline"
            label="Uninstall"
            destructive
            busy={preparingMutation === `plugin:uninstall:${plugin.id}`}
            onPress={onUninstall}
          />
        </View>
      ) : null}
    </>
  );
}

function SheetTitle({ children }: { children: React.ReactNode }) {
  const colors = useAppColors();
  return (
    <Text
      maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
      style={[styles.sheetTitle, { color: colors.textPrimary }]}
    >
      {children}
    </Text>
  );
}

function SheetMetadata({ children }: { children: React.ReactNode }) {
  const colors = useAppColors();
  return (
    <Text
      maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
      style={[styles.sheetMetadata, { color: colors.textTertiary }]}
    >
      {children}
    </Text>
  );
}

function SheetBody({ children }: { children: React.ReactNode }) {
  const colors = useAppColors();
  return (
    <Text
      maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
      style={[styles.sheetBody, { color: colors.textSecondary }]}
    >
      {children}
    </Text>
  );
}

function SheetSection({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  const colors = useAppColors();
  return (
    <View style={styles.sheetSection}>
      <Text
        maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
        style={[styles.sheetSectionTitle, { color: colors.textPrimary }]}
      >
        {title}
      </Text>
      <Text
        maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
        style={[styles.sheetBody, { color: colors.textSecondary }]}
      >
        {children}
      </Text>
    </View>
  );
}

function SheetSkillList({ skills }: { skills: string[] }) {
  const colors = useAppColors();
  return (
    <View style={styles.sheetSkillList}>
      <Text
        maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
        style={[styles.sheetSectionTitle, { color: colors.textPrimary }]}
      >
        Included Skills
      </Text>
      {skills.map((skill) => (
        <View
          key={skill}
          style={[styles.sheetSkillRow, { borderTopColor: colors.borderSubtle }]}
        >
          <Ionicons
            accessible={false}
            name="sparkles-outline"
            size={17}
            color={colors.textTertiary}
          />
          <Text
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.sheetSkillName, { color: colors.textSecondary }]}
          >
            {skill}
          </Text>
        </View>
      ))}
    </View>
  );
}

function SheetOption({
  icon,
  agent,
  label,
  detail,
  selected,
  disabled,
  busy,
  onPress,
}: {
  icon?: React.ComponentProps<typeof Ionicons>["name"];
  agent?: ManagedSkillAgent;
  label: string;
  detail?: string;
  selected?: boolean;
  disabled?: boolean;
  busy?: boolean;
  onPress(): void;
}) {
  const colors = useAppColors();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={[label, detail].filter(Boolean).join(", ")}
      accessibilityState={{ selected, disabled: disabled || busy }}
      disabled={disabled || busy}
      onPress={onPress}
      style={({ pressed }) => [
        styles.sheetOption,
        selected ? { backgroundColor: colors.surfaceSubtle } : null,
        pressed ? { backgroundColor: colors.surfacePressed } : null,
        disabled ? styles.disabled : null,
      ]}
    >
      <View
        accessible={false}
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
        style={styles.sheetOptionIcon}
      >
        {busy ? (
          <ActivityIndicator size="small" color={colors.accent} />
        ) : agent ? (
          <AgentKindIcon kind={managedAgentKind(agent)} size={22} variant="compact" />
        ) : icon ? (
          <Ionicons name={icon} size={21} color={colors.textSecondary} />
        ) : null}
      </View>
      <View style={styles.sheetOptionCopy}>
        <Text
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.sheetOptionLabel, { color: colors.textPrimary }]}
        >
          {label}
        </Text>
        {detail ? (
          <Text
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.sheetOptionDetail, { color: colors.textTertiary }]}
          >
            {detail}
          </Text>
        ) : null}
      </View>
      <View
        accessible={false}
        accessibilityElementsHidden
        importantForAccessibility="no-hide-descendants"
      >
        {selected ? (
          <Ionicons name="checkmark" size={20} color={colors.accent} />
        ) : (
          <Ionicons name="chevron-forward" size={17} color={colors.textTertiary} />
        )}
      </View>
    </Pressable>
  );
}

function SheetAction({
  icon,
  label,
  destructive,
  busy,
  onPress,
}: {
  icon: React.ComponentProps<typeof Ionicons>["name"];
  label: string;
  destructive?: boolean;
  busy?: boolean;
  onPress(): void;
}) {
  const colors = useAppColors();
  return (
    <AnimatedPressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled: busy }}
      disabled={busy}
      onPress={onPress}
      style={[
        styles.sheetAction,
        {
          backgroundColor: destructive ? colors.dangerSoft : colors.surfaceSubtle,
          borderColor: destructive ? colors.dangerText : colors.borderSubtle,
        },
      ]}
    >
      {busy ? (
        <ActivityIndicator size="small" color={colors.accent} />
      ) : (
        <Ionicons
          accessible={false}
          name={icon}
          size={19}
          color={destructive ? colors.dangerText : colors.textSecondary}
        />
      )}
      <Text
        maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
        style={[
          styles.sheetActionText,
          { color: destructive ? colors.dangerText : colors.textSecondary },
        ]}
      >
        {label}
      </Text>
    </AnimatedPressable>
  );
}

function ListStateHeader({
  loading,
  loadingTitle = "Loading…",
  error,
  errorTitle = "Unavailable",
  empty,
  emptyTitle = "Nothing here yet",
  noMatches,
  capabilityUnavailable,
  currentServerAvailable = true,
  onOpenSettings,
  onRetry,
}: {
  loading?: boolean;
  loadingTitle?: string;
  error?: string;
  errorTitle?: string;
  empty?: boolean;
  emptyTitle?: string;
  noMatches?: boolean;
  capabilityUnavailable?: boolean;
  currentServerAvailable?: boolean;
  onOpenSettings?(): void;
  onRetry?(): void;
}) {
  if (loading) return <RequestState title={loadingTitle} busy />;
  if (error) {
    return (
      <RequestState
        title={errorTitle}
        detail={error}
        action={currentServerAvailable ? "Retry" : "Settings"}
        onAction={currentServerAvailable ? onRetry : onOpenSettings}
      />
    );
  }
  if (capabilityUnavailable) {
    return (
      <RequestState
        title="Plugin catalog unavailable"
        action={currentServerAvailable ? "Retry" : "Settings"}
        onAction={currentServerAvailable ? onRetry : onOpenSettings}
      />
    );
  }
  if (noMatches) return <RequestState title="No matches" />;
  if (empty) return <RequestState title={emptyTitle} />;
  return null;
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
        <Text
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.requestTitle, { color: colors.textPrimary }]}
        >
          {title}
        </Text>
        {detail ? (
          <Text
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.requestDetail, { color: colors.textTertiary }]}
          >
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
          backgroundColor: destructive ? colors.dangerSoft : colors.surfaceSubtle,
          borderColor: destructive ? colors.dangerText : colors.borderSubtle,
        },
      ]}
    >
      {busy ? (
        <ActivityIndicator size="small" color={colors.accent} />
      ) : (
        <Text
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
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

function ItemIconAction({
  label,
  icon,
  onPress,
}: {
  label: string;
  icon: React.ComponentProps<typeof Ionicons>["name"];
  onPress(): void;
}) {
  const colors = useAppColors();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      hitSlop={4}
      onPress={onPress}
      style={({ pressed }) => [
        styles.itemIconAction,
        pressed ? { backgroundColor: colors.surfacePressed } : null,
      ]}
    >
      <Ionicons
        accessible={false}
        name={icon}
        size={20}
        color={colors.textTertiary}
      />
    </Pressable>
  );
}

function ItemActionIndicator({
  icon,
}: {
  icon: React.ComponentProps<typeof Ionicons>["name"];
}) {
  const colors = useAppColors();
  return (
    <View
      accessible={false}
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      style={styles.itemIconAction}
    >
      <Ionicons name={icon} size={20} color={colors.textTertiary} />
    </View>
  );
}

function InstalledCheck() {
  const colors = useAppColors();
  return (
    <View
      accessible={false}
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      style={styles.itemIconAction}
    >
      <Ionicons
        accessible={false}
        name="checkmark"
        size={20}
        color={colors.success}
      />
    </View>
  );
}

function Separator() {
  const colors = useAppColors();
  return <View style={[styles.separator, { backgroundColor: colors.borderSubtle }]} />;
}

function surfaceRefreshControl(
  refreshing: boolean,
  onRefresh: () => void,
  accent: string,
) {
  return (
    <RefreshControl
      refreshing={refreshing}
      onRefresh={onRefresh}
      colors={[accent]}
      tintColor={accent}
    />
  );
}

function searchPlaceholder(
  section: SkillsSurfaceSection,
  mode: PluginsSkillsMode,
): string {
  if (section === "plugins") {
    return mode === "installed" ? "Search installed plugins" : "Search plugin catalog";
  }
  return mode === "installed" ? "Search installed Skills" : "Search skills.sh";
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
      const changeLabel = change > 0 ? `+${formatInstalls(change)}` : formatInstalls(change);
      return `${formatInstalls(skill.currentInstalls!)} now · ${changeLabel}`;
    }
  }
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  frame: {
    width: "100%",
    maxWidth: 720,
    alignSelf: "center",
    flex: 1,
  },
  surfaceTabs: {
    minHeight: 48,
    paddingHorizontal: 16,
    flexDirection: "row",
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  surfaceTab: {
    flex: 1,
    minWidth: 0,
    minHeight: PLUGINS_SKILLS_TOUCH_TARGET,
    alignItems: "center",
    justifyContent: "center",
    flexDirection: "row",
    gap: 7,
    paddingHorizontal: 8,
    position: "relative",
  },
  surfaceTabLabel: { ...TypeScale.label },
  selectedTabLine: {
    position: "absolute",
    left: 28,
    right: 28,
    bottom: -StyleSheet.hairlineWidth,
    height: 2,
    borderRadius: 1,
  },
  tools: {
    paddingHorizontal: 16,
    paddingTop: 10,
    paddingBottom: 8,
    gap: 8,
  },
  toolbar: {
    minHeight: PLUGINS_SKILLS_TOUCH_TARGET,
    flexDirection: "row",
    flexWrap: "wrap",
    alignItems: "center",
    gap: 8,
  },
  toolbarSpacer: { flexGrow: 1, minWidth: 0 },
  toolButton: {
    minHeight: PLUGINS_SKILLS_TOUCH_TARGET,
    maxWidth: "100%",
    paddingHorizontal: 11,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: Radii.xs,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 7,
  },
  toolButtonText: {
    ...TypeScale.label,
    flexShrink: 1,
  },
  iconButton: {
    width: PLUGINS_SKILLS_TOUCH_TARGET,
    height: PLUGINS_SKILLS_TOUCH_TARGET,
    borderRadius: Radii.xs,
    alignItems: "center",
    justifyContent: "center",
  },
  searchBox: {
    borderRadius: Radii.sm,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  searchClearButton: {
    width: PLUGINS_SKILLS_TOUCH_TARGET,
    height: PLUGINS_SKILLS_TOUCH_TARGET,
    alignItems: "center",
    justifyContent: "center",
  },
  list: { flex: 1 },
  listContent: {
    width: "100%",
    paddingHorizontal: 16,
    paddingBottom: 40,
  },
  separator: { height: StyleSheet.hairlineWidth },
  itemRow: {
    minHeight: 76,
    paddingVertical: 12,
    flexDirection: "row",
    alignItems: "center",
    gap: 11,
  },
  itemMain: {
    flex: 1,
    minWidth: 0,
    minHeight: 52,
    flexDirection: "row",
    alignItems: "center",
    gap: 11,
    borderRadius: Radii.xs,
  },
  itemIcon: {
    width: 40,
    height: 40,
    flexShrink: 0,
    borderRadius: Radii.sm,
    alignItems: "center",
    justifyContent: "center",
  },
  itemCopy: { flex: 1, minWidth: 0, gap: 2 },
  itemName: {
    ...TypeScale.body,
    fontFamily: Typography.uiFontMedium,
  },
  itemMetadata: { ...TypeScale.caption },
  itemIconAction: {
    width: PLUGINS_SKILLS_TOUCH_TARGET,
    height: PLUGINS_SKILLS_TOUCH_TARGET,
    flexShrink: 0,
    borderRadius: Radii.xs,
    alignItems: "center",
    justifyContent: "center",
  },
  catalogRank: {
    ...TypeScale.caption,
    width: 20,
    textAlign: "right",
    fontVariant: ["tabular-nums"],
  },
  requestState: {
    minHeight: 72,
    paddingVertical: 12,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  requestCopy: { flex: 1, minWidth: 0, gap: 2 },
  requestTitle: {
    ...TypeScale.compact,
    fontFamily: Typography.uiFontMedium,
  },
  requestDetail: { ...TypeScale.caption },
  smallAction: {
    minWidth: 70,
    minHeight: PLUGINS_SKILLS_TOUCH_TARGET,
    flexShrink: 0,
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
  sheetContent: { gap: 4, paddingBottom: 8 },
  sheetTitle: {
    ...TypeScale.heading,
    paddingHorizontal: 4,
    paddingBottom: 6,
  },
  sheetMetadata: {
    ...TypeScale.caption,
    paddingHorizontal: 4,
    paddingBottom: 8,
  },
  sheetBody: { ...TypeScale.compact },
  sheetSection: {
    paddingHorizontal: 4,
    paddingVertical: 10,
    gap: 4,
  },
  sheetSectionTitle: {
    ...TypeScale.label,
    fontFamily: Typography.uiFontMedium,
  },
  sheetOption: {
    minHeight: 58,
    paddingHorizontal: 10,
    paddingVertical: 7,
    borderRadius: Radii.sm,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
  },
  sheetOptionIcon: {
    width: 30,
    minHeight: PLUGINS_SKILLS_TOUCH_TARGET,
    alignItems: "center",
    justifyContent: "center",
  },
  sheetOptionCopy: { flex: 1, minWidth: 0, gap: 1 },
  sheetOptionLabel: {
    ...TypeScale.compact,
    fontFamily: Typography.uiFontMedium,
  },
  sheetOptionDetail: { ...TypeScale.caption },
  disabled: { opacity: 0.48 },
  sheetSkillList: { paddingHorizontal: 4, paddingTop: 8 },
  sheetSkillRow: {
    minHeight: PLUGINS_SKILLS_TOUCH_TARGET,
    paddingVertical: 8,
    flexDirection: "row",
    alignItems: "center",
    gap: 9,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  sheetSkillName: { ...TypeScale.compact, flex: 1 },
  sheetActions: {
    paddingTop: 8,
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 8,
  },
  sheetAction: {
    minHeight: PLUGINS_SKILLS_TOUCH_TARGET,
    flexGrow: 1,
    paddingHorizontal: 14,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: Radii.xs,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
  },
  sheetActionText: {
    ...TypeScale.label,
    fontFamily: Typography.uiFontMedium,
  },
  sheetClose: {
    minHeight: PLUGINS_SKILLS_TOUCH_TARGET,
    marginTop: 8,
    borderTopWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  sheetCloseText: { ...TypeScale.label },
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
