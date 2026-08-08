import { useCallback, useEffect, useRef, useState } from "react";
import {
  ProviderError,
  ProviderRequestOwner,
  classifyMutationPersistence,
  connectionsForClient,
  connectionsForSession,
  durabilityWarningMessage,
  isDirectSessionClient,
  offlineProviderError,
  providerMutationRequiresRefresh,
  buildActivateSessionProviderRequest,
  type ProviderConnection,
  type ProviderError as ProviderErrorType,
  type ProviderModel,
  type ProviderSessionSelection,
  type ProvidersSnapshot,
} from "../../../services/providers";
import {
  sessionAllowsModelProfileActivation,
  sessionIsManagedReadOnlyProfile,
  sessionSupportsModelProfileAction,
  type AgentSessionCapabilities,
} from "../../../services/providers/sessionCapabilities";
import {
  resolveComposerModelControl,
  resolveDirectComposerModelControl,
  type ComposerModelControlPresentation,
  type DirectSessionClient,
} from "../../../services/providers/sessionModelHelpers";
import { wsClient } from "../../../services/websocket";
import type { SessionModelChoice } from "../../providers/SessionModelSheet";

export { sessionSupportsModelProfileAction } from "../../../services/providers/sessionCapabilities";

interface UseSessionProviderSheetInput {
  serverId: string;
  agentId: string;
  capabilities?: AgentSessionCapabilities | null;
  connectionConnected: boolean;
  /**
   * Codex/Claude Sessions the daemon does not manage (official-direct, no
   * route binding) still get a truthful read-only Composer control and a
   * direct sheet state. Other clients stay fully hidden.
   */
  client?: DirectSessionClient | null;
  /**
   * Load the Session selection once while the sheet stays closed so the
   * Composer model control can show a label without opening the sheet.
   * The sheet re-fetches on open; the projection survives close so the
   * Composer control does not flicker away.
   */
  eagerLoad?: boolean;
}

export type SessionProviderSheetMode =
  | "idle"
  | "direct"
  | "managed_readonly"
  | "active_switch"
  | "capability_mismatch"
  | "error";

export function useSessionProviderSheet({
  serverId,
  agentId,
  capabilities,
  connectionConnected,
  client = null,
  eagerLoad = false,
}: UseSessionProviderSheetInput) {
  const [visible, setVisible] = useState(false);
  const [loading, setLoading] = useState(false);
  const [activating, setActivating] = useState(false);
  const [error, setError] = useState<ProviderErrorType | string | null>(null);
  const [durabilityWarning, setDurabilityWarning] = useState<string | null>(
    null,
  );
  const [requiresRefreshBeforeMutation, setRequiresRefreshBeforeMutation] =
    useState(false);
  const [sheetMode, setSheetMode] =
    useState<SessionProviderSheetMode>("idle");
  const [selection, setSelection] = useState<ProviderSessionSelection | null>(
    null,
  );
  const [catalog, setCatalog] = useState<ProvidersSnapshot | null>(null);
  const ownerRef = useRef(new ProviderRequestOwner());
  const fetchedEpochRef = useRef<{ serverId: string; agentId: string } | null>(
    null,
  );
  const managed = sessionSupportsModelProfileAction(capabilities);
  const readOnlyManaged = sessionIsManagedReadOnlyProfile(capabilities);
  const activationCapable = sessionAllowsModelProfileActivation(capabilities);
  const direct = !managed && isDirectSessionClient(client);

  const syncActivationLockUi = useCallback(() => {
    setRequiresRefreshBeforeMutation(
      ownerRef.current.activationRequiresRefresh(),
    );
  }, []);

  const clearProjection = useCallback(() => {
    ownerRef.current.invalidateAll();
    fetchedEpochRef.current = null;
    setSelection(null);
    setCatalog(null);
    setError(null);
    setDurabilityWarning(null);
    setRequiresRefreshBeforeMutation(false);
    setSheetMode("idle");
    setLoading(false);
    setActivating(false);
  }, []);

  const close = useCallback(() => {
    // Keep the loaded projection so the Composer model control stays stable
    // across sheet open/close. Rebinding a different server/agent clears it.
    setVisible(false);
  }, []);

  const open = useCallback(() => {
    setVisible(true);
  }, []);

  const fetchProjection = useCallback(
    async (mode: "sheet" | "eager") => {
      if (!serverId || !agentId) return;
      if (!connectionConnected) {
        if (mode === "sheet") {
          setLoading(false);
          setSelection(null);
          setSheetMode("error");
          setError(offlineProviderError());
        }
        return;
      }
      if (!managed) {
        if (!direct) {
          if (mode === "sheet") {
            setLoading(false);
            setSelection(null);
            setSheetMode("error");
            setError("This Session does not support Model switching.");
          }
          return;
        }
        if (mode === "sheet") {
          // Direct official-login Session: no route binding exists, so there
          // is no Session selection to load. Show the configured Provider
          // catalog read-only and let the sheet explain what is possible.
          ownerRef.current.rebind(serverId, agentId);
          const admission = ownerRef.current.admitCatalogLoad();
          if (!admission.ok) {
            setLoading(false);
            setSelection(null);
            setSheetMode("error");
            setError(admission.reason);
            return;
          }
          const token = admission.token;
          setLoading(true);
          setError(null);
          setSelection(null);
          setSheetMode("direct");
          try {
            const nextCatalog = await wsClient.listProviders(serverId);
            if (!ownerRef.current.isCurrent(token)) return;
            if (
              !ownerRef.current.acceptCatalog(token, nextCatalog.revision)
            ) {
              return;
            }
            setCatalog(nextCatalog);
          } catch (catalogError) {
            if (!ownerRef.current.isCurrent(token)) return;
            setCatalog(null);
            setError(
              catalogError instanceof ProviderError
                ? catalogError
                : "Could not load Providers for this direct Session.",
            );
          } finally {
            if (ownerRef.current.isCurrent(token)) {
              setLoading(false);
            }
          }
        }
        return;
      }
      ownerRef.current.rebind(serverId, agentId);
      const admission = ownerRef.current.admitSessionLoad();
      if (!admission.ok) return;
      const token = admission.token;
      if (mode === "sheet") setLoading(true);
      setError(null);
      try {
        const nextSelection = await wsClient.getSessionProvider(
          serverId,
          agentId,
        );
        if (!ownerRef.current.acceptSession(token)) return;
        fetchedEpochRef.current = { serverId, agentId };
        syncActivationLockUi();

        const hot = nextSelection.hot_switchable === true;
        if (activationCapable && !hot) {
          setSelection(nextSelection);
          setCatalog(null);
          setSheetMode("capability_mismatch");
          setError(
            "Managed Model capability disagrees with Session selection. Refresh before relying on this Session.",
          );
          return;
        }

        setSelection(nextSelection);
        setSheetMode(readOnlyManaged ? "managed_readonly" : "active_switch");

        try {
          const nextCatalog = await wsClient.listProviders(serverId);
          if (!ownerRef.current.isCurrent(token)) return;
          setCatalog(nextCatalog);
        } catch (catalogError) {
          if (!ownerRef.current.isCurrent(token)) return;
          setCatalog(null);
          setError(
            catalogError instanceof ProviderError
              ? catalogError
              : "Could not load Providers for model selection.",
          );
        }
      } catch (loadError) {
        if (!ownerRef.current.isCurrent(token)) return;
        if (mode === "sheet") {
          setSelection(null);
          setCatalog(null);
          setSheetMode("error");
          setError(
            loadError instanceof ProviderError
              ? loadError
              : loadError instanceof Error
                ? loadError.message
                : "Failed to load session provider.",
          );
        }
      } finally {
        if (mode === "sheet" && ownerRef.current.isCurrent(token)) {
          setLoading(false);
        }
      }
    },
    [
      activationCapable,
      agentId,
      connectionConnected,
      direct,
      managed,
      readOnlyManaged,
      serverId,
      syncActivationLockUi,
    ],
  );

  const load = useCallback(() => {
    void fetchProjection("sheet");
  }, [fetchProjection]);

  useEffect(() => {
    if (!visible) return;
    void load();
  }, [load, visible]);

  useEffect(() => {
    if (!eagerLoad) return;
    if (visible) return;
    if (!managed || !serverId || !agentId || !connectionConnected) return;
    const fetched = fetchedEpochRef.current;
    if (fetched && fetched.serverId === serverId && fetched.agentId === agentId) {
      return;
    }
    void fetchProjection("eager");
  }, [
    agentId,
    connectionConnected,
    eagerLoad,
    fetchProjection,
    managed,
    serverId,
    visible,
  ]);

  useEffect(() => {
    // Clear on every identity rebind, even while the sheet stays closed: the
    // Composer chip must never project the previous Session's model onto a
    // different server/agent epoch.
    const rebound = ownerRef.current.rebind(serverId, agentId);
    if (rebound) {
      clearProjection();
      setVisible(false);
    }
  }, [agentId, clearProjection, serverId]);

  const activate = useCallback(
    async (choice: SessionModelChoice) => {
      if (!serverId || !agentId || !activationCapable || readOnlyManaged) {
        return;
      }
      if (!selection?.hot_switchable || !catalog) {
        setError("Refresh the current Model before activating.");
        return;
      }
      const connection = connectionsForSession(catalog, selection).find(
        (item) => item.id === choice.connectionId,
      );
      const model = (catalog.models[choice.connectionId] ?? []).find(
        (item) => item.id === choice.modelId && item.available,
      );
      if (!connection || !connection.credential_ready || !model) {
        setError(
          connection?.credential_ready === false
            ? "Add this Provider API key in Settings before switching."
            : "That Provider Model is not available for this Session.",
        );
        return;
      }
      if (ownerRef.current.activationRequiresRefresh()) {
        syncActivationLockUi();
        setError("Refresh required before switching model.");
        return;
      }
      const admission = ownerRef.current.admitActivation();
      if (!admission.ok) {
        setError(admission.reason);
        return;
      }
      const token = admission.token;
      setActivating(true);
      setError(null);
      try {
        const result = await wsClient.activateSessionProvider(
          serverId,
          buildActivateSessionProviderRequest({
            agentId,
            connectionId: choice.connectionId,
            modelId: choice.modelId,
          }),
        );
        if (!ownerRef.current.isCurrent(token)) return;
        const classification = classifyMutationPersistence(result.persistence);
        if (classification === "ambiguous") {
          ownerRef.current.settleActivation(token, { refreshRequired: true });
          syncActivationLockUi();
          setError("Activation settled ambiguously. Refresh and try again.");
          return;
        }
        if (
          classification === "applied_durable" ||
          classification === "applied_uncertain"
        ) {
          ownerRef.current.settleActivation(token, {
            refreshRequired: classification === "applied_uncertain",
          });
          setSelection(result.selection);
          fetchedEpochRef.current = { serverId, agentId };
          setDurabilityWarning(
            classification === "applied_uncertain"
              ? durabilityWarningMessage(result.persistence)
              : null,
          );
          syncActivationLockUi();
          if (classification === "applied_durable") {
            setVisible(false);
          }
          return;
        }
        ownerRef.current.settleActivation(token, { refreshRequired: true });
        syncActivationLockUi();
        setError("Activation was not applied.");
      } catch (activateError) {
        if (!ownerRef.current.isCurrent(token)) return;
        const typed =
          activateError instanceof ProviderError
            ? activateError
            : new ProviderError(
                "unknown",
                activateError instanceof Error
                  ? activateError.message
                  : "Activation failed",
                "unknown",
                true,
              );
        ownerRef.current.settleActivation(token, {
          refreshRequired:
            typed.kind === "busy"
              ? false
              : providerMutationRequiresRefresh(typed) ||
                typed.kind === "conflict",
        });
        syncActivationLockUi();
        setError(typed);
      } finally {
        if (ownerRef.current.isCurrent(token)) {
          setActivating(false);
        }
      }
    },
    [
      activationCapable,
      agentId,
      catalog,
      clearProjection,
      readOnlyManaged,
      selection,
      serverId,
      syncActivationLockUi,
    ],
  );

  const filteredConnections: ProviderConnection[] = direct
    ? catalog
      ? connectionsForClient(catalog, client ?? "")
      : []
    : connectionsForSession(catalog, selection);

  const modelsByConnection: Record<string, ProviderModel[]> = {};
  if (catalog) {
    for (const connection of filteredConnections) {
      modelsByConnection[connection.id] = catalog.models[connection.id] ?? [];
    }
  }

  const composerControl: ComposerModelControlPresentation | null = managed
    ? resolveComposerModelControl({
        capabilities,
        connectionConnected,
        selection,
        refreshRequired: requiresRefreshBeforeMutation,
      })
    : resolveDirectComposerModelControl({ client, capabilities });

  return {
    visible,
    open,
    close,
    load,
    retry: load,
    activate,
    loading,
    activating,
    error,
    durabilityWarning,
    requiresRefreshBeforeMutation,
    sheetMode,
    selection,
    connections: filteredConnections,
    modelsByConnection,
    managedReadOnly: readOnlyManaged,
    activationEnabled: activationCapable && sheetMode === "active_switch",
    managed,
    direct,
    client,
    composerControl,
  };
}
