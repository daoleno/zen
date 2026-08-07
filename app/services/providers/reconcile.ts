export type ProviderRequestChannel =
  | "catalog"
  | "catalog_mutation"
  | "session"
  | "activation";

export type ProviderRequestToken = {
  channel: ProviderRequestChannel;
  generation: number;
  projectionEpoch: number;
  serverId: string;
  scope: string;
};

export type ProviderAdmission =
  | { ok: true; token: ProviderRequestToken }
  | { ok: false; reason: string };

/**
 * Synchronous admission owner for current-server Provider surfaces.
 *
 * It prevents stale same-server replies, overlapping revision writes, and
 * activation retries after ambiguous results. React state is presentation only;
 * the owner remains the write gate.
 */
export class ProviderRequestOwner {
  private serverId = "";
  private scope = "";
  private projectionEpoch = 0;
  private generations: Record<ProviderRequestChannel, number> = {
    catalog: 0,
    catalog_mutation: 0,
    session: 0,
    activation: 0,
  };
  private catalogMutation: ProviderRequestToken | null = null;
  private activation: ProviderRequestToken | null = null;
  private catalogRefreshRequired = false;
  private activationRefreshRequired = false;
  private appliedRevision = 0;

  rebind(serverId: string | null | undefined, scope = ""): boolean {
    const nextServer = serverId?.trim() || "";
    const nextScope = scope.trim();
    if (nextServer === this.serverId && nextScope === this.scope) return false;
    this.serverId = nextServer;
    this.scope = nextScope;
    this.invalidateAll();
    this.catalogRefreshRequired = false;
    this.activationRefreshRequired = false;
    this.appliedRevision = 0;
    return true;
  }

  private issue(channel: ProviderRequestChannel): ProviderRequestToken {
    const generation = ++this.generations[channel];
    const projectionEpoch =
      channel === "catalog" || channel === "catalog_mutation"
        ? ++this.projectionEpoch
        : this.projectionEpoch;
    return {
      channel,
      generation,
      projectionEpoch,
      serverId: this.serverId,
      scope: this.scope,
    };
  }

  isCurrent(token: ProviderRequestToken): boolean {
    return (
      token.serverId === this.serverId &&
      token.scope === this.scope &&
      this.generations[token.channel] === token.generation
    );
  }

  admitCatalogLoad(_requestId?: string): ProviderAdmission {
    if (!this.serverId) {
      return { ok: false, reason: "No current server is connected." };
    }
    if (this.catalogMutation) {
      return {
        ok: false,
        reason: "Wait for the current Provider change to finish.",
      };
    }
    return { ok: true, token: this.issue("catalog") };
  }

  acceptCatalog(token: ProviderRequestToken, revision: number): boolean {
    if (!this.isCurrent(token) || revision < this.appliedRevision) return false;
    this.appliedRevision = revision;
    this.catalogRefreshRequired = false;
    return true;
  }

  acceptCatalogRefresh(input: {
    token: ProviderRequestToken;
    revision: number;
  }): boolean {
    return this.acceptCatalog(input.token, input.revision);
  }

  admitCatalogMutation(
    _channel?: "mutation" | "default",
    _requestId?: string,
  ): ProviderAdmission {
    if (!this.serverId) {
      return { ok: false, reason: "No current server is connected." };
    }
    if (this.catalogRefreshRequired) {
      return {
        ok: false,
        reason: "Refresh Providers before making another change.",
      };
    }
    if (this.catalogMutation) {
      return {
        ok: false,
        reason: "Another Provider change is already in progress.",
      };
    }
    this.generations.catalog += 1;
    const token = this.issue("catalog_mutation");
    this.catalogMutation = token;
    return { ok: true, token };
  }

  settleCatalogMutation(
    tokenOrInput:
      | ProviderRequestToken
      | {
          token: ProviderRequestToken;
          applied: boolean;
          durable: boolean;
          ambiguous: boolean;
          revision?: number;
        },
    options?: { refreshRequired?: boolean; revision?: number },
  ): void {
    const token =
      "token" in tokenOrInput ? tokenOrInput.token : tokenOrInput;
    const nextOptions =
      "token" in tokenOrInput
        ? {
            refreshRequired:
              tokenOrInput.ambiguous ||
              (tokenOrInput.applied && !tokenOrInput.durable),
            revision: tokenOrInput.revision,
          }
        : options;
    if (
      !this.catalogMutation ||
      this.catalogMutation.generation !== token.generation ||
      this.catalogMutation.serverId !== token.serverId ||
      this.catalogMutation.scope !== token.scope
    ) {
      return;
    }
    this.catalogMutation = null;
    if (typeof nextOptions?.revision === "number") {
      this.appliedRevision = Math.max(
        this.appliedRevision,
        nextOptions.revision,
      );
    }
    if (nextOptions?.refreshRequired) this.catalogRefreshRequired = true;
  }

  requireCatalogRefresh(): void {
    this.catalogRefreshRequired = true;
  }

  catalogRequiresRefresh(): boolean {
    return this.catalogRefreshRequired;
  }

  catalogMutationWriteLockRequired(): boolean {
    return this.catalogRequiresRefresh();
  }

  applyCatalogMutationProjection(snapshot: { revision: number }): void {
    this.appliedRevision = Math.max(this.appliedRevision, snapshot.revision);
  }

  admitSessionLoad(_requestId?: string): ProviderAdmission {
    if (!this.serverId) {
      return { ok: false, reason: "No current server is connected." };
    }
    if (this.activation) {
      return {
        ok: false,
        reason: "Wait for the current Model change to finish.",
      };
    }
    return { ok: true, token: this.issue("session") };
  }

  acceptSession(token: ProviderRequestToken): boolean {
    if (!this.isCurrent(token)) return false;
    this.activationRefreshRequired = false;
    return true;
  }

  acceptSessionRefresh(token: ProviderRequestToken): boolean {
    return this.acceptSession(token);
  }

  admitActivation(_requestId?: string): ProviderAdmission {
    if (!this.serverId) {
      return { ok: false, reason: "No current server is connected." };
    }
    if (this.activationRefreshRequired) {
      return {
        ok: false,
        reason: "Refresh the current Model before activating again.",
      };
    }
    if (this.activation) {
      return { ok: false, reason: "Another Model activation is in progress." };
    }
    this.generations.session += 1;
    const token = this.issue("activation");
    this.activation = token;
    return { ok: true, token };
  }

  settleActivation(
    tokenOrInput:
      | ProviderRequestToken
      | {
          token: ProviderRequestToken;
          kind: "release" | "lock";
        },
    options?: { refreshRequired?: boolean },
  ): void {
    const token =
      "token" in tokenOrInput ? tokenOrInput.token : tokenOrInput;
    const nextOptions =
      "token" in tokenOrInput
        ? { refreshRequired: tokenOrInput.kind === "lock" }
        : options;
    if (
      !this.activation ||
      this.activation.generation !== token.generation ||
      this.activation.serverId !== token.serverId ||
      this.activation.scope !== token.scope
    ) {
      return;
    }
    this.activation = null;
    if (nextOptions?.refreshRequired) this.activationRefreshRequired = true;
  }

  activationRequiresRefresh(): boolean {
    return this.activationRefreshRequired;
  }

  activationRefreshRequiredGate(): boolean {
    return this.activationRequiresRefresh();
  }

  invalidateAll(): void {
    for (const channel of Object.keys(this.generations) as ProviderRequestChannel[]) {
      this.generations[channel] += 1;
    }
    this.projectionEpoch += 1;
    this.catalogMutation = null;
    this.activation = null;
  }
}

export class ProvidersRequestOwner extends ProviderRequestOwner {}

export type ProvidersUiState<T> = {
  status: "idle" | "loading" | "ready" | "error";
  generation: number;
  data: T | null;
  error: import("./errors").ProviderError | null;
};

export function createProvidersUiState<T>(): ProvidersUiState<T> {
  return { status: "idle", generation: 0, data: null, error: null };
}

export function beginProvidersLoad<T>(
  state: ProvidersUiState<T>,
  generation: number,
): ProvidersUiState<T> {
  return {
    ...state,
    status: "loading",
    generation,
    error: null,
  };
}

export function completeProvidersLoad<T>(
  state: ProvidersUiState<T>,
  generation: number,
  data: T,
): ProvidersUiState<T> {
  if (generation < state.generation) return state;
  return { status: "ready", generation, data, error: null };
}

export function failProvidersLoad<T>(
  state: ProvidersUiState<T>,
  generation: number,
  error: import("./errors").ProviderError,
  retained?: T,
): ProvidersUiState<T> {
  if (generation < state.generation) return state;
  return {
    status: "error",
    generation,
    data: retained ?? state.data,
    error,
  };
}
