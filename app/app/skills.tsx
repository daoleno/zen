import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Alert } from "react-native";
import { useFocusEffect, useRouter } from "expo-router";
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
  type PackageDetail,
  type SkillsInventory,
  type SkillsRequestState,
} from "../services/skillsManagement";
import {
  buildPluginMutationConfirmation,
  pluginUninstallInput,
  type InstalledPluginCopy,
  type PluginInventory,
} from "../services/pluginsManagement";
import {
  evaluatePluginUninstall,
  groupLogicalPlugins,
} from "../services/pluginsScreenModel";
import {
  SkillsAutomaticInventoryOwner,
  groupLogicalSkills,
} from "../services/skillsScreenModel";
import { skillsOutsidePlugins } from "../services/skillsPluginOwnership";
import {
  createSkillsSurfaceState,
  reduceSkillsSurface,
  type SkillsSurfaceSection,
} from "../services/skillsSurfaceModel";
import { wsClient } from "../services/websocket";
import { useAgents } from "../store/agents";
import { useCurrentServer } from "../store/currentServer";

export default function SkillsScreen() {
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
  const [focusedPluginKey, setFocusedPluginKey] = useState<string | null>(null);
  const inventoryGeneration = useRef(0);
  const pluginsGeneration = useRef(0);
  const inspectGeneration = useRef(0);
  const mutationOwner = useRef(0);
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
  const skillsContextKey = `${serverId ?? "none"}\u0000${projectCwd}`;
  const currentSkillsContext = useRef(skillsContextKey);
  const currentServerId = useRef(serverId);
  currentSkillsContext.current = skillsContextKey;
  currentServerId.current = serverId;

  useFocusEffect(
    useCallback(() => {
      setFocusGeneration((value) => value + 1);
    }, []),
  );
  useEffect(() => {
    inventoryGeneration.current += 1;
    pluginsGeneration.current += 1;
    inspectGeneration.current += 1;
    mutationOwner.current += 1;
    setInventoryState(createSkillsRequestState());
    setPluginsState(createSkillsRequestState());
    setInspectState(createSkillsRequestState());
    setInspectedName(null);
    setInspectedCopyId(null);
    setPreparingMutation("");
    setNotice(null);
    setFocusedPluginKey(null);
  }, [serverId]);
  useEffect(() => {
    inventoryGeneration.current += 1;
    inspectGeneration.current += 1;
    setInventoryState(createSkillsRequestState());
    setInspectState(createSkillsRequestState());
    setInspectedName(null);
    setInspectedCopyId(null);
    setNotice(null);
  }, [projectCwd]);
  useEffect(() => {
    if (notice?.kind !== "success") return;
    const timer = setTimeout(() => setNotice(null), 3200);
    return () => clearTimeout(timer);
  }, [notice]);

  const refreshInventory = useCallback(async () => {
    const requestContext = skillsContextKey;
    if (currentSkillsContext.current !== requestContext) return;
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
      if (
        inventoryGeneration.current !== generation ||
        currentSkillsContext.current !== requestContext
      )
        return;
      setInventoryState((current) =>
        completeSkillsRequest(
          current,
          generation,
          response.inventory,
          response.inventory.skills.length === 0,
        ),
      );
    } catch (error) {
      if (
        inventoryGeneration.current === generation &&
        currentSkillsContext.current === requestContext
      )
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
  }, [connected, currentServer, projectCwd, serverId, skillsContextKey]);

  const refreshPlugins = useCallback(async (): Promise<
    PluginInventory | undefined
  > => {
    const requestServerId = serverId;
    if (currentServerId.current !== requestServerId) return undefined;
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
      return undefined;
    }
    try {
      const response = await wsClient.getPluginsInventory(serverId, {
        generation,
      });
      if (
        pluginsGeneration.current !== generation ||
        currentServerId.current !== requestServerId
      )
        return undefined;
      setPluginsState((current) =>
        completeSkillsRequest(
          current,
          generation,
          response.inventory,
          response.inventory.installed.length === 0,
        ),
      );
      return response.inventory;
    } catch (error) {
      if (
        pluginsGeneration.current === generation &&
        currentServerId.current === requestServerId
      )
        setPluginsState((current) =>
          failSkillsRequest(
            current,
            generation,
            error instanceof Error ? error.message : "Failed to load Plugins.",
          ),
        );
      return undefined;
    }
  }, [connected, serverId]);

  useEffect(() => {
    if (
      automaticInventory.current.shouldRefresh(
        focusGeneration,
        skillsContextKey,
      )
    )
      void refreshInventory();
  }, [focusGeneration, refreshInventory, skillsContextKey]);
  // Plugin ownership must be known on the Skills tab too, so the Plugins
  // inventory loads with the same focus/server cadence as Skills instead of
  // waiting for the Plugins tab to be opened.
  useEffect(() => {
    if (automaticPlugins.current.shouldRefresh(focusGeneration, serverId))
      void refreshPlugins();
  }, [focusGeneration, refreshPlugins, serverId]);

  const inspectSkill = useCallback(
    async (skill: InstalledSkill, path?: string) => {
      const requestContext = skillsContextKey;
      if (currentSkillsContext.current !== requestContext) return;
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
        if (
          inspectGeneration.current !== generation ||
          currentSkillsContext.current !== requestContext
        )
          return;
        if (
          response.detail.copyId !== skill.id ||
          response.detail.skillName !== skill.name
        ) {
          throw new Error(
            "The selected Skill copy changed while it was loading.",
          );
        }
        setInspectState((current) =>
          completeSkillsRequest(current, generation, response.detail, false),
        );
      } catch (error) {
        if (
          inspectGeneration.current === generation &&
          currentSkillsContext.current === requestContext
        )
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
    [connected, projectCwd, serverId, skillsContextKey],
  );

  const dismissInspector = useCallback(() => {
    inspectGeneration.current += 1;
    setInspectedName(null);
    setInspectedCopyId(null);
    setInspectState(createSkillsRequestState());
  }, []);

  /**
   * Read-only detail fetch for the Plugin inspector's Skills directory. It
   * never touches the Skills tab inspector state; Plugin-owned copies are
   * inspected in place inside their owning Plugin.
   */
  const pluginInspectGeneration = useRef(0);
  const inspectSkillCopyDetail = useCallback(
    async (copy: InstalledSkill, path?: string) => {
      if (!serverId || !connected)
        throw new Error(
          "Connect the current server to inspect this Skill.",
        );
      const response = await wsClient.getSkillsInspect(serverId, {
        skillName: copy.name,
        skillId: copy.id,
        path,
        generation: ++pluginInspectGeneration.current,
        cwd: projectCwd || undefined,
      });
      if (
        response.detail.copyId !== copy.id ||
        response.detail.skillName !== copy.name
      )
        throw new Error("The selected Skill copy changed while it was loading.");
      return response.detail;
    },
    [connected, projectCwd, serverId],
  );

  const runSkillDelete = useCallback(
    async (skill: InstalledSkill) => {
      if (!serverId || !connected || preparingMutation) return;
      if (!skill.capability.canDelete) return;
      const requestContext = skillsContextKey;
      if (currentSkillsContext.current !== requestContext) return;
      const owner = ++mutationOwner.current;
      const key = `delete:${skill.id}`;
      setNotice(null);
      setPreparingMutation(key);
      try {
        const input = {
          operation: "delete" as const,
          cwd: projectCwd || undefined,
          skillId: skill.id,
          skillName: skill.name,
          rootPath: skill.rootPath,
          canonicalPath: skill.canonicalPath,
          allowedRoot: skill.allowedRoot,
        };
        const command = await wsClient.buildSkillsCommand(serverId, input);
        if (currentSkillsContext.current !== requestContext) return;
        const approved = await confirm(
          buildSkillsMutationConfirmation(command),
          command.destructive,
        );
        if (
          !approved ||
          currentSkillsContext.current !== requestContext
        )
          return;
        const result = await wsClient.executeSkillsMutation(serverId, input);
        if (currentSkillsContext.current !== requestContext) return;
        setNotice({
          kind: result.execution.success ? "success" : "error",
          message: result.execution.success
            ? `Deleted ${skill.name}.`
            : result.execution.output || "The Skill could not be deleted.",
        });
        if (result.execution.success) {
          await refreshInventory();
          if (currentSkillsContext.current !== requestContext) return;
          if (inspectedCopyId === skill.id) dismissInspector();
        }
      } catch (error) {
        if (currentSkillsContext.current === requestContext)
          setNotice({
            kind: "error",
            message:
              error instanceof Error
                ? error.message
                : "The Skill could not be deleted.",
          });
      } finally {
        if (mutationOwner.current === owner) setPreparingMutation("");
      }
    },
    [
      connected,
      dismissInspector,
      inspectedCopyId,
      preparingMutation,
      projectCwd,
      refreshInventory,
      serverId,
      skillsContextKey,
    ],
  );

  const runPluginUninstall = useCallback(
    async (copy: InstalledPluginCopy) => {
      if (!serverId || !connected || preparingMutation) return;
      const evaluation = evaluatePluginUninstall(copy);
      if (!evaluation.supported) {
        // The daemon reports this copy as protected; never pretend to remove it.
        setNotice({ kind: "error", message: evaluation.reason });
        return;
      }
      const requestServerId = serverId;
      if (currentServerId.current !== requestServerId) return;
      const owner = ++mutationOwner.current;
      const input = pluginUninstallInput(copy);
      setNotice(null);
      setPreparingMutation(`uninstall:${copy.copyId}`);
      try {
        const command = await wsClient.buildPluginCommand(serverId, input);
        if (currentServerId.current !== requestServerId) return;
        const approved = await confirm(
          buildPluginMutationConfirmation(command),
          command.destructive,
        );
        if (!approved || currentServerId.current !== requestServerId)
          return;
        const result = await wsClient.executePluginMutation(serverId, input);
        if (currentServerId.current !== requestServerId) return;
        if (!result.execution.success) {
          setNotice({
            kind: "error",
            message:
              result.execution.output ||
              "The Plugin could not be uninstalled.",
          });
          return;
        }
        // Report what actually remains instead of implying the whole Plugin
        // vanished: other copies (including protected ones) may still exist.
        const refreshed = await refreshPlugins();
        if (currentServerId.current !== requestServerId) return;
        const remaining = (refreshed?.installed ?? []).filter(
          (candidate) => candidate.name === copy.name,
        ).length;
        setNotice({
          kind: "success",
          message: remaining
            ? `Uninstalled the ${command.displayName || command.name} copy from ${copy.location}. ${remaining} ${remaining === 1 ? "copy remains" : "copies remain"}.`
            : `Uninstalled ${command.displayName || command.name}.`,
        });
      } catch (error) {
        if (currentServerId.current === requestServerId)
          setNotice({
            kind: "error",
            message:
              error instanceof Error
                ? error.message
                : "The Plugin could not be uninstalled.",
          });
      } finally {
        if (mutationOwner.current === owner) setPreparingMutation("");
      }
    },
    [connected, preparingMutation, refreshPlugins, serverId],
  );

  const inventory = skillsRequestData(inventoryState);
  const plugins = skillsRequestData(pluginsState);
  const logicalPlugins = groupLogicalPlugins(plugins?.installed ?? []);
  // Plugin-owned copies are presented once, inside their owning Plugin's
  // expandable Skills directory. Exclusion waits for a completed Plugins read
  // so ownership is never guessed from a partially loaded inventory.
  const pluginsResolved =
    pluginsState.status === "ready" || pluginsState.status === "empty";
  const listableSkills = useMemo(
    () =>
      pluginsResolved
        ? skillsOutsidePlugins(inventory?.skills ?? [], logicalPlugins)
        : inventory?.skills ?? [],
    [inventory?.skills, logicalPlugins, pluginsResolved],
  );
  const pluginOwnedSkillCount =
    (inventory?.skills.length ?? 0) - listableSkills.length;
  const logicalSkills = groupLogicalSkills(listableSkills);
  return (
    <SkillsPresentation
      section={surface.section}
      inventoryState={inventoryState}
      logicalSkills={logicalSkills}
      pluginsState={pluginsState}
      logicalPlugins={logicalPlugins}
      skills={inventory?.skills ?? []}
      mutationOperations={inventory?.mutationOperations ?? []}
      preparingMutation={preparingMutation}
      mutationNotice={notice}
      currentServerAvailable={Boolean(currentServer)}
      inspectedName={inspectedName}
      inspectedCopyId={inspectedCopyId}
      inspectState={inspectState}
      pluginOwnedSkillCount={pluginOwnedSkillCount}
      onSelectSection={(section: SkillsSurfaceSection) =>
        setSurface((current) =>
          reduceSkillsSurface(current, { type: "select_section", section }),
        )
      }
      onOpenSettings={() => router.push("/settings")}
      onRefreshSkills={() => void refreshInventory()}
      onRetryPlugins={() => void refreshPlugins()}
      onInspectSkill={(skill, path) => void inspectSkill(skill, path)}
      onInspectSkillCopy={inspectSkillCopyDetail}
      onDismissInspector={dismissInspector}
      onDeleteSkill={(skill) => void runSkillDelete(skill)}
      onUninstallPlugin={(copy: InstalledPluginCopy) =>
        void runPluginUninstall(copy)
      }
      onDismissNotice={() => setNotice(null)}
      onViewSkillPlugin={(pluginKey) => {
        setFocusedPluginKey(pluginKey);
        setSurface((current) =>
          reduceSkillsSurface(current, {
            type: "select_section",
            section: "plugins",
          }),
        );
      }}
      focusedPluginKey={focusedPluginKey}
      onFocusPluginConsumed={() => setFocusedPluginKey(null)}
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
