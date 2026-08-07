/**
 * Daemon-owned flat Session capabilities. Field names remain model_profile_*
 * on the daemon capability wire even though the visible product is Provider /
 * Model-first.
 */
export type AgentSessionCapabilities = {
  structured_events: boolean;
  model_profile_managed: boolean;
  model_profile_active_switch: boolean;
};

export function normalizeAgentSessionCapabilities(
  raw: unknown,
): AgentSessionCapabilities | undefined {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return undefined;
  const record = raw as Record<string, unknown>;
  return {
    structured_events: record.structured_events === true,
    model_profile_managed: record.model_profile_managed === true,
    model_profile_active_switch: record.model_profile_active_switch === true,
  };
}

export function sessionSupportsModelProfileAction(
  capabilities?: AgentSessionCapabilities | null,
): boolean {
  return capabilities?.model_profile_managed === true;
}

export function sessionIsManagedReadOnlyProfile(
  capabilities?: AgentSessionCapabilities | null,
): boolean {
  return (
    capabilities?.model_profile_managed === true &&
    capabilities.model_profile_active_switch !== true
  );
}

export function sessionAllowsModelProfileActivation(
  capabilities?: AgentSessionCapabilities | null,
): boolean {
  return (
    capabilities?.model_profile_managed === true &&
    capabilities.model_profile_active_switch === true
  );
}

export function capabilityProviderDisagreementMessage(input: {
  managed: boolean;
  activeSwitch: boolean;
  selectionFound: boolean;
  hotSwitchable: boolean;
}): string | null {
  if (!input.managed) return null;
  if (!input.selectionFound) {
    return "The daemon advertised a managed Model, but no Session Provider selection was found. Refresh before relying on this Session.";
  }
  if (input.activeSwitch && !input.hotSwitchable) {
    return "The daemon advertised Model switching, but this Session is read-only. Refresh before activating.";
  }
  return null;
}

export function isMissingSessionProviderError(error: unknown): boolean {
  if (!error || typeof error !== "object") return false;
  const code =
    "code" in error && typeof (error as { code?: unknown }).code === "string"
      ? (error as { code: string }).code
      : "";
  return (
    code === "route_binding_not_found" ||
    code === "route_binding_not_routed" ||
    code === "model_profiles_unavailable"
  );
}
