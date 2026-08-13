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
  assertNoCredentialRetention,
  classifyMutationPersistence,
  clientForConnection,
  customGatewayCreateInput,
  durabilityWarningMessage,
  firstSupportedModel,
  mayDiscoverAfterCredential,
  offlineProviderError,
  planAfterCredentialWrite,
  presentProviderError,
  providerMutationRequiresRefresh,
  providersScreenAfterBlur,
  resolveCreatedConnection,
  settleCredentialPersistence,
  toggleModelSupport,
  type ProviderConnection,
  type ProviderClient,
  type ProvidersMutationResult,
  type ProvidersSnapshot,
} from "../services/providers";
import { wsClient } from "../services/websocket";
import { useAgents } from "../store/agents";
import { useCurrentServer } from "../store/currentServer";

export default function ProvidersScreen() {
  const router = useRouter();
  const { state } = useAgents();
  const { currentServer } = useCurrentServer();
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

  const saveCredential = async (
    connectionId: string,
    apiKey: string,
  ): Promise<boolean> => {
    if (!currentServerId || !currentConnected) return false;
    if (ownerRef.current.catalogRequiresRefresh()) {
      syncWriteLockUi();
      Alert.alert(
        "Refresh required",
        "Refresh Providers before saving an API key.",
        [{ text: "Refresh", onPress: () => void loadCatalog({ soft: true }) }],
      );
      return false;
    }
    const admission = ownerRef.current.admitCatalogMutation();
    if (!admission.ok) {
      Alert.alert("Busy", admission.reason);
      return false;
    }
    const token = admission.token;
    let transientApiKey = apiKey;
    setMutating(true);
    try {
      const result = await wsClient.setProviderCredential(
        currentServerId,
        connectionId,
        transientApiKey,
      );
      transientApiKey = "";
      assertNoCredentialRetention(result);
      if (!ownerRef.current.isCurrent(token)) return false;
      const settled = settleCredentialPersistence(result.persistence);
      ownerRef.current.settleCatalogMutation(token, {
        refreshRequired: settled.refreshRequired,
      });
      syncWriteLockUi();
      const followUp = planAfterCredentialWrite({ connectionId, result });
      if (followUp.kind === "refresh_lock") {
        setDurabilityWarning(followUp.reason);
        await loadCatalog({ soft: true });
        return result.credential_ready;
      }
      if (followUp.kind === "retry_key") {
        Alert.alert("API key not ready", followUp.reason);
        await loadCatalog({ soft: true });
        return false;
      }
      if (
        mayDiscoverAfterCredential({
          catalogRefreshRequired: ownerRef.current.catalogRequiresRefresh(),
          followUp,
        })
      ) {
        const discoverAdmission = ownerRef.current.admitCatalogMutation();
        if (discoverAdmission.ok) {
          const discoverToken = discoverAdmission.token;
          try {
            const discovery = await wsClient.discoverProviderModels(
              currentServerId,
              connectionId,
            );
            assertNoCredentialRetention(discovery);
            if (ownerRef.current.isCurrent(discoverToken)) {
              ownerRef.current.settleCatalogMutation(discoverToken, {
                refreshRequired: Boolean(
                  discovery.persistenceWarning &&
                    discovery.persistenceDurable === false,
                ),
              });
              if (discovery.persistenceWarning || discovery.discoveryWarning) {
                setDurabilityWarning(
                  discovery.persistenceWarning ??
                    discovery.discoveryWarning ??
                    null,
                );
              }
            }
          } catch (discoveryError) {
            if (!ownerRef.current.isCurrent(discoverToken)) return false;
            ownerRef.current.settleCatalogMutation(discoverToken, {
              refreshRequired: providerMutationRequiresRefresh(discoveryError),
            });
            const presented = presentProviderError(discoveryError);
            setDurabilityWarning(
              `API key saved. Model discovery can be retried: ${presented.message}`,
            );
          }
        }
      }
      await loadCatalog({ soft: true });
      return true;
    } catch (credentialError) {
      if (!ownerRef.current.isCurrent(token)) return false;
      ownerRef.current.settleCatalogMutation(token, {
        refreshRequired: providerMutationRequiresRefresh(credentialError),
      });
      syncWriteLockUi();
      const presented = presentProviderError(credentialError);
      Alert.alert(presented.title, presented.message);
      await loadCatalog({ soft: true });
      return false;
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
        // Selecting a connection is a provider choice; the client-selected
        // model for new Sessions is the existing selection when this
        // connection already owns it, else the deterministic first supported
        // model of the allowlist (never a gateway-owned default).
        const current = catalogRef.current;
        const entry = current?.defaults[client];
        const keepModel =
          entry?.connection_id === connection.id
            ? entry.model_id?.trim()
            : undefined;
        const modelId =
          keepModel ||
          firstSupportedModel(current, connection.id) ||
          undefined;
        void runMutation(() =>
          wsClient.setProviderDefault(currentServerId!, {
            client,
            connectionId: connection.id,
            modelId,
            revision,
          }),
        );
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
      onSaveCustom={async ({ client, baseUrl, apiKey }) => {
        const previous = catalogRef.current;
        if (!previous) return { status: "create_failed" as const };
        const created = await runMutation(() =>
          wsClient.upsertProviderConnection(currentServerId!, {
            revision,
            operation: "create",
            connection: customGatewayCreateInput({ client, baseUrl }),
          }),
        );
        if (!created) return { status: "create_failed" as const };
        let connection: ProviderConnection;
        try {
          connection = resolveCreatedConnection({
            previous,
            next: created.snapshot,
            presetId: "custom",
          });
        } catch (identityError) {
          ownerRef.current.requireCatalogRefresh();
          syncWriteLockUi();
          const presented = presentProviderError(identityError);
          Alert.alert(presented.title, presented.message);
          void loadCatalog({ soft: true });
          return { status: "create_failed" as const };
        }
        const ok = await saveCredential(connection.id, apiKey);
        const outcome: ProviderSaveOutcome = ok
          ? { status: "saved" }
          : { status: "credential_failed", connection };
        applySaveOutcome(outcome);
        return outcome;
      }}
      onSaveCredential={async (connection, apiKey) => {
        const ok = await saveCredential(connection.id, apiKey);
        const outcome: ProviderSaveOutcome = ok
          ? { status: "saved" }
          : { status: "credential_failed", connection };
        applySaveOutcome(outcome);
        return outcome;
      }}
    />
  );
}
