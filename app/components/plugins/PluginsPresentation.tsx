import React, { useEffect, useMemo, useState } from "react";
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { TypeScale, Typography, useAppColors } from "../../constants/tokens";
import type {
  InstalledPluginCopy,
  PluginInventory,
} from "../../services/pluginsManagement";
import type {
  InstalledSkill,
  PackageDetail,
  ManagedSkillAgent,
  SkillsRequestState,
} from "../../services/skillsManagement";
import { skillsRequestData } from "../../services/skillsManagement";
import { PLUGINS_SKILLS_SCREEN_PADDING } from "../../services/pluginsSkillsSurfaceModel";
import {
  filterLogicalPlugins,
  pluginCopyLabel,
  pluginReadonlyReason,
  type LogicalPlugin,
  type PluginCapabilityFilter,
  type PluginFilters,
} from "../../services/pluginsScreenModel";
import {
  pluginSkillEntries,
} from "../../services/pluginSkillsDirectory";
import { MANAGED_SKILL_AGENTS } from "../../services/skillsScreenModel";
import { AgentLogoSet } from "../agents/AgentLogoSet";
import { ExtensionListRow } from "../extensions/ExtensionListRow";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import { PluginSkillsDirectory } from "./PluginSkillsDirectory";

interface PluginsPresentationProps {
  state: SkillsRequestState<PluginInventory>;
  plugins: LogicalPlugin[];
  /** Raw Skills inventory copies used to resolve Plugin-provided Skills. */
  skills: InstalledSkill[];
  preparingMutation: string;
  currentServerAvailable: boolean;
  wide: boolean;
  /** Requested inspector focus coming from the Skill inspector's jump action. */
  focusedPluginKey: string | null;
  onFocusPluginConsumed(): void;
  onOpenSettings(): void;
  onRefresh(): void;
  onUninstall(copy: InstalledPluginCopy): void;
  /** Fetches read-only detail for one exact Skill copy. */
  onInspectSkillCopy(
    copy: InstalledSkill,
    path?: string,
  ): Promise<PackageDetail>;
}

const DEFAULT_FILTERS: PluginFilters = { agents: [], capability: "all" };

export function PluginsPresentation(props: PluginsPresentationProps) {
  const colors = useAppColors();
  const inventory = skillsRequestData(props.state);
  const [query, setQuery] = useState("");
  const [filters, setFilters] = useState<PluginFilters>(DEFAULT_FILTERS);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const [selectedCopyId, setSelectedCopyId] = useState<string | null>(null);
  const [deletePickerKey, setDeletePickerKey] = useState<string | null>(null);
  const visible = useMemo(
    () => filterLogicalPlugins(props.plugins, query, filters),
    [filters, props.plugins, query],
  );
  const selected = props.plugins.find((plugin) => plugin.key === selectedKey);
  const deletePicker = props.plugins.find(
    (plugin) => plugin.key === deletePickerKey,
  );
  const activeFilterCount =
    filters.agents.length + Number(filters.capability !== "all");

  useEffect(() => {
    if (selectedKey && !selected) {
      setSelectedKey(null);
      setSelectedCopyId(null);
    }
    if (deletePickerKey && !deletePicker) setDeletePickerKey(null);
  }, [
    deletePicker,
    deletePickerKey,
    inventory?.generatedAt,
    selected,
    selectedKey,
  ]);

  const openPlugin = (plugin: LogicalPlugin) => {
    setSelectedKey(plugin.key);
    setSelectedCopyId(plugin.copies[0]?.copyId ?? null);
  };
  const closeInspector = () => {
    setSelectedKey(null);
    setSelectedCopyId(null);
  };
  // One-shot handoff from the Skill inspector: open the owning Plugin, then
  // consume the request so a later server switch cannot reopen it.
  useEffect(() => {
    if (!props.focusedPluginKey) return;
    const focused = props.plugins.find(
      (plugin) => plugin.key === props.focusedPluginKey,
    );
    props.onFocusPluginConsumed();
    if (focused) openPlugin(focused);
  }, [props.focusedPluginKey, props.plugins, props.onFocusPluginConsumed]);
  const chooseUninstall = (plugin: LogicalPlugin) => {
    const uninstallable = plugin.copies.filter(
      (copy) => copy.capability.canUninstall,
    );
    // Protected Plugin (for example Codex-managed copies): no uninstall is
    // attempted; the inspector explains why instead of faking success.
    if (uninstallable.length === 0) {
      openPlugin(plugin);
      return;
    }
    if (uninstallable.length > 1) {
      setDeletePickerKey(plugin.key);
      return;
    }
    props.onUninstall(uninstallable[0]!);
  };

  const list = (
    <View style={styles.flex}>
      <View style={styles.searchArea}>
        <View
          style={[
            styles.search,
            {
              borderColor: colors.borderSubtle,
              backgroundColor: colors.surfaceSubtle,
            },
          ]}
        >
          <Ionicons
            name="search-outline"
            size={19}
            color={colors.textTertiary}
          />
          <TextInput
            accessibilityLabel="Search installed Plugins"
            value={query}
            onChangeText={setQuery}
            placeholder="Search installed Plugins"
            placeholderTextColor={colors.textTertiary}
            style={[styles.input, { color: colors.textPrimary }]}
          />
          {query ? (
            <Pressable
              accessibilityLabel="Clear Plugin search"
              onPress={() => setQuery("")}
              style={styles.smallIcon}
            >
              <Ionicons
                name="close-circle"
                size={19}
                color={colors.textTertiary}
              />
            </Pressable>
          ) : null}
        </View>
        <Pressable
          accessibilityLabel="Filter Plugins"
          onPress={() => setFiltersOpen(true)}
          style={[
            styles.filterButton,
            {
              borderColor: activeFilterCount
                ? colors.accent
                : colors.borderSubtle,
              backgroundColor: activeFilterCount
                ? colors.accentSoft
                : colors.surfaceSubtle,
            },
          ]}
        >
          <Ionicons
            name="options-outline"
            size={20}
            color={activeFilterCount ? colors.accent : colors.textSecondary}
          />
          {activeFilterCount ? (
            <Text style={{ color: colors.accent }}>{activeFilterCount}</Text>
          ) : null}
        </Pressable>
      </View>
      {activeFilterCount ? (
        <PluginActiveFilters filters={filters} onChange={setFilters} />
      ) : null}
      <PluginsList
        {...props}
        inventory={inventory}
        rows={visible}
        onOpen={openPlugin}
        onChooseUninstall={chooseUninstall}
      />
    </View>
  );

  return (
    <View style={styles.root}>
      {list}
      {props.wide ? (
        <View style={[styles.panel, { borderLeftColor: colors.borderSubtle }]}>
          {selected ? (
            <PluginInspector
              plugin={selected}
              skills={props.skills}
              selectedCopyId={selectedCopyId}
              preparingMutation={props.preparingMutation}
              onSelectCopy={setSelectedCopyId}
              onClose={closeInspector}
              onUninstall={props.onUninstall}
              onInspectSkillCopy={props.onInspectSkillCopy}
            />
          ) : (
            <PluginState
              icon="extension-puzzle-outline"
              title="Plugin details"
              detail="Select an installed Plugin to inspect its components, availability, location, and uninstall capability."
            />
          )}
        </View>
      ) : (
        <BottomSheetFrame
          visible={Boolean(selected)}
          maxHeight="94%"
          dragToDismiss
          onClose={closeInspector}
          cardStyle={styles.inspectorSheetCard}
          contentStyle={styles.sheetContent}
        >
          {selected ? (
            <PluginInspector
              plugin={selected}
              skills={props.skills}
              selectedCopyId={selectedCopyId}
              preparingMutation={props.preparingMutation}
              onSelectCopy={setSelectedCopyId}
              onClose={closeInspector}
              onUninstall={props.onUninstall}
              onInspectSkillCopy={props.onInspectSkillCopy}
            />
          ) : null}
        </BottomSheetFrame>
      )}
      <PluginFilterSheet
        visible={filtersOpen}
        filters={filters}
        onChange={setFilters}
        onClose={() => setFiltersOpen(false)}
      />
      <PluginCopySheet
        plugin={deletePicker ?? null}
        preparingMutation={props.preparingMutation}
        onClose={() => setDeletePickerKey(null)}
        onUninstall={(copy) => {
          setDeletePickerKey(null);
          props.onUninstall(copy);
        }}
      />
    </View>
  );
}

function PluginsList(
  props: PluginsPresentationProps & {
    inventory?: PluginInventory;
    rows: LogicalPlugin[];
    onOpen(plugin: LogicalPlugin): void;
    onChooseUninstall(plugin: LogicalPlugin): void;
  },
) {
  const colors = useAppColors();
  if (!props.currentServerAvailable) {
    return (
      <PluginState
        icon="server-outline"
        title="No current server"
        detail="Choose a server in Settings to view its installed Plugins."
        action="Open Settings"
        onAction={props.onOpenSettings}
      />
    );
  }
  if (props.state.status === "error" && !props.inventory) {
    return (
      <PluginState
        icon="warning-outline"
        title="Plugins unavailable"
        detail={props.state.error}
      />
    );
  }
  if (
    (props.state.status === "idle" || props.state.status === "loading") &&
    !props.inventory
  ) {
    return (
      <PluginState
        loading
        title="Loading installed Plugins"
        detail="Reading local Plugin managers and safe cache locations."
      />
    );
  }
  if ((props.inventory?.installed.length ?? 0) === 0) {
    return (
      <PluginState
        icon="extension-puzzle-outline"
        title="No installed Plugins"
        detail="No local Plugins were found for supported Agents on this server."
      />
    );
  }
  return (
    <FlatList
      data={props.rows}
      keyExtractor={(plugin) => plugin.key}
      refreshControl={
        <RefreshControl
          refreshing={props.state.status === "loading"}
          onRefresh={props.onRefresh}
          tintColor={colors.accent}
        />
      }
      contentContainerStyle={props.rows.length ? styles.list : styles.emptyList}
      ListEmptyComponent={
        <PluginState
          icon="search-outline"
          title="No matches"
          detail="Adjust your Plugin search or filters."
        />
      }
      renderItem={({ item }) => (
        <PluginRow
          plugin={item}
          preparingMutation={props.preparingMutation}
          onOpen={() => props.onOpen(item)}
          onUninstall={() => props.onChooseUninstall(item)}
        />
      )}
    />
  );
}

function PluginRow({
  plugin,
  preparingMutation,
  onOpen,
  onUninstall,
}: {
  plugin: LogicalPlugin;
  preparingMutation: string;
  onOpen(): void;
  onUninstall(): void;
}) {
  const canUninstall = plugin.copies.some(
    (copy) => copy.capability.canUninstall,
  );
  const uninstalling = plugin.copies.some(
    (copy) => preparingMutation === `uninstall:${copy.copyId}`,
  );
  const readonlyReason = plugin.copies.find(
    (copy) => !copy.capability.canUninstall && copy.capability.reason,
  )?.capability.reason;
  return (
    <ExtensionListRow
      name={plugin.displayName}
      summary={plugin.description || readonlyReason || "Local Plugin"}
      agents={plugin.agents}
      openAccessibilityLabel={`Open ${plugin.displayName} Plugin details`}
      onOpen={onOpen}
      action={{
        accessibilityLabel: canUninstall
          ? `Uninstall ${plugin.displayName}`
          : `Open why ${plugin.displayName} is protected`,
        icon: canUninstall ? "trash-outline" : "lock-closed-outline",
        destructive: canUninstall,
        busy: uninstalling,
        disabled: canUninstall && Boolean(preparingMutation),
        onPress: canUninstall ? onUninstall : onOpen,
      }}
    />
  );
}

function PluginInspector({
  plugin,
  skills,
  selectedCopyId,
  preparingMutation,
  onSelectCopy,
  onClose,
  onUninstall,
  onInspectSkillCopy,
}: {
  plugin: LogicalPlugin;
  skills: InstalledSkill[];
  selectedCopyId: string | null;
  preparingMutation: string;
  onSelectCopy(copyId: string): void;
  onClose(): void;
  onUninstall(copy: InstalledPluginCopy): void;
  onInspectSkillCopy(
    copy: InstalledSkill,
    path?: string,
  ): Promise<PackageDetail>;
}) {
  const colors = useAppColors();
  const copy =
    plugin.copies.find((candidate) => candidate.copyId === selectedCopyId) ??
    plugin.copies[0]!;
  const uninstalling = preparingMutation === `uninstall:${copy.copyId}`;
  const skillEntries = useMemo(
    () => pluginSkillEntries(plugin, skills),
    [plugin, skills],
  );
  const otherComponents = copy.components.filter(
    (component) => component.kind !== "skill",
  );
  return (
    <View style={styles.inspector}>
      <View
        style={[
          styles.inspectorHeader,
          { borderBottomColor: colors.borderSubtle },
        ]}
      >
        <View style={styles.flex}>
          <Text style={[styles.inspectorTitle, { color: colors.textPrimary }]}>
            {plugin.displayName}
          </Text>
          <Text
            numberOfLines={1}
            style={[styles.metadata, { color: colors.textTertiary }]}
          >
            {pluginCopyLabel(copy)}
          </Text>
        </View>
        <Pressable
          accessibilityLabel="Close Plugin details"
          onPress={onClose}
          style={styles.close}
        >
          <Ionicons name="close" size={22} color={colors.textSecondary} />
        </Pressable>
      </View>
      <ScrollView contentContainerStyle={styles.inspectorScroll}>
        <PluginDetailSection title="Overview">
          <Text style={{ color: colors.textSecondary }}>
            {copy.description ||
              `${plugin.displayName} is provided by ${pluginCopyLabel(copy)}.`}
          </Text>
        </PluginDetailSection>
        {skillEntries.length ? (
          <PluginDetailSection title={`Skills (${skillEntries.length})`}>
            <Text style={[styles.metadata, { color: colors.textTertiary }]}>
              Provided and managed by this Plugin. Expand a Skill to read its
              files.
            </Text>
            <PluginSkillsDirectory
              entries={skillEntries}
              onInspectCopy={onInspectSkillCopy}
            />
          </PluginDetailSection>
        ) : null}
        {otherComponents.length ? (
          <PluginDetailSection title="Components">
            {otherComponents.map((component) => (
              <View
                key={`${component.kind}:${component.path || component.name}`}
                style={styles.componentRow}
              >
                <Ionicons
                  name={componentIcon(component.kind)}
                  size={18}
                  color={colors.textTertiary}
                />
                <View style={styles.flex}>
                  <Text style={{ color: colors.textSecondary }}>
                    {component.name}
                  </Text>
                  {component.path ? (
                    <Text
                      numberOfLines={1}
                      style={[styles.path, { color: colors.textTertiary }]}
                    >
                      {component.path}
                    </Text>
                  ) : null}
                </View>
              </View>
            ))}
          </PluginDetailSection>
        ) : null}
        <PluginDetailSection title="Available to">
          <AgentLogoSet agents={copy.agents} showLabels size={20} />
        </PluginDetailSection>
        {plugin.copies.length > 1 ? (
          <PluginDetailSection title={`Copies (${plugin.copies.length})`}>
            {plugin.copies.map((candidate) => (
              <PluginCopyCard
                key={candidate.copyId}
                copy={candidate}
                selected={candidate.copyId === copy.copyId}
                preparingMutation={preparingMutation}
                onSelect={() => onSelectCopy(candidate.copyId)}
                onUninstall={() => onUninstall(candidate)}
              />
            ))}
          </PluginDetailSection>
        ) : (
          <PluginDetailSection title="Location">
            <Text style={{ color: colors.textSecondary }}>{copy.location}</Text>
            <Text
              selectable
              style={[styles.path, { color: colors.textTertiary }]}
            >
              {copy.rootPath}
            </Text>
          </PluginDetailSection>
        )}
        <View style={styles.lifecycleSection}>
          <Text style={[styles.sectionTitle, { color: colors.textPrimary }]}>
            Uninstall
          </Text>
          {copy.capability.canUninstall ? (
            <>
              <Text style={[styles.metadata, { color: colors.textTertiary }]}>
                Permanently removes only this exact copy from {copy.location}.
              </Text>
              <Pressable
                accessibilityRole="button"
                accessibilityLabel={`Uninstall ${plugin.displayName} from ${copy.location}`}
                disabled={Boolean(preparingMutation)}
                onPress={() => onUninstall(copy)}
                style={[
                  styles.uninstallButton,
                  { borderColor: colors.dangerText },
                  Boolean(preparingMutation) && styles.dimmed,
                ]}
              >
                {uninstalling ? (
                  <ActivityIndicator size="small" color={colors.dangerText} />
                ) : (
                  <Text style={{ color: colors.dangerText }}>
                    Uninstall Plugin
                  </Text>
                )}
              </Pressable>
            </>
          ) : (
            <>
              <View style={styles.protectedRow}>
                <Ionicons
                  name="lock-closed-outline"
                  size={15}
                  color={colors.textTertiary}
                />
                <Text
                  style={[styles.metadata, { color: colors.textSecondary }]}
                >
                  Protected
                </Text>
              </View>
              <Text style={[styles.metadata, { color: colors.textTertiary }]}>
                {pluginReadonlyReason(copy)}
              </Text>
              <Text style={[styles.metadata, { color: colors.textTertiary }]}>
                Zen does not attempt to remove this copy, so an uninstall here
                can never report success for it.
              </Text>
            </>
          )}
        </View>
      </ScrollView>
    </View>
  );
}

function PluginCopyCard({
  copy,
  selected,
  preparingMutation,
  onSelect,
  onUninstall,
}: {
  copy: InstalledPluginCopy;
  selected: boolean;
  preparingMutation: string;
  onSelect?(): void;
  onUninstall(): void;
}) {
  const colors = useAppColors();
  const uninstalling = preparingMutation === `uninstall:${copy.copyId}`;
  return (
    <View
      style={[
        styles.copyCard,
        {
          borderColor: selected ? colors.accent : colors.borderSubtle,
          backgroundColor: selected ? colors.accentSoft : "transparent",
        },
      ]}
    >
      {onSelect ? (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={`Select ${pluginCopyLabel(copy)} copy`}
          onPress={onSelect}
          style={styles.copyOpen}
        >
          <PluginCopyContent copy={copy} />
        </Pressable>
      ) : (
        <View style={styles.copyOpen}>
          <PluginCopyContent copy={copy} />
        </View>
      )}
      {copy.capability.canUninstall ? (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={`Uninstall copy from ${copy.location}`}
          disabled={Boolean(preparingMutation)}
          onPress={onUninstall}
          style={({ pressed }) => [
            styles.rowDelete,
            (pressed || Boolean(preparingMutation)) && styles.dimmed,
          ]}
        >
          {uninstalling ? (
            <ActivityIndicator size="small" color={colors.dangerText} />
          ) : (
            <Ionicons
              name="trash-outline"
              size={20}
              color={colors.dangerText}
            />
          )}
        </Pressable>
      ) : null}
    </View>
  );
}

function PluginCopyContent({ copy }: { copy: InstalledPluginCopy }) {
  const colors = useAppColors();
  return (
    <View style={styles.flex}>
      <Text style={[styles.copyTitle, { color: colors.textPrimary }]}>
        {pluginCopyLabel(copy)}
      </Text>
      <AgentLogoSet agents={copy.agents} size={17} />
      <Text
        numberOfLines={1}
        style={[styles.path, { color: colors.textTertiary }]}
      >
        {copy.rootPath}
      </Text>
      {!copy.capability.canUninstall ? (
        <Text style={[styles.metadata, { color: colors.textTertiary }]}>
          {pluginReadonlyReason(copy)}
        </Text>
      ) : null}
    </View>
  );
}

function PluginCopySheet({
  plugin,
  preparingMutation,
  onClose,
  onUninstall,
}: {
  plugin: LogicalPlugin | null;
  preparingMutation: string;
  onClose(): void;
  onUninstall(copy: InstalledPluginCopy): void;
}) {
  const colors = useAppColors();
  return (
    <BottomSheetFrame
      visible={Boolean(plugin)}
      onClose={onClose}
      maxHeight="78%"
      dragToDismiss
      contentStyle={styles.copySheet}
    >
      <View style={styles.sheetHeader}>
        <View style={styles.flex}>
          <Text style={[styles.sheetTitle, { color: colors.textPrimary }]}>
            Choose copy
          </Text>
          <Text style={[styles.metadata, { color: colors.textTertiary }]}>
            {plugin?.displayName}
          </Text>
        </View>
        <Pressable
          accessibilityLabel="Close Plugin copy selector"
          onPress={onClose}
          style={styles.close}
        >
          <Ionicons name="close" size={22} color={colors.textSecondary} />
        </Pressable>
      </View>
      <ScrollView contentContainerStyle={styles.copySheetBody}>
        {(plugin?.copies ?? []).map((copy) => (
          <PluginCopyCard
            key={copy.copyId}
            copy={copy}
            selected={false}
            preparingMutation={preparingMutation}
            onUninstall={() => onUninstall(copy)}
          />
        ))}
      </ScrollView>
    </BottomSheetFrame>
  );
}

function PluginActiveFilters({
  filters,
  onChange,
}: {
  filters: PluginFilters;
  onChange(filters: PluginFilters): void;
}) {
  const colors = useAppColors();
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      contentContainerStyle={styles.chips}
    >
      {filters.agents.map((agent) => (
        <Pressable
          key={agent}
          accessibilityLabel={`Remove ${agent} Plugin filter`}
          onPress={() =>
            onChange({
              ...filters,
              agents: filters.agents.filter((item) => item !== agent),
            })
          }
          style={[styles.chip, { backgroundColor: colors.accentSoft }]}
        >
          <AgentLogoSet agents={[agent]} size={16} />
          <Ionicons name="close" size={14} color={colors.accent} />
        </Pressable>
      ))}
      {filters.capability !== "all" ? (
        <Pressable
          accessibilityLabel="Remove Plugin capability filter"
          onPress={() => onChange({ ...filters, capability: "all" })}
          style={[styles.chip, { backgroundColor: colors.accentSoft }]}
        >
          <Text style={{ color: colors.accent }}>
            {filters.capability === "uninstallable"
              ? "Uninstallable"
              : "Read-only"}
          </Text>
          <Ionicons name="close" size={14} color={colors.accent} />
        </Pressable>
      ) : null}
    </ScrollView>
  );
}

function PluginFilterSheet({
  visible,
  filters,
  onChange,
  onClose,
}: {
  visible: boolean;
  filters: PluginFilters;
  onChange(filters: PluginFilters): void;
  onClose(): void;
}) {
  const colors = useAppColors();
  return (
    <BottomSheetFrame
      visible={visible}
      onClose={onClose}
      maxHeight="68%"
      dragToDismiss
      contentStyle={styles.filterSheet}
    >
      <View style={styles.sheetHeader}>
        <Text style={[styles.sheetTitle, { color: colors.textPrimary }]}>
          Filter Plugins
        </Text>
        <Pressable
          accessibilityLabel="Close Plugin filters"
          onPress={onClose}
          style={styles.close}
        >
          <Ionicons name="close" size={22} color={colors.textSecondary} />
        </Pressable>
      </View>
      <ScrollView contentContainerStyle={styles.filterBody}>
        <View style={styles.filterSection}>
          <Text style={[styles.sectionLabel, { color: colors.textPrimary }]}>
            Available to
          </Text>
          <View style={styles.optionWrap}>
            {MANAGED_SKILL_AGENTS.map((agent) => {
              const selected = filters.agents.includes(agent);
              return (
                <Pressable
                  key={agent}
                  accessibilityRole="checkbox"
                  accessibilityState={{ checked: selected }}
                  accessibilityLabel={`Filter Plugins available to ${agent}`}
                  onPress={() =>
                    onChange({
                      ...filters,
                      agents: selected
                        ? filters.agents.filter((item) => item !== agent)
                        : [...filters.agents, agent],
                    })
                  }
                  style={[
                    styles.choice,
                    {
                      borderColor: selected
                        ? colors.accent
                        : colors.borderSubtle,
                      backgroundColor: selected
                        ? colors.accentSoft
                        : "transparent",
                    },
                  ]}
                >
                  <AgentLogoSet agents={[agent]} showLabels size={17} />
                </Pressable>
              );
            })}
          </View>
        </View>
        <View style={styles.filterSection}>
          <Text style={[styles.sectionLabel, { color: colors.textPrimary }]}>
            Uninstall
          </Text>
          <View style={styles.optionWrap}>
            {(["all", "uninstallable", "readonly"] as const).map((value) => {
              const selected = filters.capability === value;
              const label =
                value === "all"
                  ? "All"
                  : value === "uninstallable"
                    ? "Uninstallable"
                    : "Read-only";
              return (
                <Pressable
                  key={value}
                  accessibilityRole="radio"
                  accessibilityState={{ selected }}
                  onPress={() => onChange({ ...filters, capability: value })}
                  style={[
                    styles.choice,
                    {
                      borderColor: selected
                        ? colors.accent
                        : colors.borderSubtle,
                      backgroundColor: selected
                        ? colors.accentSoft
                        : "transparent",
                    },
                  ]}
                >
                  <Text
                    style={{
                      color: selected ? colors.accent : colors.textSecondary,
                    }}
                  >
                    {label}
                  </Text>
                </Pressable>
              );
            })}
          </View>
        </View>
        <Pressable
          accessibilityRole="button"
          onPress={onClose}
          style={[styles.doneButton, { backgroundColor: colors.accent }]}
        >
          <Text style={{ color: colors.textOnAccent }}>Done</Text>
        </Pressable>
      </ScrollView>
    </BottomSheetFrame>
  );
}

function PluginDetailSection({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  const colors = useAppColors();
  return (
    <View
      style={[styles.detailSection, { borderBottomColor: colors.borderSubtle }]}
    >
      <Text style={[styles.sectionTitle, { color: colors.textPrimary }]}>
        {title}
      </Text>
      {children}
    </View>
  );
}

function PluginState({
  loading,
  icon,
  title,
  detail,
  action,
  onAction,
}: {
  loading?: boolean;
  icon?: React.ComponentProps<typeof Ionicons>["name"];
  title: string;
  detail: string;
  action?: string;
  onAction?(): void;
}) {
  const colors = useAppColors();
  return (
    <View style={styles.state}>
      {loading ? (
        <ActivityIndicator color={colors.accent} />
      ) : icon ? (
        <Ionicons name={icon} size={28} color={colors.textTertiary} />
      ) : null}
      <Text style={[styles.stateTitle, { color: colors.textPrimary }]}>
        {title}
      </Text>
      <Text style={[styles.stateDetail, { color: colors.textTertiary }]}>
        {detail}
      </Text>
      {action && onAction ? (
        <Pressable onPress={onAction} style={styles.stateAction}>
          <Text style={{ color: colors.accent }}>{action}</Text>
        </Pressable>
      ) : null}
    </View>
  );
}

function componentIcon(
  kind: string,
): React.ComponentProps<typeof Ionicons>["name"] {
  switch (kind) {
    case "skill":
      return "book-outline";
    case "agent":
      return "people-outline";
    case "command":
      return "terminal-outline";
    case "hook":
      return "git-branch-outline";
    case "mcp":
      return "server-outline";
    case "app":
      return "apps-outline";
    default:
      return "document-outline";
  }
}

const styles = StyleSheet.create({
  root: { flex: 1, flexDirection: "row" },
  flex: { flex: 1, minWidth: 0 },
  searchArea: {
    flexDirection: "row",
    gap: 8,
    paddingHorizontal: PLUGINS_SKILLS_SCREEN_PADDING,
    paddingTop: 10,
    paddingBottom: 8,
  },
  search: {
    flex: 1,
    height: 44,
    borderWidth: 1,
    borderRadius: 6,
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 11,
    gap: 8,
  },
  input: { flex: 1, fontFamily: Typography.uiFont, paddingVertical: 0 },
  smallIcon: {
    width: 32,
    height: 40,
    alignItems: "center",
    justifyContent: "center",
  },
  filterButton: {
    minWidth: 44,
    height: 44,
    paddingHorizontal: 10,
    borderWidth: 1,
    borderRadius: 6,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 4,
  },
  chips: {
    paddingHorizontal: PLUGINS_SKILLS_SCREEN_PADDING,
    paddingBottom: 8,
    gap: 6,
  },
  chip: {
    height: 30,
    paddingHorizontal: 9,
    borderRadius: 6,
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
  list: {
    paddingHorizontal: PLUGINS_SKILLS_SCREEN_PADDING,
    paddingBottom: 28,
  },
  emptyList: {
    flexGrow: 1,
    paddingHorizontal: PLUGINS_SKILLS_SCREEN_PADDING,
  },
  rowDelete: {
    width: 44,
    height: 44,
    flexShrink: 0,
    alignItems: "center",
    justifyContent: "center",
  },
  metadata: TypeScale.compact,
  dimmed: { opacity: 0.45 },
  panel: {
    width: 420,
    maxWidth: "44%",
    borderLeftWidth: StyleSheet.hairlineWidth,
  },
  inspectorSheetCard: { height: "90%" },
  sheetContent: { flex: 1, minHeight: 0 },
  inspector: { flex: 1 },
  inspectorHeader: {
    minHeight: 64,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: PLUGINS_SKILLS_SCREEN_PADDING,
    paddingVertical: 9,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  inspectorTitle: { ...TypeScale.title, fontFamily: Typography.uiFontMedium },
  close: {
    width: 44,
    height: 44,
    alignItems: "center",
    justifyContent: "center",
  },
  inspectorScroll: { paddingBottom: 36 },
  detailSection: {
    paddingHorizontal: PLUGINS_SKILLS_SCREEN_PADDING,
    paddingVertical: 14,
    gap: 10,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  sectionTitle: { ...TypeScale.body, fontFamily: Typography.uiFontMedium },
  sectionLabel: { ...TypeScale.compact, fontFamily: Typography.uiFontMedium },
  componentRow: {
    minHeight: 40,
    flexDirection: "row",
    alignItems: "center",
    gap: 9,
  },
  path: { fontFamily: Typography.terminalFont, fontSize: 11, marginTop: 2 },
  lifecycleSection: {
    paddingHorizontal: PLUGINS_SKILLS_SCREEN_PADDING,
    paddingVertical: 16,
    gap: 10,
  },
  protectedRow: { flexDirection: "row", alignItems: "center", gap: 6 },
  uninstallButton: {
    minHeight: 44,
    borderWidth: 1,
    borderRadius: 6,
    paddingHorizontal: 14,
    alignItems: "center",
    justifyContent: "center",
    alignSelf: "flex-start",
  },
  copyCard: {
    minHeight: 78,
    borderWidth: 1,
    borderRadius: 6,
    flexDirection: "row",
    alignItems: "center",
  },
  copyOpen: {
    flex: 1,
    minWidth: 0,
    paddingLeft: 11,
    paddingRight: 6,
    paddingVertical: 9,
  },
  copyTitle: { ...TypeScale.compact, fontFamily: Typography.uiFontMedium },
  copySheet: { maxHeight: 640 },
  copySheetBody: { gap: 10, paddingTop: 8, paddingBottom: 20 },
  filterSheet: { maxHeight: 620 },
  sheetHeader: {
    height: 44,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
  },
  sheetTitle: { ...TypeScale.title, fontFamily: Typography.uiFontMedium },
  filterBody: { gap: 18, paddingBottom: 18 },
  filterSection: { gap: 8 },
  optionWrap: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
  choice: {
    minHeight: 40,
    paddingHorizontal: 12,
    borderWidth: 1,
    borderRadius: 6,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  doneButton: {
    height: 46,
    borderRadius: 6,
    alignItems: "center",
    justifyContent: "center",
  },
  state: {
    flex: 1,
    minHeight: 180,
    alignItems: "center",
    justifyContent: "center",
    padding: 24,
    gap: 7,
  },
  stateTitle: {
    ...TypeScale.body,
    fontFamily: Typography.uiFontMedium,
    textAlign: "center",
  },
  stateDetail: { ...TypeScale.compact, textAlign: "center", maxWidth: 360 },
  stateAction: {
    minHeight: 44,
    paddingHorizontal: 12,
    alignItems: "center",
    justifyContent: "center",
  },
});
