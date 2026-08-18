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

  const refreshPlugins = useCallback(async () => {
    const requestServerId = serverId;
    if (currentServerId.current !== requestServerId) return;
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
      if (
        pluginsGeneration.current !== generation ||
        currentServerId.current !== requestServerId
      )
        return;
      setPluginsState((current) =>
        completeSkillsRequest(
          current,
          generation,
          response.inventory,
          response.inventory.installed.length === 0,
        ),
      );
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
  useEffect(() => {
    if (
      surface.section === "plugins" &&
      automaticPlugins.current.shouldRefresh(focusGeneration, serverId)
    )
      void refreshPlugins();
  }, [focusGeneration, refreshPlugins, serverId, surface.section]);

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
      if (!evaluatePluginUninstall(copy).supported) return;
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
        setNotice({
          kind: result.execution.success ? "success" : "error",
          message: result.execution.success
            ? `Uninstalled ${command.displayName || command.name}.`
            : result.execution.output || "The Plugin could not be uninstalled.",
        });
        if (result.execution.success) await refreshPlugins();
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
  const logicalSkills = groupLogicalSkills(inventory?.skills ?? []);
  const plugins = skillsRequestData(pluginsState);
  const logicalPlugins = groupLogicalPlugins(plugins?.installed ?? []);
  return (
    <SkillsPresentation
      section={surface.section}
      inventoryState={inventoryState}
      logicalSkills={logicalSkills}
      pluginsState={pluginsState}
      logicalPlugins={logicalPlugins}
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
      onDeleteSkill={(skill) => void runSkillDelete(skill)}
      onUninstallPlugin={(copy: InstalledPluginCopy) =>
        void runPluginUninstall(copy)
      }
      onDismissNotice={() => setNotice(null)}
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
