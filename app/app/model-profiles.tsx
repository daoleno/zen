import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert } from "react-native";
import { useFocusEffect, useRouter } from "expo-router";
import {
  ProvidersPresentation,
  type ModelSyncPickerState,
  type ProvidersEditorState,
  type ProviderSaveOutcome,
} from "../components/providers/ProvidersPresentation";
import { providerEditorAfterSave } from "../components/providers/providersPresentationModel";
import {
  ProviderError,
  ProviderRequestOwner,
  advancedConnectionInput,
  assertNoCredentialRetention,
  classifyMutationPersistence,
  clientForConnection,
  durabilityWarningMessage,
  firstSupportedModel,
  offlineProviderError,
  presentProviderError,
  providerMutationRequiresRefresh,
  providersScreenAfterBlur,
  resolveCreatedConnection,
  toggleModelSupport,
  type ProviderConnection,
  type ProviderClient,
  type ProvidersMutationResult,
  type ProvidersSnapshot,
} from "../services/providers";
import {
  currentSessionForClient,
} from "../services/providers/sessionModelHelpers";
import { planSettingsProviderSwitch } from "../services/providers/settingsOrchestration";
import { wsClient } from "../services/websocket";
import { useAgents } from "../store/agents";
import { useCurrentServer } from "../store/currentServer";
import { useCurrentSession } from "../store/currentSession";

export default function ProvidersScreen() {
  const router = useRouter();
  const { state } = useAgents();
  const { currentServer } = useCurrentServer();
  const { currentSession } = useCurrentSession();
  const currentServerId = currentServer?.id ?? null;
  const currentConnected = Boolean(
    currentServerId && state.serverConnections[currentServerId] === "connected",
  );
  const currentServerIdRef = useRef<string | null>(currentServerId);
  const ownerRef = useRef(new ProviderRequestOwner());
  const catalogRef = useRef<ProvidersSnapshot | null>(null);

  const [catalog, setCatalog] = useState<ProvidersSnapshot | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<ProviderError | null>(null);
  const [editor, setEditor] = useState<ProvidersEditorState>(null);
  const [modelPicker, setModelPicker] = useState<ModelSyncPickerState | null>(
    null,
  );
  const [mutating, setMutating] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [durabilityWarning, setDurabilityWarning] = useState<string | null>(
    null,
  );
  const [requiresRefreshBeforeMutation, setRequiresRefreshBeforeMutation] =
    useState(false);

  currentServerIdRef.current = currentServerId;
  catalogRef.current = catalog;
  const revision = catalog?.revision ?? 0;

  const clearProjection = useCallback(() => {
    ownerRef.current.invalidateAll();
    setEditor(null);
    setModelPicker(null);
    setMutating(false);
    setRefreshing(false);
    setDurabilityWarning(null);
    setRequiresRefreshBeforeMutation(false);
    setCatalog(null);
    setError(null);
    catalogRef.current = null;
  }, []);

  const syncWriteLockUi = useCallback(() => {
    setRequiresRefreshBeforeMutation(ownerRef.current.catalogRequiresRefresh());
  }, []);

  const loadCatalog = useCallback(
    async (opts?: { soft?: boolean }) => {
      if (!currentServerId || !currentConnected) {
        setError(offlineProviderError());
        setLoading(false);
        return;
      }
      const admission = ownerRef.current.admitCatalogLoad();
      if (!admission.ok) return;
      const token = admission.token;
      setLoading(true);
      setError(null);
      if (opts?.soft) setRefreshing(true);
      try {
        const next = await wsClient.listProviders(currentServerId);
        if (!ownerRef.current.acceptCatalog(token, next.revision)) return;
        setCatalog(next);
        syncWriteLockUi();
      } catch (loadError) {
        if (!ownerRef.current.isCurrent(token)) return;
        setError(
          loadError instanceof ProviderError
            ? loadError
            : new ProviderError(
                "unknown",
                loadError instanceof Error
                  ? loadError.message
                  : "Load failed",
                "unknown",
                true,
              ),
        );
      } finally {
        if (ownerRef.current.isCurrent(token)) {
          setLoading(false);
          if (opts?.soft) setRefreshing(false);
        }
      }
    },
    [currentConnected, currentServerId, syncWriteLockUi],
  );

  useEffect(() => {
    const rebound = ownerRef.current.rebind(currentServerId);
    if (rebound) clearProjection();
    if (currentServerId && currentConnected) {
      void loadCatalog();
    } else {
      setError(offlineProviderError());
    }
  }, [clearProjection, currentConnected, currentServerId, loadCatalog]);

  useFocusEffect(
    useCallback(() => {
      if (currentServerId && currentConnected) {
        void loadCatalog();
      }
      return () => {
        ownerRef.current.invalidateAll();
        setModelPicker(null);
        const after = providersScreenAfterBlur({
          flags: {
            loading: true,
            refreshing: true,
            mutating: true,
          },
          catalog: catalogRef.current,
        });
        setLoading(after.flags.loading);
        setRefreshing(after.flags.refreshing);
        setMutating(after.flags.mutating);
        setRequiresRefreshBeforeMutation(false);
      };
    }, [currentConnected, currentServerId, loadCatalog]),
  );

  const runMutation = async (
    action: () => Promise<ProvidersMutationResult>,
  ): Promise<ProvidersMutationResult | null> => {
    if (!currentServerId || !currentConnected) {
      Alert.alert("Offline", "Connect the current server first.");
      return null;
    }
    if (ownerRef.current.catalogRequiresRefresh()) {
      syncWriteLockUi();
      Alert.alert(
        "Refresh required",
        "Writes are locked until a successful same-server catalog refresh completes.",
        [{ text: "Refresh", onPress: () => void loadCatalog({ soft: true }) }],
      );
      return null;
    }
    const admission = ownerRef.current.admitCatalogMutation();
    if (!admission.ok) {
      Alert.alert("Busy", admission.reason);
      return null;
    }
    const token = admission.token;
    setMutating(true);
    try {
      const result = await action();
      if (!ownerRef.current.isCurrent(token)) return null;
      const classification = classifyMutationPersistence(result.persistence);
      ownerRef.current.settleCatalogMutation(token, {
        refreshRequired:
          classification === "applied_uncertain" ||
          classification === "ambiguous",
        revision: result.snapshot.revision,
      });
      if (classification === "ambiguous") {
        syncWriteLockUi();
        Alert.alert(
          "Refresh required",
          "Provider write settled ambiguously. Refresh before mutating again.",
        );
        void loadCatalog({ soft: true });
        return null;
      }
      setCatalog(result.snapshot);
      setDurabilityWarning(
        classification === "applied_uncertain"
          ? durabilityWarningMessage(result.persistence)
          : null,
      );
      syncWriteLockUi();
      return result;
    } catch (mutationError) {
      if (!ownerRef.current.isCurrent(token)) return null;
      ownerRef.current.settleCatalogMutation(token, {
        refreshRequired: providerMutationRequiresRefresh(mutationError),
      });
      syncWriteLockUi();
      const presented = presentProviderError(mutationError);
      Alert.alert(presented.title, presented.message);
      return null;
    } finally {
      if (ownerRef.current.isCurrent(token)) {
        setMutating(false);
      }
    }
  };

  /**
   * Unified Add/Edit Provider save: Name, Base URL and API key are written
   * atomically by the daemon in one upsert. An empty apiKey preserves the
   * stored secret (edit mode); a non-empty key replaces it as part of the
   * edit. There is no separate Replace/Clear key flow.
   */
  const saveProvider = async (input: {
    client: ProviderClient;
    connection?: ProviderConnection;
    name: string;
    baseUrl: string;
    apiKey: string;
  }): Promise<ProviderSaveOutcome> => {
    const previous = catalogRef.current;
    if (!currentServerId || !currentConnected || !previous) {
      return { status: "create_failed" };
    }
    if (ownerRef.current.catalogRequiresRefresh()) {
      syncWriteLockUi();
      Alert.alert(
        "Refresh required",
        "Refresh Providers before saving.",
        [{ text: "Refresh", onPress: () => void loadCatalog({ soft: true }) }],
      );
      return { status: "create_failed" };
    }
    const admission = ownerRef.current.admitCatalogMutation();
    if (!admission.ok) {
      Alert.alert("Busy", admission.reason);
      return { status: "create_failed" };
    }
    const token = admission.token;
    let transientApiKey = input.apiKey;
    setMutating(true);
    try {
      const result = await wsClient.upsertProviderConnection(
        currentServerId,
        {
          revision,
          operation: input.connection ? "update" : "create",
          connection: advancedConnectionInput({
            existingId: input.connection?.id,
            name: input.name,
            client: input.client,
            baseUrl: input.baseUrl,
            presetId: input.connection?.preset_id,
            // Curated connections keep the official endpoint; only
            // custom/advanced connections carry an editable Base URL.
            advanced: input.connection
              ? Boolean(input.connection.base_url)
              : true,
          }),
          credential: transientApiKey.trim() || undefined,
        },
      );
      transientApiKey = "";
      assertNoCredentialRetention(result.snapshot);
      if (!ownerRef.current.isCurrent(token)) return { status: "create_failed" };
      const classification = classifyMutationPersistence(result.persistence);
      ownerRef.current.settleCatalogMutation(token, {
        refreshRequired:
          classification === "applied_uncertain" ||
          classification === "ambiguous",
        revision: result.snapshot.revision,
      });
      if (classification === "ambiguous") {
        syncWriteLockUi();
        Alert.alert(
          "Refresh required",
          "Provider write settled ambiguously. Refresh before mutating again.",
        );
        void loadCatalog({ soft: true });
        return { status: "create_failed" };
      }
      setCatalog(result.snapshot);
      setDurabilityWarning(
        classification === "applied_uncertain"
          ? durabilityWarningMessage(result.persistence)
          : null,
      );
      syncWriteLockUi();

      // The saved connection: an update is the target row; a create must be
      // identified uniquely from the revision diff.
      let connection: ProviderConnection;
      if (input.connection) {
        connection = result.snapshot.connections.find(
          (item) => item.id === input.connection?.id,
        ) ?? input.connection;
      } else {
        try {
          connection = resolveCreatedConnection({
            previous,
            next: result.snapshot,
            presetId: "custom",
          });
        } catch (identityError) {
          ownerRef.current.requireCatalogRefresh();
          syncWriteLockUi();
          const presented = presentProviderError(identityError);
          Alert.alert(presented.title, presented.message);
          void loadCatalog({ soft: true });
          return { status: "create_failed" };
        }
      }

      const submittedKey = input.apiKey.trim().length > 0;
      const saved = result.snapshot.connections.find(
        (item) => item.id === connection.id,
      );
      if (submittedKey && saved && !saved.credential_ready) {
        // The edit applied but the key did not take effect: rebind the editor
        // for a retry of the same unified form.
        await loadCatalog({ soft: true });
        return { status: "credential_failed", connection: saved };
      }

      // A fresh connection with a saved key proceeds straight into discovery
      // so the model support picker can open.
      if (!input.connection && saved?.credential_ready) {
        const client = clientForConnection(saved);
        const discoverAdmission = ownerRef.current.admitCatalogMutation();
        if (client && discoverAdmission.ok) {
          const discoverToken = discoverAdmission.token;
          try {
            const discovery = await wsClient.discoverProviderModels(
              currentServerId,
              saved.id,
            );
            assertNoCredentialRetention(discovery);
            if (ownerRef.current.isCurrent(discoverToken)) {
              ownerRef.current.settleCatalogMutation(discoverToken, {
                refreshRequired: Boolean(
                  discovery.persistenceWarning &&
                    discovery.persistenceDurable === false,
                ),
              });
              if (
                discovery.persistenceWarning ||
                discovery.discoveryWarning
              ) {
                setDurabilityWarning(
                  discovery.persistenceWarning ??
                    discovery.discoveryWarning ??
                    null,
                );
              }
              if (discovery.models.length > 0) {
                setModelPicker({
                  client,
                  connection: saved,
                  models: discovery.models,
                });
              }
            }
          } catch (discoveryError) {
            if (!ownerRef.current.isCurrent(discoverToken)) {
              return { status: "saved" };
            }
            ownerRef.current.settleCatalogMutation(discoverToken, {
              refreshRequired: providerMutationRequiresRefresh(discoveryError),
            });
            const presented = presentProviderError(discoveryError);
            setDurabilityWarning(
              `Provider saved. Model discovery can be retried: ${presented.message}`,
            );
          }
        }
      }
      await loadCatalog({ soft: true });
      return { status: "saved" };
    } catch (saveError) {
      transientApiKey = "";
      if (!ownerRef.current.isCurrent(token)) return { status: "create_failed" };
      ownerRef.current.settleCatalogMutation(token, {
        refreshRequired: providerMutationRequiresRefresh(saveError),
      });
      syncWriteLockUi();
      const presented = presentProviderError(saveError);
      Alert.alert(presented.title, presented.message);
      await loadCatalog({ soft: true });
      return { status: "create_failed" };
    } finally {
      transientApiKey = "";
      if (currentServerIdRef.current === currentServerId) {
        setMutating(false);
      }
    }
  };

  const runDiscover = async (connection: ProviderConnection) => {
    if (!currentServerId || !currentConnected) return;
    if (ownerRef.current.catalogRequiresRefresh()) {
      syncWriteLockUi();
      Alert.alert(
        "Refresh required",
        "Refresh Providers before discovering models.",
        [{ text: "Refresh", onPress: () => void loadCatalog({ soft: true }) }],
      );
      return;
    }
    const admission = ownerRef.current.admitCatalogMutation();
    if (!admission.ok) {
      Alert.alert("Busy", admission.reason);
      return;
    }
    const token = admission.token;
    setMutating(true);
    try {
      const discovery = await wsClient.discoverProviderModels(
        currentServerId,
        connection.id,
      );
      assertNoCredentialRetention(discovery);
      if (!ownerRef.current.isCurrent(token)) return;
      ownerRef.current.settleCatalogMutation(token, {
        refreshRequired: Boolean(
          discovery.persistenceWarning && discovery.persistenceDurable === false,
        ),
      });
      if (discovery.persistenceWarning || discovery.discoveryWarning) {
        setDurabilityWarning(
          discovery.persistenceWarning ?? discovery.discoveryWarning ?? null,
        );
      }
      syncWriteLockUi();
      await loadCatalog({ soft: true });
      if (discovery.models.length > 0) {
        const client = clientForConnection(connection);
        if (client) {
          setModelPicker({
            client,
            connection,
            models: discovery.models,
          });
          return;
        }
        Alert.alert(
          "Connection successful",
          "Models were discovered, but this connection has no supported client.",
        );
      } else {
        Alert.alert(
          "Connection successful",
          "The endpoint accepted the saved API key but reported no models.",
        );
      }
    } catch (discoverError) {
      if (!ownerRef.current.isCurrent(token)) return;
      ownerRef.current.settleCatalogMutation(token, {
        refreshRequired: providerMutationRequiresRefresh(discoverError),
      });
      syncWriteLockUi();
      const presented = presentProviderError(discoverError);
      Alert.alert(presented.title, presented.message);
    } finally {
      if (ownerRef.current.isCurrent(token)) {
        setMutating(false);
      }
    }
  };

  const runSetModels = async (
    connection: ProviderConnection,
    enabledIds: string[],
  ) => {
    if (!currentServerId || !currentConnected) return;
    const result = await runMutation(() =>
      wsClient.setProviderModels(currentServerId!, {
        connectionId: connection.id,
        modelIds: enabledIds,
      }),
    );
    if (!result) return;
    // Keep the client-selected model aligned with the allowlist: when the
    // model new Sessions would launch with was just disabled, move the
    // selection to the deterministic first supported model (or clear it).
    const client = clientForConnection(connection);
    if (!client) return;
    const entry = result.snapshot.defaults[client];
    const selected = entry?.model_id?.trim();
    if (
      entry?.connection_id === connection.id &&
      selected &&
      !enabledIds.includes(selected)
    ) {
      const next = firstSupportedModel(result.snapshot, connection.id);
      await runMutation(() =>
        wsClient.setProviderDefault(currentServerId!, {
          client,
          connectionId: connection.id,
          modelId: next ?? undefined,
          revision: result.snapshot.revision,
        }),
      );
    }
  };

  const offline = !currentConnected;
  const unavailable = error?.kind === "unavailable";

  /**
   * Settings-only Provider switch: the single surface that changes which
   * Provider a client prefers.
   *
   * 1. Persist the preferred Provider (catalog client default) with NO
   *    fabricated model — a new default connection starts model-required
   *    until the client chooses a model (the daemon keeps the recorded
   *    selection when the same connection is re-selected).
   * 2. Painless carryover: when the exact current compatible routed Session
   *    runs a model that is enabled+available on the new Provider, activate
   *    the exact new Provider + current Model pair on that same Session — no
   *    new Session, no restart.
   * 3. Never a fallback: an unsupported current model leaves the preferred
   *    Provider recorded without a model, the Session keeps its old route,
   *    and the Composer enters the explicit model-required state (sending
   *    blocked until the user picks a model).
   * 4. On acknowledged activation, carry the model into the preferred
   *    Provider's recorded selection (best-effort) so future Sessions and
   *    restart restoration stay deterministic.
   */
  const switchPreferredProvider = useCallback(
    async (client: ProviderClient, connection: ProviderConnection) => {
      if (!currentServerId || !currentConnected) return;
      const result = await runMutation(() =>
        wsClient.setProviderDefault(currentServerId!, {
          client,
          connectionId: connection.id,
          modelId: undefined,
          revision,
        }),
      );
      if (!result) return;
      const snapshot = result.snapshot;

      // The current compatible routed Session for this client (last-focused
      // managed Session on the current server). Without one the switch only
      // persists the preferred Provider; the Composer of that client's
      // Sessions then shows the model-required state.
      const target = currentSessionForClient({
        agents: state.agents,
        currentSession,
        client,
      });
      if (!target) return;

      let currentSelection;
      try {
        currentSelection = await wsClient.getSessionProvider(
          currentServerId!,
          target.agentId,
        );
      } catch {
        // Session vanished or no longer managed: preferred already recorded.
        return;
      }

      const plan = planSettingsProviderSwitch({
        snapshot,
        connection,
        currentSession: target,
        currentSelection,
      });
      if (plan.unsupportedCurrentModel) {
        Alert.alert(
          "Model required",
          `${currentSelection.model_id.trim()} is not available on ${connection.name}. Pick a model in the chat to finish switching.`,
        );
        return;
      }
      const carryover = plan.carryover;
      if (!carryover) return;
      try {
        const activated = await wsClient.activateSessionProvider(
          currentServerId!,
          carryover,
        );
        const classification = classifyMutationPersistence(
          activated.persistence,
        );
        if (
          classification === "applied_durable" ||
          classification === "applied_uncertain"
        ) {
          // Carryover: record the carried model as the client-selected model
          // of the preferred Provider (best-effort; a stale revision simply
          // skips it and the next Composer activation retries).
          try {
            await wsClient.setProviderDefault(currentServerId!, {
              client,
              connectionId: connection.id,
              modelId: carryover.modelId,
              revision: snapshot.revision,
            });
          } catch {
            // Best-effort only; the activation itself is acknowledged.
          }
        }
      } catch (activateError) {
        // The daemon failed inline (e.g. the model is no longer admitted):
        // the preferred Provider stays recorded, the Session keeps its old
        // route, and the Composer enters the model-required state. Recover
        // by choosing a model there.
        const presented = presentProviderError(activateError);
        Alert.alert(presented.title, presented.message);
      }
    },
    [
      currentConnected,
      currentServerId,
      currentSession,
      revision,
      runMutation,
      state.agents,
    ],
  );

  const closeEditor = useCallback(() => {
    setEditor(null);
  }, []);

  // A save outcome that settles after the user already closed the overlay
  // must not reopen it: only confirmed success or explicit close resets.
  const editorOpenRef = useRef(false);
  editorOpenRef.current = editor !== null;

  const applySaveOutcome = useCallback(
    (outcome: ProviderSaveOutcome) => {
      if (outcome.status === "saved") {
        closeEditor();
        return;
      }
      if (outcome.status === "credential_failed" && !editorOpenRef.current) {
        return;
      }
      setEditor((previous) => providerEditorAfterSave(previous, outcome));
    },
    [closeEditor],
  );

  return (
    <ProvidersPresentation
      catalog={catalog}
      loading={loading && !catalog}
      refreshing={refreshing}
      error={error}
      offline={offline}
      unavailable={unavailable}
      currentServerAvailable={Boolean(currentServerId)}
      durabilityWarning={durabilityWarning}
      requiresRefreshBeforeMutation={requiresRefreshBeforeMutation}
      editor={editor}
      mutating={mutating}
      onRefresh={() => void loadCatalog({ soft: true })}
      onOpenSettings={() => router.push("/settings")}
      onOpenEditor={setEditor}
      onCloseEditor={closeEditor}
      onDelete={(connection) => {
        Alert.alert("Delete Provider?", connection.name, [
          { text: "Cancel", style: "cancel" },
          {
            text: "Delete",
            style: "destructive",
            onPress: () => {
              void runMutation(() =>
                wsClient.deleteProviderConnection(
                  currentServerId!,
                  connection.id,
                  revision,
                ),
              );
            },
          },
        ]);
      }}

      onUseDirect={(client: ProviderClient) => {
        void runMutation(() =>
          wsClient.setProviderDefault(currentServerId!, {
            client,
            connectionId: "",
            revision,
          }),
        );
      }}
      onSetDefault={(client, connection) => {
        void switchPreferredProvider(client, connection);
      }}
      onDiscover={(connection) => {
        void runDiscover(connection);
      }}
      modelPicker={modelPicker}
      onCloseModelPicker={() => setModelPicker(null)}
      onSelectModel={(client, connection, modelId) => {
        void runSetModels(
          connection,
          toggleModelSupport(catalog, connection.id, modelId),
        );
      }}
      onTestConnection={async ({ client, baseUrl, apiKey }) => {
        if (!currentServerId || !currentConnected) {
          throw offlineProviderError();
        }
        return wsClient.testProviderConnection(currentServerId, {
          client,
          baseUrl,
          apiKey,
        });
      }}
      onTestConnectionById={async (connection) => {
        if (!currentServerId || !currentConnected) {
          throw offlineProviderError();
        }
        return wsClient.testSavedProviderConnection(
          currentServerId,
          connection.id,
          connection.clients[0] ?? "codex",
        );
      }}
      onSaveProvider={async ({ client, connection, name, baseUrl, apiKey }) => {
        const outcome = await saveProvider({
          client,
          connection,
          name,
          baseUrl,
          apiKey,
        });
        applySaveOutcome(outcome);
        return outcome;
      }}
    />
  );
}
