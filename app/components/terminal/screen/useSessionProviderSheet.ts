import { useCallback, useEffect, useRef, useState } from "react";import {
  ProviderError,
  ProviderRequestOwner,
  classifyMutationPersistence,
  durabilityWarningMessage,
  offlineProviderError,
  providerMutationRequiresRefresh,
  buildActivateSessionProviderRequest,
  type ProviderError as ProviderErrorType,
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
  refetchFoundBindingNotSwitchable,
  sessionModelRequired,
  sessionModelSheetRows,
  preferredProviderConnectionId,
  activationTargetModel,
  sessionEffortContract,
  resolveEffortChoiceForModel,
  type ComposerModelControlPresentation,
  type ProviderPickerModelRow,
  type SessionEffortContract,
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
  /**
   * Refetch the projection every time the hosting screen regains focus, so a
   * Settings Provider switch performed elsewhere converges the Composer
   * control (model-required state, preferred Provider inventory) when the
   * user returns instead of showing a stale cached projection.
   */
  focusActive?: boolean;
}

/**
 * Composer model picker state: one quiet Model-only control, one minimal
 * native sheet, one acknowledged live-switch path. Sessions without a
 * daemon-acknowledged hot-switch capability (direct official logins, OpenCode,
 * Pi, managed read-only, mismatch) keep the whole surface hidden — a dead
 * Settings explanation is never rendered.
 *
 * Product boundary: Settings owns Provider selection; this hook owns only
 * Model selection for the Settings-selected (preferred) Provider. Inventory
 * is that Provider's enabled+available models — never every saved Provider.
 * When the Session route still runs another Provider (pending switch), the
 * Composer enters the model-required state: sending is blocked and the user
 * must pick a model, which activates the exact preferred connection_id +
 * model_id on the current Session. The daemon never falls back.
 */
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
  const [selection, setSelection] = useState<ProviderSessionSelection | null>(
    null,
  );
  const [catalog, setCatalog] = useState<ProvidersSnapshot | null>(null);
  // Transient sheet-local effort choice, initialized from the authoritative
  // daemon projection (override ?? model default); reset whenever the
  // projection changes so the sheet never invents its own state.
  const [effortChoice, setEffortChoice] = useState<string | null>(null);
  // Last managed-Codex handoff outcome of an activation (truthful display:
  // the route activation is authoritative; a failed handoff means the running
  // Codex window may still show the previous identity).
  const [handoffWarning, setHandoffWarning] = useState<string | null>(null);
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
    setLoading(false);
    setActivating(false);
    setEffortChoice(null);
    setHandoffWarning(null);
  }, []);

  const close = useCallback(() => {
    // Keep the loaded projection so the Composer model control stays stable
    // across sheet open/close. Rebinding a different server/agent clears it.
    setVisible(false);
  }, []);

  /**
   * The sheet opens only for the acknowledged live-switch Session. Every
   * other capability state keeps the surface hidden; open() is a no-op so a
   * stale entry point can never show a dead picker. The sheet is a native
   * bottom sheet, so no on-screen anchor is needed — open() takes none.
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
        if (
          refetchFoundBindingNotSwitchable({
            activationCapable,
            hotSwitchable: hot,
          })
        ) {
          // The binding no longer admits live switching. Close the sheet when
          // a refetch discovers this while it is open and keep the Composer
          // control hidden (the fresh selection is not hot-switchable); never
          // render an empty fabricated "No models discovered" inventory.
          if (mode === "sheet") {
            setVisible(false);
          }
          setSelection(nextSelection);
          setCatalog(null);
          return;
        }

        setSelection(nextSelection);
        // Rebase the transient effort choice on the authoritative projection
        // (override ?? model default); a changed selection must never leave a
        // stale effort choice visible.
        const contract = sessionEffortContract(nextSelection);
        setEffortChoice((current) => {
          const base = contract?.current ?? "";
          if (current === null) return base || null;
          if (contract && !contract.supported.includes(current)) {
            return resolveEffortChoiceForModel({
              currentChoice: current,
              targetSupported: contract.supported,
              targetDefault: contract.defaultEffort,
            });
          }
          return current;
        });
        setHandoffWarning(null);

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
    // Converge after cross-screen changes (e.g. a Settings Provider switch
    // while this Session was covered): refetch the projection each time the
    // hosting screen regains focus so the Composer control and sheet show the
    // fresh preferred Provider + model-required truth, never a stale cached
    // projection.
    if (!focusActive) return;
    if (visible) return;
    if (!managed || !serverId || !agentId || !connectionConnected) return;
    void fetchProjection("eager");
  }, [
    agentId,
    connectionConnected,
    fetchProjection,
    focusActive,
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

  /**
   * Best-effort carryover of the client-selected model onto the preferred
   * Provider's default after an acknowledged activation, so future Sessions
   * and restart restoration stay deterministic (preferred model == route
   * model in the steady state). Failure never blocks the acknowledged
   * activation: the same-provider derivation keeps the UI consistent either
   * way, and the next successful activation retries the carryover.
   */
  const carryPreferredModel = useCallback(
    async (next: ProviderSessionSelection, currentCatalog: ProvidersSnapshot) => {
      if (!serverId) return;
      const preferredId = preferredProviderConnectionId(
        currentCatalog,
        next.client,
      );
      if (!preferredId || preferredId !== next.connection_id) return;
      const modelId = next.model_id.trim();
      if (!modelId) return;
      try {
        await wsClient.setProviderDefault(serverId, {
          client: next.client,
          connectionId: preferredId,
          modelId,
          revision: currentCatalog.revision,
        });
      } catch {
        // Best-effort only; the activation itself is already acknowledged.
      }
    },
    [serverId],
  );

  const activate = useCallback(
    async (choice: SessionModelChoice) => {
      if (!serverId || !agentId || !activationCapable) {
        return;
      }
      if (!selection?.hot_switchable || !catalog) {
        setError("Refresh the current Model before activating.");
        return;
      }
      // The Composer owns Model selection only: the target connection must be
      // the exact Settings-selected (preferred) Provider. Refuse anything else
      // — the Composer never switches Providers.
      const preferredId = preferredProviderConnectionId(
        catalog,
        selection.client,
      );
      if (!preferredId || preferredId !== choice.connectionId) {
        setError("Choose a model for the Provider selected in Settings.");
        return;
      }
      if (!activationTargetModel(catalog, choice)) {
        // The catalog does not admit this exact pair (unknown model, or model
        // no longer available). Refuse inline and keep the old route — never substitute another model.
        setError("That model is not available for this Session.");
        return;
      }
      // Reasoning Effort rides the same acknowledged activation: the resolved
      // choice is the transient sheet selection rebased on the authoritative
      // projection (override ?? model default), so a compatible model switch
      // preserves the current effort and the daemon enforces the contract. In
      // the model-required state the target contract is not this Session's
      // yet — omit the effort so the daemon's preservation rule applies
      // (preserve when the target supports it, else its documented default).
      const effortContract = sessionEffortContract(selection);
      const targetModelRequired = sessionModelRequired({
        snapshot: catalog,
        selection,
      });
      const resolvedEffort =
        targetModelRequired || !effortContract
          ? ""
          : resolveEffortChoiceForModel({
              currentChoice: effortChoice ?? effortContract.current,
              targetSupported: effortContract.supported,
              targetDefault: effortContract.defaultEffort,
            });
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
      setHandoffWarning(null);
      try {
        const result = await wsClient.activateSessionProvider(
          serverId,
          buildActivateSessionProviderRequest({
            agentId,
            connectionId: choice.connectionId,
            modelId: choice.modelId,
            reasoningEffort: resolvedEffort,
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
          void carryPreferredModel(result.selection, catalog);
          // Truthful live-Codex display: a failed/partial handoff means the
          // running Codex window may still show the previous identity even
          // though the Session route already runs the new one. Never claim
          // UI convergence that was not proven.
          if (result.handoff && result.handoff.state !== "applied") {
            const reason =
              result.handoff.message ??
              (result.handoff.state === "failed"
                ? "The Codex window could not be switched to the new model; the switch applies to the next message, and the window will show the previous model until it reloads."
                : "The Codex window was not restarted; the switch applies to the next message.");
            setHandoffWarning(reason);
          } else {
            setHandoffWarning(null);
          }
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
      carryPreferredModel,
      selection,
      serverId,
      syncActivationLockUi,
    ],
  );

  const preferredConnectionId = preferredProviderConnectionId(
    catalog,
    selection?.client ?? "",
  );
  const modelRequired = sessionModelRequired({
    snapshot: catalog,
    selection,
  });
  const rows: ProviderPickerModelRow[] = sessionModelSheetRows({
    snapshot: catalog,
    selection,
    activating,
  });
  const effortContract: SessionEffortContract | null =
    sessionEffortContract(selection);
  const effectiveEffortChoice = effortContract
    ? resolveEffortChoiceForModel({
        currentChoice: effortChoice ?? effortContract.current,
        targetSupported: effortContract.supported,
        targetDefault: effortContract.defaultEffort,
      })
    : "";

  const composerControl: ComposerModelControlPresentation | null =
    resolveComposerModelControl({
      capabilities,
      connectionConnected,
      selection,
      refreshRequired: requiresRefreshBeforeMutation,
      preferredConnectionId,
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
    modelRequired,
    composerControl,
    effortContract,
    effortChoice: effectiveEffortChoice,
    selectEffort: setEffortChoice,
    handoffWarning,
  };
}
