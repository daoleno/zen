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
import type { PluginsUnifiedView } from "../../services/pluginsScreenModel";
import {
  PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER,
  PLUGINS_SKILLS_TOUCH_TARGET,
  availablePluginBadges,
  availablePluginOwnership,
  catalogSkillBadges,
  compactSkillTargets,
  filterAvailablePlugins,
  filterInstalledPlugins,
  filterInstalledSkills,
  installedPluginBadges,
  installedPluginMetadata,
  installedPluginOwnership,
  installedSkillBadges,
  installedSkillMetadata,
  installedSkillOwnership,
  filterCatalogSkills,
  type LifecycleBadge,
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
  skillsLeaderboardLabel,
  type SkillsAgentCounts,
  type SkillsLeaderboardView,
  type SkillsUnifiedRow,
  skillsUnifiedRows,
} from "../../services/skillsScreenModel";
import type { SkillsSurfaceSection } from "../../services/skillsSurfaceModel";
import { AgentKindIcon } from "../terminal/AgentKindIcon";
import { AnimatedPressable } from "../ui/AnimatedPressable";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import { MobileSingleLineInput } from "../ui/MobileSingleLineInput";

export interface SurfaceMutationNotice {
  kind: "success" | "error";
  message: string;
}

export interface SkillsPresentationProps {
  section: SkillsSurfaceSection;
  selectedAgent: ManagedSkillAgent;
  agentCounts: SkillsAgentCounts;
  inventoryState: SkillsRequestState<unknown>;
  installedSkills: InstalledSkill[];
  catalogSkills: Array<CatalogSkill | RankedCatalogSkill>;
  /** catalogId -> other Agent labels that already have this Skill installed. */
  catalogInstalledElsewhere: Record<string, readonly string[]>;
  browsing: boolean;
  catalogState: SkillsRequestState<SkillsLeaderboards>;
  leaderboard?: SkillsLeaderboard;
  searchState: SkillsRequestState<SkillsCatalogResult>;
  searchResult?: SkillsCatalogResult;
  query: string;
  submittedQuery: string;
  leaderboardView: SkillsLeaderboardView;
  pluginsState: SkillsRequestState<PluginInventory>;
  pluginsView: PluginsUnifiedView;
  mutationOperations: readonly SkillMutationOperation[];
  hasProjectCwd: boolean;
  preparingMutation: string;
  mutationNotice: SurfaceMutationNotice | null;
  currentServerAvailable: boolean;
  onSelectSection(section: SkillsSurfaceSection): void;
  onSelectAgent(agent: ManagedSkillAgent): void;
  onOpenSettings(): void;
  onRefreshSkills(): void;
  onRetryPlugins(): void;
  onRemove(skill: InstalledSkill): void;
  onInstall(skill: CatalogSkill | RankedCatalogSkill): void;
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
  onDismissNotice(): void;
}

type SurfaceSheet =
  | { kind: "target" }
  | { kind: "ranking" }
  | { kind: "skills-update" }
  | { kind: "skill-details"; skill: InstalledSkill }
  | { kind: "plugin-details"; plugin: InstalledPluginRow }
  | { kind: "plugin-available"; plugin: AvailablePlugin }
  | null;

export function SkillsPresentation(props: SkillsPresentationProps) {
  const {
    section,
    selectedAgent,
    agentCounts,
    inventoryState,
    installedSkills,
    catalogSkills,
    catalogInstalledElsewhere,
    browsing,
    catalogState,
    leaderboard,
    searchState,
    searchResult,
    query,
    submittedQuery,
    leaderboardView,
    pluginsState,
    pluginsView,
    mutationOperations,
    hasProjectCwd,
    preparingMutation,
    mutationNotice,
    currentServerAvailable,
    onSelectSection,
    onSelectAgent,
    onOpenSettings,
    onRefreshSkills,
    onRetryPlugins,
    onRemove,
    onInstall,
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
    onDismissNotice,
  } = props;
  const colors = useAppColors();
  const [localQuery, setLocalQuery] = useState("");
  const [sheet, setSheet] = useState<SurfaceSheet>(null);

  useEffect(() => {
    setLocalQuery("");
    setSheet(null);
  }, [section, selectedAgent]);

  const showingSkillsSearch = section === "skills" && browsing;
  const refresh = () => {
    if (section === "plugins") {
      onRetryPlugins();
      return;
    }
    onRefreshSkills();
  };
  const refreshing =
    section === "plugins"
      ? pluginsState.status === "loading"
      : inventoryState.status === "loading" ||
        (browsing
          ? searchState.status === "loading"
          : catalogState.status === "loading");
  const searchValue = showingSkillsSearch ? query : localQuery;
  const clearSearch = showingSkillsSearch ? onClearSearch : () => setLocalQuery("");
  const updateSupported =
    section === "skills" && mutationOperations.includes("update");

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
            selectedAgent={selectedAgent}
            leaderboardView={leaderboardView}
            showingLeaderboard={showingSkillsSearch === false}
            updateSupported={updateSupported}
            refreshing={refreshing}
            onOpenTarget={() => setSheet({ kind: "target" })}
            onOpenRanking={() => setSheet({ kind: "ranking" })}
            onOpenUpdate={() => setSheet({ kind: "skills-update" })}
            onRefresh={refresh}
          />
          <SurfaceSearch
            value={searchValue}
            remote={showingSkillsSearch}
            loading={showingSkillsSearch && searchState.status === "loading"}
            placeholder={searchPlaceholder(section)}
            onChange={showingSkillsSearch ? onChangeQuery : setLocalQuery}
            onSubmit={showingSkillsSearch ? onSubmitSearch : undefined}
            onClear={clearSearch}
          />
        </View>

        {mutationNotice ? (
          <MutationNoticeBanner notice={mutationNotice} onDismiss={onDismissNotice} />
        ) : null}

        {section === "plugins" ? (
          <PluginsList
            query={localQuery}
            state={pluginsState}
            view={pluginsView}
            currentServerAvailable={currentServerAvailable}
            refreshing={refreshing}
            preparingMutation={preparingMutation}
            onOpenSettings={onOpenSettings}
            onRetry={onRetryPlugins}
            onInstall={onInstallPlugin}
            onInspectInstalled={(plugin) =>
              setSheet({ kind: "plugin-details", plugin })
            }
            onInspectAvailable={(plugin) =>
              setSheet({ kind: "plugin-available", plugin })
            }
          />
        ) : (
          <SkillsList
            selectedAgent={selectedAgent}
            query={localQuery}
            state={inventoryState}
            skills={installedSkills}
            catalogSkills={catalogSkills}
            catalogInstalledElsewhere={catalogInstalledElsewhere}
            leaderboardView={leaderboardView}
            browsing={browsing}
            submittedQuery={submittedQuery}
            leaderboard={leaderboard}
            catalogState={catalogState}
            searchState={searchState}
            searchResult={searchResult}
            currentServerAvailable={currentServerAvailable}
            refreshing={refreshing}
            preparingMutation={preparingMutation}
            onOpenSettings={onOpenSettings}
            onRefresh={refresh}
            onRemove={onRemove}
            onInstall={onInstall}
            onRetryCatalog={onRetryCatalog}
            onRetrySearch={onRetrySearch}
            onInspect={(skill) => setSheet({ kind: "skill-details", skill })}
          />
        )}
      </View>

      <SurfaceSheet
        sheet={sheet}
        section={section}
        selectedAgent={selectedAgent}
        agentCounts={agentCounts}
        leaderboardView={leaderboardView}
        hasProjectCwd={hasProjectCwd}
        preparingMutation={preparingMutation}
        onClose={() => setSheet(null)}
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
    icon: "extension-puzzle-outline" | "library-outline";
  }> = [
    {
      section: "plugins",
      label: "Plugins",
      icon: "extension-puzzle-outline",
    },
    { section: "skills", label: "Skills", icon: "library-outline" },
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
  selectedAgent,
  leaderboardView,
  showingLeaderboard,
  updateSupported,
  refreshing,
  onOpenTarget,
  onOpenRanking,
  onOpenUpdate,
  onRefresh,
}: {
  section: SkillsSurfaceSection;
  selectedAgent: ManagedSkillAgent;
  leaderboardView: SkillsLeaderboardView;
  showingLeaderboard: boolean;
  updateSupported: boolean;
  refreshing: boolean;
  onOpenTarget(): void;
  onOpenRanking(): void;
  onOpenUpdate(): void;
  onRefresh(): void;
}) {
  const colors = useAppColors();
  return (
    <View style={styles.toolbar}>
      {section === "skills" ? (
        <ToolButton
          accessibilityLabel={`Target ${skillAgentLabel(selectedAgent)}`}
          label={skillAgentLabel(selectedAgent)}
          agent={selectedAgent}
          onPress={onOpenTarget}
        />
      ) : null}
      {section === "skills" && showingLeaderboard ? (
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

function MutationNoticeBanner({
  notice,
  onDismiss,
}: {
  notice: SurfaceMutationNotice;
  onDismiss(): void;
}) {
  const colors = useAppColors();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${notice.kind === "success" ? "Success" : "Failed"}: ${notice.message}`}
      onPress={onDismiss}
      style={[
        styles.noticeBanner,
        {
          backgroundColor:
            notice.kind === "success" ? colors.successSoft : colors.dangerSoft,
          borderColor:
            notice.kind === "success" ? colors.success : colors.dangerText,
        },
      ]}
    >
      <Ionicons
        accessible={false}
        name={notice.kind === "success" ? "checkmark-circle" : "alert-circle"}
        size={19}
        color={notice.kind === "success" ? colors.success : colors.dangerText}
      />
      <Text
        numberOfLines={3}
        maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
        style={[
          styles.noticeText,
          {
            color:
              notice.kind === "success"
                ? colors.textPrimary
                : colors.dangerText,
          },
        ]}
      >
        {notice.message}
      </Text>
      <Ionicons
        accessible={false}
        name="close"
        size={18}
        color={colors.textTertiary}
      />
    </Pressable>
  );
}

function PluginsList({
  query,
  state,
  view,
  currentServerAvailable,
  refreshing,
  preparingMutation,
  onOpenSettings,
  onRetry,
  onInstall,
  onInspectInstalled,
  onInspectAvailable,
}: {
  query: string;
  state: SkillsRequestState<PluginInventory>;
  view: PluginsUnifiedView;
  currentServerAvailable: boolean;
  refreshing: boolean;
  preparingMutation: string;
  onOpenSettings(): void;
  onRetry(): void;
  onInstall(plugin: AvailablePlugin): void;
  onInspectInstalled(plugin: InstalledPluginRow): void;
  onInspectAvailable(plugin: AvailablePlugin): void;
}) {
  const colors = useAppColors();
  const hasData = skillsRequestData(state) !== undefined;
  const visibleInstalled = useMemo(
    () =>
      view.rows
        .filter((row) => row.kind === "installed")
        .map((row) => (row.kind === "installed" ? row.plugin : null))
        .filter((plugin): plugin is InstalledPluginRow => plugin != null),
    [view],
  );
  const visibleAvailable = useMemo(
    () =>
      view.rows
        .filter((row) => row.kind === "available")
        .map((row) => (row.kind === "available" ? row.plugin : null))
        .filter((plugin): plugin is AvailablePlugin => plugin != null),
    [view],
  );
  const filteredInstalled = useMemo(
    () => filterInstalledPlugins(visibleInstalled, query),
    [query, visibleInstalled],
  );
  const filteredAvailable = useMemo(
    () => filterAvailablePlugins(visibleAvailable, query),
    [query, visibleAvailable],
  );
  const data: Array<
    | { kind: "installed"; plugin: InstalledPluginRow }
    | { kind: "available"; plugin: AvailablePlugin }
  > = [
    ...filteredInstalled.map((plugin) => ({ kind: "installed" as const, plugin })),
    ...filteredAvailable.map((plugin) => ({ kind: "available" as const, plugin })),
  ];

  return (
    <FlatList
      style={styles.list}
      data={data}
      keyExtractor={(row) =>
        row.kind === "installed" ? row.plugin.id : row.plugin.pluginId
      }
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
          empty={hasData && view.rows.length === 0 && view.catalogReady}
          emptyTitle="No plugins found"
          noMatches={
            hasData &&
            view.rows.length > 0 &&
            filteredInstalled.length + filteredAvailable.length === 0
          }
          capabilityUnavailable={hasData && !view.catalogReady && view.rows.length > 0}
          capabilityTitle={
            view.rows.length === 0
              ? "Plugin catalog unavailable"
              : "Catalog unavailable — installed rows only"
          }
          catalogUnavailable={hasData && !view.catalogReady && view.rows.length === 0}
          currentServerAvailable={currentServerAvailable}
          onOpenSettings={onOpenSettings}
          onRetry={onRetry}
        />
      }
      renderItem={({ item }) =>
        item.kind === "installed" ? (
          <InstalledPluginItem
            plugin={item.plugin}
            onInspect={() => onInspectInstalled(item.plugin)}
          />
        ) : (
          <AvailablePluginItem
            plugin={item.plugin}
            busy={preparingMutation === `plugin:install:${item.plugin.pluginId}`}
            onInstall={() => onInstall(item.plugin)}
            onInspect={() => onInspectAvailable(item.plugin)}
          />
        )
      }
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
  const badges = installedPluginBadges(plugin);
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
        <BadgeRow badges={badges} />
      </View>
      <ItemActionIndicator
        icon={ownership.manageable ? "ellipsis-horizontal" : "information-circle-outline"}
      />
    </Pressable>
  );
}

function AvailablePluginItem({
  plugin,
  busy,
  onInstall,
  onInspect,
}: {
  plugin: AvailablePlugin;
  busy: boolean;
  onInstall(): void;
  onInspect(): void;
}) {
  const colors = useAppColors();
  const badges = availablePluginBadges(plugin);
  return (
    <View style={styles.itemRow}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${plugin.name}, available plugin`}
        accessibilityHint="Show plugin details"
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
          <Ionicons
            name="extension-puzzle-outline"
            size={20}
            color={colors.textSecondary}
          />
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
            {plugin.description ||
              [`@${plugin.marketplaceName}`, plugin.sourceRef].filter(Boolean).join(" · ")}
          </Text>
          <BadgeRow badges={badges} />
        </View>
      </Pressable>
      {plugin.installable ? (
        <SmallAction
          label="Install"
          accessibilityLabel={`Install ${plugin.name}`}
          busy={busy}
          onPress={onInstall}
        />
      ) : (
        <InstalledCheck />
      )}
    </View>
  );
}

function SkillsList({
  selectedAgent,
  query,
  state,
  skills,
  catalogSkills,
  catalogInstalledElsewhere,
  leaderboardView,
  browsing,
  submittedQuery,
  leaderboard,
  catalogState,
  searchState,
  searchResult,
  currentServerAvailable,
  refreshing,
  preparingMutation,
  onOpenSettings,
  onRefresh,
  onRemove,
  onInstall,
  onRetryCatalog,
  onRetrySearch,
  onInspect,
}: {
  selectedAgent: ManagedSkillAgent;
  query: string;
  state: SkillsRequestState<unknown>;
  skills: InstalledSkill[];
  catalogSkills: Array<CatalogSkill | RankedCatalogSkill>;
  catalogInstalledElsewhere: Record<string, readonly string[]>;
  leaderboardView: SkillsLeaderboardView;
  browsing: boolean;
  submittedQuery: string;
  leaderboard?: SkillsLeaderboard;
  catalogState: SkillsRequestState<SkillsLeaderboards>;
  searchState: SkillsRequestState<SkillsCatalogResult>;
  searchResult?: SkillsCatalogResult;
  currentServerAvailable: boolean;
  refreshing: boolean;
  preparingMutation: string;
  onOpenSettings(): void;
  onRefresh(): void;
  onRemove(skill: InstalledSkill): void;
  onInstall(skill: CatalogSkill | RankedCatalogSkill): void;
  onRetryCatalog(): void;
  onRetrySearch(): void;
  onInspect(skill: InstalledSkill): void;
}) {
  const colors = useAppColors();
  const hasInventory = skillsRequestData(state) !== undefined;
  const visibleSkills = useMemo(
    () => filterInstalledSkills(skills, query),
    [query, skills],
  );
  const visibleCatalog = useMemo(
    () => filterCatalogSkills(catalogSkills, query),
    [query, catalogSkills],
  );
  const rows = useMemo(
    () => skillsUnifiedRows(visibleSkills, visibleCatalog),
    [visibleCatalog, visibleSkills],
  );
  return (
    <FlatList
      style={styles.list}
      data={rows}
      keyExtractor={rowKey}
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
          empty={
            hasInventory && skills.length === 0 && !browsing
          }
          emptyTitle={`No Skills for ${skillAgentLabel(selectedAgent)}`}
          noMatches={
            hasInventory &&
            skills.length + catalogSkills.length > 0 &&
            visibleSkills.length + visibleCatalog.length === 0
          }
          currentServerAvailable={currentServerAvailable}
          onOpenSettings={onOpenSettings}
          onRetry={onRefresh}
        />
      }
      ListFooterComponent={
        <CatalogBrowseNote
          browsing={browsing}
          submittedQuery={submittedQuery}
          leaderboard={leaderboard}
          catalogState={catalogState}
          searchState={searchState}
          searchResult={searchResult}
          showsInstalled={hasInventory && skills.length > 0}
          currentServerAvailable={currentServerAvailable}
          onOpenSettings={onOpenSettings}
          onRetryCatalog={onRetryCatalog}
          onRetrySearch={onRetrySearch}
        />
      }
      renderItem={({ item }) =>
        item.kind === "installed" ? (
          <InstalledSkillItem
            skill={item.skill}
            selectedAgent={selectedAgent}
            busy={preparingMutation === `remove:${item.skill.id}`}
            onRemove={() => onRemove(item.skill)}
            onInspect={() => onInspect(item.skill)}
          />
        ) : (
          <CatalogSkillItem
            skill={item.skill}
            leaderboardView={leaderboardView}
            installedElsewhere={catalogInstalledElsewhere[item.catalogId] ?? []}
            selectedAgent={selectedAgent}
            busy={preparingMutation === `install:${item.catalogId}`}
            onInstall={() => onInstall(item.skill)}
          />
        )
      }
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
  const badges = installedSkillBadges(skill, skill.agents.length);
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
          <Ionicons name="library-outline" size={21} color={colors.textSecondary} />
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
          <BadgeRow badges={badges} />
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

function CatalogSkillItem({
  skill,
  leaderboardView,
  installedElsewhere,
  selectedAgent,
  busy,
  onInstall,
}: {
  skill: CatalogSkill | RankedCatalogSkill;
  leaderboardView: SkillsLeaderboardView;
  installedElsewhere: readonly string[];
  selectedAgent: ManagedSkillAgent;
  busy: boolean;
  onInstall(): void;
}) {
  const colors = useAppColors();
  const ranked = isRankedCatalogSkill(skill);
  const metadata = ranked
    ? `${skill.source} · ${rankedMetric(skill, leaderboardView)}`
    : `${skill.source} · ${formatInstalls(skill.installs)} installs`;
  const badges = catalogSkillBadges(skill, [...installedElsewhere]);
  return (
    <View style={styles.itemRow}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${skill.name}, ${metadata}`}
        accessibilityHint="Show why this Skill can be installed"
        onPress={() =>
          Alert.alert(
            skill.name,
            `${skill.source}\n\nInstall adds this Skill for ${skillAgentLabel(selectedAgent)} through the official skills CLI.`,
          )
        }
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
          <Ionicons name="library-outline" size={21} color={colors.textSecondary} />
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
            {metadata}
          </Text>
          <BadgeRow badges={badges} />
        </View>
      </Pressable>
      {skill.installable ? (
        <SmallAction
          label="Install"
          accessibilityLabel={`Install ${skill.name} for ${skillAgentLabel(selectedAgent)}`}
          busy={busy}
          onPress={onInstall}
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
      )}
    </View>
  );
}

function CatalogBrowseNote({
  browsing,
  submittedQuery,
  leaderboard,
  catalogState,
  searchState,
  searchResult,
  showsInstalled,
  currentServerAvailable,
  onOpenSettings,
  onRetryCatalog,
  onRetrySearch,
}: {
  browsing: boolean;
  submittedQuery: string;
  leaderboard?: SkillsLeaderboard;
  catalogState: SkillsRequestState<SkillsLeaderboards>;
  searchState: SkillsRequestState<SkillsCatalogResult>;
  searchResult?: SkillsCatalogResult;
  showsInstalled: boolean;
  currentServerAvailable: boolean;
  onOpenSettings(): void;
  onRetryCatalog(): void;
  onRetrySearch(): void;
}) {
  const colors = useAppColors();
  if (!browsing) {
    if (catalogState.status === "error") {
      return (
        <View style={[styles.browseNote, { borderTopColor: colors.borderSubtle }]}>
          <Text
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.browseNoteTitle, { color: colors.textPrimary }]}
          >
            Catalog unavailable
          </Text>
          <Text
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.browseNoteDetail, { color: colors.textTertiary }]}
          >
            {catalogState.error}
          </Text>
          <SmallAction label="Retry" onPress={onRetryCatalog} />
        </View>
      );
    }
    if (leaderboard && leaderboard.skills.length === 0 && !showsInstalled) {
      return (
        <View style={[styles.browseNote, { borderTopColor: colors.borderSubtle }]}>
          <Text
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.browseNoteTitle, { color: colors.textPrimary }]}
          >
            No catalog Skills
          </Text>
          <Text
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.browseNoteDetail, { color: colors.textTertiary }]}
          >
            Use Search above to discover Skills from skills.sh.
          </Text>
        </View>
      );
    }
    return null;
  }
  if (searchState.status === "loading") {
    return <LoadingNote title={`Searching for “${submittedQuery}”…`} />;
  }
  if (searchState.status === "error") {
    return (
      <View style={[styles.browseNote, { borderTopColor: colors.borderSubtle }]}>
        <Text
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.browseNoteTitle, { color: colors.textPrimary }]}
        >
          Search unavailable
        </Text>
        <Text
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.browseNoteDetail, { color: colors.textTertiary }]}
        >
          {searchState.error}
        </Text>
        <SmallAction label="Retry" onPress={onRetrySearch} />
      </View>
    );
  }
  if (searchState.status === "empty") {
    return (
      <View style={[styles.browseNote, { borderTopColor: colors.borderSubtle }]}>
        <Text
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.browseNoteTitle, { color: colors.textPrimary }]}
        >
          No results for “{submittedQuery}”
        </Text>
        <Text
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.browseNoteDetail, { color: colors.textTertiary }]}
        >
          {searchResult?.skills.length === 0
            ? "Nothing on skills.sh matched this query."
            : ""}
        </Text>
      </View>
    );
  }
  return null;
}

function LoadingNote({ title }: { title: string }) {
  const colors = useAppColors();
  return (
    <View style={[styles.browseNote, { borderTopColor: colors.borderSubtle }]}>
      <ActivityIndicator size="small" color={colors.accent} />
      <Text
        maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
        style={[styles.browseNoteTitle, { color: colors.textSecondary }]}
      >
        {title}
      </Text>
    </View>
  );
}

function BadgeRow({ badges }: { badges: LifecycleBadge[] }) {
  const colors = useAppColors();
  if (badges.length === 0) {
    return null;
  }
  return (
    <View style={styles.badgeRow}>
      {badges.map((badge) => (
        <View
          key={badge.label}
          style={[
            styles.badgePill,
            {
              backgroundColor:
                badge.tone === "accent"
                  ? colors.accentSoft
                  : badge.tone === "warning"
                    ? colors.dangerSoft
                    : colors.surfaceSubtle,
              borderColor:
                badge.tone === "accent"
                  ? colors.accent
                  : badge.tone === "warning"
                    ? colors.dangerText
                    : colors.borderSubtle,
            },
          ]}
        >
          <Text
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[
              styles.badgeLabel,
              {
                color:
                  badge.tone === "accent"
                    ? colors.accent
                    : badge.tone === "warning"
                      ? colors.dangerText
                      : colors.textSecondary,
              },
            ]}
          >
            {badge.label}
          </Text>
        </View>
      ))}
    </View>
  );
}

function SurfaceSheet({
  sheet,
  section,
  selectedAgent,
  agentCounts,
  leaderboardView,
  hasProjectCwd,
  preparingMutation,
  onClose,
  onSelectAgent,
  onSelectLeaderboard,
  onUpdateSkills,
  onUpdatePlugin,
  onUninstallPlugin,
}: {
  sheet: SurfaceSheet;
  section: SkillsSurfaceSection;
  selectedAgent: ManagedSkillAgent;
  agentCounts: SkillsAgentCounts;
  leaderboardView: SkillsLeaderboardView;
  hasProjectCwd: boolean;
  preparingMutation: string;
  onClose(): void;
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

        {sheet?.kind === "plugin-available" ? (
          <AvailablePluginDetailSheet plugin={sheet.plugin} />
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
  const badges = installedSkillBadges(skill, skill.agents.length);
  return (
    <>
      <SheetTitle>{skill.name}</SheetTitle>
      <SheetMetadata>{installedSkillMetadata(skill)}</SheetMetadata>
      <BadgeRow badges={badges} />
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
  const badges = installedPluginBadges(plugin);
  return (
    <>
      <SheetTitle>{plugin.name}</SheetTitle>
      <SheetMetadata>{installedPluginMetadata(plugin)}</SheetMetadata>
      <BadgeRow badges={badges} />
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

function AvailablePluginDetailSheet({ plugin }: { plugin: AvailablePlugin }) {
  const ownership = availablePluginOwnership(plugin);
  const badges = availablePluginBadges(plugin);
  return (
    <>
      <SheetTitle>{plugin.name}</SheetTitle>
      <SheetMetadata>{`@${plugin.marketplaceName}`}</SheetMetadata>
      <BadgeRow badges={badges} />
      {plugin.description ? <SheetBody>{plugin.description}</SheetBody> : null}
      <SheetSection title={ownership.summary}>{ownership.detail}</SheetSection>
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
            name="library-outline"
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
  capabilityTitle = "Capability unavailable",
  catalogUnavailable,
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
  capabilityTitle?: string;
  catalogUnavailable?: boolean;
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
  if (capabilityUnavailable || catalogUnavailable) {
    return (
      <RequestState
        title={
          catalogUnavailable && !capabilityUnavailable
            ? "Plugin catalog unavailable"
            : capabilityTitle
        }
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

function rowKey(row: SkillsUnifiedRow): string {
  return row.kind === "installed" ? `installed:${row.skill.id}` : `catalog:${row.catalogId}`;
}

function searchPlaceholder(section: SkillsSurfaceSection): string {
  return section === "plugins"
    ? "Search plugins"
    : "Search your Skills or skills.sh";
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
  noticeBanner: {
    marginHorizontal: 16,
    marginBottom: 8,
    paddingHorizontal: 12,
    paddingVertical: 9,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: Radii.sm,
    flexDirection: "row",
    alignItems: "center",
    gap: 9,
  },
  noticeText: {
    ...TypeScale.compact,
    flex: 1,
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
  badgeRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 6,
    paddingTop: 3,
  },
  badgePill: {
    paddingHorizontal: 7,
    paddingVertical: 2,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: Radii.pill,
    alignSelf: "flex-start",
  },
  badgeLabel: { ...TypeScale.micro },
  browseNote: {
    paddingVertical: 10,
    gap: 4,
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  browseNoteTitle: {
    ...TypeScale.compact,
    fontFamily: Typography.uiFontMedium,
  },
  browseNoteDetail: { ...TypeScale.caption },
  itemIconAction: {
    width: PLUGINS_SKILLS_TOUCH_TARGET,
    height: PLUGINS_SKILLS_TOUCH_TARGET,
    flexShrink: 0,
    borderRadius: Radii.xs,
    alignItems: "center",
    justifyContent: "center",
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
});