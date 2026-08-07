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
} from "../components/skills/SkillsPresentation";
import {
  buildSkillsMutationConfirmation,
  completeSkillsRequest,
  createSkillsRequestState,
  failSkillsRequest,
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
  SkillsAutomaticInventoryOwner,
  createSkillsDiscoverState,
  reduceSkillsDiscover,
  skillsAgentCounts,
  skillsAgentProjection,
  skillsInstallTargets,
  skillsRemovalPlanForAgent,
  type SkillsLeaderboardView,
} from "../services/skillsScreenModel";
import {
  SkillsServerRequestOwner,
  type SkillsServerRequestToken,
} from "../services/skillsServerBoundary";
import {
  createOwnedSkillsTerminalSession,
  skillsTerminalHandoff,
} from "../services/skillsTerminalHandoff";
import { makeSessionKey } from "../services/sessionKeys";
import { markAgentOpened } from "../services/storage";
import { wsClient } from "../services/websocket";
import { useAgents } from "../store/agents";
import { useCurrentServer } from "../store/currentServer";

interface ReviewedSkillsCommand {
  command: SkillsMutationCommand;
  cwd: string;
  serverId: string;
  token: SkillsServerRequestToken;
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

  const [mode, setMode] = useState<SkillsMode>("installed");
  const [selectedAgent, setSelectedAgent] =
    useState<ManagedSkillAgent>("codex");
  const [boundServerId, setBoundServerId] = useState(currentServerId);
  const [focusGeneration, setFocusGeneration] = useState(0);
  const [inventoryState, setInventoryState] = useState<
    SkillsRequestState<SkillsInventory>
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
    setCatalogState(createSkillsRequestState());
    setSearchState(createSkillsRequestState());
    setDiscover((current) => ({ ...current, submittedQuery: "" }));
    setPreparingMutation("");
  }, [cancelActiveSearch, currentServerId]);

  const refreshInventory = useCallback(async () => {
    const token = requestOwnerRef.current.issue("inventory");
    if (!token.serverId || !currentServer) {
      setInventoryState({
        status: "error",
        generation: token.generation,
        error: "Choose a current server in Settings to view installed Skills.",
      });
      return;
    }
    if (!currentConnected) {
      setInventoryState({
        status: "error",
        generation: token.generation,
        error:
          "The current server is offline. Connect it in Settings and retry.",
      });
      return;
    }

    setInventoryState({ status: "loading", generation: token.generation });
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

  const loadLeaderboards = useCallback(async () => {
    const token = requestOwnerRef.current.issue("catalog");
    if (!token.serverId || !currentServer) {
      setCatalogState({
        status: "error",
        generation: token.generation,
        error: "Choose a current server in Settings to browse Skills.",
      });
      return;
    }
    if (!currentConnected) {
      setCatalogState({
        status: "error",
        generation: token.generation,
        error:
          "The current server is offline. Connect it in Settings and retry.",
      });
      return;
    }

    setCatalogState({ status: "loading", generation: token.generation });
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
    async (rawQuery: string) => {
      cancelActiveSearch();
      const normalizedQuery = rawQuery.trim();
      if (normalizedQuery.length < 2) {
        return;
      }
      const token = requestOwnerRef.current.issue("search");
      if (!token.serverId || !currentServer) {
        setSearchState({
          status: "error",
          generation: token.generation,
          error: "Choose a current server in Settings before searching.",
        });
        return;
      }
      if (!currentConnected) {
        setSearchState({
          status: "error",
          generation: token.generation,
          error:
            "The current server is offline. Connect it in Settings and retry.",
        });
        return;
      }

      setSearchState({ status: "loading", generation: token.generation });
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
      void runSearch(transition.effect.query);
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

  const handoffToTerminal = useCallback(
    async (reviewed: ReviewedSkillsCommand) => {
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
      if (reviewed.command.scope === "project" && !reviewed.cwd) {
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
              name: `Skills: ${reviewed.command.operation} ${reviewed.command.skillName}`,
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
        const token = skillsTerminalHandoff.issue(sessionKey, reviewed.command);
        issuedGrant = { sessionKey, token };
        void markAgentOpened(sessionKey, Date.now());
        router.push({
          pathname: "/terminal/[id]",
          params: {
            id: agentId,
            serverId: reviewed.serverId,
            name: `Skills: ${reviewed.command.operation} ${reviewed.command.skillName}`,
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
    (reviewed: ReviewedSkillsCommand) => {
      const confirmation = buildSkillsMutationConfirmation(reviewed.command);
      Alert.alert(confirmation.title, confirmation.message, [
        { text: "Cancel", style: "cancel" },
        {
          text: confirmation.confirmLabel,
          style:
            reviewed.command.operation === "remove" ? "destructive" : "default",
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
        confirmCommand({
          command,
          cwd: projectCwd,
          serverId: currentServerId,
          token,
        });
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
      preparingMutation,
      projectCwd,
      selectedAgent,
    ],
  );

  const prepareInstall = useCallback(
    async (skill: CatalogSkill | RankedCatalogSkill) => {
      if (
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
        confirmCommand({
          command,
          cwd: "",
          serverId: currentServerId,
          token,
        });
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
      preparingMutation,
      selectedAgent,
    ],
  );

  const presentationIsCurrent = boundServerId === currentServerId;
  const visibleInventoryState = presentationIsCurrent
    ? inventoryState
    : createSkillsRequestState<SkillsInventory>();
  const visibleCatalogState = presentationIsCurrent
    ? catalogState
    : createSkillsRequestState<SkillsLeaderboards>();
  const visibleSearchState = presentationIsCurrent
    ? searchState
    : createSkillsRequestState<SkillsCatalogResult>();
  const inventory =
    visibleInventoryState.status === "ready" ||
    visibleInventoryState.status === "empty"
      ? visibleInventoryState.data
      : undefined;
  const search =
    visibleSearchState.status === "ready" ||
    visibleSearchState.status === "empty"
      ? visibleSearchState.data
      : undefined;
  const leaderboards =
    visibleCatalogState.status === "ready" ||
    visibleCatalogState.status === "empty"
      ? visibleCatalogState.data
      : undefined;
  const leaderboard = leaderboards
    ? leaderboardForView(leaderboards, discover.view)
    : undefined;
  const projection = skillsAgentProjection(inventory, selectedAgent);
  const agentCounts = skillsAgentCounts(inventory);

  return (
    <SkillsPresentation
      mode={mode}
      selectedAgent={selectedAgent}
      agentCounts={agentCounts}
      inventoryState={visibleInventoryState}
      installedSkills={projection.skills}
      inventoryWarnings={inventory?.warnings ?? []}
      catalogState={visibleCatalogState}
      leaderboard={leaderboard}
      searchState={visibleSearchState}
      searchResult={search}
      query={discover.query}
      submittedQuery={discover.submittedQuery}
      leaderboardView={discover.view}
      preparingMutation={preparingMutation}
      creatingTerminal={creatingTerminal}
      currentServerAvailable={Boolean(currentServer)}
      onSelectMode={setMode}
      onSelectAgent={selectAgent}
      onOpenSettings={() => router.push("/settings")}
      onRefreshInventory={() => void refreshInventory()}
      onRemove={(skill) => void prepareRemove(skill)}
      onChangeQuery={changeDiscoverQuery}
      onSubmitSearch={submitDiscoverSearch}
      onClearSearch={clearDiscoverSearch}
      onSelectLeaderboard={selectLeaderboardView}
      onRetryCatalog={() => void loadLeaderboards()}
      onRetrySearch={() => void runSearch(discover.submittedQuery)}
      onInstall={(skill) => void prepareInstall(skill)}
    />
  );
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
