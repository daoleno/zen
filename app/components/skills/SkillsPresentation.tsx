import React, { useEffect, useMemo, useState } from "react";
import {
  ActivityIndicator,
  Alert,
  FlatList,
  Linking,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  View,
  useWindowDimensions,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import { EnrichedMarkdownText } from "react-native-enriched-markdown";
import {
  Radii,
  TypeScale,
  Typography,
  useAppColors,
} from "../../constants/tokens";
import { openSafeMarkdownUrl } from "../markdown/markdownLinks";
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
  PackageDetail,
  RankedCatalogSkill,
  SkillBinding,
  SkillMutationOperation,
  SkillsCatalogResult,
  SkillsLeaderboard,
  SkillsLeaderboards,
  SkillsRequestState,
} from "../../services/skillsManagement";
import {
  scopeLabel,
  skillAgentLabel,
  skillsRequestData,
} from "../../services/skillsManagement";
import {
  DEFAULT_SKILLS_FACETS,
  filterSkillsByFacets,
  skillsInspectionTarget,
  skillsLeaderboardLabel,
  type SkillsAgentCounts,
  type SkillsInspectionTarget,
  type SkillsLeaderboardView,
  type SkillsUnifiedRow,
  skillsUnifiedRows,
  type SkillsFacets,
} from "../../services/skillsScreenModel";
import type { SkillsSurfaceSection } from "../../services/skillsSurfaceModel";
import { skillBindingSupports } from "../../services/skillsSurfaceModel";
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
  inspectedName: string | null;
  inspectState: SkillsRequestState<PackageDetail>;
  onSelectSection(section: SkillsSurfaceSection): void;
  onSelectAgent(agent: ManagedSkillAgent): void;
  onOpenSettings(): void;
  onRefreshSkills(): void;
  onRetryPlugins(): void;
  onInspectSkill(name: string, path?: string): void;
  onDismissInspector(): void;
  onImport(skill: CatalogSkill | RankedCatalogSkill): void;
  onMigrate(): void;
  onBinding(
    skill: InstalledSkill,
    operation: "bind" | "unbind" | "enable" | "disable",
    agent: ManagedSkillAgent,
    scope: "project" | "global",
  ): void;
  onUninstall(skill: InstalledSkill): void;
  onForget(skill: InstalledSkill): void;
  onAdopt(skill: InstalledSkill): void;
  onUpdate(skill: InstalledSkill): void;
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

/** Desktop breakpoint: at wide widths the inspector becomes a persistent side panel. */
const INSPECTOR_PANEL_BREAKPOINT = 920;
const INSPECTOR_PANEL_WIDTH = 400;

type PluginSheet =
  | { kind: "plugin-details"; plugin: InstalledPluginRow }
  | { kind: "plugin-available"; plugin: AvailablePlugin }
  | null;

type SurfaceSheet = { kind: "options" } | PluginSheet;

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
    inspectedName,
    inspectState,
    onSelectSection,
    onSelectAgent,
    onOpenSettings,
    onRefreshSkills,
    onRetryPlugins,
    onInspectSkill,
    onDismissInspector,
    onImport,
    onMigrate,
    onBinding,
    onUninstall,
    onForget,
    onAdopt,
    onUpdate,
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
  const { width } = useWindowDimensions();
  const [localQuery, setLocalQuery] = useState("");
  const [pluginSheet, setPluginSheet] = useState<PluginSheet>(null);
  const [optionsSheet, setOptionsSheet] = useState(false);
  const [facets, setFacets] = useState<SkillsFacets>(DEFAULT_SKILLS_FACETS);
  const [inspectionTarget, setInspectionTarget] =
    useState<SkillsInspectionTarget | null>(null);

  useEffect(() => {
    setLocalQuery("");
    setPluginSheet(null);
  }, [section, selectedAgent]);

  useEffect(() => {
    if (!inspectedName) {
      setInspectionTarget(null);
      return;
    }
    setInspectionTarget((current) =>
      current?.name === inspectedName
        ? current
        : skillsInspectionTarget(inspectedName),
    );
  }, [inspectedName]);

  const requestInspection = (name: string, path?: string) => {
    const target = skillsInspectionTarget(name, path);
    setInspectionTarget(target);
    onInspectSkill(target.name, target.path);
  };
  const retryInspection = () => {
    if (inspectionTarget) {
      onInspectSkill(inspectionTarget.name, inspectionTarget.path);
    }
  };

  const desktopInspector =
    width >= INSPECTOR_PANEL_BREAKPOINT &&
    Boolean(inspectedName || pluginSheet);
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
        catalogState.status === "loading" ||
        searchState.status === "loading";
  const inspectDetail = inspectedName
    ? skillsRequestData(inspectState)
    : undefined;
  const inspectedInstalled = useMemo(
    () => installedSkills.find((skill) => skill.name === inspectedName) ?? null,
    [inspectedName, installedSkills],
  );
  const facetedSkills = useMemo(
    () => filterSkillsByFacets(installedSkills, catalogSkills, facets),
    [catalogSkills, facets, installedSkills],
  );

  const skillInspectorElement = inspectedName ? (
    inspectDetail ? (
      <SkillInspector
        detail={inspectDetail}
        installed={inspectedInstalled}
        inspectState={inspectState}
        selectedAgent={selectedAgent}
        capabilities={mutationOperations}
        hasProjectCwd={hasProjectCwd}
        preparingMutation={preparingMutation}
        retryPath={inspectionTarget?.path}
        onClose={onDismissInspector}
        onBinding={(operation, agent, scope) => {
          if (inspectedInstalled) {
            onBinding(inspectedInstalled, operation, agent, scope);
          }
        }}
        onUninstall={() => {
          if (inspectedInstalled) {
            onUninstall(inspectedInstalled);
          }
        }}
        onForget={() => {
          if (inspectedInstalled) {
            onForget(inspectedInstalled);
          }
        }}
        onAdopt={() => {
          if (inspectedInstalled) {
            onAdopt(inspectedInstalled);
          }
        }}
        onUpdate={() => {
          if (inspectedInstalled) {
            onUpdate(inspectedInstalled);
          }
        }}
        onInspectFile={(path) =>
          requestInspection(inspectDetail.skillName, path)
        }
        onRetryInspect={retryInspection}
      />
    ) : (
      <SkillInspectorStatus
        name={inspectedName}
        inspectState={inspectState}
        onClose={onDismissInspector}
        onRetry={retryInspection}
      />
    )
  ) : null;
  const pluginInspectorElement = pluginSheet ? (
    <View
      style={[styles.inspectorPanel, { borderLeftColor: colors.borderSubtle }]}
    >
      <ScrollView contentContainerStyle={styles.sheetContent}>
        {pluginSheet.kind === "plugin-details" ? (
          <PluginDetailSheet
            plugin={pluginSheet.plugin}
            onUpdate={() => onUpdatePlugin(pluginSheet.plugin)}
            onUninstall={() => onUninstallPlugin(pluginSheet.plugin)}
          />
        ) : (
          <AvailablePluginDetailSheet
            plugin={pluginSheet.plugin}
            onInstall={() => onInstallPlugin(pluginSheet.plugin)}
          />
        )}
      </ScrollView>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Close inspector"
        onPress={() => setPluginSheet(null)}
        style={[styles.sheetClose, { borderTopColor: colors.borderSubtle }]}
      >
        <Text style={[styles.sheetCloseText, { color: colors.textSecondary }]}>
          Close
        </Text>
      </Pressable>
    </View>
  ) : null;
  const inspectorElement =
    section === "plugins" ? pluginInspectorElement : skillInspectorElement;

  return (
    <SafeAreaView edges={["top"]} style={styles.root}>
      <SurfaceTabs section={section} onSelect={onSelectSection} />
      {section === "skills" ? (
        <View style={styles.tools}>
          <SurfaceSearch
            value={localQuery || query}
            remote={showingSkillsSearch}
            loading={searchState.status === "loading"}
            placeholder={searchPlaceholder("skills")}
            onChange={(value) => {
              setLocalQuery(value);
              onChangeQuery(value);
            }}
            onSubmit={onSubmitSearch}
            onClear={() => {
              setLocalQuery("");
              onClearSearch();
            }}
          />
          <CompactToolbar
            section="skills"
            refreshing={refreshing}
            onOpenOptions={() => setOptionsSheet(true)}
            onRefresh={refresh}
          />
        </View>
      ) : (
        <View style={styles.tools}>
          <SurfaceSearch
            value={localQuery || query}
            remote={false}
            loading={false}
            placeholder={searchPlaceholder("plugins")}
            onChange={(value) => {
              setLocalQuery(value);
              onChangeQuery(value);
            }}
            onClear={() => {
              setLocalQuery("");
              onClearSearch();
            }}
          />
          <CompactToolbar
            section="plugins"
            refreshing={refreshing}
            onRefresh={refresh}
          />
        </View>
      )}
      {mutationNotice ? (
        <MutationNoticeBanner
          notice={mutationNotice}
          onDismiss={onDismissNotice}
        />
      ) : null}
      {desktopInspector ? (
        <View style={styles.desktopRow}>
          <View style={styles.desktopList}>
            <SurfaceBody
              {...props}
              installedSkills={facetedSkills.installed}
              catalogSkills={facetedSkills.catalog}
              section={section}
              selectedAgent={selectedAgent}
              query={localQuery || query}
              refreshing={refreshing}
              preparingMutation={preparingMutation}
              onInspectSkill={requestInspection}
              onInspectPlugin={(plugin) =>
                setPluginSheet(
                  "pluginId" in plugin
                    ? { kind: "plugin-available", plugin }
                    : { kind: "plugin-details", plugin },
                )
              }
            />
          </View>
          <View style={styles.desktopPanel}>{inspectorElement}</View>
        </View>
      ) : (
        <SurfaceBody
          {...props}
          installedSkills={facetedSkills.installed}
          catalogSkills={facetedSkills.catalog}
          section={section}
          selectedAgent={selectedAgent}
          query={localQuery || query}
          refreshing={refreshing}
          preparingMutation={preparingMutation}
          onInspectSkill={requestInspection}
          onInspectPlugin={(plugin) =>
            setPluginSheet(
              "pluginId" in plugin
                ? { kind: "plugin-available", plugin }
                : { kind: "plugin-details", plugin },
            )
          }
        />
      )}
      <SurfaceSheet
        sheet={
          optionsSheet
            ? { kind: "options" }
            : desktopInspector
              ? null
              : pluginSheet
        }
        selectedAgent={selectedAgent}
        agentCounts={agentCounts}
        leaderboardView={leaderboardView}
        facets={facets}
        preparingMutation={preparingMutation}
        onClose={() => {
          setOptionsSheet(false);
          setPluginSheet(null);
        }}
        onSelectAgent={onSelectAgent}
        onSelectLeaderboard={onSelectLeaderboard}
        onChangeFacets={setFacets}
        onDiscover={onMigrate}
        onUpdatePlugin={(plugin) => {
          setPluginSheet(null);
          onUpdatePlugin(plugin);
        }}
        onInstallPlugin={(plugin) => {
          setPluginSheet(null);
          onInstallPlugin(plugin);
        }}
        onUninstallPlugin={(plugin) => {
          setPluginSheet(null);
          onUninstallPlugin(plugin);
        }}
      />
      {inspectedName && !desktopInspector ? (
        inspectDetail ? (
          <SkillInspectorSheet
            detail={inspectDetail}
            inspectState={inspectState}
            installed={inspectedInstalled}
            selectedAgent={selectedAgent}
            capabilities={mutationOperations}
            hasProjectCwd={hasProjectCwd}
            preparingMutation={preparingMutation}
            retryPath={inspectionTarget?.path}
            onClose={onDismissInspector}
            onBinding={(operation, agent, scope) => {
              if (inspectedInstalled) {
                onBinding(inspectedInstalled, operation, agent, scope);
              }
            }}
            onUninstall={() => {
              if (inspectedInstalled) {
                onUninstall(inspectedInstalled);
              }
            }}
            onForget={() => {
              if (inspectedInstalled) {
                onForget(inspectedInstalled);
              }
            }}
            onAdopt={() => {
              if (inspectedInstalled) {
                onAdopt(inspectedInstalled);
              }
            }}
            onUpdate={() => {
              if (inspectedInstalled) {
                onUpdate(inspectedInstalled);
              }
            }}
            onInspectFile={(path) =>
              requestInspection(inspectDetail.skillName, path)
            }
            onRetryInspect={retryInspection}
          />
        ) : (
          <SkillInspectorStatusSheet
            name={inspectedName}
            inspectState={inspectState}
            onClose={onDismissInspector}
            onRetry={retryInspection}
          />
        )
      ) : null}
      {mutationNotice ? null : null}
    </SafeAreaView>
  );
}

function SurfaceBody({
  section,
  selectedAgent,
  query,
  inventoryState,
  installedSkills,
  catalogSkills,
  catalogInstalledElsewhere,
  browsing,
  catalogState,
  leaderboard,
  searchState,
  searchResult,
  submittedQuery,
  leaderboardView,
  pluginsState,
  pluginsView,
  currentServerAvailable,
  refreshing,
  preparingMutation,
  onOpenSettings,
  onRetryPlugins,
  onRefreshSkills,
  onInspectSkill,
  onImport,
  onBinding,
  onUninstall,
  onForget,
  onAdopt,
  onUpdate,
  onInstallPlugin,
  onChangeQuery,
  onSubmitSearch,
  onClearSearch,
  onSelectLeaderboard,
  onRetryCatalog,
  onRetrySearch,
  onInspectPlugin,
}: {
  section: SkillsSurfaceSection;
  selectedAgent: ManagedSkillAgent;
  query: string;
  inventoryState: SkillsRequestState<unknown>;
  installedSkills: InstalledSkill[];
  catalogSkills: Array<CatalogSkill | RankedCatalogSkill>;
  catalogInstalledElsewhere: Record<string, readonly string[]>;
  browsing: boolean;
  catalogState: SkillsRequestState<SkillsLeaderboards>;
  leaderboard?: SkillsLeaderboard;
  searchState: SkillsRequestState<SkillsCatalogResult>;
  searchResult?: SkillsCatalogResult;
  submittedQuery: string;
  leaderboardView: SkillsLeaderboardView;
  pluginsState: SkillsRequestState<PluginInventory>;
  pluginsView: PluginsUnifiedView;
  currentServerAvailable: boolean;
  refreshing: boolean;
  preparingMutation: string;
  onOpenSettings(): void;
  onRetryPlugins(): void;
  onRefreshSkills(): void;
  onInspectSkill(name: string, path?: string): void;
  onImport(skill: CatalogSkill | RankedCatalogSkill): void;
  onBinding(
    skill: InstalledSkill,
    operation: "bind" | "unbind" | "enable" | "disable",
    agent: ManagedSkillAgent,
    scope: "project" | "global",
  ): void;
  onUninstall(skill: InstalledSkill): void;
  onForget(skill: InstalledSkill): void;
  onAdopt(skill: InstalledSkill): void;
  onUpdate(skill: InstalledSkill): void;
  onInstallPlugin(entry: AvailablePlugin): void;
  onChangeQuery(value: string): void;
  onSubmitSearch(): void;
  onClearSearch(): void;
  onSelectLeaderboard(view: SkillsLeaderboardView): void;
  onRetryCatalog(): void;
  onRetrySearch(): void;
  onInspectPlugin(plugin: InstalledPluginRow | AvailablePlugin): void;
}) {
  if (section === "plugins") {
    return (
      <PluginsList
        query={query}
        state={pluginsState}
        view={pluginsView}
        currentServerAvailable={currentServerAvailable}
        refreshing={refreshing}
        preparingMutation={preparingMutation}
        onOpenSettings={onOpenSettings}
        onRetry={onRetryPlugins}
        onInstall={onInstallPlugin}
        onInspectInstalled={(plugin) => onInspectPlugin(plugin)}
        onInspectAvailable={(plugin) => onInspectPlugin(plugin)}
      />
    );
  }
  return (
    <SkillsList
      selectedAgent={selectedAgent}
      query={query}
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
      onRefresh={onRefreshSkills}
      onInspect={onInspectSkill}
      onImport={onImport}
      onBinding={onBinding}
      onUninstall={onUninstall}
      onForget={onForget}
      onAdopt={onAdopt}
      onUpdate={onUpdate}
      onRetryCatalog={onRetryCatalog}
      onRetrySearch={onRetrySearch}
    />
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
    { section: "skills", label: "Skills", icon: "library-outline" },
    {
      section: "plugins",
      label: "Plugins",
      icon: "extension-puzzle-outline",
    },
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
                style={[
                  styles.selectedTabLine,
                  { backgroundColor: colors.accent },
                ]}
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
  refreshing,
  onOpenOptions,
  onRefresh,
}: {
  section: SkillsSurfaceSection;
  refreshing: boolean;
  onOpenOptions?(): void;
  onRefresh(): void;
}) {
  const colors = useAppColors();
  return (
    <View style={styles.toolbar}>
      <View style={styles.toolbarSpacer} />
      {section === "skills" && onOpenOptions ? (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Skills options"
          accessibilityHint="Target, ranking, and discovery actions"
          onPress={onOpenOptions}
          style={({ pressed }) => [
            styles.iconButton,
            pressed ? { backgroundColor: colors.surfacePressed } : null,
          ]}
        >
          <Ionicons
            accessible={false}
            name="ellipsis-horizontal-circle-outline"
            size={22}
            color={colors.textSecondary}
          />
        </Pressable>
      ) : null}
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
    ...filteredInstalled.map((plugin) => ({
      kind: "installed" as const,
      plugin,
    })),
    ...filteredAvailable.map((plugin) => ({
      kind: "available" as const,
      plugin,
    })),
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
          capabilityUnavailable={
            hasData && !view.catalogReady && view.rows.length > 0
          }
          capabilityTitle={
            view.rows.length === 0
              ? "Plugin catalog unavailable"
              : "Catalog unavailable — installed rows only"
          }
          catalogUnavailable={
            hasData && !view.catalogReady && view.rows.length === 0
          }
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
            busy={
              preparingMutation === `plugin:install:${item.plugin.pluginId}`
            }
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
        <AgentKindIcon
          kind={pluginHostKind(plugin.host)}
          size={22}
          variant="compact"
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
          {installedPluginMetadata(plugin)}
        </Text>
        <BadgeRow badges={badges} />
      </View>
      <ItemActionIndicator
        icon={
          ownership.manageable
            ? "ellipsis-horizontal"
            : "information-circle-outline"
        }
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
              [`@${plugin.marketplaceName}`, plugin.sourceRef]
                .filter(Boolean)
                .join(" · ")}
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
  onInspect,
  onImport,
  onBinding,
  onUninstall,
  onForget,
  onAdopt,
  onUpdate,
  onRetryCatalog,
  onRetrySearch,
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
  onInspect(name: string): void;
  onImport(skill: CatalogSkill | RankedCatalogSkill): void;
  onBinding(
    skill: InstalledSkill,
    operation: "bind" | "unbind" | "enable" | "disable",
    agent: ManagedSkillAgent,
    scope: "project" | "global",
  ): void;
  onUninstall(skill: InstalledSkill): void;
  onForget(skill: InstalledSkill): void;
  onAdopt(skill: InstalledSkill): void;
  onUpdate(skill: InstalledSkill): void;
  onRetryCatalog(): void;
  onRetrySearch(): void;
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
      refreshControl={surfaceRefreshControl(
        refreshing,
        onRefresh,
        colors.accent,
      )}
      ItemSeparatorComponent={() => <Separator />}
      ListHeaderComponent={
        <ListStateHeader
          loading={state.status === "loading" && !hasInventory}
          loadingTitle="Loading installed Skills…"
          error={state.status === "error" ? state.error : undefined}
          errorTitle="Installed Skills unavailable"
          empty={hasInventory && skills.length === 0 && !browsing}
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
          <InstalledSkillRow
            skill={item.skill}
            selectedAgent={selectedAgent}
            preparingMutation={preparingMutation}
            onInspect={() => onInspect(item.skill.name)}
            onBinding={onBinding}
            onUninstall={onUninstall}
            onForget={onForget}
            onAdopt={onAdopt}
            onUpdate={onUpdate}
          />
        ) : (
          <CatalogSkillRow
            skill={item.skill}
            leaderboardView={leaderboardView}
            installedElsewhere={catalogInstalledElsewhere[item.catalogId] ?? []}
            selectedAgent={selectedAgent}
            busy={preparingMutation === `install:${item.catalogId}`}
            onImport={() => onImport(item.skill)}
          />
        )
      }
    />
  );
}

/** One installed/discovered row: lifecycle state is row facets, not chrome. */
function InstalledSkillRow({
  skill,
  selectedAgent,
  preparingMutation,
  onInspect,
  onBinding,
  onUninstall,
  onForget,
  onAdopt,
  onUpdate,
}: {
  skill: InstalledSkill;
  selectedAgent: ManagedSkillAgent;
  preparingMutation: string;
  onInspect(): void;
  onBinding(
    skill: InstalledSkill,
    operation: "bind" | "unbind" | "enable" | "disable",
    agent: ManagedSkillAgent,
    scope: "project" | "global",
  ): void;
  onUninstall(skill: InstalledSkill): void;
  onForget(skill: InstalledSkill): void;
  onAdopt(skill: InstalledSkill): void;
  onUpdate(skill: InstalledSkill): void;
}) {
  const colors = useAppColors();
  const ownership = installedSkillOwnership(skill, selectedAgent);
  const badges = installedSkillBadges(skill, skill.agents.length);
  const capabilities = skill.capability.canManage
    ? skill.capability.operations
    : [];
  const binding = skill.bindings.find(
    (candidate) => candidate.agent === selectedAgent,
  );
  const quickAction = skillQuickAction(
    skill,
    capabilities,
    binding,
    onUpdate,
    onUninstall,
    onForget,
    onAdopt,
  );
  return (
    <View style={styles.itemRow}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${skill.name}, ${installedSkillMetadata(skill)}`}
        accessibilityHint="Open the Skill inspector"
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
            name="library-outline"
            size={21}
            color={colors.textSecondary}
          />
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
      {quickAction ? (
        <SmallAction
          label={quickAction.label}
          accessibilityLabel={`${quickAction.label} ${skill.name}`}
          destructive={quickAction.destructive}
          busy={preparingMutation === quickAction.busyKey}
          onPress={() => quickAction.run()}
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

function skillQuickAction(
  skill: InstalledSkill,
  capabilities: readonly SkillMutationOperation[],
  binding: SkillBinding | undefined,
  onUpdate: (skill: InstalledSkill) => void,
  onUninstall: (skill: InstalledSkill) => void,
  onForget: (skill: InstalledSkill) => void,
  onAdopt: (skill: InstalledSkill) => void,
): {
  label: string;
  destructive?: boolean;
  busyKey: string;
  run(): void;
} | null {
  if (skill.owned) {
    if (capabilities.includes("update")) {
      return {
        label: "Update",
        busyKey: `update:${skill.id}`,
        run: () => onUpdate(skill),
      };
    }
    if (capabilities.includes("uninstall")) {
      return {
        label: "Uninstall",
        destructive: true,
        busyKey: `uninstall:${skill.id}`,
        run: () => onUninstall(skill),
      };
    }
    return null;
  }
  if (skill.tracked) {
    if (capabilities.includes("forget")) {
      return {
        label: "Forget",
        busyKey: `forget:${skill.id}`,
        run: () => onForget(skill),
      };
    }
    return null;
  }
  if (capabilities.includes("adopt")) {
    return {
      label: "Adopt",
      busyKey: `adopt:${skill.id}`,
      run: () => onAdopt(skill),
    };
  }
  return null;
}

function CatalogSkillRow({
  skill,
  leaderboardView,
  installedElsewhere,
  selectedAgent,
  busy,
  onImport,
}: {
  skill: CatalogSkill | RankedCatalogSkill;
  leaderboardView: SkillsLeaderboardView;
  installedElsewhere: readonly string[];
  selectedAgent: ManagedSkillAgent;
  busy: boolean;
  onImport(): void;
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
        accessibilityHint="Import this Skill from its pinned source"
        onPress={() =>
          Alert.alert(
            skill.name,
            `${skill.source}\n\nImporting pins this Skill at its recorded ref and binds it for ${skillAgentLabel(selectedAgent)}.`,
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
          <Ionicons
            name="library-outline"
            size={21}
            color={colors.textSecondary}
          />
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
          label="Import"
          accessibilityLabel={`Import ${skill.name} for ${skillAgentLabel(selectedAgent)}`}
          busy={busy}
          onPress={onImport}
        />
      ) : (
        <ItemIconAction
          label={`Why ${skill.name} is unavailable`}
          icon="information-circle-outline"
          onPress={() =>
            Alert.alert(
              skill.name,
              "This catalog entry does not expose an installable identity.",
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
        <View
          style={[styles.browseNote, { borderTopColor: colors.borderSubtle }]}
        >
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
        <View
          style={[styles.browseNote, { borderTopColor: colors.borderSubtle }]}
        >
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
      <View
        style={[styles.browseNote, { borderTopColor: colors.borderSubtle }]}
      >
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
      <View
        style={[styles.browseNote, { borderTopColor: colors.borderSubtle }]}
      >
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
        style={[styles.browseNoteTitle, { color: colors.textPrimary }]}
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
      {badges.map((badge) => {
        const toneColor =
          badge.tone === "accent"
            ? colors.accent
            : badge.tone === "warning"
              ? colors.warning
              : colors.textTertiary;
        return (
          <View
            key={badge.label}
            style={[
              styles.badgePill,
              {
                backgroundColor: colors.surfaceSubtle,
                borderColor: colors.borderSubtle,
              },
            ]}
          >
            <Text
              maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
              style={[styles.badgeLabel, { color: toneColor }]}
            >
              {badge.label}
            </Text>
          </View>
        );
      })}
    </View>
  );
}

function SurfaceSheet({
  sheet,
  selectedAgent,
  agentCounts,
  leaderboardView,
  facets,
  preparingMutation,
  onClose,
  onSelectAgent,
  onSelectLeaderboard,
  onChangeFacets,
  onDiscover,
  onUpdatePlugin,
  onUninstallPlugin,
  onInstallPlugin,
}: {
  sheet: SurfaceSheet;
  selectedAgent: ManagedSkillAgent;
  agentCounts: SkillsAgentCounts;
  leaderboardView: SkillsLeaderboardView;
  facets: SkillsFacets;
  preparingMutation: string;
  onClose(): void;
  onSelectAgent(agent: ManagedSkillAgent): void;
  onSelectLeaderboard(view: SkillsLeaderboardView): void;
  onChangeFacets(facets: SkillsFacets): void;
  onDiscover(): void;
  onUpdatePlugin(plugin: InstalledPluginRow): void;
  onUninstallPlugin(plugin: InstalledPluginRow): void;
  onInstallPlugin(plugin: AvailablePlugin): void;
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
        {sheet?.kind === "options" ? (
          <>
            <SheetTitle>Skills options</SheetTitle>
            <SheetSectionHeading>Target</SheetSectionHeading>
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
            <SheetSectionHeading>Status</SheetSectionHeading>
            {(["all", "enabled", "disabled", "available"] as const).map(
              (status) => (
                <SheetOption
                  key={status}
                  icon="options-outline"
                  label={
                    status === "all"
                      ? "Any status"
                      : status[0]!.toUpperCase() + status.slice(1)
                  }
                  selected={facets.status === status}
                  onPress={() => onChangeFacets({ ...facets, status })}
                />
              ),
            )}
            <SheetSectionHeading>Scope</SheetSectionHeading>
            {(["all", "global", "project"] as const).map((scope) => (
              <SheetOption
                key={scope}
                icon="folder-outline"
                label={
                  scope === "all"
                    ? "Any scope"
                    : scope[0]!.toUpperCase() + scope.slice(1)
                }
                selected={facets.scope === scope}
                onPress={() => onChangeFacets({ ...facets, scope })}
              />
            ))}
            <SheetSectionHeading>Ownership / source</SheetSectionHeading>
            {(["all", "zen", "external", "catalog"] as const).map(
              (ownership) => (
                <SheetOption
                  key={ownership}
                  icon="layers-outline"
                  label={
                    ownership === "all"
                      ? "Any source"
                      : ownership === "zen"
                        ? "Zen-owned"
                        : ownership[0]!.toUpperCase() + ownership.slice(1)
                  }
                  selected={facets.ownership === ownership}
                  onPress={() => onChangeFacets({ ...facets, ownership })}
                />
              ),
            )}
            <SheetSectionHeading>Ranking</SheetSectionHeading>
            {(["all-time", "trending", "hot"] as SkillsLeaderboardView[]).map(
              (view) => (
                <SheetOption
                  key={view}
                  icon={
                    view === "hot" ? "flame-outline" : "stats-chart-outline"
                  }
                  label={skillsLeaderboardLabel(view)}
                  selected={view === leaderboardView}
                  onPress={() => onSelectLeaderboard(view)}
                />
              ),
            )}
            <SheetSectionHeading>Discovery</SheetSectionHeading>
            <SheetOption
              icon="scan-outline"
              label="Scan existing Skills"
              detail="Track external installations across all six Agents; no files are changed"
              busy={preparingMutation === "migrate"}
              onPress={onDiscover}
            />
          </>
        ) : null}

        {sheet?.kind === "plugin-details" ? (
          <PluginDetailSheet
            plugin={sheet.plugin}
            onUpdate={() => onUpdatePlugin(sheet.plugin)}
            onUninstall={() => onUninstallPlugin(sheet.plugin)}
          />
        ) : null}
        {sheet?.kind === "plugin-available" ? (
          <AvailablePluginDetailSheet
            plugin={sheet.plugin}
            onInstall={() => onInstallPlugin(sheet.plugin)}
          />
        ) : null}
      </ScrollView>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Close"
        onPress={onClose}
        style={({ pressed }) => [
          styles.sheetClose,
          { borderTopColor: useAppColors().borderSubtle },
          pressed ? { backgroundColor: useAppColors().surfacePressed } : null,
        ]}
      >
        <Text
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[
            styles.sheetCloseText,
            { color: useAppColors().textSecondary },
          ]}
        >
          Close
        </Text>
      </Pressable>
    </BottomSheetFrame>
  );
}

function PluginDetailSheet({
  plugin,
  onUpdate,
  onUninstall,
}: {
  plugin: InstalledPluginRow;
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
      <SheetSection title={ownership.summary}>
        <SheetBody>{ownership.detail}</SheetBody>
      </SheetSection>
      {plugin.skills.length > 0 ? (
        <SheetSkillList skills={plugin.skills.map((skill) => skill.name)} />
      ) : null}
      {ownership.manageable ? (
        <View style={styles.sheetActions}>
          <SheetAction
            icon="arrow-up-circle-outline"
            label="Update"
            onPress={onUpdate}
          />
          <SheetAction
            icon="trash-outline"
            label="Uninstall"
            destructive
            onPress={onUninstall}
          />
        </View>
      ) : null}
    </>
  );
}

function AvailablePluginDetailSheet({
  plugin,
  onInstall,
}: {
  plugin: AvailablePlugin;
  onInstall(): void;
}) {
  const ownership = availablePluginOwnership(plugin);
  return (
    <>
      <SheetTitle>{plugin.name}</SheetTitle>
      <SheetMetadata>
        {`@${plugin.marketplaceName}`}
        {plugin.sourceRef ? ` · ${plugin.sourceRef}` : ""}
      </SheetMetadata>
      <SheetSection title={ownership.summary}>
        <SheetBody>{ownership.detail}</SheetBody>
      </SheetSection>
      {plugin.installable ? (
        <View style={styles.sheetActions}>
          <SheetAction
            icon="download-outline"
            label="Install"
            onPress={onInstall}
          />
        </View>
      ) : null}
    </>
  );
}

/**
 * The Skill inspector body. Rendered inside a bottom sheet on mobile and as a
 * persistent right-side panel on desktop; content is identical either way.
 */
function SkillInspectorBody({
  detail,
  inspectState,
  retryPath,
  installed,
  selectedAgent,
  capabilities,
  hasProjectCwd,
  preparingMutation,
  onBinding,
  onUninstall,
  onForget,
  onAdopt,
  onUpdate,
  onInspectFile,
  onRetryInspect,
}: {
  detail: PackageDetail;
  inspectState: SkillsRequestState<PackageDetail>;
  retryPath?: string;
  installed: InstalledSkill | null;
  selectedAgent: ManagedSkillAgent;
  capabilities: readonly SkillMutationOperation[];
  hasProjectCwd: boolean;
  preparingMutation: string;
  onBinding(
    operation: "bind" | "unbind" | "enable" | "disable",
    agent: ManagedSkillAgent,
    scope: "project" | "global",
  ): void;
  onUninstall(): void;
  onForget(): void;
  onAdopt(): void;
  onUpdate(): void;
  onInspectFile(path: string): void;
  onRetryInspect(): void;
}) {
  const colors = useAppColors();
  const operationSupported = (operation: SkillMutationOperation) =>
    capabilities.includes(operation) &&
    (!installed ||
      (installed.capability.canManage &&
        installed.capability.operations.includes(operation)));
  const boundTargets = new Set(
    detail.bindings.map((binding) => `${binding.agent}:${binding.scope}`),
  );
  const showBody = Boolean(detail.skillMd?.trim());
  return (
    <>
      <SheetTitle>{detail.skillName}</SheetTitle>
      {inspectState.status === "error" ? (
        <SheetSection
          title={retryPath ? "File unavailable" : "Skill unavailable"}
        >
          <SheetBody>{inspectState.error}</SheetBody>
          <View style={styles.sheetActions}>
            <SheetAction
              icon="refresh-outline"
              label="Retry"
              onPress={onRetryInspect}
            />
          </View>
        </SheetSection>
      ) : null}
      {inspectState.status === "loading" ? (
        <SheetSection title={retryPath ? "Loading file" : "Refreshing Skill"}>
          <View style={styles.inspectLoadingRow}>
            <ActivityIndicator size="small" color={colors.accent} />
            <SheetBody>
              {retryPath
                ? `Loading ${retryPath}…`
                : `Loading ${detail.skillName}…`}
            </SheetBody>
          </View>
        </SheetSection>
      ) : null}
      <SheetMetadata>
        {[
          detail.source,
          detail.ref ? `ref ${detail.ref}` : undefined,
          detail.contentHash ? shortHash(detail.contentHash) : undefined,
          scopeLabel(detail.scope),
        ]
          .filter(Boolean)
          .join(" · ")}
      </SheetMetadata>
      <BadgeRow
        badges={[
          detail.owned
            ? {
                label: detail.enabled
                  ? "Zen-owned · Enabled"
                  : "Zen-owned · Disabled",
                tone: detail.enabled ? "accent" : "warning",
              }
            : {
                label: detail.tracked ? "Tracked external" : "External",
                tone: "warning",
              },
          { label: scopeLabel(detail.scope), tone: "neutral" },
        ]}
      />
      {detail.description ? <SheetBody>{detail.description}</SheetBody> : null}

      {(detail.warnings ?? []).length > 0 ? (
        <SheetSection title="Warnings">
          {detail.warnings!.map((warning) => (
            <Text
              key={warning}
              maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
              style={[styles.warningLine, { color: colors.warning }]}
            >
              ⚠ {warning}
            </Text>
          ))}
        </SheetSection>
      ) : null}

      {(detail.risk ?? []).length > 0 ? (
        <SheetSection title="Static risk signals">
          {detail.risk!.map((signal, index) => (
            <Text
              key={`${signal.type}:${index}`}
              maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
              style={[
                styles.warningLine,
                {
                  color:
                    signal.severity === "alert"
                      ? colors.dangerText
                      : signal.severity === "warn"
                        ? colors.warning
                        : colors.textSecondary,
                },
              ]}
            >
              {signal.severity === "alert" ? "▲" : "•"}{" "}
              {signal.detail || signal.type}
              {signal.file ? ` (${signal.file})` : ""}
            </Text>
          ))}
          <Text
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.inspectNote, { color: colors.textTertiary }]}
          >
            Static signals only; they never prove safety.
          </Text>
        </SheetSection>
      ) : null}

      <SheetSection title="Provenance">
        {[
          ["Source", detail.source || "—"],
          ["Source type", detail.sourceType || "—"],
          ["Ref", detail.ref || "—"],
          ["Content hash", detail.contentHash || "—"],
          ["Installed", detail.installedAt || "—"],
          ["Updated", detail.updatedAt || "—"],
          ["Manager", detail.manager],
        ].map(([label, value]) => (
          <Text
            key={label}
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.provenanceLine, { color: colors.textSecondary }]}
          >
            <Text style={{ color: colors.textTertiary }}>{label}: </Text>
            {value}
          </Text>
        ))}
      </SheetSection>

      <SheetSection title="Files">
        {detail.files && detail.files.length > 0 ? (
          (detail.files.length > 64
            ? detail.files.slice(0, 64)
            : detail.files
          ).map((file) => (
            <Pressable
              key={file.path}
              accessibilityRole="button"
              accessibilityLabel={`View ${file.path}`}
              onPress={() => onInspectFile(file.path)}
            >
              <Text
                numberOfLines={1}
                maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
                style={[styles.fileLine, { color: colors.accent }]}
              >
                {file.path} · {file.size} B
              </Text>
            </Pressable>
          ))
        ) : (
          <Text
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.inspectNote, { color: colors.textTertiary }]}
          >
            No files listed.
          </Text>
        )}
        {detail.files && detail.files.length > 64 ? (
          <Text
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.inspectNote, { color: colors.textTertiary }]}
          >
            …{detail.files.length - 64} more files
          </Text>
        ) : null}
      </SheetSection>

      {detail.filePath ? (
        <SheetSection title={detail.filePath}>
          <Text
            selectable
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.fileContent, { color: colors.textSecondary }]}
          >
            {detail.fileContent || "This file is empty."}
          </Text>
        </SheetSection>
      ) : null}

      <SheetSection title="Agent bindings">
        {detail.bindings.length === 0 ? (
          <Text
            maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
            style={[styles.inspectNote, { color: colors.textTertiary }]}
          >
            No bindings yet.
          </Text>
        ) : null}
        {detail.bindings.map((binding) => (
          <View
            key={`${binding.agent}:${binding.scope}`}
            style={styles.bindingRow}
          >
            <View style={styles.bindingCopy}>
              <Text
                maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
                style={[styles.bindingName, { color: colors.textPrimary }]}
              >
                {skillAgentLabel(binding.agent)} · {scopeLabel(binding.scope)}
              </Text>
              <Text
                numberOfLines={1}
                maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
                style={[styles.bindingDetail, { color: colors.textTertiary }]}
              >
                {binding.mode === "symlink" ? "symlink" : "copy"}
                {binding.enabled ? "" : " · disabled"}
                {binding.driftHash ? " · drifted" : ""}
              </Text>
            </View>
            {skillBindingSupports(binding, "enable", capabilities) ? (
              <SmallAction
                label="Enable"
                accessibilityLabel={`Enable ${detail.skillName} for ${skillAgentLabel(binding.agent)}`}
                busy={
                  preparingMutation ===
                  `enable:${installed?.id ?? detail.skillName}:${binding.agent}:${binding.scope}`
                }
                onPress={() =>
                  onBinding(
                    "enable",
                    binding.agent,
                    binding.scope as "project" | "global",
                  )
                }
              />
            ) : null}
            {skillBindingSupports(binding, "disable", capabilities) ? (
              <SmallAction
                label="Disable"
                accessibilityLabel={`Disable ${detail.skillName} for ${skillAgentLabel(binding.agent)}`}
                busy={
                  preparingMutation ===
                  `disable:${installed?.id ?? detail.skillName}:${binding.agent}:${binding.scope}`
                }
                onPress={() =>
                  onBinding(
                    "disable",
                    binding.agent,
                    binding.scope as "project" | "global",
                  )
                }
              />
            ) : null}
            {skillBindingSupports(binding, "unbind", capabilities) ? (
              <SmallAction
                label="Unbind"
                destructive
                accessibilityLabel={`Unbind ${detail.skillName} from ${skillAgentLabel(binding.agent)}`}
                busy={
                  preparingMutation ===
                  `unbind:${installed?.id ?? detail.skillName}:${binding.agent}:${binding.scope}`
                }
                onPress={() =>
                  onBinding(
                    "unbind",
                    binding.agent,
                    binding.scope as "project" | "global",
                  )
                }
              />
            ) : null}
          </View>
        ))}
        {operationSupported("bind") && detail.owned ? (
          <View style={styles.bindGrid}>
            {compactSkillTargets(agentCountsFromDetail(detail)).flatMap(
              (target) =>
                (["global", "project"] as const).map((scope) => {
                  if (
                    boundTargets.has(`${target.agent}:${scope}`) ||
                    (scope === "project" && !hasProjectCwd)
                  ) {
                    return null;
                  }
                  const scopeLabelText =
                    scope === "global" ? "globally" : "to project";
                  return (
                    <SmallAction
                      key={`${target.agent}:${scope}`}
                      label={`Bind ${target.label} ${scopeLabelText}`}
                      accessibilityLabel={`Bind ${detail.skillName} to ${target.label} ${scopeLabelText}`}
                      busy={
                        preparingMutation ===
                        `bind:${installed?.id ?? detail.skillName}:${target.agent}:${scope}`
                      }
                      onPress={() => onBinding("bind", target.agent, scope)}
                    />
                  );
                }),
            )}
          </View>
        ) : null}
      </SheetSection>

      {showBody ? (
        <SheetSection title="SKILL.md">
          <EnrichedMarkdownText
            markdown={detail.skillMd!}
            selectable
            containerStyle={styles.markdownBody}
            markdownStyle={markdownStyle(
              colors.textPrimary,
              colors.textSecondary,
              colors.accent,
            )}
            onLinkPress={(event) => {
              void openSafeMarkdownUrl(event.url, (safeUrl) =>
                Linking.openURL(safeUrl),
              );
            }}
          />
        </SheetSection>
      ) : null}

      <View style={styles.sheetActions}>
        {operationSupported("update") && detail.owned ? (
          <SheetAction
            icon="arrow-up-circle-outline"
            label="Update"
            busy={
              preparingMutation ===
              `update:${installed?.id ?? detail.skillName}`
            }
            onPress={onUpdate}
          />
        ) : null}
        {operationSupported("adopt") && !detail.owned ? (
          <SheetAction
            icon="arrow-forward-circle-outline"
            label="Adopt"
            busy={
              preparingMutation === `adopt:${installed?.id ?? detail.skillName}`
            }
            onPress={onAdopt}
          />
        ) : null}
        {operationSupported("forget") && !detail.owned && detail.tracked ? (
          <SheetAction
            icon="close-circle-outline"
            label="Forget"
            busy={
              preparingMutation ===
              `forget:${installed?.id ?? detail.skillName}`
            }
            onPress={onForget}
          />
        ) : null}
        {operationSupported("uninstall") && detail.owned ? (
          <SheetAction
            icon="trash-outline"
            label="Uninstall"
            destructive
            busy={
              preparingMutation ===
              `uninstall:${installed?.id ?? detail.skillName}`
            }
            onPress={onUninstall}
          />
        ) : null}
      </View>
      <Text
        maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
        style={[styles.inspectNote, { color: colors.textTertiary }]}
      >
        Mutations run on the daemon and refresh the authoritative inventory
        afterward. Destructive actions always show an exact confirmation.
      </Text>
    </>
  );
}

function agentCountsFromDetail(detail: PackageDetail): SkillsAgentCounts {
  const counts: SkillsAgentCounts = {
    codex: 0,
    "claude-code": 0,
    cursor: 0,
    grok: 0,
    opencode: 0,
    pi: 0,
  };
  for (const binding of detail.bindings) {
    counts[binding.agent] += 1;
  }
  return counts;
}

function markdownStyle(
  text: string,
  secondary: string,
  accent: string,
): Parameters<typeof EnrichedMarkdownText>[0]["markdownStyle"] {
  return {
    paragraph: { color: text, fontSize: 14, lineHeight: 20 },
    h1: { color: text },
    h2: { color: text },
    h3: { color: text },
    link: { color: accent },
    codeBlock: { color: secondary },
    strong: { color: text },
    list: { color: text },
    blockquote: { color: secondary },
  };
}

/** Mobile presentation: bottom sheet. */
function SkillInspectorSheet(props: {
  detail: PackageDetail;
  inspectState: SkillsRequestState<PackageDetail>;
  installed: InstalledSkill | null;
  selectedAgent: ManagedSkillAgent;
  capabilities: readonly SkillMutationOperation[];
  hasProjectCwd: boolean;
  preparingMutation: string;
  retryPath?: string;
  onClose(): void;
  onBinding(
    operation: "bind" | "unbind" | "enable" | "disable",
    agent: ManagedSkillAgent,
    scope: "project" | "global",
  ): void;
  onUninstall(): void;
  onForget(): void;
  onAdopt(): void;
  onUpdate(): void;
  onInspectFile(path: string): void;
  onRetryInspect(): void;
}) {
  const colors = useAppColors();
  return (
    <BottomSheetFrame
      visible
      maxHeight="88%"
      dragToDismiss
      onClose={props.onClose}
    >
      <ScrollView
        contentContainerStyle={styles.sheetContent}
        showsVerticalScrollIndicator={false}
      >
        <SkillInspectorBody {...props} />
      </ScrollView>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Close"
        onPress={props.onClose}
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

/** Desktop presentation: persistent right-side panel. */
function SkillInspector(props: {
  detail: PackageDetail;
  inspectState: SkillsRequestState<PackageDetail>;
  installed: InstalledSkill | null;
  selectedAgent: ManagedSkillAgent;
  capabilities: readonly SkillMutationOperation[];
  hasProjectCwd: boolean;
  preparingMutation: string;
  retryPath?: string;
  onClose(): void;
  onBinding(
    operation: "bind" | "unbind" | "enable" | "disable",
    agent: ManagedSkillAgent,
    scope: "project" | "global",
  ): void;
  onUninstall(): void;
  onForget(): void;
  onAdopt(): void;
  onUpdate(): void;
  onInspectFile(path: string): void;
  onRetryInspect(): void;
}) {
  const colors = useAppColors();
  return (
    <View
      style={[styles.inspectorPanel, { borderLeftColor: colors.borderSubtle }]}
    >
      <View style={styles.inspectorHeader}>
        <Text
          numberOfLines={1}
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.inspectorHeaderTitle, { color: colors.textPrimary }]}
        >
          Inspector
        </Text>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Close inspector"
          onPress={props.onClose}
          style={({ pressed }) => [
            styles.iconButton,
            pressed ? { backgroundColor: colors.surfacePressed } : null,
          ]}
        >
          <Ionicons
            accessible={false}
            name="close"
            size={20}
            color={colors.textSecondary}
          />
        </Pressable>
      </View>
      <ScrollView
        contentContainerStyle={styles.inspectorContent}
        showsVerticalScrollIndicator={false}
      >
        <SkillInspectorBody {...props} />
      </ScrollView>
    </View>
  );
}

function SkillInspectRequestBody({
  name,
  inspectState,
  onRetry,
}: {
  name: string;
  inspectState: SkillsRequestState<PackageDetail>;
  onRetry(): void;
}) {
  const colors = useAppColors();
  const loading =
    inspectState.status === "loading" || inspectState.status === "idle";
  return (
    <>
      <SheetTitle>{name}</SheetTitle>
      {loading ? (
        <View style={styles.inspectLoadingRow}>
          <ActivityIndicator size="small" color={colors.accent} />
          <SheetBody>Loading Skill…</SheetBody>
        </View>
      ) : null}
      {inspectState.status === "error" ? (
        <SheetSection title="Skill unavailable">
          <SheetBody>{inspectState.error}</SheetBody>
          <View style={styles.sheetActions}>
            <SheetAction
              icon="refresh-outline"
              label="Retry"
              onPress={onRetry}
            />
          </View>
        </SheetSection>
      ) : null}
    </>
  );
}

function SkillInspectorStatus({
  name,
  inspectState,
  onClose,
  onRetry,
}: {
  name: string;
  inspectState: SkillsRequestState<PackageDetail>;
  onClose(): void;
  onRetry(): void;
}) {
  const colors = useAppColors();
  return (
    <View
      style={[styles.inspectorPanel, { borderLeftColor: colors.borderSubtle }]}
    >
      <View style={styles.inspectorHeader}>
        <Text
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[styles.inspectorHeaderTitle, { color: colors.textPrimary }]}
        >
          Inspector
        </Text>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Close inspector"
          onPress={onClose}
          style={styles.iconButton}
        >
          <Ionicons name="close" size={20} color={colors.textSecondary} />
        </Pressable>
      </View>
      <View style={styles.inspectorContent}>
        <SkillInspectRequestBody
          name={name}
          inspectState={inspectState}
          onRetry={onRetry}
        />
      </View>
    </View>
  );
}

function SkillInspectorStatusSheet({
  name,
  inspectState,
  onClose,
  onRetry,
}: {
  name: string;
  inspectState: SkillsRequestState<PackageDetail>;
  onClose(): void;
  onRetry(): void;
}) {
  const colors = useAppColors();
  return (
    <BottomSheetFrame visible maxHeight="88%" dragToDismiss onClose={onClose}>
      <View style={styles.sheetContent}>
        <SkillInspectRequestBody
          name={name}
          inspectState={inspectState}
          onRetry={onRetry}
        />
      </View>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Close"
        onPress={onClose}
        style={[styles.sheetClose, { borderTopColor: colors.borderSubtle }]}
      >
        <Text style={[styles.sheetCloseText, { color: colors.textSecondary }]}>
          Close
        </Text>
      </Pressable>
    </BottomSheetFrame>
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

function SheetSectionHeading({ children }: { children: React.ReactNode }) {
  const colors = useAppColors();
  return (
    <Text
      maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
      style={[styles.sheetSectionHeading, { color: colors.textTertiary }]}
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
    <View
      style={[styles.sheetSection, { borderTopColor: colors.borderSubtle }]}
    >
      <SheetSectionHeading>{title}</SheetSectionHeading>
      {children}
    </View>
  );
}

function SheetSkillList({ skills }: { skills: string[] }) {
  const colors = useAppColors();
  return (
    <View style={styles.sheetSkillList}>
      {skills.map((skill) => (
        <View
          key={skill}
          style={[
            styles.sheetSkillRow,
            { borderTopColor: colors.borderSubtle },
          ]}
        >
          <Ionicons
            accessible={false}
            name="library-outline"
            size={16}
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
  agent,
  icon,
  label,
  detail,
  selected,
  disabled,
  busy,
  onPress,
}: {
  agent?: ManagedSkillAgent;
  icon?: React.ComponentProps<typeof Ionicons>["name"];
  label: string;
  detail?: string;
  selected?: boolean;
  disabled?: boolean;
  busy?: boolean;
  onPress(): void;
}) {
  const colors = useAppColors();
  return (
    <AnimatedPressable
      accessibilityRole="button"
      accessibilityLabel={[label, detail].filter(Boolean).join(", ")}
      accessibilityState={{ selected: selected, disabled: disabled || busy }}
      disabled={disabled || busy}
      onPress={onPress}
      style={[
        styles.sheetOption,
        selected ? { backgroundColor: colors.surfaceSubtle } : null,
        disabled ? styles.disabled : null,
      ]}
    >
      {agent ? (
        <View
          accessible={false}
          accessibilityElementsHidden
          importantForAccessibility="no-hide-descendants"
          style={styles.sheetOptionIcon}
        >
          <AgentKindIcon
            kind={managedAgentKind(agent)}
            size={20}
            variant="compact"
          />
        </View>
      ) : (
        <View
          accessible={false}
          accessibilityElementsHidden
          importantForAccessibility="no-hide-descendants"
          style={styles.sheetOptionIcon}
        >
          <Ionicons
            accessible={false}
            name={icon ?? "ellipse-outline"}
            size={18}
            color={selected ? colors.accent : colors.textTertiary}
          />
        </View>
      )}
      <View style={styles.sheetOptionCopy}>
        <Text
          maxFontSizeMultiplier={PLUGINS_SKILLS_MAX_FONT_SIZE_MULTIPLIER}
          style={[
            styles.sheetOptionLabel,
            { color: selected ? colors.accent : colors.textPrimary },
          ]}
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
      {busy ? <ActivityIndicator size="small" color={colors.accent} /> : null}
    </AnimatedPressable>
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
        <Ionicons
          accessible={false}
          name={icon}
          size={18}
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
  return (
    <View
      style={[styles.separator, { backgroundColor: colors.borderSubtle }]}
    />
  );
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
  return row.kind === "installed"
    ? `installed:${row.skill.id}`
    : `catalog:${row.catalogId}`;
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
    case "grok":
      return "grok";
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

function shortHash(value: string): string {
  return value.length > 12 ? `${value.slice(0, 12)}…` : value;
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  frame: {
    width: "100%",
    maxWidth: 720,
    alignSelf: "center",
    flex: 1,
  },
  desktopRow: { flex: 1, flexDirection: "row" },
  desktopList: { flex: 1, minWidth: 0 },
  desktopPanel: { width: INSPECTOR_PANEL_WIDTH, flexShrink: 0 },
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
    borderTopWidth: StyleSheet.hairlineWidth,
  },
  sheetSectionHeading: {
    paddingHorizontal: 4,
    paddingTop: 12,
    paddingBottom: 2,
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
  inspectorPanel: {
    flex: 1,
    borderLeftWidth: StyleSheet.hairlineWidth,
    backgroundColor: "transparent",
  },
  inspectorHeader: {
    minHeight: PLUGINS_SKILLS_TOUCH_TARGET,
    paddingHorizontal: 12,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  inspectorHeaderTitle: {
    ...TypeScale.label,
    fontFamily: Typography.uiFontMedium,
    flex: 1,
  },
  inspectorContent: { padding: 12, gap: 4, paddingBottom: 40 },
  inspectLoadingRow: {
    minHeight: PLUGINS_SKILLS_TOUCH_TARGET,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
  },
  warningLine: {
    ...TypeScale.compact,
    paddingVertical: 1,
  },
  inspectNote: {
    ...TypeScale.caption,
    paddingTop: 4,
  },
  provenanceLine: {
    ...TypeScale.compact,
    paddingVertical: 1,
  },
  fileLine: {
    ...TypeScale.caption,
    minHeight: PLUGINS_SKILLS_TOUCH_TARGET,
    paddingVertical: 12,
  },
  fileContent: {
    ...TypeScale.caption,
    fontFamily: Typography.chatMonoFont,
    lineHeight: 20,
  },
  bindingRow: {
    minHeight: PLUGINS_SKILLS_TOUCH_TARGET,
    paddingVertical: 6,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    flexWrap: "wrap",
  },
  bindingCopy: { flex: 1, minWidth: 120, gap: 1 },
  bindingName: {
    ...TypeScale.compact,
    fontFamily: Typography.uiFontMedium,
  },
  bindingDetail: { ...TypeScale.caption },
  bindGrid: {
    paddingTop: 8,
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 8,
  },
  markdownBody: { paddingTop: 4 },
});
