import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Alert, Keyboard } from "react-native";
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
  skillAgentLabel,
  skillsRequestData,
  type CatalogSkill,
  type InstalledSkill,
  type ManagedSkillAgent,
  type PackageDetail,
  type RankedCatalogSkill,
  type SkillMutationOperation,
  type SkillsCatalogResult,
  type SkillsInventory,
  type SkillsLeaderboard,
  type SkillsLeaderboards,
  type SkillsMutationCommand,
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
  SkillsAutomaticInventoryOwner,
  catalogSkillId,
  installedSkillCatalogId,
  skillsInstallTargets,
  skillsSectionAgentCounts,
  skillsSectionProjection,
  type SkillsLeaderboardView,
} from "../services/skillsScreenModel";
import {
  evaluatePluginMutation,
  pluginsUnifiedView,
  type PluginsUnifiedView,
} from "../services/pluginsScreenModel";
import {
  createSkillsSurfaceState,
  evaluateSkillMutation,
  reduceSkillsSurface,
  type SkillsSurfaceSection,
} from "../services/skillsSurfaceModel";
import {
  SkillsServerRequestOwner,
  type SkillsServerRequestToken,
} from "../services/skillsServerBoundary";
import { DaemonRequestError, wsClient } from "../services/websocket";
import { useAgents } from "../store/agents";
import { useCurrentServer } from "../store/currentServer";

const DEFAULT_CATALOG_REF = "main";
const SKILLS_INSPECT_TIMEOUT_KEY = "skills-inspect";

interface SkillsMutationInput {
  operation: SkillMutationOperation;
  cwd?: string;
  skillId?: string;
  source?: string;
  skillName?: string;
  scope: "project" | "global";
  agents?: ManagedSkillAgent[];
  ref?: string;
  path?: string;
}

interface PluginMutationInput {
  operation: PluginMutationOperation;
  pluginId: string;
  scope: "user";
}

export default function SkillsScreen() {
  const router = useRouter();
  const { state } = useAgents();
  const { currentServer } = useCurrentServer();
  const currentServerId = currentServer?.id ?? null;
  const currentConnected = Boolean(
    currentServerId && state.serverConnections[currentServerId] === "connected",
  );
  const requestOwnerRef = useRef(new SkillsServerRequestOwner());
  requestOwnerRef.current.rebind(currentServerId);
  const automaticInventoryOwnerRef = useRef(
    new SkillsAutomaticInventoryOwner(),
  );
  const automaticCatalogOwnerRef = useRef(new SkillsAutomaticInventoryOwner());
  const automaticPluginsOwnerRef = useRef(new SkillsAutomaticInventoryOwner());

  const [surface, setSurface] = useState(createSkillsSurfaceState);
  const [selectedAgent, setSelectedAgent] =
    useState<ManagedSkillAgent>("codex");
  const [boundServerId, setBoundServerId] = useState(currentServerId);
  const [focusGeneration, setFocusGeneration] = useState(0);
  const [inventoryState, setInventoryState] = useState<
    SkillsRequestState<SkillsInventory>
  >(createSkillsRequestState);
  const [pluginsState, setPluginsState] = useState<
    SkillsRequestState<PluginInventory>
  >(createSkillsRequestState);
  const [leaderboardView, setLeaderboardView] =
    useState<SkillsLeaderboardView>("all-time");
  const [catalogState, setCatalogState] = useState<
    SkillsRequestState<SkillsLeaderboards>
  >(createSkillsRequestState);
  const [searchState, setSearchState] = useState<
    SkillsRequestState<SkillsCatalogResult>
  >(createSkillsRequestState);
  const [searchQuery, setSearchQuery] = useState("");
  const [submittedQuery, setSubmittedQuery] = useState("");
  const [preparingMutation, setPreparingMutation] = useState("");
  const [mutationNotice, setMutationNotice] =
    useState<SurfaceMutationNotice | null>(null);
  const [inspectedName, setInspectedName] = useState<string | null>(null);
  const [inspectState, setInspectState] = useState<
    SkillsRequestState<PackageDetail>
  >(createSkillsRequestState);
  const activeSearchRef = useRef<{
    generation: number;
    serverId: string;
  } | null>(null);
  const focusedRef = useRef(false);

  const projectCwd = useMemo(() => {
    if (!currentServerId) {
      return "";
    }
    return (
      state.agents
        .filter(
          (agent) =>
            agent.serverId === currentServerId && Boolean(agent.cwd?.trim()),
        )
        .sort(
          (left, right) => (right.updated_at || 0) - (left.updated_at || 0),
        )[0]
        ?.cwd?.trim() || ""
    );
  }, [currentServerId, state.agents]);

  const cancelActiveSearch = useCallback(() => {
    const active = activeSearchRef.current;
    if (!active) {
      return;
    }
    activeSearchRef.current = null;
    wsClient.cancelSkillsCatalogSearch(active.serverId, {
      generation: active.generation,
    });
  }, []);

  useFocusEffect(
    useCallback(() => {
      focusedRef.current = true;
      setFocusGeneration((current) => current + 1);
      return () => {
        focusedRef.current = false;
        requestOwnerRef.current.invalidateAll();
        cancelActiveSearch();
      };
    }, [cancelActiveSearch]),
  );

  useEffect(() => {
    cancelActiveSearch();
    setBoundServerId(currentServerId);
    setInventoryState(createSkillsRequestState());
    setPluginsState(createSkillsRequestState());
    setCatalogState(createSkillsRequestState());
    setSearchState(createSkillsRequestState());
    setSearchQuery("");
    setSubmittedQuery("");
    setPreparingMutation("");
    setMutationNotice(null);
    setInspectedName(null);
    setInspectState(createSkillsRequestState());
  }, [cancelActiveSearch, currentServerId]);

  const refreshInventory = useCallback(async () => {
    const token = requestOwnerRef.current.issue("inventory");
    setInventoryState((current) =>
      beginSkillsRequest(current, token.generation),
    );
    if (!token.serverId || !currentServer) {
      setInventoryState((current) =>
        failSkillsRequest(
          current,
          token.generation,
          "Choose a current server in Settings to view installed Skills.",
        ),
      );
      return;
    }
    if (!currentConnected) {
      setInventoryState((current) =>
        failSkillsRequest(
          current,
          token.generation,
          "The current server is offline. Connect it in Settings and retry.",
        ),
      );
      return;
    }

    try {
      const response = await wsClient.getSkillsInventory(token.serverId, {
        cwd: projectCwd || undefined,
        generation: token.generation,
      });
      if (
        !focusedRef.current ||
        response.generation !== token.generation ||
        !requestOwnerRef.current.isCurrent(token)
      ) {
        return;
      }
      setInventoryState((current) =>
        completeSkillsRequest(
          current,
          token.generation,
          response.inventory,
          response.inventory.skills.length === 0,
        ),
      );
    } catch (error: unknown) {
      if (!focusedRef.current || !requestOwnerRef.current.isCurrent(token)) {
        return;
      }
      setInventoryState((current) =>
        failSkillsRequest(
          current,
          token.generation,
          error instanceof Error
            ? error.message
            : "Failed to load installed Skills.",
        ),
      );
    }
  }, [currentConnected, currentServer, projectCwd]);

  useEffect(() => {
    if (
      !automaticInventoryOwnerRef.current.shouldRefresh(
        focusGeneration,
        currentServerId,
      )
    ) {
      return;
    }
    void refreshInventory();
  }, [currentServerId, focusGeneration, projectCwd, refreshInventory]);

  const loadPlugins = useCallback(async () => {
    const token = requestOwnerRef.current.issue("plugins");
    setPluginsState((current) => beginSkillsRequest(current, token.generation));
    if (!token.serverId || !currentServer) {
      setPluginsState((current) =>
        failSkillsRequest(
          current,
          token.generation,
          "Choose a current server in Settings to view Plugins.",
        ),
      );
      return;
    }
    if (!currentConnected) {
      setPluginsState((current) =>
        failSkillsRequest(
          current,
          token.generation,
          "The current server is offline. Connect it in Settings and retry.",
        ),
      );
      return;
    }

    try {
      const response = await wsClient.getPluginsInventory(token.serverId, {
        generation: token.generation,
      });
      if (
        !focusedRef.current ||
        response.generation !== token.generation ||
        !requestOwnerRef.current.isCurrent(token)
      ) {
        return;
      }
      setPluginsState((current) =>
        completeSkillsRequest(
          current,
          token.generation,
          response.inventory,
          response.inventory.installed.length === 0 &&
            (response.inventory.catalog.status !== "ready" ||
              response.inventory.catalog.available.length === 0),
        ),
      );
    } catch (error: unknown) {
      if (!focusedRef.current || !requestOwnerRef.current.isCurrent(token)) {
        return;
      }
      if (
        error instanceof DaemonRequestError &&
        error.code === "unknown_message_type"
      ) {
        setPluginsState((current) =>
          failSkillsRequest(
            current,
            token.generation,
            "Update the Zen daemon to manage Plugins.",
            false,
          ),
        );
        return;
      }
      setPluginsState((current) =>
        failSkillsRequest(
          current,
          token.generation,
          error instanceof Error ? error.message : "Failed to load Plugins.",
        ),
      );
    }
  }, [currentConnected, currentServer]);

  useEffect(() => {
    if (
      surface.section !== "plugins" ||
      !automaticPluginsOwnerRef.current.shouldRefresh(
        focusGeneration,
        currentServerId,
      )
    ) {
      return;
    }
    void loadPlugins();
  }, [currentServerId, focusGeneration, loadPlugins, surface.section]);

  const loadLeaderboards = useCallback(async () => {
    const token = requestOwnerRef.current.issue("catalog");
    setCatalogState((current) => beginSkillsRequest(current, token.generation));
    if (!token.serverId || !currentServer) {
      setCatalogState((current) =>
        failSkillsRequest(
          current,
          token.generation,
          "Choose a current server in Settings to browse Skills.",
        ),
      );
      return;
    }
    if (!currentConnected) {
      setCatalogState((current) =>
        failSkillsRequest(
          current,
          token.generation,
          "The current server is offline. Connect it in Settings and retry.",
        ),
      );
      return;
    }

    try {
      const response = await wsClient.getSkillsLeaderboards(token.serverId, {
        generation: token.generation,
        limit: 30,
      });
      if (
        !focusedRef.current ||
        response.generation !== token.generation ||
        !requestOwnerRef.current.isCurrent(token)
      ) {
        return;
      }
      const empty =
        response.leaderboards.allTime.skills.length === 0 &&
        response.leaderboards.trending.skills.length === 0 &&
        response.leaderboards.hot.skills.length === 0;
      setCatalogState((current) =>
        completeSkillsRequest(
          current,
          token.generation,
          response.leaderboards,
          empty,
        ),
      );
    } catch (error: unknown) {
      if (!focusedRef.current || !requestOwnerRef.current.isCurrent(token)) {
        return;
      }
      setCatalogState((current) =>
        failSkillsRequest(
          current,
          token.generation,
          error instanceof Error
            ? error.message
            : "Failed to load skills.sh rankings.",
        ),
      );
    }
  }, [currentConnected, currentServer]);

  useEffect(() => {
    if (
      submittedQuery ||
      !automaticCatalogOwnerRef.current.shouldRefresh(
        focusGeneration,
        currentServerId,
      )
    ) {
      return;
    }
    void loadLeaderboards();
  }, [currentServerId, focusGeneration, loadLeaderboards, submittedQuery]);

  const runSearch = useCallback(
    async (rawQuery: string, intent: "new-query" | "same-query") => {
      cancelActiveSearch();
      const normalizedQuery = rawQuery.trim();
      if (normalizedQuery.length < 2) {
        return;
      }
      const token = requestOwnerRef.current.issue("search");
      setSearchState((current) =>
        beginSkillsRequest(current, token.generation, intent === "same-query"),
      );
      if (!token.serverId || !currentServer) {
        setSearchState((current) =>
          failSkillsRequest(
            current,
            token.generation,
            "Choose a current server in Settings before searching.",
          ),
        );
        return;
      }
      if (!currentConnected) {
        setSearchState((current) =>
          failSkillsRequest(
            current,
            token.generation,
            "The current server is offline. Connect it in Settings and retry.",
          ),
        );
        return;
      }

      activeSearchRef.current = {
        serverId: token.serverId,
        generation: token.generation,
      };
      try {
        const response = await wsClient.searchSkillsCatalog(token.serverId, {
          query: normalizedQuery,
          generation: token.generation,
          limit: 20,
        });
        if (
          !focusedRef.current ||
          response.generation !== token.generation ||
          !requestOwnerRef.current.isCurrent(token)
        ) {
          return;
        }
        setSearchState((current) =>
          completeSkillsRequest(
            current,
            token.generation,
            response.result,
            response.result.skills.length === 0,
          ),
        );
      } catch (error: unknown) {
        if (!focusedRef.current || !requestOwnerRef.current.isCurrent(token)) {
          return;
        }
        setSearchState((current) =>
          failSkillsRequest(
            current,
            token.generation,
            error instanceof Error
              ? error.message
              : "Failed to search skills.sh.",
          ),
        );
      } finally {
        if (
          activeSearchRef.current?.serverId === token.serverId &&
          activeSearchRef.current.generation === token.generation
        ) {
          activeSearchRef.current = null;
        }
      }
    },
    [cancelActiveSearch, currentConnected, currentServer],
  );

  const clearSearch = useCallback(() => {
    cancelActiveSearch();
    requestOwnerRef.current.invalidate("search");
    setSearchState(createSkillsRequestState());
  }, [cancelActiveSearch]);

  const changeSearchQuery = useCallback(
    (value: string) => {
      setSearchQuery(value);
      if (!value.trim() && submittedQuery) {
        setSubmittedQuery("");
        clearSearch();
      }
    },
    [clearSearch, submittedQuery],
  );

  const submitSearch = useCallback(() => {
    const query = searchQuery.trim();
    if (query.length < 2) {
      if (submittedQuery) {
        setSubmittedQuery("");
        clearSearch();
      }
      return;
    }
    setSubmittedQuery(query);
    void runSearch(query, "new-query");
  }, [clearSearch, runSearch, searchQuery, submittedQuery]);

  const clearSearchState = useCallback(() => {
    setSearchQuery("");
    setSubmittedQuery("");
    clearSearch();
  }, [clearSearch]);

  const selectLeaderboardView = useCallback((view: SkillsLeaderboardView) => {
    setLeaderboardView(view);
  }, []);

  const selectAgent = useCallback(
    (agent: ManagedSkillAgent) => {
      if (agent === selectedAgent) {
        return;
      }
      requestOwnerRef.current.invalidate("mutation");
      setPreparingMutation("");
      setSelectedAgent(agent);
    },
    [selectedAgent],
  );

  const selectSection = useCallback((section: SkillsSurfaceSection) => {
    setSurface((current) =>
      reduceSkillsSurface(current, { type: "select_section", section }),
    );
  }, []);

  const refreshSkills = useCallback(() => {
    void refreshInventory();
    if (submittedQuery) {
      void runSearch(submittedQuery, "same-query");
    } else {
      void loadLeaderboards();
    }
  }, [loadLeaderboards, refreshInventory, runSearch, submittedQuery]);

  /** Loads the inspector detail for one Skill name (generation-cancelable). */
  const inspectSkill = useCallback(
    async (name: string, path?: string) => {
      setInspectedName(name);
      const token = requestOwnerRef.current.issue("inspect");
      setInspectState((current) =>
        beginSkillsRequest(current, token.generation),
      );
      if (!token.serverId || !currentConnected || !currentServer) {
        setInspectState((current) =>
          failSkillsRequest(
            current,
            token.generation,
            "Connect the current server to inspect this Skill.",
          ),
        );
        return;
      }
      try {
        const response = await wsClient.getSkillsInspect(token.serverId, {
          skillName: name,
          generation: token.generation,
          cwd: projectCwd || undefined,
          path,
        });
        if (
          !focusedRef.current ||
          response.generation !== token.generation ||
          !requestOwnerRef.current.isCurrent(token)
        ) {
          return;
        }
        setInspectState((current) =>
          completeSkillsRequest(
            current,
            token.generation,
            response.detail,
            false,
          ),
        );
      } catch (error: unknown) {
        if (!requestOwnerRef.current.isCurrent(token)) {
          return;
        }
        setInspectState((current) =>
          failSkillsRequest(
            current,
            token.generation,
            error instanceof Error
              ? error.message
              : "Could not inspect this Skill.",
          ),
        );
      }
    },
    [currentConnected, currentServer, projectCwd],
  );

  const dismissInspector = useCallback(() => {
    requestOwnerRef.current.invalidate("inspect");
    setInspectedName(null);
    setInspectState(createSkillsRequestState());
  }, []);

  const executeSkillsMutation = useCallback(
    async (
      token: SkillsServerRequestToken,
      input: SkillsMutationInput,
      key: string,
      confirmation: { title: string; message: string; confirmLabel: string },
      destructive: boolean,
    ) => {
      if (!requestOwnerRef.current.isCurrent(token) || !token.serverId) {
        return;
      }
      const confirmed = await confirmMutation(confirmation, destructive);
      if (!confirmed || !requestOwnerRef.current.isCurrent(token)) {
        return;
      }
      setPreparingMutation(key);
      setMutationNotice(null);
      try {
        const result = await wsClient.executeSkillsMutation(token.serverId, {
          operation: input.operation,
          cwd: input.cwd,
          skillId: input.skillId,
          source: input.source,
          skillName: input.skillName,
          scope: input.scope,
          agents: input.agents,
          ref: input.ref,
          path: input.path,
        });
        if (!requestOwnerRef.current.isCurrent(token)) {
          return;
        }
        if (result.execution.success) {
          setMutationNotice({
            kind: "success",
            message: skillsMutationSuccessCopy(input),
          });
          void refreshInventory();
          if (inspectedName) {
            void inspectSkill(inspectedName);
          }
        } else {
          setMutationNotice({
            kind: "error",
            message: mutationFailureCopy(
              input,
              result.execution.output,
              result.execution.exitCode,
            ),
          });
        }
      } catch (error: unknown) {
        if (requestOwnerRef.current.isCurrent(token)) {
          setMutationNotice({
            kind: "error",
            message:
              error instanceof Error
                ? error.message
                : "The Skills mutation could not be run.",
          });
        }
      } finally {
        if (requestOwnerRef.current.isCurrent(token)) {
          setPreparingMutation("");
        }
      }
    },
    [inspectSkill, inspectedName, refreshInventory],
  );

  const executePluginMutation = useCallback(
    async (
      token: SkillsServerRequestToken,
      input: PluginMutationInput,
      key: string,
      confirmation: { title: string; message: string; confirmLabel: string },
      destructive: boolean,
    ) => {
      if (!requestOwnerRef.current.isCurrent(token) || !token.serverId) {
        return;
      }
      const confirmed = await confirmMutation(confirmation, destructive);
      if (!confirmed || !requestOwnerRef.current.isCurrent(token)) {
        return;
      }
      setPreparingMutation(key);
      setMutationNotice(null);
      try {
        const result = await wsClient.executePluginMutation(token.serverId, {
          operation: input.operation,
          pluginId: input.pluginId,
          scope: input.scope,
        });
        if (!requestOwnerRef.current.isCurrent(token)) {
          return;
        }
        if (result.execution.success) {
          setMutationNotice({
            kind: "success",
            message: pluginMutationSuccessCopy(input),
          });
          void loadPlugins();
        } else {
          setMutationNotice({
            kind: "error",
            message: pluginMutationFailureCopy(
              input,
              result.execution.output,
              result.execution.exitCode,
            ),
          });
        }
      } catch (error: unknown) {
        if (requestOwnerRef.current.isCurrent(token)) {
          setMutationNotice({
            kind: "error",
            message:
              error instanceof Error
                ? error.message
                : "The Plugin mutation could not be run.",
          });
        }
      } finally {
        if (requestOwnerRef.current.isCurrent(token)) {
          setPreparingMutation("");
        }
      }
    },
    [loadPlugins],
  );

  /**
   * One generic lifecycle pipeline: evaluate the intent gate, build the
   * reviewable plan, confirm it exactly, execute it daemon-side.
   */
  const runSkillsMutation = useCallback(
    async (
      intent: Parameters<typeof evaluateSkillMutation>[0],
      build: (
        token: SkillsServerRequestToken,
      ) => Promise<SkillsMutationCommand>,
      input: () => SkillsMutationInput,
      key: string,
      destructive: boolean,
    ) => {
      const capabilities =
        skillsRequestData(inventoryState)?.mutationOperations ?? [];
      const gate = evaluateSkillMutation(
        intent,
        capabilities,
        Boolean(projectCwd?.trim()),
      );
      if (!gate.supported) {
        return;
      }
      if (!currentServerId || !currentConnected || preparingMutation) {
        return;
      }
      Keyboard.dismiss();
      const token = requestOwnerRef.current.issue("mutation");
      setPreparingMutation(key);
      try {
        const command = await build(token);
        if (!requestOwnerRef.current.isCurrent(token)) {
          return;
        }
        await executeSkillsMutation(
          token,
          input(),
          key,
          buildSkillsMutationConfirmation(command),
          destructive,
        );
      } catch (error: unknown) {
        if (requestOwnerRef.current.isCurrent(token)) {
          Alert.alert(
            "Command rejected",
            error instanceof Error
              ? error.message
              : "This Skill cannot be safely managed.",
          );
        }
      } finally {
        if (requestOwnerRef.current.isCurrent(token)) {
          setPreparingMutation("");
        }
      }
    },
    [
      currentConnected,
      currentServerId,
      executeSkillsMutation,
      inventoryState,
      preparingMutation,
      projectCwd,
    ],
  );

  const prepareImport = useCallback(
    (skill: CatalogSkill | RankedCatalogSkill, scope: "project" | "global") => {
      void runSkillsMutation(
        { kind: "import", skill, scope },
        async (token) => {
          if (!token.serverId) {
            throw new Error("No server is connected.");
          }
          return wsClient.buildSkillsCommand(token.serverId, {
            operation: "import",
            cwd: scope === "project" ? projectCwd || undefined : undefined,
            skillId: catalogSkillId(skill),
            source: skill.source,
            skillName: skill.skillId,
            scope,
            agents: skillsInstallTargets(selectedAgent),
            ref: DEFAULT_CATALOG_REF,
          });
        },
        () => ({
          operation: "import",
          cwd: scope === "project" ? projectCwd || undefined : undefined,
          skillId: catalogSkillId(skill),
          source: skill.source,
          skillName: skill.skillId,
          scope,
          agents: skillsInstallTargets(selectedAgent),
          ref: DEFAULT_CATALOG_REF,
        }),
        `install:${catalogSkillId(skill)}`,
        false,
      );
    },
    [projectCwd, runSkillsMutation, selectedAgent],
  );

  const prepareMigrate = useCallback(() => {
    void runSkillsMutation(
      { kind: "migrate" },
      async (token) => {
        if (!token.serverId) {
          throw new Error("No server is connected.");
        }
        return wsClient.buildSkillsCommand(token.serverId, {
          operation: "migrate",
          scope: "global",
          cwd: projectCwd || undefined,
        });
      },
      () => ({
        operation: "migrate",
        scope: "global",
        cwd: projectCwd || undefined,
      }),
      "migrate",
      false,
    );
  }, [projectCwd, runSkillsMutation]);

  const prepareBinding = useCallback(
    (
      skill: InstalledSkill,
      operation: "bind" | "unbind" | "enable" | "disable",
      agent: ManagedSkillAgent,
      scope: "project" | "global",
    ) => {
      void runSkillsMutation(
        { kind: "binding", operation, skill, agent, scope },
        async (token) => {
          if (!token.serverId) {
            throw new Error("No server is connected.");
          }
          return wsClient.buildSkillsCommand(token.serverId, {
            operation,
            cwd: scope === "project" ? projectCwd || undefined : undefined,
            skillName: skill.name,
            scope,
            agents: [agent],
          });
        },
        () => ({
          operation,
          cwd: scope === "project" ? projectCwd || undefined : undefined,
          skillName: skill.name,
          scope,
          agents: [agent],
        }),
        `${operation}:${skill.id}:${agent}:${scope}`,
        operation === "unbind" || operation === "disable",
      );
    },
    [projectCwd, runSkillsMutation],
  );

  const prepareUninstall = useCallback(
    (skill: InstalledSkill) => {
      void runSkillsMutation(
        { kind: "uninstall", skill },
        async (token) => {
          if (!token.serverId) {
            throw new Error("No server is connected.");
          }
          return wsClient.buildSkillsCommand(token.serverId, {
            operation: "uninstall",
            cwd: projectCwd || undefined,
            skillName: skill.name,
            scope: "global",
          });
        },
        () => ({
          operation: "uninstall",
          cwd: projectCwd || undefined,
          skillName: skill.name,
          scope: "global",
        }),
        `uninstall:${skill.id}`,
        true,
      );
    },
    [projectCwd, runSkillsMutation],
  );

  const prepareForget = useCallback(
    (skill: InstalledSkill) => {
      void runSkillsMutation(
        { kind: "forget", skill },
        async (token) => {
          if (!token.serverId) {
            throw new Error("No server is connected.");
          }
          return wsClient.buildSkillsCommand(token.serverId, {
            operation: "forget",
            cwd: projectCwd || undefined,
            skillName: skill.name,
            scope: "global",
          });
        },
        () => ({
          operation: "forget",
          cwd: projectCwd || undefined,
          skillName: skill.name,
          scope: "global",
        }),
        `forget:${skill.id}`,
        false,
      );
    },
    [projectCwd, runSkillsMutation],
  );

  const prepareAdopt = useCallback(
    (skill: InstalledSkill) => {
      void runSkillsMutation(
        { kind: "adopt", skill },
        async (token) => {
          if (!token.serverId) {
            throw new Error("No server is connected.");
          }
          return wsClient.buildSkillsCommand(token.serverId, {
            operation: "adopt",
            cwd: projectCwd || undefined,
            skillName: skill.name,
            scope: "global",
          });
        },
        () => ({
          operation: "adopt",
          cwd: projectCwd || undefined,
          skillName: skill.name,
          scope: "global",
        }),
        `adopt:${skill.id}`,
        false,
      );
    },
    [projectCwd, runSkillsMutation],
  );

  const prepareUpdate = useCallback(
    (skill: InstalledSkill) => {
      void runSkillsMutation(
        { kind: "update", skill },
        async (token) => {
          if (!token.serverId) {
            throw new Error("No server is connected.");
          }
          return wsClient.buildSkillsCommand(token.serverId, {
            operation: "update",
            cwd: projectCwd || undefined,
            skillName: skill.name,
            scope: "global",
          });
        },
        () => ({
          operation: "update",
          cwd: projectCwd || undefined,
          skillName: skill.name,
          scope: "global",
        }),
        `update:${skill.id}`,
        false,
      );
    },
    [projectCwd, runSkillsMutation],
  );

  const preparePluginMutation = useCallback(
    async (
      operation: PluginMutationOperation,
      identity: { pluginId: string; name: string },
      row?: InstalledPluginRow,
      entry?: AvailablePlugin,
    ) => {
      if (!currentServerId || !currentConnected || preparingMutation) {
        return;
      }
      const gate =
        operation === "install" && entry
          ? evaluatePluginMutation({
              kind: "install",
              entry,
              installedIds: pluginInstalledIds(pluginsState),
            })
          : operation === "uninstall" && row
            ? evaluatePluginMutation({ kind: "uninstall", row })
            : row
              ? evaluatePluginMutation({ kind: "update", row })
              : null;
      if (!gate?.supported) {
        return;
      }
      const token = requestOwnerRef.current.issue("plugin-mutation");
      const key = `plugin:${operation}:${identity.pluginId}`;
      setPreparingMutation(key);
      try {
        const command = await wsClient.buildPluginCommand(currentServerId, {
          operation,
          pluginId: identity.pluginId,
          scope: "user",
        });
        if (!requestOwnerRef.current.isCurrent(token)) {
          return;
        }
        await executePluginMutation(
          token,
          { operation, pluginId: identity.pluginId, scope: "user" },
          key,
          buildPluginMutationConfirmation(command),
          operation === "uninstall",
        );
      } catch (error: unknown) {
        if (requestOwnerRef.current.isCurrent(token)) {
          Alert.alert(
            "Command rejected",
            error instanceof Error
              ? error.message
              : "This plugin cannot be managed safely.",
          );
        }
      } finally {
        if (requestOwnerRef.current.isCurrent(token)) {
          setPreparingMutation("");
        }
      }
    },
    [
      currentConnected,
      currentServerId,
      executePluginMutation,
      pluginsState,
      preparingMutation,
    ],
  );

  const presentationIsCurrent = boundServerId === currentServerId;
  const visibleInventoryState = presentationIsCurrent
    ? inventoryState
    : createSkillsRequestState<SkillsInventory>();
  const visiblePluginsState = presentationIsCurrent
    ? pluginsState
    : createSkillsRequestState<PluginInventory>();
  const visibleCatalogState = presentationIsCurrent
    ? catalogState
    : createSkillsRequestState<SkillsLeaderboards>();
  const visibleSearchState = presentationIsCurrent
    ? searchState
    : createSkillsRequestState<SkillsCatalogResult>();
  const visibleInspectState = presentationIsCurrent
    ? inspectState
    : createSkillsRequestState<PackageDetail>();
  const visibleSearchQuery = presentationIsCurrent ? searchQuery : "";
  const visibleSubmittedQuery = presentationIsCurrent ? submittedQuery : "";
  const visibleLeaderboardView = presentationIsCurrent
    ? leaderboardView
    : "all-time";
  const visibleNotice = presentationIsCurrent ? mutationNotice : null;
  const visiblePreparing = presentationIsCurrent ? preparingMutation : "";
  const visibleInspectedName = presentationIsCurrent ? inspectedName : null;

  const inventory = skillsRequestData(visibleInventoryState);
  const search = skillsRequestData(visibleSearchState);
  const leaderboards = skillsRequestData(visibleCatalogState);
  const leaderboard = leaderboards
    ? leaderboardForView(leaderboards, visibleLeaderboardView)
    : undefined;
  const projection = useMemo(
    () => skillsSectionProjection(inventory, selectedAgent),
    [inventory, selectedAgent],
  );
  const agentCounts = useMemo(
    () => skillsSectionAgentCounts(inventory),
    [inventory],
  );
  const pluginsInventory = skillsRequestData(visiblePluginsState);
  const pluginsView: PluginsUnifiedView = useMemo(
    () => pluginsUnifiedView(pluginsInventory),
    [pluginsInventory],
  );
  const catalogSkills = useMemo(
    () =>
      visibleSubmittedQuery
        ? (search?.skills ?? [])
        : (leaderboard?.skills ?? []),
    [leaderboard, search, visibleSubmittedQuery],
  );
  const browsing = Boolean(visibleSubmittedQuery);
  const catalogInstalledElsewhere = useMemo(() => {
    const result: Record<string, readonly string[]> = {};
    for (const skill of inventory?.skills ?? []) {
      const identity = installedSkillCatalogId(skill);
      if (!identity) {
        continue;
      }
      const agents = skill.agents
        .filter((agent) => agent !== selectedAgent)
        .map(skillAgentLabel);
      if (agents.length > 0) {
        result[identity] = agents;
      }
    }
    return result;
  }, [inventory, selectedAgent]);
  const mutationOperations = inventory?.mutationOperations ?? [];

  return (
    <SkillsPresentation
      key={currentServerId ?? "none"}
      section={surface.section}
      selectedAgent={selectedAgent}
      agentCounts={agentCounts}
      inventoryState={visibleInventoryState}
      installedSkills={projection.skills}
      catalogSkills={catalogSkills}
      catalogInstalledElsewhere={catalogInstalledElsewhere}
      browsing={browsing}
      catalogState={visibleCatalogState}
      leaderboard={leaderboard}
      searchState={visibleSearchState}
      searchResult={search}
      query={visibleSearchQuery}
      submittedQuery={visibleSubmittedQuery}
      leaderboardView={visibleLeaderboardView}
      pluginsState={visiblePluginsState}
      pluginsView={pluginsView}
      mutationOperations={mutationOperations}
      hasProjectCwd={Boolean(projectCwd?.trim())}
      preparingMutation={visiblePreparing}
      mutationNotice={visibleNotice}
      currentServerAvailable={Boolean(currentServer)}
      inspectedName={visibleInspectedName}
      inspectState={visibleInspectState}
      onSelectSection={selectSection}
      onSelectAgent={selectAgent}
      onOpenSettings={() => router.push("/settings")}
      onRefreshSkills={refreshSkills}
      onRetryPlugins={() => void loadPlugins()}
      onInspectSkill={(name, path) => void inspectSkill(name, path)}
      onDismissInspector={dismissInspector}
      onImport={(skill) => void prepareImport(skill, "global")}
      onMigrate={() => void prepareMigrate()}
      onBinding={(skill, operation, agent, scope) =>
        void prepareBinding(skill, operation, agent, scope)
      }
      onUninstall={(skill) => void prepareUninstall(skill)}
      onForget={(skill) => void prepareForget(skill)}
      onAdopt={(skill) => void prepareAdopt(skill)}
      onUpdate={(skill) => void prepareUpdate(skill)}
      onInstallPlugin={(entry) =>
        void preparePluginMutation(
          "install",
          { pluginId: entry.pluginId, name: entry.name },
          undefined,
          entry,
        )
      }
      onUpdatePlugin={(row) =>
        void preparePluginMutation(
          "update",
          { pluginId: row.id, name: row.name },
          row,
        )
      }
      onUninstallPlugin={(row) =>
        void preparePluginMutation(
          "uninstall",
          { pluginId: row.id, name: row.name },
          row,
        )
      }
      onChangeQuery={changeSearchQuery}
      onSubmitSearch={submitSearch}
      onClearSearch={clearSearchState}
      onSelectLeaderboard={selectLeaderboardView}
      onRetryCatalog={() => void loadLeaderboards()}
      onRetrySearch={() => void runSearch(submittedQuery, "same-query")}
      onDismissNotice={() => setMutationNotice(null)}
    />
  );
}

function confirmMutation(
  confirmation: { title: string; message: string; confirmLabel: string },
  destructive: boolean,
): Promise<boolean> {
  return new Promise((resolve) => {
    Alert.alert(confirmation.title, confirmation.message, [
      { text: "Cancel", style: "cancel", onPress: () => resolve(false) },
      {
        text: confirmation.confirmLabel,
        style: destructive ? "destructive" : "default",
        onPress: () => resolve(true),
      },
    ]);
  });
}

function pluginInstalledIds(
  state: SkillsRequestState<PluginInventory>,
): Set<string> {
  const ids = new Set<string>();
  for (const row of skillsRequestData(state)?.installed ?? []) {
    ids.add(row.id);
  }
  return ids;
}

function skillsMutationSuccessCopy(input: SkillsMutationInput): string {
  const agentLabel = (input.agents ?? []).map(skillAgentLabel).join(", ");
  switch (input.operation) {
    case "import":
      return `Imported ${input.skillName} into the canonical store${agentLabel ? ` for ${agentLabel}` : ""}.`;
    case "migrate":
      return "Scanned the six Agent surfaces; external installations are tracked without touching their files.";
    case "bind":
      return `Bound ${input.skillName} to ${agentLabel}.`;
    case "unbind":
      return `Unbound ${input.skillName} from ${agentLabel}. Package content stays in the store.`;
    case "enable":
      return `Enabled ${input.skillName} for ${agentLabel}.`;
    case "disable":
      return `Disabled ${input.skillName} for ${agentLabel}.`;
    case "uninstall":
      return `Uninstalled ${input.skillName}: bindings, store content, and inventory entry removed.`;
    case "forget":
      return `Forgot ${input.skillName}. External files were left untouched.`;
    case "adopt":
      return `Adopted ${input.skillName} into Zen's store.`;
    case "update":
      return `Updated ${input.skillName} to its pinned provenance.`;
  }
}

function pluginMutationSuccessCopy(input: PluginMutationInput): string {
  switch (input.operation) {
    case "install":
      return `Installed ${input.pluginId}.`;
    case "update":
      return `Updated ${input.pluginId}.`;
    case "uninstall":
      return `Uninstalled ${input.pluginId}.`;
  }
}

function mutationFailureCopy(
  input: SkillsMutationInput & { skillName?: string },
  output: string,
  exitCode: number,
): string {
  const headline = `The operation exited with code ${exitCode}.`;
  const tail = output
    ? output.length > 500
      ? `${output.slice(0, 500)}…`
      : output
    : "";
  return tail ? `${headline}\n\n${tail}` : headline;
}

function pluginMutationFailureCopy(
  input: PluginMutationInput,
  output: string,
  exitCode: number,
): string {
  const headline = `The command exited with code ${exitCode}.`;
  const tail = output
    ? output.length > 500
      ? `${output.slice(0, 500)}…`
      : output
    : "";
  return tail ? `${headline}\n\n${tail}` : headline;
}

function leaderboardForView(
  leaderboards: SkillsLeaderboards,
  view: SkillsLeaderboardView,
): SkillsLeaderboard {
  switch (view) {
    case "all-time":
      return leaderboards.allTime;
    case "trending":
      return leaderboards.trending;
    case "hot":
      return leaderboards.hot;
  }
}
