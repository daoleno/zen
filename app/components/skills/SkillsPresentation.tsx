import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  FlatList,
  Linking,
  Pressable,
  RefreshControl,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
  useWindowDimensions,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import { EnrichedMarkdownText } from "react-native-enriched-markdown";
import { TypeScale, Typography, useAppColors } from "../../constants/tokens";
import { openSafeMarkdownUrl } from "../markdown/markdownLinks";
import { BottomSheetFrame } from "../ui/BottomSheetFrame";
import {
  evaluatePluginMutation,
  type PluginsUnifiedView,
} from "../../services/pluginsScreenModel";
import type {
  AvailablePlugin,
  InstalledPluginRow,
  PluginInventory,
} from "../../services/pluginsManagement";
import type {
  InstalledSkill,
  ManagedSkillAgent,
  PackageDetail,
  SkillMutationOperation,
  SkillsInventory,
  SkillsRequestState,
} from "../../services/skillsManagement";
import {
  skillAgentLabel,
  skillsRequestData,
} from "../../services/skillsManagement";
import {
  buildSkillFileTree,
  defaultSkillFile,
  filterLogicalSkills,
  MANAGED_SKILL_AGENTS,
  skillCopyLocation,
  skillRenderer,
  type LogicalSkill,
  type SkillFilters,
  type SkillScopeFilter,
  type SkillStatusFilter,
  type SkillTreeNode,
} from "../../services/skillsScreenModel";
import type { SkillsSurfaceSection } from "../../services/skillsSurfaceModel";

export interface SurfaceMutationNotice {
  kind: "success" | "error";
  message: string;
}

export interface SkillsPresentationProps {
  section: SkillsSurfaceSection;
  inventoryState: SkillsRequestState<SkillsInventory>;
  logicalSkills: LogicalSkill[];
  pluginsState: SkillsRequestState<PluginInventory>;
  pluginsView: PluginsUnifiedView;
  mutationOperations: readonly SkillMutationOperation[];
  preparingMutation: string;
  mutationNotice: SurfaceMutationNotice | null;
  currentServerAvailable: boolean;
  inspectedName: string | null;
  inspectedCopyId: string | null;
  inspectState: SkillsRequestState<PackageDetail>;
  onSelectSection(section: SkillsSurfaceSection): void;
  onOpenSettings(): void;
  onRefreshSkills(): void;
  onRetryPlugins(): void;
  onInspectSkill(skill: InstalledSkill, path?: string): void;
  onDismissInspector(): void;
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
  onDismissNotice(): void;
}

const WIDE_INSPECTOR = 920;
const DEFAULT_FILTERS: SkillFilters = {
  agents: [],
  status: "all",
  scope: "all",
};

export function SkillsPresentation(props: SkillsPresentationProps) {
  const colors = useAppColors();
  const { width } = useWindowDimensions();
  const [query, setQuery] = useState("");
  const [filters, setFilters] = useState<SkillFilters>(DEFAULT_FILTERS);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const wide = width >= WIDE_INSPECTOR;
  const inventory = skillsRequestData(props.inventoryState);
  const visible = useMemo(
    () => filterLogicalSkills(props.logicalSkills, query, filters),
    [filters, props.logicalSkills, query],
  );
  const detail = skillsRequestData(props.inspectState);
  const selectedLogical = props.logicalSkills.find(
    (skill) => skill.key === props.inspectedName?.toLocaleLowerCase(),
  );
  const activeFilterCount =
    filters.agents.length +
    Number(filters.status !== "all") +
    Number(filters.scope !== "all");
  const main = (
    <SafeAreaView
      style={[styles.safe, { backgroundColor: colors.bgPrimary }]}
      edges={[]}
    >
      <View
        style={[styles.modeBar, { borderBottomColor: colors.borderSubtle }]}
      >
        {(["skills", "plugins"] as const).map((section) => (
          <Pressable
            key={section}
            accessibilityRole="tab"
            accessibilityState={{ selected: props.section === section }}
            onPress={() => props.onSelectSection(section)}
            style={[
              styles.modeItem,
              props.section === section && { borderBottomColor: colors.accent },
            ]}
          >
            <Text
              style={[
                styles.modeText,
                {
                  color:
                    props.section === section
                      ? colors.textPrimary
                      : colors.textTertiary,
                },
              ]}
            >
              {section === "skills" ? "Skills" : "Plugins"}
            </Text>
          </Pressable>
        ))}
      </View>
      {props.mutationNotice ? (
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Dismiss status message"
          onPress={props.onDismissNotice}
          style={[
            styles.notice,
            {
              backgroundColor:
                props.mutationNotice.kind === "error"
                  ? colors.dangerSoft
                  : colors.accentSoft,
            },
          ]}
        >
          <Text
            style={{
              color:
                props.mutationNotice.kind === "error"
                  ? colors.dangerText
                  : colors.textPrimary,
            }}
          >
            {props.mutationNotice.message}
          </Text>
        </Pressable>
      ) : null}
      {props.section === "skills" ? (
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
                accessibilityLabel="Search local Skills"
                value={query}
                onChangeText={setQuery}
                placeholder="Search local Skills"
                placeholderTextColor={colors.textTertiary}
                style={[styles.input, { color: colors.textPrimary }]}
              />
              {query ? (
                <Pressable
                  accessibilityLabel="Clear search"
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
              accessibilityLabel="Filter Skills"
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
                <Text style={{ color: colors.accent }}>
                  {activeFilterCount}
                </Text>
              ) : null}
            </Pressable>
          </View>
          {activeFilterCount ? (
            <ActiveFilters filters={filters} onChange={setFilters} />
          ) : null}
          <LocalSkillsList {...props} rows={visible} inventory={inventory} />
          <FilterSheet
            visible={filtersOpen}
            filters={filters}
            onChange={setFilters}
            onClose={() => setFiltersOpen(false)}
          />
        </View>
      ) : (
        <PluginsList {...props} />
      )}
    </SafeAreaView>
  );
  return (
    <View style={[styles.root, { backgroundColor: colors.bgPrimary }]}>
      <View style={styles.flex}>{main}</View>
      {wide && props.inspectedCopyId ? (
        <View style={[styles.panel, { borderLeftColor: colors.borderSubtle }]}>
          <Inspector
            {...props}
            logical={selectedLogical}
            detail={
              detail?.copyId === props.inspectedCopyId ? detail : undefined
            }
          />
        </View>
      ) : null}
      {!wide ? (
        <BottomSheetFrame
          visible={Boolean(props.inspectedCopyId)}
          maxHeight="94%"
          dragToDismiss
          onClose={props.onDismissInspector}
          contentStyle={styles.sheetContent}
        >
          <Inspector
            {...props}
            logical={selectedLogical}
            detail={
              detail?.copyId === props.inspectedCopyId ? detail : undefined
            }
          />
        </BottomSheetFrame>
      ) : null}
    </View>
  );
}

function ActiveFilters({
  filters,
  onChange,
}: {
  filters: SkillFilters;
  onChange(value: SkillFilters): void;
}) {
  const colors = useAppColors();
  const chips = [
    ...filters.agents.map((agent) => ({
      key: `agent:${agent}`,
      label: skillAgentLabel(agent),
      remove: () =>
        onChange({
          ...filters,
          agents: filters.agents.filter((item) => item !== agent),
        }),
    })),
    ...(filters.status !== "all"
      ? [
          {
            key: "status",
            label: filters.status,
            remove: () => onChange({ ...filters, status: "all" }),
          },
        ]
      : []),
    ...(filters.scope !== "all"
      ? [
          {
            key: "scope",
            label: filters.scope,
            remove: () => onChange({ ...filters, scope: "all" }),
          },
        ]
      : []),
  ];
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      contentContainerStyle={styles.chips}
    >
      {chips.map((chip) => (
        <Pressable
          key={chip.key}
          accessibilityLabel={`Remove ${chip.label} filter`}
          onPress={chip.remove}
          style={[styles.chip, { backgroundColor: colors.accentSoft }]}
        >
          <Text style={{ color: colors.accent }}>{chip.label}</Text>
          <Ionicons name="close" size={15} color={colors.accent} />
        </Pressable>
      ))}
    </ScrollView>
  );
}

function FilterSheet({
  visible,
  filters,
  onChange,
  onClose,
}: {
  visible: boolean;
  filters: SkillFilters;
  onChange(value: SkillFilters): void;
  onClose(): void;
}) {
  const colors = useAppColors();
  return (
    <BottomSheetFrame
      visible={visible}
      onClose={onClose}
      maxHeight="76%"
      contentStyle={styles.filterSheet}
    >
      <View style={styles.sheetHeader}>
        <Text style={[styles.sheetTitle, { color: colors.textPrimary }]}>
          Filters
        </Text>
        <Pressable
          onPress={() => onChange(DEFAULT_FILTERS)}
          style={styles.textButton}
        >
          <Text style={{ color: colors.accent }}>Reset</Text>
        </Pressable>
      </View>
      <ScrollView contentContainerStyle={styles.filterBody}>
        <FilterSection title="Agents">
          <View style={styles.optionWrap}>
            {MANAGED_SKILL_AGENTS.map((agent) => {
              const selected = filters.agents.includes(agent);
              return (
                <Choice
                  key={agent}
                  label={skillAgentLabel(agent)}
                  selected={selected}
                  onPress={() =>
                    onChange({
                      ...filters,
                      agents: selected
                        ? filters.agents.filter((item) => item !== agent)
                        : [...filters.agents, agent],
                    })
                  }
                />
              );
            })}
          </View>
        </FilterSection>
        <FilterSection title="Status">
          <View style={styles.optionWrap}>
            {(["all", "enabled", "disabled"] as SkillStatusFilter[]).map(
              (value) => (
                <Choice
                  key={value}
                  label={titleCase(value)}
                  selected={filters.status === value}
                  onPress={() => onChange({ ...filters, status: value })}
                />
              ),
            )}
          </View>
        </FilterSection>
        <FilterSection title="Scope">
          <View style={styles.optionWrap}>
            {(["all", "global", "project"] as SkillScopeFilter[]).map(
              (value) => (
                <Choice
                  key={value}
                  label={titleCase(value)}
                  selected={filters.scope === value}
                  onPress={() => onChange({ ...filters, scope: value })}
                />
              ),
            )}
          </View>
        </FilterSection>
      </ScrollView>
      <Pressable
        onPress={onClose}
        style={[styles.doneButton, { backgroundColor: colors.accent }]}
      >
        <Text
          style={{
            color: colors.textOnAccent,
            fontFamily: Typography.uiFontMedium,
          }}
        >
          Done
        </Text>
      </Pressable>
    </BottomSheetFrame>
  );
}

function FilterSection({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  const colors = useAppColors();
  return (
    <View style={styles.filterSection}>
      <Text style={[styles.sectionLabel, { color: colors.textTertiary }]}>
        {title}
      </Text>
      {children}
    </View>
  );
}

function Choice({
  label,
  selected,
  onPress,
}: {
  label: string;
  selected: boolean;
  onPress(): void;
}) {
  const colors = useAppColors();
  return (
    <Pressable
      accessibilityRole="checkbox"
      accessibilityState={{ checked: selected }}
      onPress={onPress}
      style={[
        styles.choice,
        {
          borderColor: selected ? colors.accent : colors.borderSubtle,
          backgroundColor: selected ? colors.accentSoft : colors.surfaceSubtle,
        },
      ]}
    >
      <Text style={{ color: selected ? colors.accent : colors.textSecondary }}>
        {label}
      </Text>
      {selected ? (
        <Ionicons name="checkmark" size={16} color={colors.accent} />
      ) : null}
    </Pressable>
  );
}

function LocalSkillsList(
  props: SkillsPresentationProps & {
    rows: LogicalSkill[];
    inventory?: SkillsInventory;
  },
) {
  const colors = useAppColors();
  const state = props.inventoryState;
  if (!props.currentServerAvailable)
    return (
      <State
        icon="server-outline"
        title="No current server"
        detail="Choose a server in Settings to view its local Skills."
        action="Open Settings"
        onAction={props.onOpenSettings}
      />
    );
  if (state.status === "error" && !props.inventory)
    return (
      <State
        icon="warning-outline"
        title={
          state.error.includes("offline")
            ? "Server disconnected"
            : "Skills unavailable"
        }
        detail={state.error}
      />
    );
  if (
    (state.status === "idle" || state.status === "loading") &&
    !props.inventory
  )
    return (
      <State
        loading
        title="Loading local Skills"
        detail="Reading supported Agent locations on the current server."
      />
    );
  if ((props.inventory?.skills.length ?? 0) === 0)
    return (
      <State
        icon="folder-open-outline"
        title="No local Skills"
        detail="No Skill packages were found in supported Agent locations."
      />
    );
  return (
    <FlatList
      data={props.rows}
      keyExtractor={(item) => item.key}
      refreshControl={
        <RefreshControl
          refreshing={state.status === "loading"}
          onRefresh={props.onRefreshSkills}
          tintColor={colors.accent}
        />
      }
      contentContainerStyle={props.rows.length ? styles.list : styles.emptyList}
      ListEmptyComponent={
        <State
          icon="search-outline"
          title="No matches"
          detail="Adjust your search or active filters."
        />
      }
      renderItem={({ item }) => (
        <SkillRow
          skill={item}
          onOpen={() => props.onInspectSkill(item.primaryCopy)}
        />
      )}
    />
  );
}

function SkillRow({ skill, onOpen }: { skill: LogicalSkill; onOpen(): void }) {
  const colors = useAppColors();
  const agentText = skill.agents.length
    ? skill.agents.map(skillAgentLabel).join(", ")
    : "Not enabled";
  const copyText =
    skill.copies.length > 1 ? ` · ${skill.copies.length} copies` : "";
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`Open ${skill.name}`}
      onPress={onOpen}
      style={({ pressed }) => [
        styles.row,
        {
          borderBottomColor: colors.borderSubtle,
          backgroundColor: pressed ? colors.surfacePressed : "transparent",
        },
      ]}
    >
      <View style={styles.flex}>
        <View style={styles.rowHeading}>
          <Text
            style={[styles.rowTitle, { color: colors.textPrimary }]}
            numberOfLines={1}
          >
            {skill.name}
          </Text>
          <View
            style={[
              styles.statusDot,
              {
                backgroundColor: skill.enabled
                  ? colors.success
                  : colors.textTertiary,
              },
            ]}
          />
        </View>
        {skill.description ? (
          <Text
            numberOfLines={1}
            style={[styles.description, { color: colors.textSecondary }]}
          >
            {skill.description}
          </Text>
        ) : null}
        <Text
          numberOfLines={1}
          style={[
            styles.metadata,
            { color: skill.hasConflict ? colors.warning : colors.textTertiary },
          ]}
        >
          {skill.hasConflict
            ? "Copies need review · "
            : skill.enabledVariantCount > 1
              ? `${skill.enabledVariantCount} enabled content variants · `
              : ""}
          {agentText}
          {copyText}
        </Text>
      </View>
      <Ionicons name="chevron-forward" size={18} color={colors.textTertiary} />
    </Pressable>
  );
}

function Inspector(
  props: SkillsPresentationProps & {
    logical?: LogicalSkill;
    detail?: PackageDetail;
  },
) {
  const colors = useAppColors();
  const detail = props.detail;
  const copy = props.logical?.copies.find(
    (item) => item.id === props.inspectedCopyId,
  );
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selectedPath, setSelectedPath] = useState<string>();
  const previousCopy = useRef<string | null>(null);
  const tree = useMemo(
    () => buildSkillFileTree(detail?.files ?? []),
    [detail?.files],
  );
  useEffect(() => {
    if (!detail || !copy) return;
    const selected =
      detail.preview?.path ?? defaultSkillFile(detail.files ?? []);
    setSelectedPath(selected);
    const changed = previousCopy.current !== copy.id;
    previousCopy.current = copy.id;
    if (changed)
      setExpanded(
        new Set(
          selected
            ?.split("/")
            .slice(0, -1)
            .map((_, index, parts) => parts.slice(0, index + 1).join("/")),
        ),
      );
  }, [copy?.id, detail?.skillName]);
  useEffect(() => {
    if (detail?.preview?.path) setSelectedPath(detail.preview.path);
  }, [detail?.preview?.path]);
  if (!detail && props.inspectState.status === "loading")
    return (
      <State
        loading
        title="Loading Skill"
        detail="Reading the selected local copy."
      />
    );
  if (!detail && props.inspectState.status === "error")
    return (
      <State
        icon="warning-outline"
        title="Skill unavailable"
        detail={props.inspectState.error}
      />
    );
  if (!detail || !copy || !props.logical) return null;
  const location = skillCopyLocation(copy);
  const preview = detail.preview;
  const renderer = preview
    ? skillRenderer(preview.kind, preview.content)
    : null;
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
            {detail.skillName}
          </Text>
          <Text
            style={[styles.metadata, { color: colors.textTertiary }]}
            numberOfLines={1}
          >
            {location.label}
          </Text>
        </View>
        <Pressable
          accessibilityLabel="Close Skill details"
          onPress={props.onDismissInspector}
          style={styles.close}
        >
          <Ionicons name="close" size={22} color={colors.textSecondary} />
        </Pressable>
      </View>
      <ScrollView contentContainerStyle={styles.inspectorScroll}>
        <DetailSection title="Overview">
          {detail.description ? (
            <Text style={{ color: colors.textSecondary }}>
              {detail.description}
            </Text>
          ) : null}
          <View style={styles.summaryRow}>
            <StatusLabel enabled={detail.enabled} />
            <Text style={{ color: colors.textTertiary }}>{detail.scope}</Text>
            <Text style={{ color: colors.textTertiary }}>
              {copy.manager === "zen" ? "Managed by Zen" : "Available to Agent"}
            </Text>
          </View>
          {(detail.warnings ?? []).map((warning) => (
            <Text key={warning} style={{ color: colors.warning }}>
              {warning}
            </Text>
          ))}
        </DetailSection>
        <DetailSection title="Files">
          {tree.length ? (
            tree.map((node) => (
              <TreeNode
                key={node.path}
                node={node}
                depth={0}
                expanded={expanded}
                selected={selectedPath}
                onToggle={(path) =>
                  setExpanded((current) => {
                    const next = new Set(current);
                    next.has(path) ? next.delete(path) : next.add(path);
                    return next;
                  })
                }
                onSelect={(path) => {
                  setSelectedPath(path);
                  props.onInspectSkill(copy, path);
                }}
              />
            ))
          ) : (
            <Text style={{ color: colors.textTertiary }}>
              This copy contains no readable files.
            </Text>
          )}
          <FilePreview
            detail={detail}
            selectedPath={selectedPath}
            renderer={renderer}
            loading={props.inspectState.status === "loading"}
            error={
              props.inspectState.status === "error"
                ? props.inspectState.error
                : undefined
            }
          />
        </DetailSection>
        <DetailSection title={`Copies (${props.logical.copies.length})`}>
          {props.logical.copies.map((item) => (
            <CopyRow
              key={item.id}
              copy={item}
              selected={item.id === copy.id}
              enabled={item.enabled}
              onPress={() => props.onInspectSkill(item)}
            />
          ))}
        </DetailSection>
        <DetailSection title="Agent bindings">
          {detail.bindings.length ? (
            detail.bindings.map((binding) => {
              const toggleOperation = binding.enabled ? "disable" : "enable";
              const canToggle =
                props.mutationOperations.includes(toggleOperation) &&
                binding.operations.includes(toggleOperation);
              const canUnbind =
                props.mutationOperations.includes("unbind") &&
                binding.operations.includes("unbind");
              return (
                <View
                  key={`${binding.agent}:${binding.scope}`}
                  style={[
                    styles.bindingRow,
                    { borderBottomColor: colors.borderSubtle },
                  ]}
                >
                  <View style={styles.flex}>
                    <Text style={{ color: colors.textPrimary }}>
                      {skillAgentLabel(binding.agent)}
                    </Text>
                    <Text
                      style={[styles.metadata, { color: colors.textTertiary }]}
                    >
                      {binding.scope} · {binding.mode} ·{" "}
                      {binding.enabled ? "enabled" : "disabled"}
                    </Text>
                    {binding.note ? (
                      <Text
                        style={[styles.metadata, { color: colors.textTertiary }]}
                      >
                        {binding.note}
                      </Text>
                    ) : null}
                  </View>
                  <View style={styles.bindingControls}>
                    {canUnbind ? (
                      <Pressable
                        accessibilityLabel={`Unbind ${skillAgentLabel(binding.agent)} ${binding.scope} binding`}
                        disabled={Boolean(props.preparingMutation)}
                        onPress={() =>
                          props.onBinding(
                            copy,
                            "unbind",
                            binding.agent,
                            binding.scope === "project" ? "project" : "global",
                          )
                        }
                        style={({ pressed }) => [
                          styles.bindingIcon,
                          (pressed || props.preparingMutation) && styles.dimmed,
                        ]}
                      >
                        <Ionicons
                          name="unlink-outline"
                          size={19}
                          color={colors.textSecondary}
                        />
                      </Pressable>
                    ) : null}
                    {canToggle ? (
                      <Pressable
                        accessibilityRole="switch"
                        accessibilityState={{ checked: binding.enabled }}
                        accessibilityLabel={`${binding.enabled ? "Disable" : "Enable"} ${skillAgentLabel(binding.agent)} binding`}
                        disabled={Boolean(props.preparingMutation)}
                        onPress={() =>
                          props.onBinding(
                            copy,
                            toggleOperation,
                            binding.agent,
                            binding.scope === "project" ? "project" : "global",
                          )
                        }
                        style={[
                          styles.toggle,
                          {
                            backgroundColor: binding.enabled
                              ? colors.accent
                              : colors.borderStrong,
                          },
                          props.preparingMutation && styles.dimmed,
                        ]}
                      >
                        <View
                          style={[
                            styles.toggleKnob,
                            binding.enabled && styles.toggleKnobOn,
                            { backgroundColor: colors.bgPrimary },
                          ]}
                        />
                      </Pressable>
                    ) : null}
                  </View>
                </View>
              );
            })
          ) : (
            <Text style={{ color: colors.textTertiary }}>
              {copy.manager === "external"
                ? "This Skill is read directly from an Agent location."
                : "This managed copy is not bound to an Agent."}
            </Text>
          )}
          <BindingTargets copy={copy} detail={detail} props={props} />
        </DetailSection>
        <LifecycleActions copy={copy} detail={detail} props={props} />
      </ScrollView>
    </View>
  );
}

function DetailSection({
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
function StatusLabel({ enabled }: { enabled: boolean }) {
  const colors = useAppColors();
  return (
    <View style={styles.statusLabel}>
      <View
        style={[
          styles.statusDot,
          { backgroundColor: enabled ? colors.success : colors.textTertiary },
        ]}
      />
      <Text style={{ color: colors.textSecondary }}>
        {enabled ? "Enabled" : "Disabled"}
      </Text>
    </View>
  );
}

function CopyRow({
  copy,
  selected,
  enabled,
  onPress,
}: {
  copy: InstalledSkill;
  selected: boolean;
  enabled: boolean;
  onPress(): void;
}) {
  const colors = useAppColors();
  const location = skillCopyLocation(copy);
  return (
    <Pressable
      onPress={onPress}
      style={[
        styles.copyRow,
        {
          borderColor: selected ? colors.accent : colors.borderSubtle,
          backgroundColor: selected ? colors.accentSoft : "transparent",
        },
      ]}
    >
      <View style={styles.flex}>
        <View style={styles.rowHeading}>
          <Text
            style={{
              color: colors.textPrimary,
              fontFamily: Typography.uiFontMedium,
            }}
          >
            {location.label}
          </Text>
          {enabled ? (
            <Text style={[styles.enabledLabel, { color: colors.accent }]}>
              ENABLED
            </Text>
          ) : null}
        </View>
        <Text
          selectable
          numberOfLines={1}
          style={[styles.path, { color: colors.textTertiary }]}
        >
          {location.path}
        </Text>
      </View>
      {!selected ? (
        <Ionicons
          name="chevron-forward"
          size={17}
          color={colors.textTertiary}
        />
      ) : null}
    </Pressable>
  );
}

function LifecycleActions({
  copy,
  detail,
  props,
}: {
  copy: InstalledSkill;
  detail: PackageDetail;
  props: SkillsPresentationProps;
}) {
  const colors = useAppColors();
  const ops = detail.capability.canManage
    ? detail.capability.operations.filter((operation) =>
        props.mutationOperations.includes(operation),
      )
    : [];
  if (!ops.length) return null;
  return (
    <View style={styles.lifecycleSection}>
      <Text style={[styles.sectionTitle, { color: colors.textPrimary }]}>
        Management
      </Text>
      {ops.includes("adopt") ? (
        <>
          <Text style={{ color: colors.textSecondary }}>
            Manage with Zen copies this Skill into Zen's managed store. The
            current external files stay in place and remain untouched. Agent
            bindings can be changed afterward.
          </Text>
          <Action
            label="Manage with Zen"
            disabled={Boolean(props.preparingMutation)}
            onPress={() => props.onAdopt(copy)}
          />
        </>
      ) : null}
      {ops.includes("update") ? (
        <Action
          label="Update managed copy"
          disabled={Boolean(props.preparingMutation)}
          onPress={() => props.onUpdate(copy)}
        />
      ) : null}
      {ops.includes("forget") ? (
        <>
          <Text style={[styles.metadata, { color: colors.textTertiary }]}>
            Forget removes only Zen bookkeeping. It never deletes the external
            Skill files.
          </Text>
          <Action
            label="Forget from Zen"
            disabled={Boolean(props.preparingMutation)}
            onPress={() => props.onForget(copy)}
          />
        </>
      ) : null}
      {ops.includes("uninstall") ? (
        <>
          <Text style={[styles.metadata, { color: colors.textTertiary }]}>
            Uninstall removes Zen's managed copy and its bindings. External
            source folders are not deleted.
          </Text>
          <Action
            label="Uninstall managed copy"
            destructive
            disabled={Boolean(props.preparingMutation)}
            onPress={() => props.onUninstall(copy)}
          />
        </>
      ) : null}
    </View>
  );
}

function BindingTargets({
  copy,
  detail,
  props,
}: {
  copy: InstalledSkill;
  detail: PackageDetail;
  props: SkillsPresentationProps;
}) {
  const colors = useAppColors();
  const inventory = skillsRequestData(props.inventoryState);
  if (
    !inventory ||
    !props.mutationOperations.includes("bind") ||
    !detail.capability.canManage ||
    !detail.capability.operations.includes("bind")
  ) {
    return null;
  }
  const existing = new Set(
    detail.bindings.map((binding) => `${binding.agent}:${binding.scope}`),
  );
  const targets = inventory.agents.flatMap((agent) => {
    if (!agent.supported) return [];
    const scopes: Array<"global" | "project"> = [];
    if (agent.globalScope && !existing.has(`${agent.agent}:global`)) {
      scopes.push("global");
    }
    if (
      inventory.cwd &&
      agent.projectScope &&
      !existing.has(`${agent.agent}:project`)
    ) {
      scopes.push("project");
    }
    return scopes.map((scope) => ({ agent: agent.agent, scope }));
  });
  if (!targets.length) return null;
  return (
    <View style={styles.bindTargets}>
      <Text style={[styles.metadata, { color: colors.textTertiary }]}>Add binding</Text>
      <View style={styles.optionWrap}>
        {targets.map((target) => (
          <Pressable
            key={`${target.agent}:${target.scope}`}
            accessibilityLabel={`Bind ${skillAgentLabel(target.agent)} ${target.scope}`}
            disabled={Boolean(props.preparingMutation)}
            onPress={() =>
              props.onBinding(copy, "bind", target.agent, target.scope)
            }
            style={({ pressed }) => [
              styles.bindTarget,
              { borderColor: colors.borderSubtle },
              (pressed || props.preparingMutation) && styles.dimmed,
            ]}
          >
            <Ionicons name="link-outline" size={16} color={colors.accent} />
            <Text style={{ color: colors.accent }}>
              {skillAgentLabel(target.agent)} · {target.scope}
            </Text>
          </Pressable>
        ))}
      </View>
    </View>
  );
}

function FilePreview({
  detail,
  selectedPath,
  renderer,
  loading,
  error,
}: {
  detail: PackageDetail;
  selectedPath?: string;
  renderer: ReturnType<typeof skillRenderer> | null;
  loading: boolean;
  error?: string;
}) {
  const colors = useAppColors();
  const preview = detail.preview;
  const markdownStyle = useMemo(
    () => ({
      paragraph: {
        color: colors.textSecondary,
        fontFamily: Typography.uiFont,
        fontSize: 15,
        lineHeight: 23,
      },
      h1: {
        color: colors.textPrimary,
        fontFamily: Typography.uiFontMedium,
        fontSize: 22,
        lineHeight: 30,
      },
      h2: {
        color: colors.textPrimary,
        fontFamily: Typography.uiFontMedium,
        fontSize: 19,
        lineHeight: 27,
      },
      h3: {
        color: colors.textPrimary,
        fontFamily: Typography.uiFontMedium,
        fontSize: 17,
        lineHeight: 25,
      },
      h4: { color: colors.textPrimary, fontFamily: Typography.uiFontMedium },
      h5: { color: colors.textPrimary, fontFamily: Typography.uiFontMedium },
      h6: { color: colors.textPrimary, fontFamily: Typography.uiFontMedium },
      list: {
        color: colors.textSecondary,
        bulletColor: colors.accent,
        markerColor: colors.accent,
      },
      link: { color: colors.accent },
      strong: {
        color: colors.textPrimary,
        fontFamily: Typography.uiFontMedium,
        fontWeight: "normal" as const,
      },
      code: {
        color: colors.textPrimary,
        backgroundColor: colors.surfaceSubtle,
        fontFamily: Typography.terminalFont,
      },
      codeBlock: {
        color: colors.textSecondary,
        backgroundColor: colors.surfaceSubtle,
        borderColor: colors.borderSubtle,
        fontFamily: Typography.terminalFont,
      },
    }),
    [colors],
  );
  return (
    <View style={[styles.preview, { borderTopColor: colors.borderSubtle }]}>
      <Text style={[styles.fileTitle, { color: colors.textPrimary }]}>
        {selectedPath || "No file selected"}
      </Text>
      {loading ? (
        <View style={styles.loadingRow}>
          <ActivityIndicator size="small" color={colors.accent} />
          <Text style={{ color: colors.textSecondary }}>Loading file</Text>
        </View>
      ) : null}
      {error ? (
        <State icon="warning-outline" title="File unavailable" detail={error} />
      ) : null}
      {preview?.notice ? (
        <Text style={[styles.previewNotice, { color: colors.warning }]}>
          {preview.notice}
        </Text>
      ) : null}
      {preview?.status === "binary" ? (
        <State
          icon="document-attach-outline"
          title="Binary file"
          detail={`${preview.mediaType} · ${preview.size} bytes. Content preview is unavailable.`}
        />
      ) : null}
      {renderer === "markdown" ? (
        <EnrichedMarkdownText
          markdown={preview?.content ?? ""}
          markdownStyle={markdownStyle}
          selectable
          onLinkPress={(event) =>
            void openSafeMarkdownUrl(event.url, (url) => Linking.openURL(url))
          }
        />
      ) : null}
      {renderer === "json" ? (
        <Code
          content={JSON.stringify(
            JSON.parse(preview?.content ?? "null"),
            null,
            2,
          )}
        />
      ) : null}
      {renderer === "invalid-json" ? (
        <>
          <Text style={[styles.previewNotice, { color: colors.warning }]}>
            Invalid JSON; showing original text.
          </Text>
          <Code content={preview?.content ?? ""} />
        </>
      ) : null}
      {renderer === "text" ? <Code content={preview?.content ?? ""} /> : null}
    </View>
  );
}

function TreeNode({
  node,
  depth,
  expanded,
  selected,
  onToggle,
  onSelect,
}: {
  node: SkillTreeNode;
  depth: number;
  expanded: Set<string>;
  selected?: string;
  onToggle(path: string): void;
  onSelect(path: string): void;
}) {
  const colors = useAppColors();
  const open = expanded.has(node.path);
  return (
    <>
      <Pressable
        accessibilityRole="button"
        accessibilityState={
          node.kind === "directory"
            ? { expanded: open }
            : { selected: selected === node.path }
        }
        onPress={() =>
          node.kind === "directory" ? onToggle(node.path) : onSelect(node.path)
        }
        style={[
          styles.treeRow,
          {
            paddingLeft: 8 + depth * 16,
            backgroundColor:
              selected === node.path ? colors.accentSoft : "transparent",
          },
        ]}
      >
        <Ionicons
          name={
            node.kind === "directory"
              ? open
                ? "folder-open-outline"
                : "folder-outline"
              : node.file?.kind === "binary"
                ? "document-attach-outline"
                : "document-text-outline"
          }
          size={17}
          color={
            node.kind === "directory" ? colors.warning : colors.textTertiary
          }
        />
        <Text
          numberOfLines={1}
          style={{
            color:
              selected === node.path ? colors.accent : colors.textSecondary,
            flex: 1,
          }}
        >
          {node.name}
        </Text>
      </Pressable>
      {node.kind === "directory" && open
        ? node.children.map((child) => (
            <TreeNode
              key={child.path}
              node={child}
              depth={depth + 1}
              expanded={expanded}
              selected={selected}
              onToggle={onToggle}
              onSelect={onSelect}
            />
          ))
        : null}
    </>
  );
}

function PluginsList(props: SkillsPresentationProps) {
  const colors = useAppColors();
  const data = props.pluginsView.rows;
  const stored = skillsRequestData(props.pluginsState);
  if (props.pluginsState.status === "error" && !stored)
    return (
      <State
        icon="warning-outline"
        title="Plugins unavailable"
        detail={props.pluginsState.error}
      />
    );
  if (
    (props.pluginsState.status === "idle" ||
      props.pluginsState.status === "loading") &&
    !stored
  )
    return (
      <State
        loading
        title="Loading Plugins"
        detail="Reading plugin state from the current server."
      />
    );
  return (
    <FlatList
      data={data}
      keyExtractor={(row) =>
        row.kind === "installed"
          ? `i:${row.plugin.id}`
          : `a:${row.plugin.pluginId}`
      }
      refreshControl={
        <RefreshControl
          refreshing={props.pluginsState.status === "loading"}
          onRefresh={props.onRetryPlugins}
          tintColor={colors.accent}
        />
      }
      contentContainerStyle={styles.list}
      ListEmptyComponent={
        <State
          icon="extension-puzzle-outline"
          title="No Plugins"
          detail="No Plugins are available on this server."
        />
      }
      renderItem={({ item }) =>
        item.kind === "installed" ? (
          <PluginRow
            name={item.plugin.name}
            detail={`${item.plugin.host} · ${item.plugin.version}`}
            actions={installedPluginActions(item.plugin, props)}
          />
        ) : (
          <PluginRow
            name={item.plugin.name}
            detail={item.plugin.description || item.plugin.marketplaceName}
            actions={[
              {
                label: "Install",
                run: () => props.onInstallPlugin(item.plugin),
              },
            ]}
          />
        )
      }
    />
  );
}

function PluginRow({
  name,
  detail,
  actions,
}: {
  name: string;
  detail: string;
  actions: Array<{ label: string; run(): void }>;
}) {
  const colors = useAppColors();
  return (
    <View style={[styles.row, { borderBottomColor: colors.borderSubtle }]}>
      <View style={styles.flex}>
        <Text style={[styles.rowTitle, { color: colors.textPrimary }]}>
          {name}
        </Text>
        <Text style={{ color: colors.textTertiary }}>{detail}</Text>
      </View>
      {actions.map((action) => (
        <Pressable
          key={action.label}
          onPress={action.run}
          style={styles.iconAction}
        >
          <Text
            style={{
              color:
                action.label === "Uninstall"
                  ? colors.dangerText
                  : colors.accent,
            }}
          >
            {action.label}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}
function installedPluginActions(
  plugin: InstalledPluginRow,
  props: Pick<SkillsPresentationProps, "onUpdatePlugin" | "onUninstallPlugin">,
) {
  const actions: Array<{ label: string; run(): void }> = [];
  if (evaluatePluginMutation({ kind: "update", row: plugin }).supported)
    actions.push({ label: "Update", run: () => props.onUpdatePlugin(plugin) });
  if (evaluatePluginMutation({ kind: "uninstall", row: plugin }).supported)
    actions.push({
      label: "Uninstall",
      run: () => props.onUninstallPlugin(plugin),
    });
  return actions;
}
function Code({ content }: { content: string }) {
  const colors = useAppColors();
  return (
    <ScrollView horizontal>
      <Text selectable style={[styles.code, { color: colors.textSecondary }]}>
        {content || "This file is empty."}
      </Text>
    </ScrollView>
  );
}
function Action({
  label,
  destructive,
  disabled,
  onPress,
}: {
  label: string;
  destructive?: boolean;
  disabled?: boolean;
  onPress(): void;
}) {
  const colors = useAppColors();
  return (
    <Pressable
      disabled={disabled}
      onPress={onPress}
      style={[
        styles.action,
        { borderColor: destructive ? colors.dangerText : colors.borderSubtle },
        disabled && styles.dimmed,
      ]}
    >
      <Text style={{ color: destructive ? colors.dangerText : colors.accent }}>
        {label}
      </Text>
    </Pressable>
  );
}
function State({
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
      ) : (
        <Ionicons
          name={icon ?? "information-circle-outline"}
          size={28}
          color={colors.textTertiary}
        />
      )}
      <Text style={[styles.stateTitle, { color: colors.textPrimary }]}>
        {title}
      </Text>
      <Text style={[styles.stateDetail, { color: colors.textTertiary }]}>
        {detail}
      </Text>
      {action && onAction ? <Action label={action} onPress={onAction} /> : null}
    </View>
  );
}
function titleCase(value: string) {
  return value[0]!.toUpperCase() + value.slice(1);
}

const styles = StyleSheet.create({
  root: { flex: 1, flexDirection: "row" },
  safe: { flex: 1 },
  flex: { flex: 1 },
  modeBar: {
    height: 44,
    flexDirection: "row",
    paddingHorizontal: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  modeItem: {
    minWidth: 76,
    height: 44,
    alignItems: "center",
    justifyContent: "center",
    borderBottomWidth: 2,
    borderBottomColor: "transparent",
  },
  modeText: { ...TypeScale.compact, fontFamily: Typography.uiFontMedium },
  notice: {
    marginHorizontal: 12,
    marginTop: 8,
    paddingHorizontal: 12,
    paddingVertical: 9,
    borderRadius: 6,
  },
  searchArea: {
    flexDirection: "row",
    gap: 8,
    paddingHorizontal: 12,
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
  chips: { paddingHorizontal: 12, paddingBottom: 8, gap: 6 },
  chip: {
    height: 30,
    paddingHorizontal: 9,
    borderRadius: 6,
    flexDirection: "row",
    alignItems: "center",
    gap: 4,
  },
  list: { paddingBottom: 28 },
  emptyList: { flexGrow: 1 },
  row: {
    minHeight: 82,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingHorizontal: 16,
    paddingVertical: 11,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  rowHeading: { flexDirection: "row", alignItems: "center", gap: 7 },
  rowTitle: {
    ...TypeScale.body,
    fontFamily: Typography.uiFontMedium,
    flexShrink: 1,
  },
  description: { ...TypeScale.compact, marginTop: 2 },
  metadata: { ...TypeScale.compact },
  statusDot: { width: 7, height: 7, borderRadius: 4 },
  iconAction: {
    minWidth: 44,
    minHeight: 44,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 6,
  },
  panel: {
    width: 470,
    maxWidth: "48%",
    borderLeftWidth: StyleSheet.hairlineWidth,
  },
  sheetContent: { height: "100%", minHeight: 560 },
  inspector: { flex: 1 },
  inspectorHeader: {
    minHeight: 64,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 16,
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
    paddingHorizontal: 16,
    paddingVertical: 14,
    gap: 9,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  sectionTitle: { ...TypeScale.body, fontFamily: Typography.uiFontMedium },
  sectionLabel: { ...TypeScale.compact, fontFamily: Typography.uiFontMedium },
  summaryRow: {
    flexDirection: "row",
    flexWrap: "wrap",
    alignItems: "center",
    gap: 12,
  },
  statusLabel: { flexDirection: "row", alignItems: "center", gap: 6 },
  treeRow: {
    minHeight: 38,
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
    paddingRight: 8,
    borderRadius: 4,
  },
  preview: {
    marginTop: 8,
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingTop: 12,
    minHeight: 160,
  },
  fileTitle: {
    ...TypeScale.compact,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 7,
  },
  previewNotice: { ...TypeScale.compact, marginBottom: 8 },
  loadingRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingVertical: 8,
  },
  code: {
    fontFamily: Typography.terminalFont,
    fontSize: 13,
    lineHeight: 20,
    paddingVertical: 8,
  },
  copyRow: {
    minHeight: 58,
    borderWidth: 1,
    borderRadius: 6,
    paddingHorizontal: 10,
    paddingVertical: 8,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  path: { fontFamily: Typography.terminalFont, fontSize: 11, marginTop: 2 },
  enabledLabel: { fontFamily: Typography.uiFontMedium, fontSize: 10 },
  bindingRow: {
    minHeight: 56,
    flexDirection: "row",
    alignItems: "center",
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  bindingControls: { flexDirection: "row", alignItems: "center", gap: 6 },
  bindingIcon: {
    width: 40,
    height: 40,
    alignItems: "center",
    justifyContent: "center",
  },
  toggle: { width: 46, height: 28, borderRadius: 14, padding: 3 },
  toggleKnob: { width: 22, height: 22, borderRadius: 11 },
  toggleKnobOn: { transform: [{ translateX: 18 }] },
  bindTargets: { gap: 8, paddingTop: 4 },
  bindTarget: {
    minHeight: 38,
    borderWidth: 1,
    borderRadius: 6,
    paddingHorizontal: 10,
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
  },
  dimmed: { opacity: 0.45 },
  lifecycleSection: { paddingHorizontal: 16, paddingVertical: 16, gap: 10 },
  action: {
    minHeight: 44,
    borderWidth: 1,
    borderRadius: 6,
    paddingHorizontal: 14,
    alignItems: "center",
    justifyContent: "center",
    alignSelf: "flex-start",
  },
  filterSheet: { maxHeight: 600 },
  sheetHeader: {
    height: 44,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
  },
  sheetTitle: { ...TypeScale.title, fontFamily: Typography.uiFontMedium },
  textButton: {
    minWidth: 44,
    height: 44,
    alignItems: "center",
    justifyContent: "center",
  },
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
});
