import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Alert,
  FlatList,
  Keyboard,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useFocusEffect, useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { AnimatedPressable } from "../components/ui/AnimatedPressable";
import { RisingSheet } from "../components/ui/RisingSheet";
import {
  Radii,
  TypeScale,
  Typography,
  useAppColors,
} from "../constants/tokens";
import {
  buildSkillsMutationConfirmation,
  completeSkillsRequest,
  createSkillsRequestState,
  failSkillsRequest,
  scopeLabel,
  skillAgentLabel,
  type CatalogSkill,
  type InstalledSkill,
  type ManagedSkillAgent,
  type SkillsCatalogResult,
  type SkillsInventory,
  type SkillsMutationCommand,
  type SkillsRequestState,
} from "../services/skillsManagement";
import { skillsTerminalHandoff } from "../services/skillsTerminalHandoff";
import { makeSessionKey } from "../services/sessionKeys";
import {
  getServers,
  markAgentOpened,
  type StoredServer,
} from "../services/storage";
import { wsClient } from "../services/websocket";
import { useAgents } from "../store/agents";

type SkillsMode = "installed" | "discover";

const MANAGED_AGENTS: ManagedSkillAgent[] = [
  "codex",
  "claude-code",
  "cursor",
];
const SEARCH_DEBOUNCE_MS = 350;

export default function SkillsScreen() {
  const router = useRouter();
  const colors = useAppColors();
  const { state } = useAgents();
  const [mode, setMode] = useState<SkillsMode>("installed");
  const [servers, setServers] = useState<StoredServer[]>([]);
  const [selectedServerId, setSelectedServerId] = useState("");
  const [focusGeneration, setFocusGeneration] = useState(0);
  const [inventoryState, setInventoryState] = useState<
    SkillsRequestState<SkillsInventory>
  >(createSkillsRequestState);
  const [query, setQuery] = useState("");
  const [searchState, setSearchState] = useState<
    SkillsRequestState<SkillsCatalogResult>
  >(createSkillsRequestState);
  const [installCandidate, setInstallCandidate] = useState<CatalogSkill | null>(
    null,
  );
  const [installScope, setInstallScope] = useState<"global" | "project">(
    "global",
  );
  const [installAgents, setInstallAgents] = useState<Set<ManagedSkillAgent>>(
    () => new Set(MANAGED_AGENTS),
  );
  const [preparingMutation, setPreparingMutation] = useState("");
  const [creatingTerminal, setCreatingTerminal] = useState(false);
  const inventoryGenerationRef = useRef(0);
  const searchGenerationRef = useRef(0);
  const activeSearchRef = useRef<{ serverId: string; generation: number } | null>(null);
  const focusedRef = useRef(false);
  const automaticRefreshKeyRef = useRef("");

  const cancelActiveSearch = useCallback(() => {
    const active = activeSearchRef.current;
    if (!active) return;
    activeSearchRef.current = null;
    wsClient.cancelSkillsCatalogSearch(active.serverId, {
      generation: active.generation,
    });
  }, []);

  const selectedServer = useMemo(
    () => servers.find((server) => server.id === selectedServerId) || null,
    [selectedServerId, servers],
  );
  const selectedConnected =
    selectedServer != null &&
    state.serverConnections[selectedServer.id] === "connected";
  const projectCwd = useMemo(() => {
    return (
      state.agents
        .filter((agent) => agent.serverId === selectedServerId && agent.cwd?.trim())
        .sort((left, right) => (right.updated_at || 0) - (left.updated_at || 0))[0]
        ?.cwd?.trim() || ""
    );
  }, [selectedServerId, state.agents]);

  useFocusEffect(
    useCallback(() => {
      focusedRef.current = true;
      let cancelled = false;
      void getServers().then((storedServers) => {
        if (cancelled) return;
        setServers(storedServers);
        setSelectedServerId((current) => {
          if (storedServers.some((server) => server.id === current)) {
            return current;
          }
          return (
            storedServers.find((server) => wsClient.isConnected(server.id))?.id ||
            storedServers[0]?.id ||
            ""
          );
        });
        setFocusGeneration((current) => current + 1);
      });
      return () => {
        cancelled = true;
        focusedRef.current = false;
        inventoryGenerationRef.current += 1;
        searchGenerationRef.current += 1;
        cancelActiveSearch();
      };
    }, [cancelActiveSearch]),
  );

  const refreshInventory = useCallback(async () => {
    if (!selectedServer || !selectedConnected) {
      const generation = ++inventoryGenerationRef.current;
      setInventoryState({
        status: "error",
        generation,
        error: selectedServer
          ? "Connect this daemon to load its installed Skills."
          : "Pair a daemon to load installed Skills.",
      });
      return;
    }
    const generation = ++inventoryGenerationRef.current;
    setInventoryState({ status: "loading", generation });
    try {
      const response = await wsClient.getSkillsInventory(selectedServer.id, {
        cwd: projectCwd || undefined,
        generation,
      });
      if (!focusedRef.current || response.generation !== inventoryGenerationRef.current) {
        return;
      }
      setInventoryState((current) =>
        completeSkillsRequest(
          current,
          generation,
          response.inventory,
          response.inventory.skills.length === 0,
        ),
      );
    } catch (error: any) {
      if (!focusedRef.current || generation !== inventoryGenerationRef.current) {
        return;
      }
      setInventoryState((current) =>
        failSkillsRequest(
          current,
          generation,
          error?.message || "Failed to load installed Skills.",
        ),
      );
    }
  }, [projectCwd, selectedConnected, selectedServer]);

  useEffect(() => {
    if (!focusGeneration || !selectedServerId) return;
    const key = `${focusGeneration}:${selectedServerId}:${projectCwd}`;
    if (automaticRefreshKeyRef.current === key) return;
    automaticRefreshKeyRef.current = key;
    void refreshInventory();
  }, [focusGeneration, projectCwd, refreshInventory, selectedServerId]);

  const runSearch = useCallback(
    async (rawQuery: string) => {
      cancelActiveSearch();
      const normalizedQuery = rawQuery.trim();
      if (normalizedQuery.length < 2) return;
      if (!selectedServer || !selectedConnected) {
        const generation = ++searchGenerationRef.current;
        setSearchState({
          status: "error",
          generation,
          error: "Connect a daemon to search the public catalog.",
        });
        return;
      }
      const generation = ++searchGenerationRef.current;
      setSearchState({ status: "loading", generation });
      activeSearchRef.current = { serverId: selectedServer.id, generation };
      try {
        const response = await wsClient.searchSkillsCatalog(selectedServer.id, {
          query: normalizedQuery,
          generation,
          limit: 20,
        });
        if (!focusedRef.current || response.generation !== searchGenerationRef.current) {
          return;
        }
        setSearchState((current) =>
          completeSkillsRequest(
            current,
            generation,
            response.result,
            response.result.skills.length === 0,
          ),
        );
      } catch (error: any) {
        if (!focusedRef.current || generation !== searchGenerationRef.current) {
          return;
        }
        setSearchState((current) =>
          failSkillsRequest(
            current,
            generation,
            error?.message || "Failed to search skills.sh.",
          ),
        );
      } finally {
        if (activeSearchRef.current?.generation === generation) {
          activeSearchRef.current = null;
        }
      }
    },
    [cancelActiveSearch, selectedConnected, selectedServer],
  );

  useEffect(() => {
    cancelActiveSearch();
    const normalizedQuery = query.trim();
    const generation = ++searchGenerationRef.current;
    setSearchState({ status: "idle", generation });
    if (mode !== "discover" || normalizedQuery.length < 2) return;
    const timer = setTimeout(() => {
      void runSearch(normalizedQuery);
    }, SEARCH_DEBOUNCE_MS);
    return () => {
      clearTimeout(timer);
      cancelActiveSearch();
    };
  }, [cancelActiveSearch, mode, query, runSearch]);

  const openInstall = (skill: CatalogSkill) => {
    Keyboard.dismiss();
    setInstallCandidate(skill);
    setInstallScope("global");
    setInstallAgents(new Set(MANAGED_AGENTS));
  };

  const handoffToTerminal = useCallback(
    async (command: SkillsMutationCommand) => {
      if (!selectedServer || !selectedConnected || creatingTerminal) return;
      if (command.scope === "project" && !projectCwd) {
        Alert.alert(
          "Project unavailable",
          "Open a Session with a project directory on this daemon first.",
        );
        return;
      }
      setCreatingTerminal(true);
      let issuedGrant: { sessionKey: string; token: string } | null = null;
      try {
        const startedAt = Date.now();
        const agentId = await wsClient.createSession(selectedServer.id, {
          cwd: projectCwd || undefined,
          name: `Skills: ${command.operation} ${command.skillName}`,
        });
        const sessionKey = makeSessionKey(selectedServer.id, agentId);
        const handoffToken = skillsTerminalHandoff.issue(sessionKey, command);
        issuedGrant = { sessionKey, token: handoffToken };
        void markAgentOpened(sessionKey, Date.now());
        router.push({
          pathname: "/terminal/[id]",
          params: {
            id: agentId,
            serverId: selectedServer.id,
            name: `Skills: ${command.operation} ${command.skillName}`,
            cwd: projectCwd,
            startedAt: String(startedAt),
            initialInterfaceRenderMode: "terminal",
            skillsHandoff: handoffToken,
          },
        });
      } catch (error: any) {
        if (issuedGrant) {
          skillsTerminalHandoff.revoke(
            issuedGrant.sessionKey,
            issuedGrant.token,
          );
        }
        Alert.alert(
          "Could not open Terminal",
          error?.message || "Reconnect to the daemon and try again.",
        );
      } finally {
        setCreatingTerminal(false);
      }
    },
    [
      creatingTerminal,
      projectCwd,
      router,
      selectedConnected,
      selectedServer,
    ],
  );

  const confirmCommand = useCallback(
    (command: SkillsMutationCommand) => {
      const confirmation = buildSkillsMutationConfirmation(command);
      Alert.alert(confirmation.title, confirmation.message, [
        { text: "Cancel", style: "cancel" },
        {
          text: confirmation.confirmLabel,
          style: command.operation === "remove" ? "destructive" : "default",
          onPress: () => {
            void handoffToTerminal(command);
          },
        },
      ]);
    },
    [handoffToTerminal],
  );

  const prepareInstalledMutation = useCallback(
    async (skill: InstalledSkill) => {
      if (!selectedServer || !selectedConnected || preparingMutation) return;
      const key = `remove:${skill.id}`;
      setPreparingMutation(key);
      try {
        const agents = skill.agents.filter(
          (agent): agent is ManagedSkillAgent => agent !== "grok",
        );
        const command = await wsClient.buildSkillsCommand(selectedServer.id, {
          operation: "remove",
          cwd: projectCwd || undefined,
          skillId: skill.id,
          skillName: skill.name,
          scope: skill.scope as "project" | "global",
          agents,
        });
        confirmCommand(command);
      } catch (error: any) {
        Alert.alert(
          "Command rejected",
          error?.message || "This Skill cannot be safely managed.",
        );
      } finally {
        setPreparingMutation("");
      }
    },
    [
      confirmCommand,
      preparingMutation,
      projectCwd,
      selectedConnected,
      selectedServer,
    ],
  );

  const prepareInstall = useCallback(async () => {
    if (
      !installCandidate ||
      !selectedServer ||
      !selectedConnected ||
      installAgents.size === 0 ||
      preparingMutation
    ) {
      return;
    }
    if (installScope === "project" && !projectCwd) return;
    setPreparingMutation(`install:${installCandidate.id}`);
    try {
      const command = await wsClient.buildSkillsCommand(selectedServer.id, {
        operation: "install",
        cwd: projectCwd || undefined,
        skillId: installCandidate.id,
        source: installCandidate.source,
        skillName: installCandidate.name,
        scope: installScope,
        agents: MANAGED_AGENTS.filter((agent) => installAgents.has(agent)),
      });
      setInstallCandidate(null);
      confirmCommand(command);
    } catch (error: any) {
      Alert.alert(
        "Command rejected",
        error?.message || "This catalog identity cannot be installed safely.",
      );
    } finally {
      setPreparingMutation("");
    }
  }, [
    confirmCommand,
    installAgents,
    installCandidate,
    installScope,
    preparingMutation,
    projectCwd,
    selectedConnected,
    selectedServer,
  ]);

  const inventory =
    inventoryState.status === "ready" || inventoryState.status === "empty"
      ? inventoryState.data
      : undefined;
  const search =
    searchState.status === "ready" || searchState.status === "empty"
      ? searchState.data
      : undefined;

  return (
    <SafeAreaView style={[styles.root, { backgroundColor: colors.bgPrimary }]} edges={["bottom"]}>
      <View style={styles.header}>
        <Text style={[styles.intro, { color: colors.textSecondary }]}>
          Inspect host Skills and hand validated changes to a visible Terminal.
        </Text>

        <ServerSelector
          servers={servers}
          selectedServerId={selectedServerId}
          connectionStates={state.serverConnections}
          onSelect={setSelectedServerId}
          onOpenSettings={() => router.push("/settings")}
        />

        <View style={[styles.modeSwitch, { backgroundColor: colors.surfaceSubtle }]}>
          {(["installed", "discover"] as SkillsMode[]).map((value) => (
            <Pressable
              key={value}
              accessibilityRole="tab"
              accessibilityState={{ selected: mode === value }}
              onPress={() => setMode(value)}
              style={[
                styles.modeButton,
                mode === value ? { backgroundColor: colors.bgElevated } : null,
              ]}
            >
              <Text
                style={[
                  styles.modeLabel,
                  {
                    color: mode === value ? colors.textPrimary : colors.textTertiary,
                    fontFamily: Typography.uiFontMedium,
                  },
                ]}
              >
                {value === "installed" ? "Installed" : "Discover"}
              </Text>
            </Pressable>
          ))}
        </View>

      </View>

      {mode === "installed" ? (
        <InstalledView
          state={inventoryState}
          inventory={inventory}
          projectCwd={projectCwd}
          refreshing={inventoryState.status === "loading"}
          preparingMutation={preparingMutation}
          onRefresh={() => void refreshInventory()}
          onRemove={(skill) => void prepareInstalledMutation(skill)}
        />
      ) : (
        <DiscoverView
          query={query}
          state={searchState}
          result={search}
          onChangeQuery={setQuery}
          onRetry={() => void runSearch(query)}
          onInstall={openInstall}
        />
      )}

      <RisingSheet
        visible={installCandidate != null}
        onClose={() => setInstallCandidate(null)}
        cardStyle={[
          styles.installSheet,
          { backgroundColor: colors.modalSurface, borderColor: colors.borderSubtle },
        ]}
      >
        {installCandidate ? (
          <>
            <Text style={[styles.sheetTitle, { color: colors.textPrimary }]}>
              Install {installCandidate.name}
            </Text>
            <Text style={[styles.sheetSource, { color: colors.textTertiary }]}>
              {installCandidate.source}
            </Text>
            <Text style={[styles.sectionLabel, { color: colors.textSecondary }]}>Scope</Text>
            <View style={styles.choiceRow}>
              {(["global", "project"] as const).map((scope) => {
                const disabled = scope === "project" && !projectCwd;
                return (
                  <ChoiceButton
                    key={scope}
                    label={scope === "global" ? "Global · recommended" : "Project"}
                    selected={installScope === scope}
                    disabled={disabled}
                    onPress={() => setInstallScope(scope)}
                  />
                );
              })}
            </View>
            {!projectCwd ? (
              <Text style={[styles.helper, { color: colors.textTertiary }]}>
                Project scope needs an active Session working directory.
              </Text>
            ) : null}
            <Text style={[styles.sectionLabel, { color: colors.textSecondary }]}>Target agents</Text>
            <View style={styles.agentChoices}>
              {MANAGED_AGENTS.map((agent) => (
                <ChoiceButton
                  key={agent}
                  label={skillAgentLabel(agent)}
                  selected={installAgents.has(agent)}
                  onPress={() =>
                    setInstallAgents((current) => {
                      const next = new Set(current);
                      if (next.has(agent)) next.delete(agent);
                      else next.add(agent);
                      return next;
                    })
                  }
                />
              ))}
              <ChoiceButton label="Grok · unsupported" selected={false} disabled onPress={() => undefined} />
            </View>
            <View style={styles.sheetActions}>
              <AnimatedPressable
                accessibilityRole="button"
                onPress={() => setInstallCandidate(null)}
                style={[styles.secondaryAction, { borderColor: colors.borderSubtle }]}
              >
                <Text style={[styles.actionText, { color: colors.textSecondary }]}>Cancel</Text>
              </AnimatedPressable>
              <AnimatedPressable
                accessibilityRole="button"
                disabled={
                  installAgents.size === 0 ||
                  (installScope === "project" && !projectCwd) ||
                  Boolean(preparingMutation)
                }
                onPress={() => void prepareInstall()}
                style={[styles.primaryAction, { backgroundColor: colors.accent }]}
              >
                {preparingMutation.startsWith("install:") ? (
                  <ActivityIndicator size="small" color={colors.textOnAccent} />
                ) : (
                  <Text style={[styles.actionText, { color: colors.textOnAccent }]}>Review command</Text>
                )}
              </AnimatedPressable>
            </View>
          </>
        ) : null}
      </RisingSheet>
    </SafeAreaView>
  );
}

function ServerSelector({
  servers,
  selectedServerId,
  connectionStates,
  onSelect,
  onOpenSettings,
}: {
  servers: StoredServer[];
  selectedServerId: string;
  connectionStates: Record<string, string>;
  onSelect(serverId: string): void;
  onOpenSettings(): void;
}) {
  const colors = useAppColors();
  if (servers.length === 0) {
    return (
      <AnimatedPressable
        accessibilityRole="button"
        onPress={onOpenSettings}
        style={[styles.emptyServer, { borderColor: colors.borderSubtle }]}
      >
        <Text style={[styles.emptyServerTitle, { color: colors.textPrimary }]}>Pair a daemon</Text>
        <Text style={[styles.helper, { color: colors.textTertiary }]}>Skills are discovered on the host, never on this device.</Text>
      </AnimatedPressable>
    );
  }
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.serverRow}>
      {servers.map((server) => {
        const selected = server.id === selectedServerId;
        const connected = connectionStates[server.id] === "connected";
        return (
          <Pressable
            key={server.id}
            accessibilityRole="button"
            accessibilityState={{ selected }}
            onPress={() => onSelect(server.id)}
            style={[
              styles.serverChip,
              {
                backgroundColor: selected ? colors.accentSoft : colors.surfaceSubtle,
                borderColor: selected ? colors.accent : colors.borderSubtle,
              },
            ]}
          >
            <View style={[styles.serverDot, { backgroundColor: connected ? colors.statusRunning : colors.statusUnknown }]} />
            <Text style={[styles.serverName, { color: colors.textPrimary }]}>{server.name}</Text>
          </Pressable>
        );
      })}
    </ScrollView>
  );
}

function InstalledView({
  state,
  inventory,
  projectCwd,
  refreshing,
  preparingMutation,
  onRefresh,
  onRemove,
}: {
  state: SkillsRequestState<SkillsInventory>;
  inventory?: SkillsInventory;
  projectCwd: string;
  refreshing: boolean;
  preparingMutation: string;
  onRefresh(): void;
  onRemove(skill: InstalledSkill): void;
}) {
  const colors = useAppColors();
  return (
    <FlatList
      style={styles.list}
      data={inventory?.skills ?? []}
      keyExtractor={(skill) => skill.id}
      keyboardShouldPersistTaps="handled"
      contentContainerStyle={styles.listContent}
      ItemSeparatorComponent={() => <View style={styles.listGap} />}
      renderItem={({ item }) => (
        <InstalledSkillCard
          skill={item}
          preparingMutation={preparingMutation}
          onRemove={onRemove}
        />
      )}
      ListHeaderComponent={(
        <View style={styles.modeContent}>
          <View style={styles.inventoryHeader}>
            <View style={styles.inventoryHeaderCopy}>
              <Text style={[styles.sectionTitle, { color: colors.textPrimary }]}>Host inventory</Text>
              <Text numberOfLines={1} style={[styles.helper, { color: colors.textTertiary }]}>
                {projectCwd || "Global scope only · no active project directory"}
              </Text>
            </View>
            <AnimatedPressable
              accessibilityRole="button"
              accessibilityLabel="Refresh installed Skills"
              disabled={refreshing}
              onPress={onRefresh}
              style={[styles.refreshButton, { borderColor: colors.borderSubtle }]}
            >
              {refreshing ? <ActivityIndicator size="small" color={colors.accent} /> : <Ionicons name="refresh" size={18} color={colors.textSecondary} />}
            </AnimatedPressable>
          </View>
          {state.status === "loading" && !inventory ? <RequestState title="Reading host inventory…" busy /> : null}
          {state.status === "error" ? <RequestState title="Inventory unavailable" detail={state.error} action="Retry" onAction={onRefresh} /> : null}
          {state.status === "empty" ? <RequestState title="No Skills found" detail="This is a current host snapshot. Install from Discover or choose another daemon." /> : null}
          {inventory ? (
            <>
              <View style={[styles.supportCard, { backgroundColor: colors.surfaceSubtle, borderColor: colors.borderSubtle }]}>
                <Text style={[styles.supportTitle, { color: colors.textSecondary }]}>Supported agents</Text>
                <Text style={[styles.supportText, { color: colors.textTertiary }]}>
                  {inventory.agents.map((agent) => `${agent.name}: ${agent.supported ? "supported" : "unsupported"}`).join("  ·  ")}
                </Text>
              </View>
              {inventory.warnings.map((warning) => <Text key={warning} style={[styles.warning, { color: colors.warning }]}>{warning}</Text>)}
            </>
          ) : null}
        </View>
      )}
      ListFooterComponent={inventory ? (
        <Text style={[styles.generated, { color: colors.textTertiary }]}>Snapshot {new Date(inventory.generatedAt).toLocaleString()}</Text>
      ) : null}
    />
  );
}

function InstalledSkillCard({
  skill,
  preparingMutation,
  onRemove,
}: {
  skill: InstalledSkill;
  preparingMutation: string;
  onRemove(skill: InstalledSkill): void;
}) {
  const colors = useAppColors();
  const busy = preparingMutation.endsWith(`:${skill.id}`);
  return (
    <View style={[styles.card, { backgroundColor: colors.bgSurface, borderColor: colors.borderSubtle }]}>
      <View style={styles.cardTitleRow}>
        <Text style={[styles.cardTitle, { color: colors.textPrimary }]}>{skill.name}</Text>
        <View style={[styles.scopePill, { backgroundColor: colors.surfaceSubtle }]}>
          <Text style={[styles.scopeText, { color: colors.textSecondary }]}>{scopeLabel(skill.scope)}</Text>
        </View>
      </View>
      {skill.description ? (
        <Text style={[styles.description, { color: colors.textSecondary }]}>{skill.description}</Text>
      ) : null}
      <Text style={[styles.meta, { color: colors.textTertiary }]}>
        {skill.agents.length ? skill.agents.map(skillAgentLabel).join(" · ") : "No supported agent binding"}
      </Text>
      <Text selectable style={[styles.path, { color: colors.textTertiary }]}>{skill.canonicalPath}</Text>
      {skill.bindings.map((binding) => (
        <Text key={`${binding.scope}:${binding.sourcePath}`} selectable style={[styles.path, { color: colors.textTertiary }]}>
          {scopeLabel(binding.scope)} binding: {binding.sourcePath}
        </Text>
      ))}
      <Text style={[styles.meta, { color: colors.textTertiary }]}>
        {skill.manager} · {skill.source || skill.plugin || skill.provenance}
      </Text>
      {!skill.capability.canRemove && skill.capability.reason ? (
        <Text style={[styles.unmanaged, { color: colors.textTertiary }]}>{skill.capability.reason}</Text>
      ) : null}
      {skill.capability.canRemove ? (
        <View style={styles.cardActions}>
          <SmallAction label="Remove" destructive busy={busy} onPress={() => onRemove(skill)} />
        </View>
      ) : null}
    </View>
  );
}

function DiscoverView({
  query,
  state,
  result,
  onChangeQuery,
  onRetry,
  onInstall,
}: {
  query: string;
  state: SkillsRequestState<SkillsCatalogResult>;
  result?: SkillsCatalogResult;
  onChangeQuery(value: string): void;
  onRetry(): void;
  onInstall(skill: CatalogSkill): void;
}) {
  const colors = useAppColors();
  const shortQuery = query.trim().length < 2;
  return (
    <FlatList
      style={styles.list}
      data={result?.skills ?? []}
      keyExtractor={(skill) => skill.id}
      keyboardShouldPersistTaps="handled"
      contentContainerStyle={styles.listContent}
      ItemSeparatorComponent={() => <View style={styles.listGap} />}
      ListHeaderComponent={(
        <View style={styles.modeContent}>
          <View style={[styles.searchBox, { backgroundColor: colors.inputBackground, borderColor: colors.borderSubtle }]}>
            <Ionicons name="search" size={18} color={colors.textTertiary} />
            <TextInput value={query} onChangeText={onChangeQuery} placeholder="Search skills.sh" placeholderTextColor={colors.textTertiary} autoCapitalize="none" autoCorrect={false} returnKeyType="search" onSubmitEditing={onRetry} style={[styles.searchInput, { color: colors.textPrimary }]} />
            {state.status === "loading" ? <ActivityIndicator size="small" color={colors.accent} /> : null}
          </View>
          {shortQuery ? <RequestState title="Search the public catalog" detail="Enter at least two characters. Short and empty queries stay on this device." /> : null}
          {state.status === "error" ? <RequestState title="Search unavailable" detail={state.error} action="Retry" onAction={onRetry} /> : null}
          {state.status === "empty" ? <RequestState title="No matching Skills" detail={`No public results for “${result?.query || query.trim()}”.`} action="Retry" onAction={onRetry} /> : null}
        </View>
      )}
      renderItem={({ item: skill }) => (
        <View style={[styles.card, { backgroundColor: colors.bgSurface, borderColor: colors.borderSubtle }]}>
          <View style={styles.catalogRow}>
            <View style={styles.catalogCopy}>
              <Text style={[styles.cardTitle, { color: colors.textPrimary }]}>{skill.name}</Text>
              <Text style={[styles.meta, { color: colors.textTertiary }]}>{skill.source} · {formatInstalls(skill.installs)} installs</Text>
            </View>
            <SmallAction label="Install" onPress={() => onInstall(skill)} />
          </View>
        </View>
      )}
    />
  );
}

function RequestState({
  title,
  detail,
  busy,
  action,
  onAction,
}: {
  title: string;
  detail?: string;
  busy?: boolean;
  action?: string;
  onAction?(): void;
}) {
  const colors = useAppColors();
  return (
    <View style={[styles.requestState, { borderColor: colors.borderSubtle }]}>
      {busy ? <ActivityIndicator color={colors.accent} /> : null}
      <Text style={[styles.requestTitle, { color: colors.textPrimary }]}>{title}</Text>
      {detail ? <Text style={[styles.requestDetail, { color: colors.textTertiary }]}>{detail}</Text> : null}
      {action && onAction ? <SmallAction label={action} onPress={onAction} /> : null}
    </View>
  );
}

function ChoiceButton({
  label,
  selected,
  disabled,
  onPress,
}: {
  label: string;
  selected: boolean;
  disabled?: boolean;
  onPress(): void;
}) {
  const colors = useAppColors();
  return (
    <Pressable
      accessibilityRole="checkbox"
      accessibilityState={{ checked: selected, disabled }}
      disabled={disabled}
      onPress={onPress}
      style={[
        styles.choiceButton,
        {
          backgroundColor: selected ? colors.accentSoft : colors.surfaceSubtle,
          borderColor: selected ? colors.accent : colors.borderSubtle,
          opacity: disabled ? 0.45 : 1,
        },
      ]}
    >
      <Text style={[styles.choiceLabel, { color: colors.textPrimary }]}>{label}</Text>
    </Pressable>
  );
}

function SmallAction({
  label,
  busy,
  destructive,
  onPress,
}: {
  label: string;
  busy?: boolean;
  destructive?: boolean;
  onPress(): void;
}) {
  const colors = useAppColors();
  return (
    <AnimatedPressable
      accessibilityRole="button"
      disabled={busy}
      onPress={onPress}
      style={[
        styles.smallAction,
        {
          backgroundColor: destructive ? colors.dangerSoft : colors.surfaceSubtle,
          borderColor: destructive ? colors.dangerText : colors.borderSubtle,
        },
      ]}
    >
      {busy ? (
        <ActivityIndicator size="small" color={colors.accent} />
      ) : (
        <Text style={[styles.smallActionText, { color: destructive ? colors.dangerText : colors.textSecondary }]}>{label}</Text>
      )}
    </AnimatedPressable>
  );
}

function formatInstalls(value: number): string {
  return Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 }).format(value);
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  header: { paddingHorizontal: 16, paddingTop: 16, gap: 14 },
  list: { flex: 1 },
  listContent: { padding: 16, paddingBottom: 40 },
  listGap: { height: 12 },
  intro: { ...TypeScale.compact },
  serverRow: { gap: 8, paddingVertical: 2 },
  serverChip: { minHeight: 42, borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.pill, paddingHorizontal: 13, flexDirection: "row", alignItems: "center", gap: 8 },
  serverDot: { width: 7, height: 7, borderRadius: 4 },
  serverName: { ...TypeScale.label },
  emptyServer: { borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.md, padding: 14 },
  emptyServerTitle: { ...TypeScale.heading },
  modeSwitch: { flexDirection: "row", padding: 3, borderRadius: Radii.sm },
  modeButton: { flex: 1, minHeight: 42, borderRadius: 10, alignItems: "center", justifyContent: "center" },
  modeLabel: { fontSize: 14, lineHeight: 20 },
  modeContent: { gap: 12 },
  inventoryHeader: { flexDirection: "row", alignItems: "center", gap: 12 },
  inventoryHeaderCopy: { flex: 1 },
  sectionTitle: { ...TypeScale.heading },
  helper: { ...TypeScale.caption, marginTop: 3 },
  refreshButton: { width: 42, height: 42, borderRadius: Radii.sm, borderWidth: StyleSheet.hairlineWidth, alignItems: "center", justifyContent: "center" },
  supportCard: { borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.sm, padding: 12 },
  supportTitle: { ...TypeScale.label },
  supportText: { ...TypeScale.caption, marginTop: 5 },
  warning: { ...TypeScale.caption },
  card: { borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.md, padding: 14 },
  cardTitleRow: { flexDirection: "row", alignItems: "flex-start", gap: 8 },
  cardTitle: { ...TypeScale.heading, flex: 1 },
  scopePill: { borderRadius: Radii.pill, paddingHorizontal: 9, paddingVertical: 4 },
  scopeText: { ...TypeScale.micro },
  description: { ...TypeScale.compact, marginTop: 7 },
  meta: { ...TypeScale.caption, marginTop: 7 },
  path: { fontFamily: Typography.terminalFont, fontSize: 11, lineHeight: 17, marginTop: 7 },
  unmanaged: { ...TypeScale.caption, marginTop: 9 },
  cardActions: { flexDirection: "row", justifyContent: "flex-end", gap: 8, marginTop: 12 },
  smallAction: { minWidth: 76, minHeight: 38, paddingHorizontal: 12, borderRadius: Radii.sm, borderWidth: StyleSheet.hairlineWidth, alignItems: "center", justifyContent: "center" },
  smallActionText: { ...TypeScale.label },
  generated: { ...TypeScale.micro, textAlign: "center", marginTop: 2 },
  searchBox: { minHeight: 48, borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.sm, paddingHorizontal: 13, flexDirection: "row", alignItems: "center", gap: 9 },
  searchInput: { ...TypeScale.body, flex: 1, paddingVertical: 0 },
  catalogRow: { flexDirection: "row", alignItems: "center", gap: 12 },
  catalogCopy: { flex: 1 },
  requestState: { borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.md, minHeight: 150, padding: 20, alignItems: "center", justifyContent: "center", gap: 8 },
  requestTitle: { ...TypeScale.heading, textAlign: "center" },
  requestDetail: { ...TypeScale.compact, textAlign: "center" },
  installSheet: { marginHorizontal: 8, borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.lg, padding: 18 },
  sheetTitle: { ...TypeScale.title },
  sheetSource: { ...TypeScale.caption, marginTop: 3 },
  sectionLabel: { ...TypeScale.label, marginTop: 18, marginBottom: 8 },
  choiceRow: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
  agentChoices: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
  choiceButton: { minHeight: 40, borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.pill, paddingHorizontal: 12, alignItems: "center", justifyContent: "center" },
  choiceLabel: { ...TypeScale.caption },
  sheetActions: { flexDirection: "row", gap: 10, marginTop: 22 },
  secondaryAction: { flex: 1, minHeight: 46, borderWidth: StyleSheet.hairlineWidth, borderRadius: Radii.sm, alignItems: "center", justifyContent: "center" },
  primaryAction: { flex: 1.4, minHeight: 46, borderRadius: Radii.sm, alignItems: "center", justifyContent: "center" },
  actionText: { ...TypeScale.label },
});
