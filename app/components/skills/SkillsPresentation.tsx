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
import type {
  InstalledPluginCopy,
  PluginInventory,
} from "../../services/pluginsManagement";
import type { LogicalPlugin } from "../../services/pluginsScreenModel";
import type {
  InstalledSkill,
  ManagedSkillAgent,
  PackageDetail,
  SkillMutationOperation,
  SkillsInventory,
  SkillsRequestState,
} from "../../services/skillsManagement";
import {
  scopeLabel,
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
import { skillRowSupportsDelete } from "../../services/skillsSurfaceModel";
import { PLUGINS_SKILLS_SCREEN_PADDING } from "../../services/pluginsSkillsSurfaceModel";
import { AgentLogoSet } from "../agents/AgentLogoSet";
import { ExtensionListRow } from "../extensions/ExtensionListRow";
import { PluginsPresentation } from "../plugins/PluginsPresentation";

export interface SurfaceMutationNotice {
  kind: "success" | "error";
  message: string;
}

export interface SkillsPresentationProps {
  section: SkillsSurfaceSection;
  inventoryState: SkillsRequestState<SkillsInventory>;
  logicalSkills: LogicalSkill[];
  pluginsState: SkillsRequestState<PluginInventory>;
  logicalPlugins: LogicalPlugin[];
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
  onDeleteSkill(skill: InstalledSkill): void;
  onUninstallPlugin(copy: InstalledPluginCopy): void;
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
  const [deletePickerKey, setDeletePickerKey] = useState<string | null>(null);
  const wide = width >= WIDE_INSPECTOR;
  const inventory = skillsRequestData(props.inventoryState);
  useEffect(() => {
    setDeletePickerKey(null);
  }, [inventory?.generatedAt]);
  const visible = useMemo(
    () => filterLogicalSkills(props.logicalSkills, query, filters),
    [filters, props.logicalSkills, query],
  );
  const detail = skillsRequestData(props.inspectState);
  const selectedLogical = props.logicalSkills.find(
    (skill) => skill.key === props.inspectedName?.toLocaleLowerCase(),
  );
  const deletePicker = props.logicalSkills.find(
    (skill) => skill.key === deletePickerKey,
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
          <LocalSkillsList
            {...props}
            rows={visible}
            inventory={inventory}
            onChooseCopy={(skill) => setDeletePickerKey(skill.key)}
          />
          <FilterSheet
            visible={filtersOpen}
            filters={filters}
            onChange={setFilters}
            onClose={() => setFiltersOpen(false)}
          />
        </View>
      ) : (
        <PluginsPresentation
          state={props.pluginsState}
          plugins={props.logicalPlugins}
          preparingMutation={props.preparingMutation}
          currentServerAvailable={props.currentServerAvailable}
          wide={wide}
          onOpenSettings={props.onOpenSettings}
          onRefresh={props.onRetryPlugins}
          onUninstall={props.onUninstallPlugin}
        />
      )}
    </SafeAreaView>
  );
  return (
    <View style={[styles.root, { backgroundColor: colors.bgPrimary }]}>
      <View style={styles.flex}>{main}</View>
      {props.section === "skills" && wide && props.inspectedCopyId ? (
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
      {props.section === "skills" && !wide ? (
        <BottomSheetFrame
          visible={Boolean(props.inspectedCopyId)}
          maxHeight="94%"
          dragToDismiss
          onClose={props.onDismissInspector}
          cardStyle={styles.inspectorSheetCard}
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
      <DeleteCopySheet
        logical={deletePicker ?? null}
        mutationOperations={props.mutationOperations}
        preparingMutation={props.preparingMutation}
        onClose={() => setDeletePickerKey(null)}
        onDelete={(copy) => {
          setDeletePickerKey(null);
          props.onDeleteSkill(copy);
        }}
      />
      <MutationToast
        notice={props.mutationNotice}
        onDismiss={props.onDismissNotice}
      />
    </View>
  );
}

function MutationToast({
  notice,
  onDismiss,
}: {
  notice: SurfaceMutationNotice | null;
  onDismiss(): void;
}) {
  const colors = useAppColors();
  if (!notice) return null;
  const error = notice.kind === "error";
  return (
    <View pointerEvents="box-none" style={styles.toastWrap}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Dismiss status message"
        accessibilityLiveRegion={error ? "assertive" : "polite"}
        onPress={onDismiss}
        style={[
          styles.toast,
          {
            backgroundColor: error ? colors.dangerSoft : colors.modalSurface,
            borderColor: error ? colors.dangerText : colors.borderStrong,
          },
        ]}
      >
        <Ionicons
          name={error ? "warning-outline" : "checkmark-circle-outline"}
          size={20}
          color={error ? colors.dangerText : colors.success}
        />
        <Text
          numberOfLines={3}
          style={[
            styles.toastText,
            { color: error ? colors.dangerText : colors.textPrimary },
          ]}
        >
          {notice.message}
        </Text>
        <Ionicons name="close" size={18} color={colors.textTertiary} />
      </Pressable>
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
  const chips: Array<{
    key: string;
    label: string;
    agent?: ManagedSkillAgent;
    remove(): void;
  }> = [
    ...filters.agents.map((agent) => ({
      key: `agent:${agent}`,
      label: skillAgentLabel(agent),
      agent,
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
          {chip.agent ? (
            <AgentLogoSet agents={[chip.agent]} size={16} />
          ) : (
            <Text style={{ color: colors.accent }}>{chip.label}</Text>
          )}
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
                  agent={agent}
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
  agent,
  selected,
  onPress,
}: {
  label: string;
  agent?: ManagedSkillAgent;
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
      {agent ? (
        <AgentLogoSet agents={[agent]} showLabels size={17} />
      ) : (
        <Text
          style={{ color: selected ? colors.accent : colors.textSecondary }}
        >
          {label}
        </Text>
      )}
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
    onChooseCopy(skill: LogicalSkill): void;
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
          mutationOperations={props.mutationOperations}
          preparingMutation={props.preparingMutation}
          onOpen={() => props.onInspectSkill(item.primaryCopy)}
          onDelete={() => {
            if (item.copies.length > 1) props.onChooseCopy(item);
            else props.onDeleteSkill(item.copies[0]!);
          }}
        />
      )}
    />
  );
}

function SkillRow({
  skill,
  mutationOperations,
  preparingMutation,
  onOpen,
  onDelete,
}: {
  skill: LogicalSkill;
  mutationOperations: readonly SkillMutationOperation[];
  preparingMutation: string;
  onOpen(): void;
  onDelete(): void;
}) {
  const canDelete = skill.copies.some((copy) =>
    skillRowSupportsDelete(copy, mutationOperations),
  );
  const deleting = skill.copies.some(
    (copy) => preparingMutation === `delete:${copy.id}`,
  );
  const readonlyReason = skill.copies.find(
    (copy) => !copy.capability.canDelete && copy.capability.reason,
  )?.capability.reason;
  return (
    <ExtensionListRow
      name={skill.name}
      summary={skill.description || readonlyReason || "Local Skill"}
      agents={skill.agents}
      openAccessibilityLabel={`Open ${skill.name}`}
      onOpen={onOpen}
      action={{
        accessibilityLabel: canDelete
          ? `Delete ${skill.name}`
          : `Open why ${skill.name} is protected`,
        icon: canDelete ? "trash-outline" : "lock-closed-outline",
        destructive: canDelete,
        busy: deleting,
        disabled: canDelete && Boolean(preparingMutation),
        onPress: canDelete ? onDelete : onOpen,
      }}
    />
  );
}

function DeleteCopySheet({
  logical,
  mutationOperations,
  preparingMutation,
  onClose,
  onDelete,
}: {
  logical: LogicalSkill | null;
  mutationOperations: readonly SkillMutationOperation[];
  preparingMutation: string;
  onClose(): void;
  onDelete(copy: InstalledSkill): void;
}) {
  const colors = useAppColors();
  return (
    <BottomSheetFrame
      visible={Boolean(logical)}
      onClose={onClose}
      maxHeight="76%"
      dragToDismiss
      contentStyle={styles.deleteSheet}
    >
      <View style={styles.sheetHeader}>
        <View style={styles.flex}>
          <Text style={[styles.sheetTitle, { color: colors.textPrimary }]}>
            Delete copy
          </Text>
          <Text style={[styles.metadata, { color: colors.textTertiary }]}>
            {logical?.name}
          </Text>
        </View>
        <Pressable
          accessibilityLabel="Close copy selector"
          onPress={onClose}
          style={styles.close}
        >
          <Ionicons name="close" size={22} color={colors.textSecondary} />
        </Pressable>
      </View>
      <ScrollView contentContainerStyle={styles.deleteSheetBody}>
        {(logical?.copies ?? []).map((copy) => {
          const location = skillCopyLocation(copy);
          const canDelete = skillRowSupportsDelete(copy, mutationOperations);
          const deleting = preparingMutation === `delete:${copy.id}`;
          return (
            <View
              key={copy.id}
              style={[
                styles.deleteCopyRow,
                { borderColor: colors.borderSubtle },
              ]}
            >
              <View style={styles.copyContent}>
                <AgentLogoSet agents={copy.agents} size={17} />
                <Text
                  numberOfLines={2}
                  style={[styles.copyLabel, { color: colors.textPrimary }]}
                >
                  {location.label}
                </Text>
                <Text
                  selectable
                  numberOfLines={1}
                  style={[styles.path, { color: colors.textTertiary }]}
                >
                  {location.path}
                </Text>
                {!canDelete ? (
                  <Text
                    style={[styles.metadata, { color: colors.textTertiary }]}
                  >
                    {copy.capability.reason ||
                      "This copy cannot be deleted from here."}
                  </Text>
                ) : null}
              </View>
              {canDelete ? (
                <Pressable
                  accessibilityRole="button"
                  accessibilityLabel={`Delete ${copy.name} from ${copy.location}`}
                  disabled={Boolean(preparingMutation)}
                  onPress={() => onDelete(copy)}
                  style={({ pressed }) => [
                    styles.rowDelete,
                    (pressed || Boolean(preparingMutation)) && styles.dimmed,
                  ]}
                >
                  {deleting ? (
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
        })}
      </ScrollView>
    </BottomSheetFrame>
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
  const canDelete =
    props.mutationOperations.includes("delete") &&
    copy.capability.canDelete &&
    detail.capability.canDelete;
  const deleting = props.preparingMutation === `delete:${copy.id}`;
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
            <Text style={{ color: colors.textTertiary }}>
              {scopeLabel(detail.scope)}
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
        <DetailSection title="Available to">
          <AgentLogoSet agents={detail.agents} showLabels size={20} />
        </DetailSection>
        {props.logical.copies.length > 1 ? (
          <DetailSection title={`Locations (${props.logical.copies.length})`}>
            {props.logical.copies.map((item) => (
              <CopyRow
                key={item.id}
                copy={item}
                selected={item.id === copy.id}
                onPress={() => props.onInspectSkill(item)}
              />
            ))}
          </DetailSection>
        ) : null}
        {canDelete ? (
          <View style={styles.lifecycleSection}>
            <Text style={[styles.sectionTitle, { color: colors.textPrimary }]}>
              Delete
            </Text>
            <Text style={[styles.metadata, { color: colors.textTertiary }]}>
              Permanently delete this copy from {location.label}.
            </Text>
            <Action
              label={deleting ? "Deleting..." : "Delete Skill"}
              destructive
              disabled={Boolean(props.preparingMutation)}
              onPress={() => props.onDeleteSkill(copy)}
            />
          </View>
        ) : detail.capability.reason || copy.capability.reason ? (
          <View style={styles.lifecycleSection}>
            <Text style={{ color: colors.textTertiary }}>
              {detail.capability.reason || copy.capability.reason}
            </Text>
          </View>
        ) : null}
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
  onPress,
}: {
  copy: InstalledSkill;
  selected: boolean;
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
      <View style={styles.copyContent}>
        <AgentLogoSet agents={copy.agents} size={17} />
        <View style={styles.rowHeading}>
          <Text
            numberOfLines={2}
            style={[styles.copyLabel, { color: colors.textPrimary }]}
          >
            {location.label}
          </Text>
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
    paddingHorizontal: PLUGINS_SKILLS_SCREEN_PADDING,
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
  toastWrap: {
    position: "absolute",
    left: PLUGINS_SKILLS_SCREEN_PADDING,
    right: PLUGINS_SKILLS_SCREEN_PADDING,
    bottom: 12,
    zIndex: 20,
    alignItems: "center",
  },
  toast: {
    width: "100%",
    maxWidth: 560,
    minHeight: 48,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 8,
    paddingHorizontal: 12,
    paddingVertical: 10,
    flexDirection: "row",
    alignItems: "center",
    gap: 9,
  },
  toastText: { ...TypeScale.compact, flex: 1 },
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
  rowHeading: { flexDirection: "row", alignItems: "center", gap: 7 },
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
  agentLabel: {
    minHeight: 36,
    borderRadius: 6,
    paddingHorizontal: 11,
    alignItems: "center",
    justifyContent: "center",
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
  copyContent: { flex: 1, minWidth: 0 },
  copyLabel: {
    flex: 1,
    flexShrink: 1,
    fontFamily: Typography.uiFontMedium,
  },
  path: { fontFamily: Typography.terminalFont, fontSize: 11, marginTop: 2 },
  deleteSheet: { maxHeight: 620 },
  deleteSheetBody: { gap: 10, paddingTop: 8, paddingBottom: 20 },
  deleteCopyRow: {
    minHeight: 68,
    borderWidth: 1,
    borderRadius: 6,
    paddingLeft: 11,
    paddingRight: 4,
    paddingVertical: 9,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  dimmed: { opacity: 0.45 },
  lifecycleSection: {
    paddingHorizontal: PLUGINS_SKILLS_SCREEN_PADDING,
    paddingVertical: 16,
    gap: 10,
  },
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
