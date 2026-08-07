import { useCallback, useEffect, useRef, useState } from "react";
import {
  ProviderError,
  ProviderRequestOwner,
  classifyMutationPersistence,
  connectionsForSession,
  durabilityWarningMessage,
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
import { wsClient } from "../../../services/websocket";
import type { SessionModelChoice } from "../../providers/SessionModelSheet";

export { sessionSupportsModelProfileAction } from "../../../services/providers/sessionCapabilities";

interface UseSessionProviderSheetInput {
  serverId: string;
  agentId: string;
  capabilities?: AgentSessionCapabilities | null;
  connectionConnected: boolean;
}

export type SessionProviderSheetMode =
  | "idle"
  | "managed_readonly"
  | "active_switch"
  | "capability_mismatch"
  | "error";

export function useSessionProviderSheet({
  serverId,
  agentId,
  capabilities,
  connectionConnected,
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
  const managed = sessionSupportsModelProfileAction(capabilities);
  const readOnlyManaged = sessionIsManagedReadOnlyProfile(capabilities);
  const activationCapable = sessionAllowsModelProfileActivation(capabilities);

  const syncActivationLockUi = useCallback(() => {
    setRequiresRefreshBeforeMutation(
      ownerRef.current.activationRequiresRefresh(),
    );
  }, []);

  const clearProjection = useCallback(() => {
    ownerRef.current.invalidateAll();
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
    setVisible(false);
    clearProjection();
  }, [clearProjection]);

  const open = useCallback(() => {
    setVisible(true);
  }, []);

  const load = useCallback(async () => {
    if (!visible || !serverId || !agentId) return;
    if (!connectionConnected) {
      setLoading(false);
      setSelection(null);
      setSheetMode("error");
      setError(offlineProviderError());
      return;
    }
    if (!managed) {
      setLoading(false);
      setSelection(null);
      setSheetMode("error");
      setError("This Session does not support Model switching.");
      return;
    }
    ownerRef.current.rebind(serverId, agentId);
    const admission = ownerRef.current.admitSessionLoad();
    if (!admission.ok) return;
    const token = admission.token;
    setLoading(true);
    setError(null);
    try {
      const nextSelection = await wsClient.getSessionProvider(serverId, agentId);
      if (!ownerRef.current.acceptSession(token)) return;
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
    } finally {
      if (ownerRef.current.isCurrent(token)) {
        setLoading(false);
      }
    }
  }, [
    activationCapable,
    agentId,
    connectionConnected,
    managed,
    readOnlyManaged,
    serverId,
    syncActivationLockUi,
    visible,
  ]);

  useEffect(() => {
    if (!visible) return;
    void load();
  }, [load, visible]);

  useEffect(() => {
    const rebound = ownerRef.current.rebind(serverId, agentId);
    if (rebound && visible) {
      clearProjection();
      setVisible(false);
    }
  }, [agentId, clearProjection, serverId, visible]);

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
          setDurabilityWarning(
            classification === "applied_uncertain"
              ? durabilityWarningMessage(result.persistence)
              : null,
          );
          syncActivationLockUi();
          if (classification === "applied_durable") {
            setVisible(false);
            clearProjection();
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

  const filteredConnections: ProviderConnection[] = connectionsForSession(
    catalog,
    selection,
  );

  const modelsByConnection: Record<string, ProviderModel[]> = {};
  if (catalog) {
    for (const connection of filteredConnections) {
      modelsByConnection[connection.id] = catalog.models[connection.id] ?? [];
    }
  }

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
  };
}
