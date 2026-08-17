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
  filterInstalledSkills,
  MANAGED_SKILL_AGENTS,
  skillRenderer,
  type SkillScopeFilter,
  type SkillsAgentCounts,
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
  selectedAgent: ManagedSkillAgent;
  agentCounts: SkillsAgentCounts;
  inventoryState: SkillsRequestState<SkillsInventory>;
  installedSkills: InstalledSkill[];
  pluginsState: SkillsRequestState<PluginInventory>;
  pluginsView: PluginsUnifiedView;
  mutationOperations: readonly SkillMutationOperation[];
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
  onDismissNotice(): void;
}

const WIDE_INSPECTOR = 920;

export function SkillsPresentation(props: SkillsPresentationProps) {
  const colors = useAppColors();
  const { width } = useWindowDimensions();
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<SkillStatusFilter>("all");
  const [scope, setScope] = useState<SkillScopeFilter>("all");
  const wide = width >= WIDE_INSPECTOR;
  const inventory = skillsRequestData(props.inventoryState);
  const visible = useMemo(
    () => filterInstalledSkills(props.installedSkills, query, status, scope),
    [props.installedSkills, query, status, scope],
  );
  const inspectedDetail = skillsRequestData(props.inspectState);
  const detail =
    inspectedDetail?.skillName === props.inspectedName
      ? inspectedDetail
      : undefined;
  const refreshing =
    props.inventoryState.status === "loading" && inventory !== undefined;

  useEffect(() => {
    setQuery("");
  }, [props.selectedAgent, props.section]);

  const main = (
    <SafeAreaView
      style={[styles.safe, { backgroundColor: colors.bgPrimary }]}
      edges={["top"]}
    >
      <View style={[styles.tabs, { borderBottomColor: colors.borderSubtle }]}>
        {(["skills", "plugins"] as const).map((item) => (
          <Pressable
            key={item}
            accessibilityRole="tab"
            accessibilityState={{ selected: props.section === item }}
            onPress={() => props.onSelectSection(item)}
            style={[
              styles.tab,
              props.section === item && { borderBottomColor: colors.accent },
            ]}
          >
            <Ionicons
              name={
                item === "skills"
                  ? "library-outline"
                  : "extension-puzzle-outline"
              }
              size={18}
              color={
                props.section === item ? colors.accent : colors.textTertiary
              }
            />
            <Text
              style={{
                color:
                  props.section === item
                    ? colors.textPrimary
                    : colors.textTertiary,
              }}
            >
              {item === "skills" ? "Skills" : "Plugins"}
            </Text>
          </Pressable>
        ))}
      </View>
      {props.mutationNotice ? (
        <Pressable
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
          <ScrollView
            horizontal
            showsHorizontalScrollIndicator={false}
            contentContainerStyle={styles.agents}
          >
            {MANAGED_SKILL_AGENTS.map((agent) => (
              <Pressable
                key={agent}
                accessibilityRole="tab"
                accessibilityState={{ selected: props.selectedAgent === agent }}
                onPress={() => props.onSelectAgent(agent)}
                style={[
                  styles.pill,
                  {
                    borderColor:
                      props.selectedAgent === agent
                        ? colors.accent
                        : colors.borderSubtle,
                    backgroundColor:
                      props.selectedAgent === agent
                        ? colors.accentSoft
                        : colors.surfaceSubtle,
                  },
                ]}
              >
                <Text
                  style={{
                    color:
                      props.selectedAgent === agent
                        ? colors.accent
                        : colors.textSecondary,
                  }}
                >
                  {skillAgentLabel(agent)} {props.agentCounts[agent]}
                </Text>
              </Pressable>
            ))}
          </ScrollView>
          <View style={styles.filters}>
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
                size={18}
                color={colors.textTertiary}
              />
              <TextInput
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
                >
                  <Ionicons
                    name="close-circle"
                    size={18}
                    color={colors.textTertiary}
                  />
                </Pressable>
              ) : null}
            </View>
            <Filter
              value={status}
              values={["all", "enabled", "disabled"]}
              onChange={(value) => setStatus(value as SkillStatusFilter)}
            />
            <Filter
              value={scope}
              values={["all", "global", "project"]}
              onChange={(value) => setScope(value as SkillScopeFilter)}
            />
            {(inventory?.migration.external ?? 0) > 0 &&
            props.mutationOperations.includes("migrate") ? (
              <Action label="Track local Skills" onPress={props.onMigrate} />
            ) : null}
          </View>
          <LocalSkillsList
            {...props}
            rows={visible}
            inventory={inventory}
            refreshing={refreshing}
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
      {wide && props.inspectedName ? (
        <View style={[styles.panel, { borderLeftColor: colors.borderSubtle }]}>
          <Inspector {...props} detail={detail} />
        </View>
      ) : null}
      {!wide ? (
        <BottomSheetFrame
          visible={Boolean(props.inspectedName)}
          maxHeight="92%"
          dragToDismiss
          onClose={props.onDismissInspector}
        >
          <View style={styles.sheet}>
            <Inspector {...props} detail={detail} />
          </View>
        </BottomSheetFrame>
      ) : null}
    </View>
  );
}

function Filter({
  value,
  values,
  onChange,
}: {
  value: string;
  values: string[];
  onChange(value: string): void;
}) {
  const colors = useAppColors();
  return (
    <View style={[styles.segment, { borderColor: colors.borderSubtle }]}>
      {values.map((item) => (
        <Pressable
          key={item}
          onPress={() => onChange(item)}
          style={[
            styles.segmentItem,
            value === item && { backgroundColor: colors.accentSoft },
          ]}
        >
          <Text
            style={{
              color: value === item ? colors.accent : colors.textSecondary,
            }}
          >
            {item[0]!.toUpperCase() + item.slice(1)}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

function LocalSkillsList(
  props: SkillsPresentationProps & {
    rows: InstalledSkill[];
    inventory?: SkillsInventory;
    refreshing: boolean;
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
        detail="No Skill packages were found in the supported Agent locations."
      />
    );
  return (
    <FlatList
      data={props.rows}
      keyExtractor={(item) => item.id}
      refreshControl={
        <RefreshControl
          refreshing={props.refreshing}
          onRefresh={props.onRefreshSkills}
          tintColor={colors.accent}
        />
      }
      contentContainerStyle={props.rows.length ? styles.list : styles.emptyList}
      ListEmptyComponent={
        <State
          icon="search-outline"
          title="No matches"
          detail="Adjust the local search or filters."
        />
      }
      renderItem={({ item }) => (
        <SkillRow
          skill={item}
          busy={props.preparingMutation}
          onOpen={() => props.onInspectSkill(item.name)}
          onUpdate={() => props.onUpdate(item)}
          onAdopt={() => props.onAdopt(item)}
          onForget={() => props.onForget(item)}
          onUninstall={() => props.onUninstall(item)}
        />
      )}
    />
  );
}

function SkillRow({
  skill,
  busy,
  onOpen,
  onUpdate,
  onAdopt,
  onForget,
  onUninstall,
}: {
  skill: InstalledSkill;
  busy: string;
  onOpen(): void;
  onUpdate(): void;
  onAdopt(): void;
  onForget(): void;
  onUninstall(): void;
}) {
  const colors = useAppColors();
  const ops = skill.capability.canManage ? skill.capability.operations : [];
  const action = ops.includes("update")
    ? (["Update", onUpdate] as const)
    : ops.includes("adopt")
      ? (["Adopt", onAdopt] as const)
      : ops.includes("forget")
        ? (["Forget", onForget] as const)
        : ops.includes("uninstall")
          ? (["Uninstall", onUninstall] as const)
          : null;
  return (
    <Pressable
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
        <Text style={[styles.rowTitle, { color: colors.textPrimary }]}>
          {skill.name}
        </Text>
        <Text numberOfLines={1} style={{ color: colors.textTertiary }}>
          {skill.description ||
            `${skill.manager === "zen" ? "Zen-managed" : "Local external"} · ${skill.scope} · ${skill.enabled ? "enabled" : "disabled"}`}
        </Text>
      </View>
      {action ? (
        <Pressable
          disabled={Boolean(busy)}
          onPress={(event) => {
            event.stopPropagation();
            action[1]();
          }}
          style={styles.iconAction}
          accessibilityLabel={`${action[0]} ${skill.name}`}
        >
          {busy ? (
            <ActivityIndicator size="small" color={colors.accent} />
          ) : (
            <Text
              style={{
                color:
                  action[0] === "Uninstall" ? colors.dangerText : colors.accent,
              }}
            >
              {action[0]}
            </Text>
          )}
        </Pressable>
      ) : null}
      <Ionicons name="chevron-forward" size={18} color={colors.textTertiary} />
    </Pressable>
  );
}

function PluginsList(props: SkillsPresentationProps) {
  const colors = useAppColors();
  const data = props.pluginsView.rows;
  if (
    props.pluginsState.status === "error" &&
    !skillsRequestData(props.pluginsState)
  )
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
    !skillsRequestData(props.pluginsState)
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
): Array<{ label: string; run(): void }> {
  const actions: Array<{ label: string; run(): void }> = [];
  if (evaluatePluginMutation({ kind: "update", row: plugin }).supported) {
    actions.push({
      label: "Update",
      run: () => props.onUpdatePlugin(plugin),
    });
  }
  if (evaluatePluginMutation({ kind: "uninstall", row: plugin }).supported) {
    actions.push({
      label: "Uninstall",
      run: () => props.onUninstallPlugin(plugin),
    });
  }
  return actions;
}

function Inspector(
  props: SkillsPresentationProps & { detail?: PackageDetail },
) {
  const colors = useAppColors();
  const detail = props.detail;
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [selectedPath, setSelectedPath] = useState<string | undefined>();
  const previousSkill = useRef<string | null>(null);
  const tree = useMemo(
    () => buildSkillFileTree(detail?.files ?? []),
    [detail?.files],
  );
  useEffect(() => {
    if (!detail) return;
    const selected =
      detail.preview?.path ?? defaultSkillFile(detail.files ?? []);
    setSelectedPath(selected);
    if (selected && detail.preview?.path !== selected)
      props.onInspectSkill(detail.skillName, selected);
    const changedSkill = previousSkill.current !== detail.skillName;
    previousSkill.current = detail.skillName;
    setExpanded((current) =>
      !changedSkill && current.size
        ? current
        : new Set(
            selected
              ?.split("/")
              .slice(0, -1)
              .map((_, index, parts) => parts.slice(0, index + 1).join("/")),
          ),
    );
  }, [detail?.skillName]);
  useEffect(() => {
    if (detail?.preview?.path) setSelectedPath(detail.preview.path);
  }, [detail?.preview?.path]);
  if (!detail && props.inspectState.status === "loading")
    return (
      <State
        loading
        title="Loading Skill"
        detail="Reading the local package."
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
  if (!detail) return null;
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
          <Text numberOfLines={1} style={{ color: colors.textTertiary }}>
            {detail.manager === "zen"
              ? "Zen-managed"
              : detail.tracked
                ? "Tracked local"
                : "Local external"}{" "}
            · {detail.scope} · {detail.enabled ? "enabled" : "disabled"}
          </Text>
        </View>
        <Pressable
          accessibilityLabel="Close inspector"
          onPress={props.onDismissInspector}
          style={styles.close}
        >
          <Ionicons name="close" size={22} color={colors.textSecondary} />
        </Pressable>
      </View>
      <ScrollView contentContainerStyle={styles.inspectorScroll}>
        {(detail.warnings ?? []).map((warning) => (
          <Text key={warning} style={{ color: colors.warning }}>
            {warning}
          </Text>
        ))}
        <Text style={[styles.sectionTitle, { color: colors.textSecondary }]}>
          Files
        </Text>
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
                props.onInspectSkill(detail.skillName, path);
              }}
            />
          ))
        ) : (
          <Text style={{ color: colors.textTertiary }}>
            This package contains no files.
          </Text>
        )}
        <View style={[styles.preview, { borderTopColor: colors.borderSubtle }]}>
          <Text style={[styles.sectionTitle, { color: colors.textPrimary }]}>
            {selectedPath || "No file selected"}
          </Text>
          {props.inspectState.status === "loading" ? (
            <View style={styles.loadingRow}>
              <ActivityIndicator size="small" color={colors.accent} />
              <Text style={{ color: colors.textSecondary }}>Loading file</Text>
            </View>
          ) : null}
          {props.inspectState.status === "error" ? (
            <State
              icon="warning-outline"
              title="File unavailable"
              detail={props.inspectState.error}
            />
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
              selectable
              onLinkPress={(event) =>
                void openSafeMarkdownUrl(event.url, (url) =>
                  Linking.openURL(url),
                )
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
                Invalid JSON; showing the original text.
              </Text>
              <Code content={preview?.content ?? ""} />
            </>
          ) : null}
          {renderer === "text" ? (
            <Code content={preview?.content ?? ""} />
          ) : null}
        </View>
        <View style={styles.lifecycle}>
          {detail.capability.canManage &&
          detail.capability.operations.includes("update") ? (
            <Action
              label="Update"
              onPress={() =>
                props.onUpdate(
                  props.installedSkills.find(
                    (item) => item.name === detail.skillName,
                  )!,
                )
              }
            />
          ) : null}
          {detail.capability.canManage &&
          detail.capability.operations.includes("adopt") ? (
            <Action
              label="Adopt"
              onPress={() =>
                props.onAdopt(
                  props.installedSkills.find(
                    (item) => item.name === detail.skillName,
                  )!,
                )
              }
            />
          ) : null}
          {detail.capability.canManage &&
          detail.capability.operations.includes("forget") ? (
            <Action
              label="Forget"
              onPress={() =>
                props.onForget(
                  props.installedSkills.find(
                    (item) => item.name === detail.skillName,
                  )!,
                )
              }
            />
          ) : null}
          {detail.capability.canManage &&
          detail.capability.operations.includes("uninstall") ? (
            <Action
              label="Uninstall"
              destructive
              onPress={() =>
                props.onUninstall(
                  props.installedSkills.find(
                    (item) => item.name === detail.skillName,
                  )!,
                )
              }
            />
          ) : null}
        </View>
      </ScrollView>
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
  onPress,
}: {
  label: string;
  destructive?: boolean;
  onPress(): void;
}) {
  const colors = useAppColors();
  return (
    <Pressable
      onPress={onPress}
      style={[
        styles.action,
        { borderColor: destructive ? colors.dangerText : colors.borderSubtle },
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

const styles = StyleSheet.create({
  root: { flex: 1, flexDirection: "row" },
  safe: { flex: 1 },
  flex: { flex: 1 },
  tabs: {
    height: 50,
    flexDirection: "row",
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  tab: {
    flex: 1,
    flexDirection: "row",
    gap: 8,
    alignItems: "center",
    justifyContent: "center",
    borderBottomWidth: 2,
    borderBottomColor: "transparent",
  },
  notice: { marginHorizontal: 12, marginTop: 8, padding: 10 },
  agents: { paddingHorizontal: 12, paddingVertical: 8, gap: 6 },
  pill: {
    height: 36,
    paddingHorizontal: 12,
    borderWidth: 1,
    justifyContent: "center",
  },
  filters: { paddingHorizontal: 12, paddingBottom: 8, gap: 8 },
  search: {
    height: 42,
    borderWidth: 1,
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 10,
    gap: 8,
  },
  input: { flex: 1, fontFamily: Typography.uiFont, paddingVertical: 0 },
  segment: { flexDirection: "row", borderWidth: 1, alignSelf: "flex-start" },
  segmentItem: {
    minHeight: 34,
    justifyContent: "center",
    paddingHorizontal: 12,
  },
  list: { paddingBottom: 28 },
  emptyList: { flexGrow: 1 },
  row: {
    minHeight: 68,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 14,
    paddingVertical: 10,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  rowTitle: { ...TypeScale.body, fontFamily: Typography.uiFontMedium },
  iconAction: {
    minWidth: 44,
    minHeight: 44,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 6,
  },
  panel: {
    width: 440,
    maxWidth: "46%",
    borderLeftWidth: StyleSheet.hairlineWidth,
  },
  sheet: { height: "100%", minHeight: 520 },
  inspector: { flex: 1 },
  inspectorHeader: {
    minHeight: 64,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 16,
    paddingVertical: 10,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  inspectorTitle: { ...TypeScale.title, fontFamily: Typography.uiFontMedium },
  close: {
    width: 44,
    height: 44,
    alignItems: "center",
    justifyContent: "center",
  },
  inspectorScroll: { padding: 14, gap: 8, paddingBottom: 34 },
  sectionTitle: {
    ...TypeScale.compact,
    fontFamily: Typography.uiFontMedium,
    marginTop: 6,
  },
  treeRow: {
    minHeight: 36,
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
    paddingRight: 8,
  },
  preview: {
    marginTop: 10,
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingTop: 10,
    minHeight: 180,
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
  lifecycle: { flexDirection: "row", flexWrap: "wrap", gap: 8, marginTop: 10 },
  action: {
    minHeight: 40,
    borderWidth: 1,
    paddingHorizontal: 14,
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
