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
  type SkillsMode,
  type PluginsMode,
} from "../components/skills/SkillsPresentation";
import {
  beginSkillsRequest,
  buildSkillsMutationConfirmation,
  completeSkillsRequest,
  createSkillsRequestState,
  failSkillsRequest,
  skillsRequestData,
  type CatalogSkill,
  type InstalledSkill,
  type ManagedSkillAgent,
  type RankedCatalogSkill,
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
  type PluginMutationCommand,
} from "../services/pluginsManagement";
import {
  SkillsAutomaticInventoryOwner,
  createSkillsDiscoverState,
  reduceSkillsDiscover,
  skillsInstallTargets,
  skillsRemovalPlanForAgent,
  skillsSectionAgentCounts,
  skillsSectionProjection,
  type SkillsLeaderboardView,
} from "../services/skillsScreenModel";
import {
  evaluatePluginMutation,
  pluginSectionView,
  type PluginSectionView,
} from "../services/pluginsScreenModel";
import {
  createSkillsSurfaceState,
  evaluateSkillMutation,
  projectUpdateAvailable,
  reduceSkillsSurface,
  type SkillsSurfaceSection,
} from "../services/skillsSurfaceModel";
import {
  SkillsServerRequestOwner,
  type SkillsServerRequestToken,
} from "../services/skillsServerBoundary";
import {
  createOwnedSkillsTerminalSession,
  skillsTerminalHandoff,
  type TerminalHandoffCommand,
} from "../services/skillsTerminalHandoff";
import { makeSessionKey } from "../services/sessionKeys";
import { markAgentOpened } from "../services/storage";
import {
  DaemonRequestError,
  wsClient,
} from "../services/websocket";
import { useAgents } from "../store/agents";
import { useCurrentServer } from "../store/currentServer";

interface ReviewedCommand {
  command: TerminalHandoffCommand & { scope: "project" | "global" | "user" };
  cwd: string;
  serverId: string;
  token: SkillsServerRequestToken;
  sessionLabel: string;
}

const LEGACY_MUTATION_CAPABILITIES = ["install", "remove"] as const;

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
  const [mode, setMode] = useState<SkillsMode>("installed");
  const [pluginsMode, setPluginsMode] = useState<PluginsMode>("installed");
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
  const [discover, setDiscover] = useState(createSkillsDiscoverState);
  const [catalogState, setCatalogState] = useState<
    SkillsRequestState<SkillsLeaderboards>
  >(createSkillsRequestState);
  const [searchState, setSearchState] = useState<
    SkillsRequestState<SkillsCatalogResult>
  >(createSkillsRequestState);
  const [preparingMutation, setPreparingMutation] = useState("");
  const [creatingTerminal, setCreatingTerminal] = useState(false);
  const activeSearchRef = useRef<{
    generation: number;
    serverId: string;
  } | null>(null);
  const focusedRef = useRef(false);
  const creatingTerminalRef = useRef(false);

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
    setDiscover((current) => ({
      ...current,
      query: "",
      submittedQuery: "",
    }));
    setPreparingMutation("");
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
    setPluginsState((current) =>
      beginSkillsRequest(current, token.generation),
    );
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
          response.inventory.installed.length === 0,
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
  }, [
    currentServerId,
    focusGeneration,
    loadPlugins,
    surface.section,
  ]);

  const loadLeaderboards = useCallback(async () => {
    const token = requestOwnerRef.current.issue("catalog");
    setCatalogState((current) =>
      beginSkillsRequest(current, token.generation),
    );
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
      mode !== "discover" ||
      !automaticCatalogOwnerRef.current.shouldRefresh(
        focusGeneration,
        currentServerId,
      )
    ) {
      return;
    }
    void loadLeaderboards();
  }, [currentServerId, focusGeneration, loadLeaderboards, mode]);

  const runSearch = useCallback(
    async (rawQuery: string, intent: "new-query" | "same-query") => {
      cancelActiveSearch();
      const normalizedQuery = rawQuery.trim();
      if (normalizedQuery.length < 2) {
        return;
      }
      const token = requestOwnerRef.current.issue("search");
      setSearchState((current) =>
        beginSkillsRequest(
          current,
          token.generation,
          intent === "same-query",
        ),
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

  const changeDiscoverQuery = useCallback(
    (value: string) => {
      const transition = reduceSkillsDiscover(discover, {
        type: "change_query",
        value,
      });
      setDiscover(transition.state);
      if (transition.effect.type === "clear_search") {
        clearSearch();
      }
    },
    [clearSearch, discover],
  );

  const submitDiscoverSearch = useCallback(() => {
    const transition = reduceSkillsDiscover(discover, { type: "submit" });
    setDiscover(transition.state);
    if (transition.effect.type === "clear_search") {
      clearSearch();
    } else if (transition.effect.type === "submit_search") {
      void runSearch(transition.effect.query, "new-query");
    }
  }, [clearSearch, discover, runSearch]);

  const clearDiscoverSearch = useCallback(() => {
    const transition = reduceSkillsDiscover(discover, { type: "clear" });
    setDiscover(transition.state);
    clearSearch();
  }, [clearSearch, discover]);

  const selectLeaderboardView = useCallback(
    (view: SkillsLeaderboardView) => {
      setDiscover(
        reduceSkillsDiscover(discover, { type: "select_view", view }).state,
      );
    },
    [discover],
  );

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

  const handoffToTerminal = useCallback(
    async (reviewed: ReviewedCommand) => {
      if (
        creatingTerminalRef.current ||
        !requestOwnerRef.current.isCurrent(reviewed.token) ||
        currentServerId !== reviewed.serverId ||
        !currentConnected
      ) {
        Alert.alert(
          "Server changed",
          "Review the command again for the current server.",
        );
        return;
      }
      if (
        reviewed.command.scope === "project" &&
        !reviewed.cwd
      ) {
        Alert.alert(
          "Project unavailable",
          "Open a Session with a project directory on this server first.",
        );
        return;
      }

      const handoffToken = requestOwnerRef.current.issue("handoff");
      creatingTerminalRef.current = true;
      setCreatingTerminal(true);
      let issuedGrant: { sessionKey: string; token: string } | null = null;
      try {
        const startedAt = Date.now();
        const creation = await createOwnedSkillsTerminalSession({
          serverId: reviewed.serverId,
          createSession: async (serverId) => {
            const created = await wsClient.createSession(serverId, {
              cwd: reviewed.cwd || undefined,
              name: reviewed.sessionLabel,
            });
            return created.agentId;
          },
          isCurrent: () => requestOwnerRef.current.isCurrent(handoffToken),
          abortSession: (serverId, agentId) =>
            wsClient.killAgent(serverId, agentId),
        });
        if (creation.status === "stale") {
          Alert.alert(
            "Server changed",
            "The Terminal was not opened because the current server changed.",
          );
          return;
        }
        const agentId = creation.agentId;
        const sessionKey = makeSessionKey(reviewed.serverId, agentId);
        const token = skillsTerminalHandoff.issue(
          sessionKey,
          reviewed.command,
        );
        issuedGrant = { sessionKey, token };
        void markAgentOpened(sessionKey, Date.now());
        router.push({
          pathname: "/terminal/[id]",
          params: {
            id: agentId,
            serverId: reviewed.serverId,
            name: reviewed.sessionLabel,
            cwd: reviewed.cwd,
            startedAt: String(startedAt),
            initialInterfaceRenderMode: "terminal",
            skillsHandoff: token,
          },
        });
      } catch (error: unknown) {
        if (issuedGrant) {
          skillsTerminalHandoff.revoke(
            issuedGrant.sessionKey,
            issuedGrant.token,
          );
        }
        Alert.alert(
          "Could not open Terminal",
          error instanceof Error
            ? error.message
            : "Reconnect to the server and try again.",
        );
      } finally {
        creatingTerminalRef.current = false;
        setCreatingTerminal(false);
      }
    },
    [currentConnected, currentServerId, router],
  );

  const confirmCommand = useCallback(
    (reviewed: ReviewedCommand, confirmation: {
      title: string;
      message: string;
      confirmLabel: string;
    }) => {
      Alert.alert(confirmation.title, confirmation.message, [
        { text: "Cancel", style: "cancel" },
        {
          text: confirmation.confirmLabel,
          style:
            reviewed.command.operation === "remove" ||
            reviewed.command.operation === "uninstall"
              ? "destructive"
              : "default",
          onPress: () => {
            void handoffToTerminal(reviewed);
          },
        },
      ]);
    },
    [handoffToTerminal],
  );

  const prepareRemove = useCallback(
    async (skill: InstalledSkill) => {
      const capabilities = mutationCapabilities(inventoryState);
      const gate = evaluateSkillMutation(
        { kind: "remove", skill, agent: selectedAgent },
        capabilities,
      );
      if (!gate.supported) {
        return;
      }
      const plan = skillsRemovalPlanForAgent(skill, selectedAgent);
      if (!plan || !currentServerId || !currentConnected || preparingMutation) {
        return;
      }
      const token = requestOwnerRef.current.issue("mutation");
      const key = `remove:${skill.id}`;
      setPreparingMutation(key);
      try {
        const command = await wsClient.buildSkillsCommand(currentServerId, {
          operation: "remove",
          cwd: projectCwd || undefined,
          skillId: skill.id,
          skillName: skill.name,
          scope: skill.scope as "project" | "global",
          agents: plan.affectedAgents,
        });
        if (!requestOwnerRef.current.isCurrent(token)) {
          return;
        }
        confirmCommand(
          {
            command,
            cwd: projectCwd,
            serverId: currentServerId,
            token,
            sessionLabel: `Skills: remove ${command.skillName}`,
          },
          buildSkillsMutationConfirmation(command),
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
      confirmCommand,
      currentConnected,
      currentServerId,
      inventoryState,
      preparingMutation,
      projectCwd,
      selectedAgent,
    ],
  );

  const prepareInstall = useCallback(
    async (skill: CatalogSkill | RankedCatalogSkill) => {
      const capabilities = mutationCapabilities(inventoryState);
      const gate = evaluateSkillMutation(
        { kind: "install", skill },
        capabilities,
      );
      if (
        !gate.supported ||
        !skill.installable ||
        !currentServerId ||
        !currentConnected ||
        preparingMutation
      ) {
        return;
      }
      Keyboard.dismiss();
      const token = requestOwnerRef.current.issue("mutation");
      const key = `install:${skill.id}`;
      setPreparingMutation(key);
      try {
        const command = await wsClient.buildSkillsCommand(currentServerId, {
          operation: "install",
          skillId: skill.id,
          source: skill.source,
          skillName: skill.skillId,
          scope: "global",
          agents: skillsInstallTargets(selectedAgent),
        });
        if (!requestOwnerRef.current.isCurrent(token)) {
          return;
        }
        confirmCommand(
          {
            command,
            cwd: "",
            serverId: currentServerId,
            token,
            sessionLabel: `Skills: install ${command.skillName}`,
          },
          buildSkillsMutationConfirmation(command),
        );
      } catch (error: unknown) {
        if (requestOwnerRef.current.isCurrent(token)) {
          Alert.alert(
            "Command rejected",
            error instanceof Error
              ? error.message
              : "This catalog identity cannot be installed safely.",
          );
        }
      } finally {
        if (requestOwnerRef.current.isCurrent(token)) {
          setPreparingMutation("");
        }
      }
    },
    [
      confirmCommand,
      currentConnected,
      currentServerId,
      inventoryState,
      preparingMutation,
      selectedAgent,
    ],
  );

  const prepareSkillsUpdate = useCallback(
    async (scope: "project" | "global") => {
      const capabilities = mutationCapabilities(inventoryState);
      const gate = evaluateSkillMutation({ kind: "update", scope }, capabilities);
      if (
        !gate.supported ||
        !currentServerId ||
        !currentConnected ||
        preparingMutation
      ) {
        return;
      }
      if (scope === "project" && !projectUpdateAvailable(projectCwd)) {
        Alert.alert(
          "Project unavailable",
          "Open a Session with a project directory on this server first.",
        );
        return;
      }
      const token = requestOwnerRef.current.issue("mutation");
      const key = `update:${scope}`;
      setPreparingMutation(key);
      try {
        const command = await wsClient.buildSkillsCommand(currentServerId, {
          operation: "update",
          cwd: scope === "project" ? projectCwd || undefined : undefined,
          scope,
        });
        if (!requestOwnerRef.current.isCurrent(token)) {
          return;
        }
        confirmCommand(
          {
            command,
            cwd: scope === "project" ? projectCwd : "",
            serverId: currentServerId,
            token,
            sessionLabel: `Skills: update ${scope}`,
          },
          buildSkillsMutationConfirmation(command),
        );
      } catch (error: unknown) {
        if (requestOwnerRef.current.isCurrent(token)) {
          Alert.alert(
            "Command rejected",
            error instanceof Error
              ? error.message
              : "This update cannot be prepared safely.",
          );
        }
      } finally {
        if (requestOwnerRef.current.isCurrent(token)) {
          setPreparingMutation("");
        }
      }
    },
    [
      confirmCommand,
      currentConnected,
      currentServerId,
      inventoryState,
      preparingMutation,
      projectCwd,
    ],
  );

  const preparePluginMutation = useCallback(
    async (
      operation: "install" | "update" | "uninstall",
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
        confirmCommand(
          {
            command,
            cwd: "",
            serverId: currentServerId,
            token,
            sessionLabel: `Plugins: ${operation} ${identity.name}`,
          },
          buildPluginMutationConfirmation(command),
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
    [confirmCommand, currentConnected, currentServerId, pluginsState, preparingMutation],
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
  const visibleDiscover = presentationIsCurrent
    ? discover
    : createSkillsDiscoverState();
  const inventory = skillsRequestData(visibleInventoryState);
  const search = skillsRequestData(visibleSearchState);
  const leaderboards = skillsRequestData(visibleCatalogState);
  const leaderboard = leaderboards
    ? leaderboardForView(leaderboards, visibleDiscover.view)
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
  const pluginSection: PluginSectionView = useMemo(
    () => pluginSectionView(pluginsInventory),
    [pluginsInventory],
  );
  const mutationOperations = inventory?.mutationOperations ?? [
    ...LEGACY_MUTATION_CAPABILITIES,
  ];
  const hasProjectCwd = Boolean(projectCwd?.trim());

  return (
    <SkillsPresentation
      key={currentServerId ?? "none"}
      section={surface.section}
      mode={mode}
      pluginsMode={pluginsMode}
      selectedAgent={selectedAgent}
      agentCounts={agentCounts}
      inventoryState={visibleInventoryState}
      installedSkills={projection.skills}
      pluginsState={visiblePluginsState}
      pluginSection={pluginSection}
      catalogState={visibleCatalogState}
      leaderboard={leaderboard}
      searchState={visibleSearchState}
      searchResult={search}
      query={visibleDiscover.query}
      submittedQuery={visibleDiscover.submittedQuery}
      leaderboardView={visibleDiscover.view}
      mutationOperations={mutationOperations}
      hasProjectCwd={hasProjectCwd}
      preparingMutation={preparingMutation}
      creatingTerminal={creatingTerminal}
      currentServerAvailable={Boolean(currentServer)}
      onSelectSection={selectSection}
      onSelectMode={setMode}
      onSelectPluginsMode={setPluginsMode}
      onSelectAgent={selectAgent}
      onOpenSettings={() => router.push("/settings")}
      onRefreshInventory={() => void refreshInventory()}
      onRetryPlugins={() => void loadPlugins()}
      onRemove={(skill) => void prepareRemove(skill)}
      onUpdateSkills={(scope) => void prepareSkillsUpdate(scope)}
      onInstallPlugin={(entry) =>
        void preparePluginMutation("install", {
          pluginId: entry.pluginId,
          name: entry.name,
        }, undefined, entry)
      }
      onUpdatePlugin={(row) =>
        void preparePluginMutation("update", {
          pluginId: row.id,
          name: row.name,
        }, row)
      }
      onUninstallPlugin={(row) =>
        void preparePluginMutation("uninstall", {
          pluginId: row.id,
          name: row.name,
        }, row)
      }
      onChangeQuery={changeDiscoverQuery}
      onSubmitSearch={submitDiscoverSearch}
      onClearSearch={clearDiscoverSearch}
      onSelectLeaderboard={selectLeaderboardView}
      onRetryCatalog={() => void loadLeaderboards()}
      onRetrySearch={() =>
        void runSearch(discover.submittedQuery, "same-query")
      }
      onInstall={(skill) => void prepareInstall(skill)}
    />
  );
}

function mutationCapabilities(
  state: SkillsRequestState<SkillsInventory>,
): readonly ("install" | "remove" | "update")[] {
  return (
    skillsRequestData(state)?.mutationOperations ?? LEGACY_MUTATION_CAPABILITIES
  );
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
