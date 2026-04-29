import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Alert,
  AppState,
  AppStateStatus,
  Keyboard,
  KeyboardAvoidingView,
  Modal,
  Platform,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
  useWindowDimensions,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useFocusEffect, useLocalSearchParams, useRouter } from "expo-router";
import {
  SafeAreaView,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
import { Agent, useAgents } from "../../store/agents";
import { useIssues } from "../../store/issues";
import {
  AgentStatus,
  Colors,
  Typography,
  statusColor,
} from "../../constants/tokens";
import {
  buildTerminalChrome,
  DefaultTerminalThemeName,
  resolveTerminalTheme,
  TerminalThemeName,
} from "../../constants/terminalThemes";
import {
  closeOtherTerminalTabs,
  closeTerminalTab,
  getAgentAliases,
  getRecentAgentOpens,
  getServerById,
  getServers,
  getTerminalTabs,
  getTerminalTheme,
  markAgentOpened,
  setAgentAlias,
  setTerminalTabPinned,
  StoredAgentAliases,
  StoredRecentAgentOpens,
  StoredServer,
  StoredTerminalTabs,
  syncTerminalTabsWithLiveSessions,
  touchTerminalTab,
} from "../../services/storage";
import { makeSessionKey, parseSessionKey } from "../../services/sessionKeys";
import { wsClient } from "../../services/websocket";
import { connectionIssueAccent } from "../../services/connectionIssue";
import {
  TerminalSurface,
  TerminalSurfaceHandle,
} from "../../components/terminal/TerminalSurface";
import { TerminalAccessoryBar } from "../../components/terminal/TerminalAccessoryBar";
import { GitDiffSheet } from "../../components/terminal/GitDiffSheet";
import { AgentKindIcon } from "../../components/terminal/AgentKindIcon";
import { NewTerminalSheet } from "../../components/terminal/NewTerminalSheet";
import { presentAgent } from "../../services/agentPresentation";
import {
  buildGitDiffChipLabel,
  type GitDiffPatchPayload,
  type GitRepoBrowserEntry,
  type GitRepoFileContentPayload,
  type GitDiffStatusSnapshot,
} from "../../services/gitDiff";
import {
  filterAgentsByPreferredServers,
  groupAgentsByDirectory,
} from "../../services/serverSelection";

const EMPTY_TABS: StoredTerminalTabs = { order: [], pinned: [] };
const MENU_POPOVER_WIDTH = 168;

function hasGitDiffPatchContent(payload?: GitDiffPatchPayload) {
  return Boolean(payload?.sections?.some((section) => section.patch.trim()));
}

const STATUS_PRIORITY: Record<AgentStatus, number> = {
  failed: 0,
  blocked: 1,
  unknown: 2,
  running: 3,
  done: 4,
};

interface TerminalTabDescriptor {
  id: string;
  name: string;
  status: AgentStatus;
  kind: "terminal" | "claude" | "codex";
  pinned: boolean;
  active: boolean;
}

export default function TerminalScreen() {
  const params = useLocalSearchParams<{ id?: string; serverId?: string }>();
  const agentId = typeof params.id === "string" ? params.id : "";
  const serverId = typeof params.serverId === "string" ? params.serverId : "";
  const sessionKey =
    agentId && serverId ? makeSessionKey(serverId, agentId) : null;
  const { state } = useAgents();
  const { state: issuesState } = useIssues();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { width: windowWidth, height: windowHeight } = useWindowDimensions();
  const [themeName, setThemeName] = useState<TerminalThemeName>(
    DefaultTerminalThemeName,
  );
  const [agentAliases, setAgentAliases] = useState<StoredAgentAliases>({});
  const [recentAgentOpens, setRecentAgentOpens] =
    useState<StoredRecentAgentOpens>({});
  const [terminalTabs, setTerminalTabs] =
    useState<StoredTerminalTabs>(EMPTY_TABS);
  const [server, setServer] = useState<StoredServer | null>(null);
  const [servers, setServers] = useState<StoredServer[]>([]);
  const [pickerVisible, setPickerVisible] = useState(false);
  const [menuVisible, setMenuVisible] = useState(false);
  const [menuAnchor, setMenuAnchor] = useState<{
    x: number;
    y: number;
    width: number;
    height: number;
  } | null>(null);
  const [renameVisible, setRenameVisible] = useState(false);
  const [renameDraft, setRenameDraft] = useState("");
  const [keyboardVisible, setKeyboardVisible] = useState(false);
  const [androidKeyboardInset, setAndroidKeyboardInset] = useState(0);
  const [accessoryHeight, setAccessoryHeight] = useState(68);
  const [ctrlArmed, setCtrlArmed] = useState(false);
  const [newTerminalVisible, setNewTerminalVisible] = useState(false);
  const [gitDiffVisible, setGitDiffVisible] = useState(false);
  const [gitDiffStatus, setGitDiffStatus] =
    useState<GitDiffStatusSnapshot | null>(null);
  const [gitDiffLoading, setGitDiffLoading] = useState(false);
  const [gitDiffError, setGitDiffError] = useState<string | null>(null);
  const [gitDiffPatchLoadingByPath, setGitDiffPatchLoadingByPath] = useState<
    Record<string, boolean>
  >({});
  const [gitDiffPatchErrorByPath, setGitDiffPatchErrorByPath] = useState<
    Record<string, string | undefined>
  >({});
  const [gitDiffPatchByPath, setGitDiffPatchByPath] = useState<
    Record<string, GitDiffPatchPayload | undefined>
  >({});
  const gitDiffPatchByPathRef = useRef(gitDiffPatchByPath);
  const gitDiffPatchLoadingByPathRef = useRef(gitDiffPatchLoadingByPath);
  const gitDiffPatchErrorByPathRef = useRef(gitDiffPatchErrorByPath);
  const [repoBrowserPath, setRepoBrowserPath] = useState("");
  const [repoBrowserEntries, setRepoBrowserEntries] = useState<
    GitRepoBrowserEntry[]
  >([]);
  const [repoBrowserLoading, setRepoBrowserLoading] = useState(false);
  const [repoBrowserError, setRepoBrowserError] = useState<string | null>(null);
  const [repoFilePath, setRepoFilePath] = useState<string | null>(null);
  const [repoFileLoadingPath, setRepoFileLoadingPath] = useState<string | null>(
    null,
  );
  const [repoFileError, setRepoFileError] = useState<string | null>(null);
  const [repoFileByPath, setRepoFileByPath] = useState<
    Record<string, GitRepoFileContentPayload | undefined>
  >({});
  const [creatingSession, setCreatingSession] = useState(false);
  const [showTerminalFallback, setShowTerminalFallback] = useState(
    !Boolean(sessionKey && serverId && agentId),
  );
  const [screenFocused, setScreenFocused] = useState(false);
  const terminalTheme = useMemo(
    () => resolveTerminalTheme(themeName),
    [themeName],
  );
  const chromeColors = useMemo(
    () => buildTerminalChrome(terminalTheme),
    [terminalTheme],
  );
  const terminalRef = useRef<TerminalSurfaceHandle>(null);
  const tabScrollRef = useRef<ScrollView>(null);
  const tabLayoutsRef = useRef<Map<string, { x: number; width: number }>>(
    new Map(),
  );
  const keyboardHeightRef = useRef(0);
  const baseWindowHeightRef = useRef(windowHeight);
  const menuAnchorRef = useRef<View | null>(null);
  const reconnectFallbackTimerRef = useRef<ReturnType<
    typeof setTimeout
  > | null>(null);
  const gitDiffRequestRef = useRef(0);

  const agentByKey = useMemo(
    () => new Map(state.agents.map((agent) => [agent.key, agent])),
    [state.agents],
  );
  const hydratedServerIds = useMemo(
    () =>
      Object.entries(state.hydratedServers)
        .filter(([, hydrated]) => hydrated)
        .map(([serverId]) => serverId),
    [state.hydratedServers],
  );
  const hydratedServerIdSet = useMemo(
    () => new Set(hydratedServerIds),
    [hydratedServerIds],
  );
  const agent = sessionKey ? agentByKey.get(sessionKey) : undefined;
  const gitDiffCwd = typeof agent?.cwd === "string" ? agent.cwd.trim() : "";

  useEffect(() => {
    gitDiffPatchByPathRef.current = gitDiffPatchByPath;
  }, [gitDiffPatchByPath]);

  useEffect(() => {
    gitDiffPatchLoadingByPathRef.current = gitDiffPatchLoadingByPath;
  }, [gitDiffPatchLoadingByPath]);

  useEffect(() => {
    gitDiffPatchErrorByPathRef.current = gitDiffPatchErrorByPath;
  }, [gitDiffPatchErrorByPath]);

  const resetGitDiffPatchCache = useCallback(() => {
    gitDiffPatchByPathRef.current = {};
    gitDiffPatchErrorByPathRef.current = {};
    gitDiffPatchLoadingByPathRef.current = {};
    setGitDiffPatchByPath({});
    setGitDiffPatchErrorByPath({});
    setGitDiffPatchLoadingByPath({});
  }, []);

  const linkedIssue = useMemo(
    () =>
      Object.values(issuesState.byKey)
        .filter((current) => current.serverId === serverId && current.frontmatter.agent_session === agentId)
        .sort((left, right) => {
          const leftTime = Date.parse(left.frontmatter.dispatched || left.frontmatter.created || "");
          const rightTime = Date.parse(right.frontmatter.dispatched || right.frontmatter.created || "");
          return (Number.isNaN(rightTime) ? 0 : rightTime) - (Number.isNaN(leftTime) ? 0 : leftTime);
        })[0],
    [agentId, issuesState.byKey, serverId],
  );
  const activePinned = sessionKey
    ? terminalTabs.pinned.includes(sessionKey)
    : false;
  const displayName = useMemo(
    () =>
      presentAgent(
        agent || { name: "", summary: "", last_output_lines: [] },
        sessionKey ? agentAliases[sessionKey] : undefined,
      ).title,
    [agent, agentAliases, sessionKey],
  );
  const connectionState = serverId
    ? state.serverConnections[serverId] || "offline"
    : "offline";
  const connectionIssue = serverId
    ? state.serverConnectionIssues[serverId] || null
    : null;
  const hasTerminalRoute = Boolean(sessionKey && serverId && agentId);
  const canRenderTerminal = hasTerminalRoute && !showTerminalFallback;
  const shouldMountTerminalSurface = hasTerminalRoute && screenFocused;
  const terminalStateAccent = connectionIssue
    ? connectionIssueAccent(connectionIssue)
    : connectionState === "connecting"
      ? "#E7B65C"
      : "#65758A";
  const terminalStateBusy =
    hasTerminalRoute && connectionState === "connecting" && !connectionIssue;
  const terminalStateTitle = !hasTerminalRoute
    ? "Terminal unavailable"
    : connectionIssue?.title ||
      (connectionState === "connecting"
        ? "Reconnecting to daemon"
        : "Daemon unavailable");
  const terminalStateDetail = !hasTerminalRoute
    ? "Open this terminal again from the Agents tab."
    : connectionIssue?.detail ||
      (connectionState === "connecting"
        ? "zen is reconnecting before reopening this terminal."
        : "Start zen-daemon on that machine, or bring the network or tunnel back.");
  const terminalStateHint = !hasTerminalRoute
    ? "The app kept your route, but the live terminal is not ready yet."
    : connectionIssue?.hint ||
      "This terminal will reopen automatically once the daemon is reachable again.";
  const gitDiffQueryEnabled = Boolean(
    hasTerminalRoute && screenFocused && serverId && agentId && gitDiffCwd,
  );
  const refreshGitDiff = useCallback(
    async (showLoading: boolean = true) => {
      if (
        !serverId
        || !agentId
        || connectionState !== "connected"
        || !gitDiffQueryEnabled
      ) {
        setGitDiffStatus(null);
        setGitDiffError(null);
        resetGitDiffPatchCache();
        setRepoBrowserEntries([]);
        setRepoBrowserPath("");
        setRepoBrowserError(null);
        setRepoFilePath(null);
        setRepoFileError(null);
        setRepoFileLoadingPath(null);
        setRepoFileByPath({});
        if (showLoading) {
          setGitDiffLoading(false);
        }
        return;
      }

      const requestId = gitDiffRequestRef.current + 1;
      gitDiffRequestRef.current = requestId;

      if (showLoading) {
        setGitDiffLoading(true);
        resetGitDiffPatchCache();
      }

      try {
        const nextStatus = await wsClient.getGitDiffStatus(serverId, {
          targetId: agentId,
          cwd: gitDiffCwd,
        });
        if (gitDiffRequestRef.current !== requestId) return;

        setGitDiffStatus(nextStatus);
        setGitDiffError(null);
        setGitDiffPatchByPath((previous) => {
          if (!nextStatus.available) {
            gitDiffPatchByPathRef.current = {};
            return {};
          }
          const allowed = new Set((nextStatus.files ?? []).map((file) => file.path));
          const next = Object.fromEntries(
            Object.entries(previous).filter(
              ([path, patch]) => allowed.has(path) && hasGitDiffPatchContent(patch),
            ),
          );
          gitDiffPatchByPathRef.current = next;
          return next;
        });
        setGitDiffPatchErrorByPath((previous) => {
          if (!nextStatus.available) {
            gitDiffPatchErrorByPathRef.current = {};
            return {};
          }
          const allowed = new Set((nextStatus.files ?? []).map((file) => file.path));
          const next = Object.fromEntries(
            Object.entries(previous).filter(([path]) => allowed.has(path)),
          );
          gitDiffPatchErrorByPathRef.current = next;
          return next;
        });
      } catch (error: any) {
        if (gitDiffRequestRef.current !== requestId) return;
        setGitDiffError(error?.message || "Could not inspect local git changes.");
      } finally {
        if (showLoading && gitDiffRequestRef.current === requestId) {
          setGitDiffLoading(false);
        }
      }
    },
    [agentId, connectionState, gitDiffCwd, gitDiffQueryEnabled, resetGitDiffPatchCache, serverId],
  );

  const syncActiveTerminal = React.useCallback(
    (appState: AppStateStatus = "active") => {
      if (
        appState !== "active" ||
        !sessionKey ||
        !serverId ||
        !agentId
      ) {
        wsClient.clearActiveAgentsExcept(null);
        return;
      }

      wsClient.clearActiveAgentsExcept({ serverId, agentId });
    },
    [agentId, serverId, sessionKey],
  );

  useFocusEffect(
    React.useCallback(() => {
      setScreenFocused(true);
      syncActiveTerminal();

      const appStateSub = AppState.addEventListener("change", (nextState) => {
        syncActiveTerminal(nextState);
      });

      return () => {
        appStateSub.remove();
        setScreenFocused(false);
        setCtrlArmed(false);
        syncActiveTerminal("background");
      };
    }, [syncActiveTerminal]),
  );

  useEffect(() => {
    let cancelled = false;

    (async () => {
      const [storedTheme, storedRecentOpens, storedAliases, storedServers] =
        await Promise.all([
          getTerminalTheme(),
          getRecentAgentOpens(),
          getAgentAliases(),
          getServers(),
        ]);
      const storedServer = serverId
        ? storedServers.find((current) => current.id === serverId) || null
        : null;

      if (!sessionKey) {
        const storedTabs = await getTerminalTabs();
        if (!cancelled) {
          setThemeName(storedTheme);
          setAgentAliases(storedAliases);
          setRecentAgentOpens(storedRecentOpens);
          setTerminalTabs(storedTabs);
          setServer(storedServer);
          setServers(storedServers);
        }
        return;
      }

      const openedAt = Date.now();
      const nextTabs = await touchTerminalTab(sessionKey);
      void markAgentOpened(sessionKey, openedAt);

      if (!cancelled) {
        setThemeName(storedTheme);
        setAgentAliases(storedAliases);
        setRecentAgentOpens({
          ...storedRecentOpens,
          [sessionKey]: openedAt,
        });
        setTerminalTabs(nextTabs);
        setServer(storedServer);
        setServers(storedServers);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [serverId, sessionKey]);

  useEffect(() => {
    setMenuVisible(false);
    setMenuAnchor(null);
  }, [sessionKey]);

  useEffect(() => {
    setRenameVisible(false);
    setRenameDraft("");
  }, [sessionKey]);

  useEffect(() => {
    setGitDiffVisible(false);
  }, [sessionKey]);

  useEffect(() => {
    resetGitDiffPatchCache();
    setRepoBrowserPath("");
    setRepoBrowserEntries([]);
    setRepoBrowserError(null);
    setRepoBrowserLoading(false);
    setRepoFilePath(null);
    setRepoFileLoadingPath(null);
    setRepoFileError(null);
    setRepoFileByPath({});
    setGitDiffError(null);
  }, [agentId, gitDiffCwd, resetGitDiffPatchCache, serverId]);

  useEffect(() => {
    if (hydratedServerIds.length === 0) return;

    let cancelled = false;

    (async () => {
      const nextTabs = await syncTerminalTabsWithLiveSessions(
        state.agents.map((currentAgent) => currentAgent.key),
        hydratedServerIds,
      );
      if (!cancelled) {
        setTerminalTabs(nextTabs);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [hydratedServerIds, state.agents]);

  useEffect(() => {
    const handleShow = (event: any) => {
      keyboardHeightRef.current = event?.endCoordinates?.height ?? 0;
      setKeyboardVisible(true);
    };
    const handleHide = () => {
      keyboardHeightRef.current = 0;
      setAndroidKeyboardInset(0);
      setKeyboardVisible(false);
    };

    const showSub = Keyboard.addListener("keyboardDidShow", handleShow);
    const hideSub = Keyboard.addListener("keyboardDidHide", handleHide);
    return () => {
      showSub.remove();
      hideSub.remove();
    };
  }, []);

  useEffect(() => {
    if (!keyboardVisible) {
      baseWindowHeightRef.current = windowHeight;
    }
  }, [keyboardVisible, windowHeight]);

  useEffect(() => {
    if (!keyboardVisible || Platform.OS !== "android") return;

    const keyboardHeight = keyboardHeightRef.current;
    if (!keyboardHeight) return;

    const adjustResizeHandled = Math.max(
      0,
      baseWindowHeightRef.current - windowHeight,
    );
    const remainingInset = Math.max(0, keyboardHeight - adjustResizeHandled);
    setAndroidKeyboardInset((previous) =>
      Math.abs(previous - remainingInset) <= 1 ? previous : remainingInset,
    );
  }, [keyboardVisible, windowHeight]);

  useEffect(() => {
    if (!keyboardVisible) {
      setCtrlArmed(false);
    }
  }, [keyboardVisible]);

  useEffect(() => {
    setCtrlArmed(false);
  }, [sessionKey]);

  useEffect(() => {
    if (renameVisible) {
      setCtrlArmed(false);
    }
  }, [renameVisible]);

  useEffect(() => {
    if (reconnectFallbackTimerRef.current) {
      clearTimeout(reconnectFallbackTimerRef.current);
      reconnectFallbackTimerRef.current = null;
    }

    if (!hasTerminalRoute) {
      setShowTerminalFallback(true);
      return;
    }

    if (connectionState === "connected") {
      setShowTerminalFallback(false);
      return;
    }

    if (connectionIssue) {
      setShowTerminalFallback(true);
      return;
    }

    setShowTerminalFallback(false);
    reconnectFallbackTimerRef.current = setTimeout(() => {
      setShowTerminalFallback(true);
      reconnectFallbackTimerRef.current = null;
    }, 1500);

    return () => {
      if (reconnectFallbackTimerRef.current) {
        clearTimeout(reconnectFallbackTimerRef.current);
        reconnectFallbackTimerRef.current = null;
      }
    };
  }, [connectionIssue, connectionState, hasTerminalRoute]);

  useEffect(() => {
    if (!gitDiffQueryEnabled || connectionState !== "connected") {
      gitDiffRequestRef.current += 1;
      setGitDiffStatus(null);
      setGitDiffError(null);
      setGitDiffLoading(false);
      resetGitDiffPatchCache();
      setRepoBrowserEntries([]);
      setRepoBrowserError(null);
      setRepoBrowserLoading(false);
      setRepoFilePath(null);
      setRepoFileError(null);
      setRepoFileLoadingPath(null);
      setRepoFileByPath({});
      return;
    }

    void refreshGitDiff(true);

    const interval = setInterval(() => {
      void refreshGitDiff(false);
    }, gitDiffVisible ? 7000 : 15000);

    return () => {
      gitDiffRequestRef.current += 1;
      clearInterval(interval);
    };
  }, [connectionState, gitDiffQueryEnabled, gitDiffVisible, refreshGitDiff, resetGitDiffPatchCache]);

  const tabs = useMemo(() => {
    const order = buildDisplayTabOrder(sessionKey, terminalTabs);
    return order
      .filter((currentSessionKey) => {
        if (currentSessionKey === sessionKey) return true;
        if (agentByKey.has(currentSessionKey)) return true;

        const parsed = parseSessionKey(currentSessionKey);
        return parsed ? !hydratedServerIdSet.has(parsed.serverId) : false;
      })
      .map((currentSessionKey) => {
        const tabAgent = agentByKey.get(currentSessionKey);
        const parsed = parseSessionKey(currentSessionKey);
        const presented = presentAgent(
          tabAgent || {
            name: parsed?.agentId || "",
            summary: "",
            last_output_lines: [],
          },
          currentSessionKey ? agentAliases[currentSessionKey] : undefined,
        );
        return {
          id: currentSessionKey,
          name: presented.cwdBase || presented.shortTitle,
          status: tabAgent?.status || "unknown",
          kind: presented.kind,
          pinned: terminalTabs.pinned.includes(currentSessionKey),
          active: currentSessionKey === sessionKey,
        } satisfies TerminalTabDescriptor;
      });
  }, [agentAliases, agentByKey, hydratedServerIdSet, sessionKey, terminalTabs]);

  // Auto-scroll to keep the active tab visible
  useEffect(() => {
    if (!sessionKey) return;
    const layout = tabLayoutsRef.current.get(sessionKey);
    if (layout && tabScrollRef.current) {
      const scrollTo = Math.max(0, layout.x - 40);
      tabScrollRef.current.scrollTo({ x: scrollTo, animated: true });
    }
  }, [sessionKey]);

  const displayAgents = useMemo(
    () =>
      filterAgentsByPreferredServers({
        agents: state.agents,
        servers,
        connectionStates: state.serverConnections,
        latencyById: state.serverLatencyById,
      }),
    [servers, state.agents, state.serverConnections, state.serverLatencyById],
  );

  const sortedAgents = useMemo(() => {
    const openTabs = new Set(terminalTabs.order);
    const pinnedTabs = new Set(terminalTabs.pinned);

    return [...displayAgents].sort((left, right) => {
      const leftPinned = pinnedTabs.has(left.key) ? 0 : 1;
      const rightPinned = pinnedTabs.has(right.key) ? 0 : 1;
      if (leftPinned !== rightPinned) return leftPinned - rightPinned;

      const leftOpen = openTabs.has(left.key) ? 0 : 1;
      const rightOpen = openTabs.has(right.key) ? 0 : 1;
      if (leftOpen !== rightOpen) return leftOpen - rightOpen;

      const leftOpenedAt = recentAgentOpens[left.key] ?? 0;
      const rightOpenedAt = recentAgentOpens[right.key] ?? 0;
      if (leftOpenedAt !== rightOpenedAt) return rightOpenedAt - leftOpenedAt;

      const leftPriority = STATUS_PRIORITY[left.status] ?? 5;
      const rightPriority = STATUS_PRIORITY[right.status] ?? 5;
      if (leftPriority !== rightPriority) return leftPriority - rightPriority;

      return (right.updated_at || 0) - (left.updated_at || 0);
    });
  }, [displayAgents, recentAgentOpens, terminalTabs]);

  const showPickerServerNames = useMemo(
    () => new Set(sortedAgents.map((agent) => agent.serverId)).size > 1,
    [sortedAgents],
  );
  const pickerSections = useMemo(
    () => groupAgentsByDirectory(sortedAgents, {
      showServerName: showPickerServerNames,
    }),
    [showPickerServerNames, sortedAgents],
  );

  const menuPosition = useMemo(
    () => buildMenuPosition(menuAnchor, windowWidth),
    [menuAnchor, windowWidth],
  );
  const accessoryVisible = canRenderTerminal && screenFocused;
  // Both platforms use absolute dock — iOS uses keyboard event height directly,
  // Android uses the window-resize calculation (handles both adjustResize and adjustPan).
  // Add insets.bottom so the bar sits above the system gesture bar, and a fixed 6px gap
  // above the keyboard so the bar never looks flush / overlapping with the keyboard top edge.
  const accessoryBottomOffset = Platform.OS === "ios"
    ? (keyboardVisible ? keyboardHeightRef.current : insets.bottom)
    : (keyboardVisible ? androidKeyboardInset + insets.bottom + 6 : insets.bottom);
  const outputBottomInset = accessoryVisible
    ? accessoryHeight + accessoryBottomOffset
    : 0;

  const closeMenu = () => {
    setMenuVisible(false);
    setMenuAnchor(null);
  };

  const openGitDiff = () => {
    closeMenu();
    setGitDiffVisible(true);
    void refreshGitDiff(true);
  };

  const ensureGitDiffPatch = useCallback(
    async (path: string) => {
      const nextPath = path.trim();
      if (!nextPath || !serverId || !agentId) {
        return;
      }

      if (
        gitDiffPatchByPathRef.current[nextPath]
        || gitDiffPatchLoadingByPathRef.current[nextPath]
        || gitDiffPatchErrorByPathRef.current[nextPath]
      ) {
        return;
      }

      gitDiffPatchLoadingByPathRef.current = {
        ...gitDiffPatchLoadingByPathRef.current,
        [nextPath]: true,
      };
      setGitDiffPatchLoadingByPath((previous) => ({
        ...previous,
        [nextPath]: true,
      }));
      gitDiffPatchErrorByPathRef.current = {
        ...gitDiffPatchErrorByPathRef.current,
        [nextPath]: undefined,
      };
      setGitDiffPatchErrorByPath((previous) => ({
        ...previous,
        [nextPath]: undefined,
      }));

      try {
        const payload = await wsClient.getGitDiffPatch(serverId, {
          targetId: agentId,
          cwd: gitDiffCwd,
          path: nextPath,
        });
        gitDiffPatchByPathRef.current = {
          ...gitDiffPatchByPathRef.current,
          [nextPath]: payload,
        };
        setGitDiffPatchByPath((previous) => ({
          ...previous,
          [nextPath]: payload,
        }));
      } catch (error: any) {
        gitDiffPatchErrorByPathRef.current = {
          ...gitDiffPatchErrorByPathRef.current,
          [nextPath]: error?.message || "Could not load this patch.",
        };
        setGitDiffPatchErrorByPath((previous) => ({
          ...previous,
          [nextPath]: error?.message || "Could not load this patch.",
        }));
      } finally {
        const nextLoading = { ...gitDiffPatchLoadingByPathRef.current };
        delete nextLoading[nextPath];
        gitDiffPatchLoadingByPathRef.current = nextLoading;
        setGitDiffPatchLoadingByPath((previous) => {
          const next = { ...previous };
          delete next[nextPath];
          return next;
        });
      }
    },
    [
      agentId,
      gitDiffCwd,
      serverId,
    ],
  );

  const loadRepoBrowserPath = useCallback(
    async (path: string = "") => {
      if (!serverId || !agentId || !gitDiffCwd) {
        return;
      }

      setRepoBrowserLoading(true);
      setRepoBrowserError(null);
      setRepoFilePath(null);
      setRepoFileError(null);

      try {
        const payload = await wsClient.getGitRepoEntries(serverId, {
          targetId: agentId,
          cwd: gitDiffCwd,
          path,
        });
        setRepoBrowserPath(payload.path || "");
        setRepoBrowserEntries(payload.entries ?? []);
      } catch (error: any) {
        setRepoBrowserError(error?.message || "Could not load repository files.");
      } finally {
        setRepoBrowserLoading(false);
      }
    },
    [agentId, gitDiffCwd, serverId],
  );

  const openRepoFile = useCallback(
    async (path: string) => {
      const nextPath = path.trim();
      if (!nextPath || !serverId || !agentId || !gitDiffCwd) {
        return;
      }

      setRepoFilePath(nextPath);
      setRepoFileError(null);

      if (repoFileByPath[nextPath]) {
        return;
      }

      setRepoFileLoadingPath(nextPath);
      try {
        const payload = await wsClient.getGitRepoFileContent(serverId, {
          targetId: agentId,
          cwd: gitDiffCwd,
          path: nextPath,
        });
        setRepoFileByPath((previous) => ({
          ...previous,
          [nextPath]: payload,
        }));
      } catch (error: any) {
        setRepoFileError(error?.message || "Could not load repository file.");
      } finally {
        setRepoFileLoadingPath((previous) =>
          previous === nextPath ? null : previous,
        );
      }
    },
    [agentId, gitDiffCwd, repoFileByPath, serverId],
  );

  const closeRepoFile = useCallback(() => {
    setRepoFilePath(null);
    setRepoFileError(null);
  }, []);

  const goUpRepoBrowserPath = useCallback(() => {
    const parent = repoBrowserPath.includes("/")
      ? repoBrowserPath.slice(0, repoBrowserPath.lastIndexOf("/"))
      : "";
    void loadRepoBrowserPath(parent);
  }, [loadRepoBrowserPath, repoBrowserPath]);

  useEffect(() => {
    if (!gitDiffVisible || !gitDiffStatus?.available) {
      return;
    }

    let cancelled = false;
    const files = gitDiffStatus.files ?? [];

    (async () => {
      for (const file of files) {
        if (cancelled) break;
        await ensureGitDiffPatch(file.path);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [ensureGitDiffPatch, gitDiffStatus, gitDiffVisible]);

  useEffect(() => {
    if (!gitDiffVisible || !gitDiffStatus?.available) {
      return;
    }
    void loadRepoBrowserPath(repoBrowserPath || "");
  }, [gitDiffStatus?.repo_root, gitDiffStatus?.available, gitDiffVisible, loadRepoBrowserPath]);

  const openRenameModal = () => {
    closeMenu();
    setRenameDraft(displayName);
    setRenameVisible(true);
  };

  const openAgentTab = async (agentId: string) => {
    setPickerVisible(false);
    closeMenu();

    if (!agentId || agentId === sessionKey) return;
    const parsed = parseSessionKey(agentId);
    if (!parsed) return;
    if (!agentByKey.has(agentId) && hydratedServerIdSet.has(parsed.serverId)) {
      const nextTabs = await closeTerminalTab(agentId);
      setTerminalTabs(nextTabs);
      return;
    }

    router.replace({
      pathname: "/terminal/[id]",
      params: { id: parsed.agentId, serverId: parsed.serverId },
    });
  };

  const goToInbox = () => {
    setPickerVisible(false);
    closeMenu();
    router.replace("/");
  };

  const openMenu = () => {
    const anchor = menuAnchorRef.current;
    if (!anchor) {
      setMenuAnchor(null);
      setMenuVisible(true);
      return;
    }

    anchor.measureInWindow((x, y, width, height) => {
      setMenuAnchor({ x, y, width, height });
      setMenuVisible(true);
    });
  };

  const handleTogglePinned = async () => {
    if (!sessionKey) return;
    const nextTabs = await setTerminalTabPinned(sessionKey, !activePinned);
    setTerminalTabs(nextTabs);
    closeMenu();
  };

  const handleCloseCurrentTab = async () => {
    if (!sessionKey) return;

    const nextTabs = await closeTerminalTab(sessionKey);
    setTerminalTabs(nextTabs);
    closeMenu();

    const nextSessionKey = pickNextTabAfterClose(
      sessionKey,
      terminalTabs,
      nextTabs,
    );
    if (nextSessionKey) {
      const parsed = parseSessionKey(nextSessionKey);
      if (parsed) {
        router.replace({
          pathname: "/terminal/[id]",
          params: { id: parsed.agentId, serverId: parsed.serverId },
        });
        return;
      }
      return;
    }

    router.replace("/");
  };

  const handleCloseOtherTabs = async () => {
    if (!sessionKey) return;

    const nextTabs = await closeOtherTerminalTabs(sessionKey);
    setTerminalTabs(nextTabs);
    closeMenu();
  };

  const handleTerminateAgent = () => {
    if (!sessionKey || !serverId || !agentId) return;

    closeMenu();

    if (connectionState !== "connected") {
      Alert.alert(
        "Daemon unavailable",
        "Reconnect to that daemon before terminating the agent.",
      );
      return;
    }

    Alert.alert(
      "Terminate?",
      "This will terminate " +
        (displayName || agentId) +
        " on " +
        (agent?.serverName || serverId) +
        ". It does more than closing the tab.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Terminate",
          style: "destructive",
          onPress: () => {
            void performTerminateAgent();
          },
        },
      ],
    );
  };

  const performTerminateAgent = async () => {
    if (!sessionKey || !serverId || !agentId) return;

    const currentTabs = terminalTabs;
    const nextTabs = await closeTerminalTab(sessionKey);
    setTerminalTabs(nextTabs);
    wsClient.killAgent(serverId, agentId);

    const nextSessionKey = pickNextTabAfterClose(
      sessionKey,
      currentTabs,
      nextTabs,
    );
    if (nextSessionKey) {
      const parsed = parseSessionKey(nextSessionKey);
      if (parsed) {
        router.replace({
          pathname: "/terminal/[id]",
          params: { id: parsed.agentId, serverId: parsed.serverId },
        });
        return;
      }
    }

    router.replace("/");
  };

  const handleSaveRename = async () => {
    if (!sessionKey) return;
    const nextAliases = await setAgentAlias(sessionKey, renameDraft);
    setAgentAliases(nextAliases);
    setRenameVisible(false);
  };

  const handleCtrlArmedChange = useCallback((next: boolean) => {
    setCtrlArmed(next);
  }, []);

  const gitDiffChip = useMemo(() => {
    if (!gitDiffQueryEnabled) {
      return null;
    }
    if (gitDiffStatus?.reason === "not_git_repo") {
      return null;
    }

    const tone: "clean" | "dirty" | "error" | "loading" =
      gitDiffLoading && !gitDiffStatus
        ? "loading"
        : gitDiffStatus?.available
          ? gitDiffStatus.clean
            ? "clean"
            : "dirty"
          : gitDiffError
            ? "error"
            : "loading";

    return {
      label: buildGitDiffChipLabel(gitDiffStatus, gitDiffLoading),
      tone,
      onPress: openGitDiff,
    };
  }, [
    gitDiffError,
    gitDiffLoading,
    gitDiffQueryEnabled,
    gitDiffStatus,
    openGitDiff,
  ]);

  const createTerminal = async (input: {
    cwd: string;
    command: string;
    name: string;
  }) => {
    if (!serverId || connectionState !== "connected" || creatingSession) {
      if (connectionState !== "connected") {
        Alert.alert(
          "Daemon unavailable",
          "Reconnect to that daemon before creating a new terminal.",
        );
      }
      return;
    }

    setNewTerminalVisible(false);
    setPickerVisible(false);
    closeMenu();
    setCreatingSession(true);
    try {
      const nextAgentId = await wsClient.createSession(serverId, {
        targetId: agentId,
        cwd: input.cwd,
        command: input.command,
        name: input.name,
      });
      const nextSessionKey = makeSessionKey(serverId, nextAgentId);
      const openedAt = Date.now();
      const nextTabs = await touchTerminalTab(nextSessionKey);
      setTerminalTabs(nextTabs);
      void markAgentOpened(nextSessionKey, openedAt);
      setRecentAgentOpens((previous) => ({
        ...previous,
        [nextSessionKey]: openedAt,
      }));
      router.replace({
        pathname: "/terminal/[id]",
        params: { id: nextAgentId, serverId },
      });
    } catch (error: any) {
      Alert.alert(
        "Could not create terminal",
        error?.message || "Try reconnecting to that daemon first.",
      );
    } finally {
      setCreatingSession(false);
    }
  };

  const openNewTerminal = () => {
    if (connectionState !== "connected") {
      Alert.alert(
        "Daemon unavailable",
        "Reconnect to that daemon before creating a new terminal.",
      );
      return;
    }
    setNewTerminalVisible(true);
  };

  const openLinkedIssue = () => {
    if (!linkedIssue) return;
    closeMenu();
    router.push({
      pathname: "/issue/[id]",
      params: { id: linkedIssue.id, serverId: linkedIssue.serverId },
    });
  };

  const retryServerConnection = async () => {
    if (!serverId) return;
    const storedServer = await getServerById(serverId);
    if (!storedServer) return;

    setServer(storedServer);
    wsClient.connectServer(storedServer);
  };

  const terminalViewport = (
    <>
      <View
        style={[
          styles.output,
          { backgroundColor: terminalTheme.background },
          outputBottomInset > 0 ? { paddingBottom: outputBottomInset } : null,
        ]}
      >
        {shouldMountTerminalSurface && sessionKey && serverId && agentId ? (
          <TerminalSurface
            key={sessionKey}
            ref={terminalRef}
            serverId={serverId}
            targetId={agentId}
            themeName={themeName}
            ctrlArmed={ctrlArmed}
            onCtrlArmedChange={handleCtrlArmedChange}
          />
        ) : null}
        {canRenderTerminal ? null : (
          <View style={styles.terminalState}>
            <View
              style={[
                styles.terminalStateCard,
                {
                  backgroundColor: chromeColors.surface,
                  borderColor: terminalStateAccent,
                },
              ]}
            >
              {terminalStateBusy ? (
                <ActivityIndicator color={terminalStateAccent} />
              ) : (
                <View
                  style={[
                    styles.terminalStateDot,
                    { backgroundColor: terminalStateAccent },
                  ]}
                />
              )}
              <Text style={[styles.terminalStateTitle, { color: chromeColors.text }]}>
                {terminalStateTitle}
              </Text>
              <Text
                style={[styles.terminalStateDetail, { color: chromeColors.textMuted }]}
              >
                {terminalStateDetail}
              </Text>
              <Text
                style={[styles.terminalStateHint, { color: chromeColors.textSubtle }]}
              >
                {terminalStateHint}
              </Text>
              {hasTerminalRoute ? (
                <TouchableOpacity
                  style={[
                    styles.terminalStateAction,
                    { backgroundColor: chromeColors.accent },
                  ]}
                  onPress={() => void retryServerConnection()}
                  activeOpacity={0.84}
                >
                  <Text
                    style={[
                      styles.terminalStateActionText,
                      { color: terminalTheme.background },
                    ]}
                  >
                    Retry Connection
                  </Text>
                </TouchableOpacity>
              ) : null}
            </View>
          </View>
        )}
      </View>

      {accessoryVisible ? (
        <View
          onLayout={(event) => {
            const nextHeight = Math.ceil(event.nativeEvent.layout.height);
            setAccessoryHeight((previous) =>
              Math.abs(previous - nextHeight) <= 1 ? previous : nextHeight,
            );
          }}
          style={[
            styles.inputShell,
            styles.inputShellDock,
            { bottom: accessoryBottomOffset },
          ]}
        >
          <TerminalAccessoryBar
            terminalRef={terminalRef}
            serverUrl={server?.url || ""}
            daemonId={server?.daemonId || ""}
            theme={terminalTheme}
            gitDiff={gitDiffChip}
            ctrlArmed={ctrlArmed}
            onCtrlArmedChange={handleCtrlArmedChange}
          />
        </View>
      ) : null}
    </>
  );

  return (
    <SafeAreaView
      style={[styles.container, { backgroundColor: chromeColors.appBackground }]}
      edges={["top"]}
    >
      <View
        style={[
          styles.topBar,
          { backgroundColor: terminalTheme.background },
        ]}
      >
        <TouchableOpacity
          onPress={goToInbox}
          style={styles.chromeButton}
          activeOpacity={0.75}
        >
          <Ionicons name="chevron-back" size={20} color={chromeColors.textMuted} />
        </TouchableOpacity>

        <ScrollView
          ref={tabScrollRef}
          horizontal
          showsHorizontalScrollIndicator={false}
          style={styles.tabScroller}
          contentContainerStyle={styles.tabScrollerContent}
        >
          {tabs.map((tab) => (
            <View
              key={tab.id}
              style={[
                styles.tabPill,
                tab.active && [
                  styles.tabPillActive,
                  { backgroundColor: chromeColors.surfaceMuted },
                ],
              ]}
              onLayout={(e) => {
                const { x, width } = e.nativeEvent.layout;
                tabLayoutsRef.current.set(tab.id, { x, width });
              }}
            >
              <TouchableOpacity
                style={styles.tabMainButton}
                onPress={() => openAgentTab(tab.id)}
                activeOpacity={0.84}
              >
                <AgentKindIcon kind={tab.kind} size={10} />
                <View style={styles.tabLabelWrapper}>
                  <Text
                    style={[
                      styles.tabLabel,
                      { color: tab.active ? chromeColors.text : chromeColors.textSubtle },
                    ]}
                    numberOfLines={1}
                  >
                    {tab.name}
                  </Text>
                </View>
                {tab.pinned ? (
                  <Ionicons
                    name="bookmark"
                    size={10}
                    color={tab.active ? chromeColors.textMuted : chromeColors.textSubtle}
                  />
                ) : null}
                <View
                  style={[
                    styles.tabStatusDot,
                    { backgroundColor: statusColor(tab.status) },
                    !tab.active && styles.tabStatusDotInactive,
                  ]}
                />
              </TouchableOpacity>

              {tab.active ? (
                <View ref={menuAnchorRef} collapsable={false}>
                  <TouchableOpacity
                    style={styles.tabMenuButton}
                    onPress={openMenu}
                    activeOpacity={0.75}
                  >
                    <Ionicons
                      name="ellipsis-vertical"
                      size={15}
                      color={chromeColors.textMuted}
                    />
                  </TouchableOpacity>
                </View>
              ) : null}
            </View>
          ))}
        </ScrollView>

        <TouchableOpacity
          onPress={openNewTerminal}
          style={styles.chromeButton}
          activeOpacity={0.75}
        >
          <Ionicons name="add" size={20} color={chromeColors.textMuted} />
        </TouchableOpacity>
      </View>

      <View style={[styles.terminalStage, { backgroundColor: terminalTheme.background }]}>
        <View style={[styles.terminalShell, { backgroundColor: terminalTheme.background }]}>
          <View style={styles.terminalContent}>{terminalViewport}</View>
        </View>
      </View>

      <Modal
        visible={pickerVisible}
        transparent
        animationType="fade"
        onRequestClose={() => setPickerVisible(false)}
      >
        <View style={styles.modalRoot}>
          <TouchableOpacity
            style={styles.modalBackdrop}
            activeOpacity={1}
            onPress={() => setPickerVisible(false)}
          />

          <View
            style={[
              styles.sheetCard,
              {
                backgroundColor: chromeColors.surface,
                borderColor: chromeColors.border,
              },
            ]}
          >
            <View
              style={[
                styles.sheetHandle,
                { backgroundColor: chromeColors.textSubtle },
              ]}
            />

            <ScrollView
              style={styles.sheetScroll}
              contentContainerStyle={styles.sheetScrollContent}
              showsVerticalScrollIndicator={false}
            >
              {sortedAgents.length === 0 ? (
                <Text style={styles.sheetEmpty}>No agents available.</Text>
              ) : (
                pickerSections.map((section) => (
                  <View key={section.key} style={styles.sheetSection}>
                    <View style={styles.sheetSectionHeader}>
                      <View style={styles.sheetSectionBody}>
                        <Text style={styles.sheetSectionTitle} numberOfLines={1}>
                          {section.title}
                        </Text>
                        <Text style={styles.sheetSectionSubtitle} numberOfLines={1}>
                          {section.subtitle}
                        </Text>
                      </View>
                      <Text style={styles.sheetSectionCount}>{section.data.length}</Text>
                    </View>

                    {section.data.map((item) => {
                      const isActive = item.key === sessionKey;
                      const presented = presentAgent(item, agentAliases[item.key]);
                      const meta = [
                        presented.typeLabel,
                        showPickerServerNames ? item.serverName : null,
                      ]
                        .filter(Boolean)
                        .join(" · ");

                      return (
                        <TouchableOpacity
                          key={item.key}
                          style={[
                            styles.agentRow,
                            isActive && styles.agentRowActive,
                          ]}
                          onPress={() => openAgentTab(item.key)}
                          activeOpacity={0.84}
                        >
                          <AgentKindIcon kind={presented.kind} size={15} />
                          <View style={styles.agentRowBody}>
                            <Text style={styles.agentRowTitle} numberOfLines={1}>
                              {presented.title}
                            </Text>
                            <Text style={styles.agentRowMeta} numberOfLines={1}>
                              {meta}
                            </Text>
                          </View>
                          <View
                            style={[
                              styles.agentRowStatusDot,
                              { backgroundColor: statusColor(item.status) },
                            ]}
                          />
                        </TouchableOpacity>
                      );
                    })}
                  </View>
                ))
              )}
            </ScrollView>

            <TouchableOpacity
              style={[
                styles.sheetCreateButton,
                {
                  backgroundColor: chromeColors.surfaceMuted,
                  borderColor: chromeColors.border,
                },
                creatingSession && styles.sheetCreateButtonDisabled,
              ]}
              onPress={openNewTerminal}
              disabled={creatingSession}
              activeOpacity={0.84}
            >
              <Ionicons name="add" size={16} color={chromeColors.textMuted} />
              <Text
                style={[styles.sheetCreateButtonText, { color: chromeColors.textMuted }]}
              >
                {creatingSession ? "Starting…" : "New Terminal"}
              </Text>
            </TouchableOpacity>
          </View>
        </View>
      </Modal>

      <Modal
        visible={menuVisible}
        transparent
        animationType="none"
        onRequestClose={closeMenu}
      >
        <View style={styles.popoverRoot}>
          <TouchableOpacity
            style={styles.popoverBackdrop}
            activeOpacity={1}
            onPress={closeMenu}
          />

          <View
            style={[
              styles.menuPopover,
              {
                backgroundColor: chromeColors.surface,
                left: menuPosition.left,
                top: menuPosition.top,
                width: MENU_POPOVER_WIDTH,
                borderColor: chromeColors.border,
              },
            ]}
          >
            <MenuAction
              label={creatingSession ? "Starting Terminal…" : "New Terminal"}
              onPress={openNewTerminal}
              disabled={creatingSession || connectionState !== "connected"}
              textColor={chromeColors.text}
              disabledTextColor={chromeColors.textSubtle}
              destructiveColor={terminalTheme.red}
            />
            <MenuAction
              label="Git Diff"
              onPress={openGitDiff}
              disabled={!gitDiffQueryEnabled || gitDiffStatus?.reason === "not_git_repo"}
              textColor={chromeColors.text}
              disabledTextColor={chromeColors.textSubtle}
              destructiveColor={terminalTheme.red}
            />
            <MenuAction
              label="Rename"
              onPress={openRenameModal}
              textColor={chromeColors.text}
              disabledTextColor={chromeColors.textSubtle}
              destructiveColor={terminalTheme.red}
            />
            <MenuAction
              label={activePinned ? "Unpin Tab" : "Pin Tab"}
              onPress={handleTogglePinned}
              textColor={chromeColors.text}
              disabledTextColor={chromeColors.textSubtle}
              destructiveColor={terminalTheme.red}
            />
            <MenuAction
              label="Close Other Tabs"
              onPress={handleCloseOtherTabs}
              disabled={tabs.length <= 1}
              textColor={chromeColors.text}
              disabledTextColor={chromeColors.textSubtle}
              destructiveColor={terminalTheme.red}
            />
            <MenuAction
              label="Close Tab"
              onPress={handleCloseCurrentTab}
              textColor={chromeColors.text}
              disabledTextColor={chromeColors.textSubtle}
              destructiveColor={terminalTheme.red}
            />
            {linkedIssue ? (
              <MenuAction
                label="Open Linked Issue"
                onPress={openLinkedIssue}
                textColor={chromeColors.text}
                disabledTextColor={chromeColors.textSubtle}
                destructiveColor={terminalTheme.red}
              />
            ) : null}
            <MenuAction
              label="Terminate"
              onPress={handleTerminateAgent}
              destructive
              textColor={chromeColors.text}
              disabledTextColor={chromeColors.textSubtle}
              destructiveColor={terminalTheme.red}
            />
          </View>
        </View>
      </Modal>

      <NewTerminalSheet
        visible={newTerminalVisible}
        title="New Terminal"
        subtitle="Start a plain shell here, or launch Claude/Codex in the current project."
        initialCwd={agent?.cwd || ""}
        selectedServerId={serverId}
        submitting={creatingSession}
        onClose={() => setNewTerminalVisible(false)}
        onSubmit={(input) => {
          void createTerminal({
            cwd: input.cwd,
            command: input.command,
            name: input.name,
          });
        }}
      />

      <GitDiffSheet
        visible={gitDiffVisible}
        theme={terminalTheme}
        snapshot={gitDiffStatus}
        loading={gitDiffLoading}
        error={gitDiffError}
        patchByPath={gitDiffPatchByPath}
        patchLoadingByPath={gitDiffPatchLoadingByPath}
        patchErrorByPath={gitDiffPatchErrorByPath}
        repoBrowserPath={repoBrowserPath}
        repoBrowserEntries={repoBrowserEntries}
        repoBrowserLoading={repoBrowserLoading}
        repoBrowserError={repoBrowserError}
        repoFilePath={repoFilePath}
        repoFileLoadingPath={repoFileLoadingPath}
        repoFileError={repoFileError}
        repoFileByPath={repoFileByPath}
        onClose={() => setGitDiffVisible(false)}
        onRefresh={() => { void refreshGitDiff(true); }}
        onOpenRepoPath={(path) => { void loadRepoBrowserPath(path); }}
        onOpenRepoFile={(path) => { void openRepoFile(path); }}
        onCloseRepoFile={closeRepoFile}
        onBackRepoPath={goUpRepoBrowserPath}
      />

      <Modal
        visible={renameVisible}
        transparent
        animationType="fade"
        onRequestClose={() => setRenameVisible(false)}
      >
        <KeyboardAvoidingView
          style={styles.renameRoot}
          behavior={Platform.OS === "ios" ? "padding" : "height"}
        >
          <TouchableOpacity
            style={styles.modalBackdrop}
            activeOpacity={1}
            onPress={() => setRenameVisible(false)}
          />

          <View
            style={[
              styles.renameCard,
              {
                backgroundColor: chromeColors.surface,
                borderColor: chromeColors.border,
              },
            ]}
          >
            <Text style={[styles.renameTitle, { color: chromeColors.text }]}>
              Rename Terminal
            </Text>
            <Text style={[styles.renameHint, { color: chromeColors.textMuted }]}>
              Only changes the local display name on this device.
            </Text>
            <TextInput
              style={[
                styles.renameInput,
                {
                  color: chromeColors.text,
                  borderColor: chromeColors.border,
                  backgroundColor: chromeColors.surfaceMuted,
                },
              ]}
              value={renameDraft}
              onChangeText={setRenameDraft}
              placeholder={agent?.name || agentId}
              placeholderTextColor={chromeColors.textSubtle}
              autoFocus
              autoCorrect={false}
              autoCapitalize="none"
              returnKeyType="done"
              onSubmitEditing={handleSaveRename}
            />
            <View style={styles.renameActions}>
              <TouchableOpacity
                style={[
                  styles.renameButton,
                  {
                    backgroundColor: chromeColors.surfaceMuted,
                    borderColor: chromeColors.border,
                  },
                ]}
                onPress={() => setRenameVisible(false)}
                activeOpacity={0.84}
              >
                <Text style={[styles.renameButtonText, { color: chromeColors.textMuted }]}>
                  Cancel
                </Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={[
                  styles.renameButton,
                  styles.renameButtonPrimary,
                  {
                    backgroundColor: chromeColors.accent,
                    borderColor: chromeColors.borderStrong,
                  },
                ]}
                onPress={handleSaveRename}
                activeOpacity={0.84}
              >
                <Text
                  style={[
                    styles.renameButtonText,
                    styles.renameButtonTextPrimary,
                    { color: terminalTheme.background },
                  ]}
                >
                  Save
                </Text>
              </TouchableOpacity>
            </View>
          </View>
        </KeyboardAvoidingView>
      </Modal>
    </SafeAreaView>
  );
}

function MenuAction({
  label,
  onPress,
  disabled = false,
  destructive = false,
  textColor,
  disabledTextColor,
  destructiveColor,
}: {
  label: string;
  onPress: () => void;
  disabled?: boolean;
  destructive?: boolean;
  textColor: string;
  disabledTextColor: string;
  destructiveColor: string;
}) {
  return (
    <TouchableOpacity
      style={[styles.menuAction, disabled && styles.menuActionDisabled]}
      onPress={onPress}
      disabled={disabled}
      activeOpacity={0.84}
    >
      <Text
        style={[
          styles.menuActionText,
          { color: textColor },
          destructive && { color: destructiveColor },
          disabled && { color: disabledTextColor },
        ]}
      >
        {label}
      </Text>
    </TouchableOpacity>
  );
}

function buildDisplayTabOrder(
  currentId: string | null | undefined,
  tabs: StoredTerminalTabs,
): string[] {
  if (!currentId) return tabs.order;
  return tabs.order.includes(currentId)
    ? tabs.order
    : [...tabs.order, currentId];
}

function buildMenuPosition(
  anchor: { x: number; y: number; width: number; height: number } | null,
  windowWidth: number,
): { left: number; top: number } {
  const top = Math.max(12, (anchor?.y ?? 12) + (anchor?.height ?? 38) + 16);
  const preferredLeft =
    (anchor?.x ?? windowWidth - 14) + (anchor?.width ?? 0) - MENU_POPOVER_WIDTH;
  const maxLeft = Math.max(12, windowWidth - MENU_POPOVER_WIDTH - 12);

  return {
    left: clamp(preferredLeft, 12, maxLeft),
    top,
  };
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}

function pickNextTabAfterClose(
  closedId: string,
  currentTabs: StoredTerminalTabs,
  nextTabs: StoredTerminalTabs,
): string | null {
  const currentOrder = buildDisplayTabOrder(null, currentTabs);
  const nextOrder = buildDisplayTabOrder(null, nextTabs);
  const closedIndex = currentOrder.indexOf(closedId);

  if (closedIndex === -1) return nextOrder[0] || null;

  return currentOrder[closedIndex + 1] || currentOrder[closedIndex - 1] || null;
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: "#0D0C0C",
  },
  topBar: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 8,
    paddingTop: 2,
    paddingBottom: 4,
  },
  terminalStage: {
    flex: 1,
    minHeight: 0,
    overflow: "hidden",
    justifyContent: "center",
  },
  terminalShell: {
    flex: 1,
    minHeight: 0,
  },
  terminalContent: {
    flex: 1,
    minHeight: 0,
  },
  chromeButton: {
    width: 32,
    height: 32,
    alignItems: "center",
    justifyContent: "center",
  },
  tabScroller: {
    flex: 1,
    marginHorizontal: 4,
  },
  tabScrollerContent: {
    paddingRight: 2,
  },
  tabPill: {
    minWidth: 110,
    maxWidth: 200,
    height: 30,
    borderRadius: 8,
    paddingLeft: 8,
    paddingRight: 4,
    marginRight: 2,
    flexDirection: "row",
    alignItems: "center",
  },
  tabPillActive: {
    borderRadius: 8,
  },
  tabMainButton: {
    flex: 1,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
  },
  tabStatusDot: {
    width: 5,
    height: 5,
    borderRadius: 2.5,
    marginLeft: 6,
  },
  tabStatusDotInactive: {
    opacity: 0.45,
  },
  tabLabelWrapper: {
    flex: 1,
    justifyContent: "center",
    marginRight: 4,
    paddingTop: 1,
  },
  tabLabel: {
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
    includeFontPadding: false,
  },
  tabMenuButton: {
    width: 22,
    height: 22,
    borderRadius: 6,
    alignItems: "center",
    justifyContent: "center",
  },
  output: {
    flex: 1,
    minHeight: 0,
    paddingTop: 4,
  },
  terminalState: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 18,
    paddingBottom: 32,
  },
  terminalStateCard: {
    width: "100%",
    maxWidth: 360,
    alignItems: "center",
    paddingHorizontal: 18,
    paddingVertical: 20,
    borderRadius: 20,
    borderWidth: 1,
    backgroundColor: "rgba(17,22,31,0.9)",
  },
  terminalStateDot: {
    width: 10,
    height: 10,
    borderRadius: 5,
  },
  terminalStateTitle: {
    marginTop: 12,
    color: Colors.textPrimary,
    fontSize: 18,
    fontFamily: Typography.uiFontMedium,
    textAlign: "center",
  },
  terminalStateDetail: {
    marginTop: 8,
    color: "#D6DFEC",
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
    textAlign: "center",
  },
  terminalStateHint: {
    marginTop: 8,
    color: "#8E9DB2",
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
    textAlign: "center",
  },
  terminalStateAction: {
    marginTop: 16,
    minHeight: 38,
    paddingHorizontal: 14,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: Colors.accent,
  },
  terminalStateActionText: {
    color: Colors.bgPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  inputShell: {
    backgroundColor: "transparent",
  },
  inputShellDock: {
    position: "absolute",
    left: 0,
    right: 0,
    bottom: 0,
    zIndex: 8,
  },
  modalRoot: {
    flex: 1,
    justifyContent: "flex-end",
  },
  popoverRoot: {
    flex: 1,
  },
  popoverBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: "transparent",
  },
  modalBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: "rgba(6, 8, 12, 0.58)",
  },
  sheetCard: {
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    paddingHorizontal: 18,
    paddingTop: 12,
    paddingBottom: 28,
    backgroundColor: "#121A25",
    borderTopWidth: 1,
    borderColor: "rgba(255,255,255,0.06)",
    maxHeight: "82%",
  },
  sheetHandle: {
    alignSelf: "center",
    width: 42,
    height: 4,
    borderRadius: 2,
    backgroundColor: "#3A475B",
    marginBottom: 14,
  },
  sheetCreateButton: {
    marginTop: 12,
    height: 40,
    borderRadius: 12,
    backgroundColor: "rgba(255,255,255,0.04)",
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: "rgba(255,255,255,0.08)",
    borderStyle: "dashed" as const,
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    gap: 6,
  },
  sheetCreateButtonDisabled: {
    opacity: 0.5,
  },
  sheetCreateButtonText: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
  },
  sheetScroll: {
    marginTop: 4,
  },
  sheetScrollContent: {
    paddingBottom: 8,
  },
  sheetSection: {
    paddingTop: 18,
  },
  sheetSectionHeader: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingBottom: 10,
  },
  sheetSectionBody: {
    flex: 1,
    minWidth: 0,
  },
  sheetSectionTitle: {
    color: Colors.textPrimary,
    fontSize: 15,
    lineHeight: 20,
    fontFamily: Typography.uiFontMedium,
  },
  sheetSectionSubtitle: {
    marginTop: 2,
    color: Colors.textSecondary,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
    opacity: 0.55,
  },
  sheetSectionCount: {
    minWidth: 24,
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 999,
    overflow: "hidden",
    textAlign: "center",
    color: Colors.textSecondary,
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFontMedium,
    backgroundColor: "rgba(255,255,255,0.05)",
  },
  sheetEmpty: {
    color: "#7D8CA0",
    fontSize: 13,
    fontFamily: Typography.uiFont,
    paddingVertical: 12,
  },
  agentRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingVertical: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: "rgba(255,255,255,0.04)",
  },
  agentRowActive: {
    opacity: 1,
  },
  agentRowBody: {
    flex: 1,
    minWidth: 0,
    gap: 2,
  },
  agentRowStatusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  agentRowTitle: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  agentRowMeta: {
    color: Colors.textSecondary,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
    opacity: 0.55,
  },
  menuPopover: {
    position: "absolute",
    borderRadius: 14,
    paddingVertical: 4,
    backgroundColor: "#161F2B",
    borderWidth: 1,
    borderColor: "rgba(255,255,255,0.06)",
    shadowColor: "#000",
    shadowOpacity: 0.2,
    shadowRadius: 8,
    elevation: 8,
  },
  menuAction: {
    minHeight: 38,
    justifyContent: "center",
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  menuActionDisabled: {
    opacity: 0.52,
  },
  menuActionText: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFont,
  },
  menuActionTextDisabled: {
    color: "#556176",
  },
  menuActionTextDestructive: {
    color: "#F09999",
  },
  renameRoot: {
    flex: 1,
    justifyContent: "center",
    paddingHorizontal: 20,
  },
  renameCard: {
    borderRadius: 18,
    padding: 16,
    backgroundColor: "#161F2B",
    borderWidth: 1,
    borderColor: "rgba(255,255,255,0.06)",
  },
  renameTitle: {
    color: Colors.textPrimary,
    fontSize: 17,
    fontFamily: Typography.uiFontMedium,
  },
  renameHint: {
    marginTop: 4,
    color: "#7D8CA0",
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  renameInput: {
    marginTop: 14,
    borderRadius: 12,
    borderWidth: 1,
    borderColor: "#263345",
    backgroundColor: "#111923",
    color: Colors.textPrimary,
    paddingHorizontal: 12,
    paddingVertical: 11,
    fontSize: 14,
    fontFamily: Typography.uiFont,
  },
  renameActions: {
    flexDirection: "row",
    justifyContent: "flex-end",
    marginTop: 14,
    gap: 10,
  },
  renameButton: {
    minWidth: 72,
    height: 38,
    borderRadius: 10,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "#202A38",
  },
  renameButtonPrimary: {
    backgroundColor: Colors.accent,
  },
  renameButtonText: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  renameButtonTextPrimary: {
    color: "#07111E",
  },
});
