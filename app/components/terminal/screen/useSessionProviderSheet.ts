import { useCallback, useEffect, useRef, useState } from "react";
import {
  ProviderError,
  ProviderRequestOwner,
  classifyMutationPersistence,
  durabilityWarningMessage,
  offlineProviderError,
  providerMutationRequiresRefresh,
  type ProviderError as ProviderErrorType,
  type ThreadRuntimeSelection,
  type ProvidersSnapshot,
  type ThreadRuntimeChoice,
} from "../../../services/providers";
import {
  sessionAllowsModelProfileActivation,
  sessionSupportsModelProfileAction,
  type AgentSessionCapabilities,
} from "../../../services/providers/sessionCapabilities";
import {
  refetchFoundBindingNotSwitchable,
  resolveComposerModelControl,
  threadRuntimeRows,
  type ComposerModelControlPresentation,
  type ProviderPickerModelRow,
} from "../../../services/providers/sessionModelHelpers";
import { wsClient } from "../../../services/websocket";

export { sessionSupportsModelProfileAction } from "../../../services/providers/sessionCapabilities";

interface UseSessionProviderSheetInput {
  serverId: string;
  agentId: string;
  capabilities?: AgentSessionCapabilities | null;
  connectionConnected: boolean;
  eagerLoad?: boolean;
  focusActive?: boolean;
}

export function useSessionProviderSheet({
  serverId,
  agentId,
  capabilities,
  connectionConnected,
  eagerLoad = false,
  focusActive = false,
}: UseSessionProviderSheetInput) {
  const [visible, setVisible] = useState(false);
  const [loading, setLoading] = useState(false);
  const [activating, setActivating] = useState(false);
  const [error, setError] = useState<ProviderErrorType | string | null>(null);
  const [requiresRefreshBeforeMutation, setRequiresRefreshBeforeMutation] =
    useState(false);
  const [selection, setSelection] = useState<ThreadRuntimeSelection | null>(
    null,
  );
  const [catalog, setCatalog] = useState<ProvidersSnapshot | null>(null);
  const ownerRef = useRef(new ProviderRequestOwner());
  const fetchedEpochRef = useRef<{ serverId: string; agentId: string } | null>(
    null,
  );
  const managed = sessionSupportsModelProfileAction(capabilities);
  const activationCapable = sessionAllowsModelProfileActivation(capabilities);

  const syncActivationLockUi = useCallback(() => {
    setRequiresRefreshBeforeMutation(
      ownerRef.current.runtimeSwitchRequiresRefresh(),
    );
  }, []);

  const clearProjection = useCallback(() => {
    ownerRef.current.invalidateAll();
    fetchedEpochRef.current = null;
    setSelection(null);
    setCatalog(null);
    setError(null);
    setRequiresRefreshBeforeMutation(false);
    setLoading(false);
    setActivating(false);
  }, []);

  const close = useCallback(() => setVisible(false), []);
  const open = useCallback(() => {
    if (
      managed &&
      activationCapable &&
      !requiresRefreshBeforeMutation &&
      selection?.hot_switchable
    ) {
      setVisible(true);
    }
  }, [activationCapable, managed, requiresRefreshBeforeMutation, selection]);

  const fetchProjection = useCallback(
    async (mode: "sheet" | "eager") => {
      if (!serverId || !agentId) return;
      if (!connectionConnected) {
        if (mode === "sheet") setError(offlineProviderError());
        return;
      }
      if (!managed) return;
      ownerRef.current.rebind(serverId, agentId);
      const admission = ownerRef.current.admitSessionLoad();
      if (!admission.ok) return;
      const token = admission.token;
      if (mode === "sheet") setLoading(true);
      setError(null);
      try {
        const nextSelection = await wsClient.getThreadRuntime(serverId, agentId);
        if (!ownerRef.current.acceptSession(token)) return;
        fetchedEpochRef.current = { serverId, agentId };
        syncActivationLockUi();
        setSelection(nextSelection);
        if (
          refetchFoundBindingNotSwitchable({
            activationCapable,
            hotSwitchable: nextSelection.hot_switchable === true,
          })
        ) {
          setCatalog(null);
          setVisible(false);
          return;
        }
        const nextCatalog = await wsClient.listProviders(serverId);
        if (!ownerRef.current.isCurrent(token)) return;
        setCatalog(nextCatalog);
      } catch (loadError) {
        if (!ownerRef.current.isCurrent(token)) return;
        if (mode === "sheet") {
          setError(
            loadError instanceof ProviderError
              ? loadError
              : loadError instanceof Error
                ? loadError.message
                : "Failed to load thread runtime.",
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
      managed,
      serverId,
      syncActivationLockUi,
    ],
  );

  const load = useCallback(() => void fetchProjection("sheet"), [fetchProjection]);

  useEffect(() => {
    if (visible) void load();
  }, [load, visible]);

  useEffect(() => {
    if (!eagerLoad || visible || !managed || !connectionConnected) return;
    const fetched = fetchedEpochRef.current;
    if (fetched?.serverId === serverId && fetched.agentId === agentId) return;
    void fetchProjection("eager");
  }, [agentId, connectionConnected, eagerLoad, fetchProjection, managed, serverId, visible]);

  useEffect(() => {
    if (!focusActive || visible || !managed || !connectionConnected) return;
    void fetchProjection("eager");
  }, [connectionConnected, fetchProjection, focusActive, managed, visible]);

  useEffect(() => {
    if (ownerRef.current.rebind(serverId, agentId)) {
      clearProjection();
      setVisible(false);
    }
  }, [agentId, clearProjection, serverId]);

  const activate = useCallback(
    async (runtime: ThreadRuntimeChoice) => {
      if (!serverId || !agentId || !activationCapable || !selection || !catalog) {
        return;
      }
      if (ownerRef.current.runtimeSwitchRequiresRefresh()) {
        syncActivationLockUi();
        setError("Refresh required before switching runtime.");
        return;
      }
      const admission = ownerRef.current.admitRuntimeSwitch();
      if (!admission.ok) {
        setError(admission.reason);
        return;
      }
      const token = admission.token;
      setActivating(true);
      setError(null);
      try {
        const result = await wsClient.setThreadRuntime(serverId, {
          agentId,
          runtime,
        });
        if (!ownerRef.current.isCurrent(token)) return;
        const classification = classifyMutationPersistence(result.persistence);
        if (classification === "ambiguous") {
          ownerRef.current.settleRuntimeSwitch(token, { refreshRequired: true });
          syncActivationLockUi();
          setError("Runtime switch was not acknowledged. Refresh and try again.");
          return;
        }
        if (
          classification === "applied_durable" ||
          classification === "applied_uncertain"
        ) {
          ownerRef.current.settleRuntimeSwitch(token, {
            refreshRequired: classification === "applied_uncertain",
          });
          setSelection(result.runtime);
          fetchedEpochRef.current = { serverId, agentId };
          syncActivationLockUi();
          if (classification === "applied_durable") {
            setVisible(false);
          } else {
            setError(
              durabilityWarningMessage(result.persistence) ??
                "Switched, but the change is not yet saved durably.",
            );
          }
          return;
        }
        ownerRef.current.settleRuntimeSwitch(token, { refreshRequired: true });
        syncActivationLockUi();
        setError("Runtime switch was not applied.");
      } catch (activateError) {
        if (!ownerRef.current.isCurrent(token)) return;
        const typed =
          activateError instanceof ProviderError
            ? activateError
            : new ProviderError(
                "unknown",
                activateError instanceof Error
                  ? activateError.message
                  : "Runtime switch failed",
                "unknown",
                true,
              );
        ownerRef.current.settleRuntimeSwitch(token, {
          refreshRequired:
            typed.kind !== "busy" &&
            (providerMutationRequiresRefresh(typed) || typed.kind === "conflict"),
        });
        syncActivationLockUi();
        setError(typed);
      } finally {
        if (ownerRef.current.isCurrent(token)) setActivating(false);
      }
    },
    [activationCapable, agentId, catalog, selection, serverId, syncActivationLockUi],
  );

  const rows: ProviderPickerModelRow[] = threadRuntimeRows({
    snapshot: catalog,
    selection,
    activating,
  });
  const composerControl: ComposerModelControlPresentation | null =
    resolveComposerModelControl({
      capabilities,
      connectionConnected,
      selection,
      refreshRequired: requiresRefreshBeforeMutation,
    });

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
    requiresRefreshBeforeMutation,
    selection,
    rows,
    composerControl,
  };
}
