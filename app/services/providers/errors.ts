export const PROVIDER_ERROR_CODES = {
  unavailable: "model_profiles_unavailable",
  notFound: "model_profile_not_found",
  conflict: "model_profile_conflict",
  invalid: "model_profile_invalid",
  inUse: "model_profile_in_use",
  unsupported: "model_profile_unsupported",
  credentialNotReady: "model_profile_credential_not_ready",
  credentialStoreUnavailable: "credential_store_unavailable",
  credentialStoreFailed: "credential_store_failed",
  secureTransportRequired: "secure_transport_required",
  bindingNotFound: "route_binding_not_found",
  bindingConflict: "route_binding_conflict",
  bindingBusy: "route_binding_busy",
  bindingIncompatible: "route_binding_incompatible",
  bindingNotRouted: "route_binding_not_routed",
  timeout: "providers_timeout",
  offline: "providers_offline",
  invalidReply: "providers_invalid_reply",
} as const;

export type ProviderErrorKind =
  | "unavailable"
  | "conflict"
  | "busy"
  | "incompatible"
  | "credential"
  | "secure_transport"
  | "not_found"
  | "invalid"
  | "offline"
  | "timeout"
  | "unknown";

export class ProviderError extends Error {
  readonly code: string;
  readonly kind: ProviderErrorKind;
  readonly refreshable: boolean;

  constructor(
    code: string,
    message: string,
    kind: ProviderErrorKind = "unknown",
    refreshable = false,
  ) {
    super(message);
    this.name = "ProviderError";
    this.code = code;
    this.kind = kind;
    this.refreshable = refreshable;
  }
}

export function classifyProviderErrorCode(
  code: string | null | undefined,
): { kind: ProviderErrorKind; refreshable: boolean } {
  switch ((code ?? "").trim().toLowerCase()) {
    case PROVIDER_ERROR_CODES.unavailable:
      return { kind: "unavailable", refreshable: true };
    case PROVIDER_ERROR_CODES.conflict:
    case PROVIDER_ERROR_CODES.bindingConflict:
      return { kind: "conflict", refreshable: true };
    case PROVIDER_ERROR_CODES.bindingBusy:
      return { kind: "busy", refreshable: true };
    case PROVIDER_ERROR_CODES.bindingIncompatible:
    case PROVIDER_ERROR_CODES.bindingNotRouted:
    case PROVIDER_ERROR_CODES.unsupported:
      return { kind: "incompatible", refreshable: false };
    case PROVIDER_ERROR_CODES.credentialNotReady:
    case PROVIDER_ERROR_CODES.credentialStoreUnavailable:
    case PROVIDER_ERROR_CODES.credentialStoreFailed:
      return { kind: "credential", refreshable: true };
    case PROVIDER_ERROR_CODES.secureTransportRequired:
      return { kind: "secure_transport", refreshable: false };
    case PROVIDER_ERROR_CODES.notFound:
    case PROVIDER_ERROR_CODES.bindingNotFound:
      return { kind: "not_found", refreshable: true };
    case PROVIDER_ERROR_CODES.invalid:
    case PROVIDER_ERROR_CODES.inUse:
    case PROVIDER_ERROR_CODES.invalidReply:
      return { kind: "invalid", refreshable: true };
    case PROVIDER_ERROR_CODES.offline:
      return { kind: "offline", refreshable: true };
    case PROVIDER_ERROR_CODES.timeout:
      return { kind: "timeout", refreshable: true };
    default:
      return { kind: "unknown", refreshable: true };
  }
}

export function defaultProviderErrorMessage(kind: ProviderErrorKind): string {
  switch (kind) {
    case "unavailable":
      return "Providers are not available on this daemon.";
    case "conflict":
      return "Providers changed. Refresh and try again.";
    case "busy":
      return "This Session is busy. Wait for the current turn, then try again.";
    case "incompatible":
      return "That Provider or Model is not compatible with this Session.";
    case "credential":
      return "The Provider API key could not be updated. You can retry from Providers.";
    case "secure_transport":
      return "API keys can only be changed over a secure connection.";
    case "not_found":
      return "The Provider or Session Model selection was not found.";
    case "invalid":
      return "The Provider request or reply was invalid. Refresh before trying again.";
    case "offline":
      return "The current server is not connected.";
    case "timeout":
      return "Timed out waiting for Providers.";
    default:
      return "The Provider request failed. Refresh and try again.";
  }
}

function sanitizeProviderMessage(message: string): string {
  return message
    .replace(/model profiles/gi, "Providers")
    .replace(/model profile/gi, "Provider")
    .replace(/\bprofiles\b/gi, "Providers")
    .replace(/\bprofile\b/gi, "Provider")
    .replace(/session route/gi, "Session Model selection")
    .replace(/route binding/gi, "Session Model selection");
}

export function providerErrorFromPayload(
  payload: { code?: string; message?: string },
  options?: { credentialWrite?: boolean },
): ProviderError {
  const code = (payload.code ?? "").trim() || "unknown";
  const classification = classifyProviderErrorCode(code);
  // Credential writes never surface daemon-provided text: a faulty provider or
  // credential store must not be able to reflect the submitted key.
  const message = options?.credentialWrite
    ? defaultProviderErrorMessage(classification.kind)
    : sanitizeProviderMessage((payload.message ?? "").trim()) ||
      defaultProviderErrorMessage(classification.kind);
  return new ProviderError(
    code,
    message,
    classification.kind,
    classification.refreshable,
  );
}

export function offlineProviderError(): ProviderError {
  return new ProviderError(
    PROVIDER_ERROR_CODES.offline,
    defaultProviderErrorMessage("offline"),
    "offline",
    true,
  );
}

export function invalidProviderReply(message?: string): ProviderError {
  return new ProviderError(
    PROVIDER_ERROR_CODES.invalidReply,
    message?.trim() || defaultProviderErrorMessage("invalid"),
    "invalid",
    true,
  );
}

export function ambiguousProviderMutation(message?: string): ProviderError {
  return new ProviderError(
    PROVIDER_ERROR_CODES.invalidReply,
    message?.trim() ||
      "The Provider change may have applied. Refresh before making another change.",
    "unknown",
    true,
  );
}

export function providerMutationRequiresRefresh(error: unknown): boolean {
  if (!(error instanceof ProviderError)) return true;
  return (
    error.kind === "offline" ||
    error.kind === "timeout" ||
    error.kind === "unknown" ||
    error.kind === "conflict" ||
    error.kind === "unavailable"
  );
}

export const mutationAmbiguityRequiresRefresh =
  providerMutationRequiresRefresh;

export function activationBusyAllowsRetryWithoutRefresh(
  error: unknown,
): boolean {
  return error instanceof ProviderError && error.kind === "busy";
}

export function activationConflictRequiresRefresh(error: unknown): boolean {
  return (
    error instanceof ProviderError &&
    (error.kind === "conflict" ||
      error.kind === "offline" ||
      error.kind === "timeout" ||
      error.kind === "unknown")
  );
}

export function presentProviderError(error: unknown): {
  title: string;
  message: string;
  refreshable: boolean;
  kind: ProviderErrorKind;
} {
  if (error instanceof ProviderError) {
    const title =
      error.kind === "conflict"
        ? "Refresh required"
        : error.kind === "credential"
          ? "API key not ready"
          : error.kind === "secure_transport"
            ? "Secure connection required"
            : error.kind === "offline"
              ? "Offline"
              : "Providers";
    return {
      title,
      message: error.message,
      refreshable: error.refreshable,
      kind: error.kind,
    };
  }
  return {
    title: "Providers",
    message:
      error instanceof Error
        ? sanitizeProviderMessage(error.message)
        : defaultProviderErrorMessage("unknown"),
    refreshable: true,
    kind: "unknown",
  };
}

/** Removes common bearer/API-key shapes before any error text reaches UI. */
export function scrubPossibleSecret(message: string): string {
  return message
    .replace(/\bsk-[A-Za-z0-9._-]+\b/g, "[redacted]")
    .replace(/\bBearer\s+\S+/gi, "Bearer [redacted]");
}
