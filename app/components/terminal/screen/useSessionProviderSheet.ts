import { useCallback, useEffect, useRef, useState } from "react";
import {
  ProviderError,
  ProviderRequestOwner,
  classifyMutationPersistence,
  durabilityWarningMessage,
  offlineProviderError,
  providerMutationRequiresRefresh,
  buildActivateSessionProviderRequest,
  type ProviderError as ProviderErrorType,
  type ProviderModelChoice,
  type ProviderSessionSelection,
  type ProvidersSnapshot,
} from "../../../services/providers";
import {
  sessionAllowsModelProfileActivation,
  sessionSupportsModelProfileAction,
  type AgentSessionCapabilities,
} from "../../../services/providers/sessionCapabilities";
import {
  resolveComposerModelControl,
  resolveSessionModelSheetMode,
  sessionModelPickerChoices,
  type ComposerModelControlPresentation,
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
   * Load the Session selection once while the sheet stays closed so the
   * Composer model control can show a label without opening the sheet.
   * The sheet re-fetches on open; the projection survives close so the
   * Composer control does not flicker away.
   */
  eagerLoad?: boolean;
}

export type SessionProviderSheetMode =
  | "idle"
  | "hidden"
  | "switchable"
  | "error";

/**
 * v2 Composer model picker state: one quiet control, one minimal picker, one
 * acknowledged live-switch path. Sessions without a daemon-acknowledged
 * hot-switch capability (direct official logins, OpenCode, Pi, managed
 * read-only, mismatch) keep the whole surface hidden — a dead Settings
 * explanation is never rendered.
 */
export function useSessionProviderSheet({
  serverId,
  agentId,
  capabilities,
  connectionConnected,
  eagerLoad = false,
}: UseSessionProviderSheetInput) {
  const [visible, setVisible] = useState(false);
  const [loading, setLoading] = useState(false);
  const [activating, setActivating] = useState(false);
  const [error, setError] = useState<ProviderErrorType | string | null>(null);
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
  const activationCapable = sessionAllowsModelProfileActivation(capabilities);

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

  /**
   * The sheet opens only for the acknowledged live-switch Session. Every
   * other capability state keeps the surface hidden; open() is a no-op so a
   * stale entry point can never show a dead picker.
   */
  const open = useCallback(() => {
    if (
      !managed ||
      !activationCapable ||
      requiresRefreshBeforeMutation ||
      !selection?.hot_switchable
    ) {
      return;
    }
    setVisible(true);
  }, [
    activationCapable,
    managed,
    requiresRefreshBeforeMutation,
    selection?.hot_switchable,
  ]);

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
        // Unsupported Session (direct official login, OpenCode, Pi, shell):
        // no inventory, no switch contract. Never fabricate a model surface.
        if (mode === "sheet") {
          setLoading(false);
          setSelection(null);
          setSheetMode("error");
          setError("This Session does not support Model switching.");
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
          setSheetMode("hidden");
          return;
        }

        setSelection(nextSelection);
        setSheetMode(
          activationCapable ? "switchable" : "hidden",
        );

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
              : "Could not load models for this Session.",
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
      managed,
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
    // Composer control must never project the previous Session's model onto a
    // different server/agent epoch.
    const rebound = ownerRef.current.rebind(serverId, agentId);
    if (rebound) {
      clearProjection();
      setVisible(false);
    }
  }, [agentId, clearProjection, serverId]);

  const activate = useCallback(
    async (choice: SessionModelChoice) => {
      if (!serverId || !agentId || !activationCapable) {
        return;
      }
      if (!selection?.hot_switchable || !catalog) {
        setError("Refresh the current Model before activating.");
        return;
      }
      const model = (catalog.models[choice.connectionId] ?? []).find(
        (item) => item.id === choice.modelId && item.available,
      );
      if (!model) {
        setError("That model is not available for this Session.");
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
          setError("Switching was not acknowledged. Refresh and try again.");
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
          syncActivationLockUi();
          if (classification === "applied_durable") {
            // Acknowledgement received: same Session now runs the new model.
            setVisible(false);
          } else {
            setError(
              durabilityWarningMessage(result.persistence) ??
                "Switched, but the change is not yet saved durably.",
            );
          }
          return;
        }
        ownerRef.current.settleActivation(token, { refreshRequired: true });
        syncActivationLockUi();
        setError("Model switch was not applied.");
      } catch (activateError) {
        if (!ownerRef.current.isCurrent(token)) return;
        const typed =
          activateError instanceof ProviderError
            ? activateError
            : new ProviderError(
                "unknown",
                activateError instanceof Error
                  ? activateError.message
                  : "Model switch failed",
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
        // Retain the prior selection on failure; the picker keeps showing it.
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
      selection,
      serverId,
      syncActivationLockUi,
    ],
  );

  const choices: ProviderModelChoice[] = sessionModelPickerChoices(
    catalog,
    selection,
  );

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
    sheetMode: resolveSessionModelSheetMode({
      capabilities,
      selection,
      refreshRequired: requiresRefreshBeforeMutation,
    }),
    selection,
    choices,
    composerControl,
  };
}
