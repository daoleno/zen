import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Alert } from "react-native";
import { useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import {
  SkillsPresentation,
  type SurfaceMutationNotice,
} from "../components/skills/SkillsPresentation";
import {
  beginSkillsRequest,
  buildSkillsMutationConfirmation,
  completeSkillsRequest,
  createSkillsRequestState,
  failSkillsRequest,
  skillsRequestData,
  type InstalledSkill,
  type ManagedSkillAgent,
  type PackageDetail,
  type SkillMutationOperation,
  type SkillsInventory,
  type SkillsRequestState,
} from "../services/skillsManagement";
import {
  buildPluginMutationConfirmation,
  type AvailablePlugin,
  type InstalledPluginRow,
  type PluginInventory,
  type PluginMutationOperation,
} from "../services/pluginsManagement";
import {
  evaluatePluginMutation,
  pluginsUnifiedView,
} from "../services/pluginsScreenModel";
import {
  SkillsAutomaticInventoryOwner,
  groupLogicalSkills,
} from "../services/skillsScreenModel";
import {
  createSkillsSurfaceState,
  reduceSkillsSurface,
  type SkillsSurfaceSection,
} from "../services/skillsSurfaceModel";
import { wsClient } from "../services/websocket";
import { useAgents } from "../store/agents";
import { useCurrentServer } from "../store/currentServer";

export default function SkillsScreen() {
  const params = useLocalSearchParams<{ demo?: string | string[] }>();
  const router = useRouter();
  const { state } = useAgents();
  const { currentServer } = useCurrentServer();
  const serverId = currentServer?.id ?? null;
  const connected = Boolean(
    serverId && state.serverConnections[serverId] === "connected",
  );
  const [surface, setSurface] = useState(createSkillsSurfaceState);
  const [inventoryState, setInventoryState] = useState<
    SkillsRequestState<SkillsInventory>
  >(createSkillsRequestState);
  const [pluginsState, setPluginsState] = useState<
    SkillsRequestState<PluginInventory>
  >(createSkillsRequestState);
  const [inspectState, setInspectState] = useState<
    SkillsRequestState<PackageDetail>
  >(createSkillsRequestState);
  const [inspectedName, setInspectedName] = useState<string | null>(null);
  const [inspectedCopyId, setInspectedCopyId] = useState<string | null>(null);
  const [preparingMutation, setPreparingMutation] = useState("");
  const [notice, setNotice] = useState<SurfaceMutationNotice | null>(null);
  const [focusGeneration, setFocusGeneration] = useState(0);
  const inventoryGeneration = useRef(0);
  const pluginsGeneration = useRef(0);
  const inspectGeneration = useRef(0);
  const automaticInventory = useRef(new SkillsAutomaticInventoryOwner());
  const automaticPlugins = useRef(new SkillsAutomaticInventoryOwner());

  const projectCwd = useMemo(
    () =>
      serverId
        ? state.agents
            .filter((item) => item.serverId === serverId && item.cwd?.trim())
            .sort((a, b) => (b.updated_at || 0) - (a.updated_at || 0))[0]
            ?.cwd?.trim() || ""
        : "",
    [serverId, state.agents],
  );

  useFocusEffect(
    useCallback(() => {
      setFocusGeneration((value) => value + 1);
    }, []),
  );
  useEffect(() => {
    inventoryGeneration.current += 1;
    pluginsGeneration.current += 1;
    inspectGeneration.current += 1;
    setInventoryState(createSkillsRequestState());
    setPluginsState(createSkillsRequestState());
    setInspectState(createSkillsRequestState());
    setInspectedName(null);
    setInspectedCopyId(null);
    setNotice(null);
  }, [serverId]);

  const refreshInventory = useCallback(async () => {
    const generation = ++inventoryGeneration.current;
    setInventoryState((current) => beginSkillsRequest(current, generation));
    if (!serverId || !currentServer) {
      setInventoryState((current) =>
        failSkillsRequest(
          current,
          generation,
          "Choose a current server in Settings to view local Skills.",
          false,
        ),
      );
      return;
    }
    if (!connected) {
      setInventoryState((current) =>
        failSkillsRequest(
          current,
          generation,
          "The current server is offline. Connect it in Settings and retry.",
          false,
        ),
      );
      return;
    }
    try {
      const response = await wsClient.getSkillsInventory(serverId, {
        generation,
        cwd: projectCwd || undefined,
      });
      if (inventoryGeneration.current !== generation) return;
      setInventoryState((current) =>
        completeSkillsRequest(
          current,
          generation,
          response.inventory,
          response.inventory.skills.length === 0,
        ),
      );
    } catch (error) {
      if (inventoryGeneration.current === generation)
        setInventoryState((current) =>
          failSkillsRequest(
            current,
            generation,
            error instanceof Error
              ? error.message
              : "Failed to read local Skills.",
          ),
        );
    }
  }, [connected, currentServer, projectCwd, serverId]);

  const refreshPlugins = useCallback(async () => {
    const generation = ++pluginsGeneration.current;
    setPluginsState((current) => beginSkillsRequest(current, generation));
    if (!serverId || !connected) {
      setPluginsState((current) =>
        failSkillsRequest(
          current,
          generation,
          "Connect the current server to manage Plugins.",
          false,
        ),
      );
      return;
    }
    try {
      const response = await wsClient.getPluginsInventory(serverId, {
        generation,
      });
      if (pluginsGeneration.current !== generation) return;
      setPluginsState((current) =>
        completeSkillsRequest(
          current,
          generation,
          response.inventory,
          response.inventory.installed.length === 0 &&
            (response.inventory.catalog.status !== "ready" ||
              response.inventory.catalog.available.length === 0),
        ),
      );
    } catch (error) {
      if (pluginsGeneration.current === generation)
        setPluginsState((current) =>
          failSkillsRequest(
            current,
            generation,
            error instanceof Error ? error.message : "Failed to load Plugins.",
          ),
        );
    }
  }, [connected, serverId]);

  useEffect(() => {
    if (automaticInventory.current.shouldRefresh(focusGeneration, serverId))
      void refreshInventory();
  }, [focusGeneration, refreshInventory, serverId]);
  useEffect(() => {
    if (
      surface.section === "plugins" &&
      automaticPlugins.current.shouldRefresh(focusGeneration, serverId)
    )
      void refreshPlugins();
  }, [focusGeneration, refreshPlugins, serverId, surface.section]);

  const inspectSkill = useCallback(
    async (skill: InstalledSkill, path?: string) => {
      const generation = ++inspectGeneration.current;
      setInspectedName(skill.name);
      setInspectedCopyId(skill.id);
      setInspectState((current) => beginSkillsRequest(current, generation));
      if (!serverId || !connected) {
        setInspectState((current) =>
          failSkillsRequest(
            current,
            generation,
            "Connect the current server to inspect this Skill.",
            false,
          ),
        );
        return;
      }
      try {
        const response = await wsClient.getSkillsInspect(serverId, {
          skillName: skill.name,
          skillId: skill.id,
          path,
          generation,
          cwd: projectCwd || undefined,
        });
        if (inspectGeneration.current !== generation) return;
        setInspectState((current) =>
          completeSkillsRequest(current, generation, response.detail, false),
        );
      } catch (error) {
        if (inspectGeneration.current === generation)
          setInspectState((current) =>
            failSkillsRequest(
              current,
              generation,
              error instanceof Error
                ? error.message
                : "Could not inspect this Skill.",
            ),
          );
      }
    },
    [connected, projectCwd, serverId],
  );

  const dismissInspector = useCallback(() => {
    inspectGeneration.current += 1;
    setInspectedName(null);
    setInspectedCopyId(null);
    setInspectState(createSkillsRequestState());
  }, []);

  const runSkillMutation = useCallback(
    async (
      skill: InstalledSkill | undefined,
      operation: SkillMutationOperation,
      agentTarget?: ManagedSkillAgent,
      scope: "project" | "global" = "global",
    ) => {
      if (!serverId || !connected || preparingMutation) return;
      const key = `${operation}:${skill?.id ?? "inventory"}`;
      setPreparingMutation(key);
      try {
        const input = {
          operation,
          cwd: projectCwd || undefined,
          skillName: skill?.name,
          scope,
          agents: agentTarget ? [agentTarget] : undefined,
          path: operation === "adopt" ? skill?.sourcePath : undefined,
        };
        const command = await wsClient.buildSkillsCommand(serverId, input);
        if (
          !(await confirm(
            buildSkillsMutationConfirmation(command),
            command.destructive,
          ))
        )
          return;
        const result = await wsClient.executeSkillsMutation(serverId, input);
        setNotice({
          kind: result.execution.success ? "success" : "error",
          message: result.execution.success
            ? `${command.summary} completed.`
            : result.execution.output || "The Skills operation failed.",
        });
        if (result.execution.success) {
          await refreshInventory();
          if (skill && inspectedCopyId === skill.id) void inspectSkill(skill);
        }
      } catch (error) {
        setNotice({
          kind: "error",
          message:
            error instanceof Error
              ? error.message
              : "The Skills operation failed.",
        });
      } finally {
        setPreparingMutation("");
      }
    },
    [
      connected,
      inspectSkill,
      inspectedName,
      inspectedCopyId,
      preparingMutation,
      projectCwd,
      refreshInventory,
      serverId,
    ],
  );

  const runPluginMutation = useCallback(
    async (operation: PluginMutationOperation, pluginId: string) => {
      if (!serverId || !connected || preparingMutation) return;
      setPreparingMutation(`${operation}:${pluginId}`);
      try {
        const command = await wsClient.buildPluginCommand(serverId, {
          operation,
          pluginId,
          scope: "user",
        });
        if (
          !(await confirm(
            buildPluginMutationConfirmation(command),
            operation === "uninstall",
          ))
        )
          return;
        const result = await wsClient.executePluginMutation(serverId, {
          operation,
          pluginId,
          scope: "user",
        });
        setNotice({
          kind: result.execution.success ? "success" : "error",
          message: result.execution.success
            ? `${command.pluginId} ${operation} completed.`
            : result.execution.output || "The Plugin operation failed.",
        });
        if (result.execution.success) await refreshPlugins();
      } catch (error) {
        setNotice({
          kind: "error",
          message:
            error instanceof Error
              ? error.message
              : "The Plugin operation failed.",
        });
      } finally {
        setPreparingMutation("");
      }
    },
    [connected, preparingMutation, refreshPlugins, serverId],
  );

  const inventory = skillsRequestData(inventoryState);
  const logicalSkills = groupLogicalSkills(inventory?.skills ?? []);
  const plugins = skillsRequestData(pluginsState);
  const demoRequested =
    __DEV__ &&
    (Array.isArray(params.demo) ? params.demo[0] : params.demo) === "1";
  if (demoRequested) return <SkillsProductDemo />;
  return (
    <SkillsPresentation
      section={surface.section}
      inventoryState={inventoryState}
      logicalSkills={logicalSkills}
      pluginsState={pluginsState}
      pluginsView={pluginsUnifiedView(plugins)}
      mutationOperations={inventory?.mutationOperations ?? []}
      preparingMutation={preparingMutation}
      mutationNotice={notice}
      currentServerAvailable={Boolean(currentServer)}
      inspectedName={inspectedName}
      inspectedCopyId={inspectedCopyId}
      inspectState={inspectState}
      onSelectSection={(section: SkillsSurfaceSection) =>
        setSurface((current) =>
          reduceSkillsSurface(current, { type: "select_section", section }),
        )
      }
      onOpenSettings={() => router.push("/settings")}
      onRefreshSkills={() => void refreshInventory()}
      onRetryPlugins={() => void refreshPlugins()}
      onInspectSkill={(skill, path) => void inspectSkill(skill, path)}
      onDismissInspector={dismissInspector}
      onBinding={(skill, operation, target, scope) =>
        void runSkillMutation(skill, operation, target, scope)
      }
      onUninstall={(skill) => void runSkillMutation(skill, "uninstall")}
      onForget={(skill) => void runSkillMutation(skill, "forget")}
      onAdopt={(skill) => void runSkillMutation(skill, "adopt")}
      onUpdate={(skill) => void runSkillMutation(skill, "update")}
      onInstallPlugin={(entry: AvailablePlugin) => {
        const decision = evaluatePluginMutation({
          kind: "install",
          entry,
          installedIds: new Set(
            plugins?.installed.map((item) => item.id) ?? [],
          ),
        });
        if (decision.supported)
          void runPluginMutation("install", entry.pluginId);
      }}
      onUpdatePlugin={(row: InstalledPluginRow) => {
        if (evaluatePluginMutation({ kind: "update", row }).supported)
          void runPluginMutation("update", row.id);
      }}
      onUninstallPlugin={(row: InstalledPluginRow) => {
        if (evaluatePluginMutation({ kind: "uninstall", row }).supported)
          void runPluginMutation("uninstall", row.id);
      }}
      onDismissNotice={() => setNotice(null)}
    />
  );
}

const DEMO_SKILLS: InstalledSkill[] = [
  demoSkill(
    "imagegen",
    "a".repeat(24),
    "codex",
    "Create and edit raster images for product and creative work.",
    "1".repeat(64),
  ),
  demoSkill(
    "imagegen",
    "b".repeat(24),
    "pi",
    "Generate images through the configured provider.",
    "2".repeat(64),
  ),
  demoSkill(
    "openai-docs",
    "c".repeat(24),
    "codex",
    "Answer questions using current OpenAI product documentation.",
    "3".repeat(64),
  ),
  demoSkill(
    "hatch-pet",
    "d".repeat(24),
    "codex",
    "Create and validate animated pet sprite packages.",
    "4".repeat(64),
  ),
  demoSkill(
    "x-growth",
    "e".repeat(24),
    "claude-code",
    "Plan technical content and publishing experiments.",
    "5".repeat(64),
  ),
];

function demoSkill(
  name: string,
  id: string,
  agent: ManagedSkillAgent,
  description: string,
  contentHash: string,
): InstalledSkill {
  return {
    id,
    name,
    description,
    manager: "external",
    owned: false,
    tracked: false,
    enabled: true,
    canonicalPath: `/demo/${agent}/${name}`,
    sourcePath: `/home/demo/.${agent}/skills/${name}`,
    scope: "global",
    agents: [agent],
    bindings: [],
    provenance: `${agent} global Skills`,
    contentHash,
    migration: name === "imagegen" ? "conflict" : "external",
    capability: { canManage: true, operations: ["adopt"] },
  };
}

function SkillsProductDemo() {
  const [selected, setSelected] = useState<InstalledSkill | null>(null);
  const inventory: SkillsInventory = {
    generatedAt: "2026-08-18T00:00:00Z",
    skills: DEMO_SKILLS,
    agents: [],
    warnings: [],
    mutationOperations: [
      "adopt",
      "bind",
      "unbind",
      "enable",
      "disable",
      "uninstall",
      "forget",
      "update",
    ],
    migration: { owned: 0, external: 5, duplicate: 1, conflict: 1, tracked: 0 },
  };
  const detail: PackageDetail | undefined = selected
    ? {
        copyId: selected.id,
        skillName: selected.name,
        description: selected.description,
        manager: selected.manager,
        owned: selected.owned,
        tracked: selected.tracked,
        enabled: selected.enabled,
        sourcePath: selected.sourcePath,
        scope: selected.scope,
        agents: selected.agents,
        bindings: selected.bindings,
        files: [
          {
            path: "SKILL.md",
            size: 180,
            mode: "0600",
            kind: "markdown",
            mediaType: "text/markdown",
            previewStatus: "ready",
          },
          {
            path: "references/provider.json",
            size: 48,
            mode: "0600",
            kind: "json",
            mediaType: "application/json",
            previewStatus: "ready",
          },
        ],
        preview: {
          path: "SKILL.md",
          kind: "markdown",
          mediaType: "text/markdown",
          status: "ready",
          size: 180,
          bytesReturned: 180,
          content: `# ${selected.name}\n\n${selected.description}`,
        },
        capability: selected.capability,
      }
    : undefined;
  return (
    <SkillsPresentation
      section="skills"
      inventoryState={{ status: "ready", generation: 1, data: inventory }}
      logicalSkills={groupLogicalSkills(DEMO_SKILLS)}
      pluginsState={createSkillsRequestState()}
      pluginsView={pluginsUnifiedView(undefined)}
      mutationOperations={inventory.mutationOperations}
      preparingMutation=""
      mutationNotice={null}
      currentServerAvailable
      inspectedName={selected?.name ?? null}
      inspectedCopyId={selected?.id ?? null}
      inspectState={
        detail
          ? { status: "ready", generation: 1, data: detail }
          : createSkillsRequestState()
      }
      onSelectSection={() => undefined}
      onOpenSettings={() => undefined}
      onRefreshSkills={() => undefined}
      onRetryPlugins={() => undefined}
      onInspectSkill={(skill) => setSelected(skill)}
      onDismissInspector={() => setSelected(null)}
      onBinding={() => undefined}
      onUninstall={() => undefined}
      onForget={() => undefined}
      onAdopt={() => undefined}
      onUpdate={() => undefined}
      onInstallPlugin={() => undefined}
      onUpdatePlugin={() => undefined}
      onUninstallPlugin={() => undefined}
      onDismissNotice={() => undefined}
    />
  );
}

function confirm(
  options: { title: string; message: string; confirmLabel: string },
  destructive: boolean,
): Promise<boolean> {
  return new Promise((resolve) =>
    Alert.alert(options.title, options.message, [
      { text: "Cancel", style: "cancel", onPress: () => resolve(false) },
      {
        text: options.confirmLabel,
        style: destructive ? "destructive" : "default",
        onPress: () => resolve(true),
      },
    ]),
  );
}
