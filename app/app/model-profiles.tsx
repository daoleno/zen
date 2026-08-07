import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert } from "react-native";
import { useFocusEffect, useRouter } from "expo-router";
import {
  ProviderAddForm,
  ProviderCredentialForm,
  ProvidersPresentation,
  type ProvidersScreenMode,
} from "../components/providers/ProvidersPresentation";
import {
  ProviderError,
  ProviderRequestOwner,
  assertNoCredentialRetention,
  classifyMutationPersistence,
  curatedCreateInput,
  customGatewayCreateInput,
  durabilityWarningMessage,
  mayDiscoverAfterCredential,
  offlineProviderError,
  planAfterCredentialWrite,
  presentProviderError,
  providerMutationRequiresRefresh,
  providersScreenAfterBlur,
  resolveCreatedConnection,
  settleCredentialPersistence,
  type ProviderConnection,
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
  const [mode, setMode] = useState<ProvidersScreenMode>("list");
  const [credentialTarget, setCredentialTarget] =
    useState<ProviderConnection | null>(null);
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
    setMode("list");
    setCredentialTarget(null);
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

  const runClearCredential = async (connection: ProviderConnection) => {
    if (!currentServerId || !currentConnected) return;
    if (ownerRef.current.catalogRequiresRefresh()) {
      syncWriteLockUi();
      Alert.alert(
        "Refresh required",
        "Refresh Providers before clearing an API key.",
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
      const result = await wsClient.clearProviderCredential(
        currentServerId,
        connection.id,
      );
      assertNoCredentialRetention(result);
      if (!ownerRef.current.isCurrent(token)) return;
      const settled = settleCredentialPersistence(result.persistence);
      ownerRef.current.settleCatalogMutation(token, {
        refreshRequired: settled.refreshRequired,
      });
      if (settled.refreshRequired) {
        setDurabilityWarning(durabilityWarningMessage(result.persistence));
      }
      syncWriteLockUi();
      await loadCatalog({ soft: true });
    } catch (clearError) {
      if (!ownerRef.current.isCurrent(token)) return;
      ownerRef.current.settleCatalogMutation(token, {
        refreshRequired: providerMutationRequiresRefresh(clearError),
      });
      syncWriteLockUi();
      const presented = presentProviderError(clearError);
      Alert.alert(presented.title, presented.message);
    } finally {
      if (ownerRef.current.isCurrent(token)) {
        setMutating(false);
      }
    }
  };

  const offline = !currentConnected;
  const unavailable = error?.kind === "unavailable";

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
      mode={mode}
      mutating={mutating}
      onRefresh={() => void loadCatalog({ soft: true })}
      onOpenSettings={() => router.push("/settings")}
      onAdd={() => setMode("add")}
      onAddApiKey={(connection) => {
        setCredentialTarget(connection);
        setMode("credential");
      }}
      onClearCredential={(connection) => {
        Alert.alert("Clear API key?", connection.name, [
          { text: "Cancel", style: "cancel" },
          {
            text: "Clear",
            style: "destructive",
            onPress: () => {
              void runClearCredential(connection);
            },
          },
        ]);
      }}
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
      onSetDefault={(client, connection) => {
        void runMutation(() =>
          wsClient.setProviderDefault(currentServerId!, {
            client,
            connectionId: connection.id,
            revision,
          }),
        );
      }}
      onDiscover={(connection) => {
        void runDiscover(connection);
      }}
      addSlot={
        <ProviderAddForm
          presets={catalog?.presets ?? []}
          mutating={mutating}
          onCancel={() => setMode("list")}
          onSaveCurated={async ({ preset, apiKey }) => {
            const previous = catalogRef.current;
            if (!previous) return;
            const created = await runMutation(() =>
              wsClient.upsertProviderConnection(currentServerId!, {
                revision,
                operation: "create",
                connection: curatedCreateInput(preset),
              }),
            );
            if (!created) return;
            let connection: ProviderConnection;
            try {
              connection = resolveCreatedConnection({
                previous,
                next: created.snapshot,
                presetId: preset.id,
              });
            } catch (identityError) {
              ownerRef.current.requireCatalogRefresh();
              syncWriteLockUi();
              const presented = presentProviderError(identityError);
              Alert.alert(presented.title, presented.message);
              setMode("list");
              void loadCatalog({ soft: true });
              return;
            }
            const ok = await saveCredential(connection.id, apiKey);
            setMode("list");
            if (!ok) setCredentialTarget(connection);
          }}
          onSaveCustom={async ({ name, client, baseUrl, apiKey, modelId }) => {
            const previous = catalogRef.current;
            if (!previous) return;
            const created = await runMutation(() =>
              wsClient.upsertProviderConnection(currentServerId!, {
                revision,
                operation: "create",
                connection: customGatewayCreateInput({
                  name,
                  client,
                  baseUrl,
                  manualModelId: modelId,
                }),
              }),
            );
            if (!created) return;
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
              setMode("list");
              void loadCatalog({ soft: true });
              return;
            }
            await saveCredential(connection.id, apiKey);
            setMode("list");
          }}
        />
      }
      credentialSlot={
        credentialTarget ? (
          <ProviderCredentialForm
            connection={credentialTarget}
            mutating={mutating}
            onCancel={() => {
              setCredentialTarget(null);
              setMode("list");
            }}
            onSave={async (apiKey) => {
              setMutating(true);
              try {
                await saveCredential(credentialTarget.id, apiKey);
                setCredentialTarget(null);
                setMode("list");
              } finally {
                setMutating(false);
              }
            }}
          />
        ) : null
      }
    />
  );
}
