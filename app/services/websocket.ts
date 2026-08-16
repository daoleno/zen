import type { StoredServer } from "./storage";
import { Platform } from "react-native";
import { buildAuthorizationHeader } from "./auth";
import { diagnoseConnectionIssue } from "./connectionIssue";
import {
  invalidateStoredServerTransport,
  resolveStoredServerURL,
} from "./pinnedTransport";
import type {
  GitDiffFileContentPayload,
  GitDiffPatchPayload,
  GitRepoBrowserPayload,
  GitRepoFileContentPayload,
  GitDiffStatusSnapshot,
} from "./gitDiff";
import type { SessionService, SessionServiceSnapshot } from "./sessionServices";
import {
  normalizeSessionResourceSnapshot,
  type SessionResourceSnapshot,
} from "./sessionResourceSnapshot";
import {
  normalizeCodexConversation,
  type CodexConversation,
} from "./codexConversation";
import {
  normalizeSessionFileMetadata,
  normalizeSessionFileText,
  type SessionFileMetadata,
  type SessionFileRequest,
  type SessionFileTextPreview,
} from "./sessionFilePreview";
import type { CalendarItem } from "../store/calendar";
import {
  normalizeSkillsCatalogResult,
  normalizeSkillsInventory,
  normalizeSkillsLeaderboards,
  normalizeSkillsMutationCommand,
  normalizeSkillsMutationResult,
  assertSkillsMutationMatchesRequest,
  type ManagedSkillAgent,
  type SkillMutationOperation,
  type SkillsCatalogResult,
  type SkillsInventory,
  type SkillsLeaderboards,
  type SkillsMutationCommand,
  type SkillsMutationResult,
} from "./skillsManagement";
import {
  normalizePluginsInventory,
  normalizePluginMutationCommand,
  normalizePluginMutationResult,
  assertPluginMutationMatchesRequest,
  type PluginInventory,
  type PluginMutationCommand,
  type PluginMutationOperation,
  type PluginMutationResult,
} from "./pluginsManagement";
import { PLUGINS_INVENTORY_TIMEOUT_MS, PLUGIN_COMMAND_TIMEOUT_MS, PLUGIN_MUTATION_TIMEOUT_MS, SKILLS_MUTATION_TIMEOUT_MS } from "./pluginsDeadlines";
import {
  dispatchStructuredCommand,
  sendWebSocketMessageNow,
  structuredActionMessage,
  structuredInputMessage,
  type StructuredCommandReceipt,
} from "./structuredWebSocketTransport";
import {
  ProviderError,
  PROVIDER_ERROR_CODES,
  ambiguousProviderMutation,
  assertThreadRuntimeMatches,
  classifyMutationPersistence,
  invalidProviderReply,
  newProviderRequestId,
  offlineProviderError,
  parseOptionalMutationPersistence,
  parseProviderCredentialResult,
  parseProviderConnectionTestResult,
  parseProviderModelsResult,
  parseThreadRuntimeSelection,
  parseProvidersSnapshot,
  providerErrorFromPayload,
  requireAppliedPersistence,
  type CreateSessionResult,
  type ThreadRuntimeMutationResult,
  type ProviderClient,
  type ProviderConnectionInput,
  type ProviderConnectionTestResult,
  type ProviderCredentialResult,
  type ProviderDefaultInput,
  type ProviderModelsResult,
  type ProviderSwitchInput,
  type ThreadRuntimeSelection,
  type ProvidersMutationResult,
  type ProvidersSnapshot,
} from "./providers";

type MessageHandler = (data: any) => void;

function normalizeCodexSlashCommandInput(value: any): CodexSlashCommandInput {
  const input = value && typeof value === "object" ? value : {};
  return {
    kind: typeof input.kind === "string" && input.kind ? input.kind : "",
    placeholder:
      typeof input.placeholder === "string" ? input.placeholder : undefined,
    picker: typeof input.picker === "string" ? input.picker : undefined,
    required: typeof input.required === "boolean" ? input.required : undefined,
  };
}

function normalizeCodexSlashCommandOutput(value: any): CodexSlashCommandOutput {
  const output = value && typeof value === "object" ? value : {};
  return {
    kind: typeof output.kind === "string" && output.kind ? output.kind : "",
  };
}

export interface CodexAssetPreview {
  path: string;
  content_type: string;
  data_url: string;
}

export interface CodexSlashCommand {
  value: string;
  name: string;
  title: string;
  description: string;
  source?: string;
  category: CodexSlashCommandCategory;
  execution: CodexSlashCommandExecution;
  input: CodexSlashCommandInput;
  output: CodexSlashCommandOutput;
  interactive: boolean;
  chat_supported: boolean;
  terminal_supported: boolean;
}

export type CodexSlashCommandCategory =
  | "session"
  | "navigation"
  | "settings"
  | "tools"
  | "management"
  | "debug"
  | "danger"
  | "unknown"
  | string;

export type CodexSlashCommandExecution =
  | "terminal-required"
  | "insert-only"
  | "native"
  | "unsupported"
  | string;

export interface CodexSlashCommandInput {
  kind: "none" | "inline-args" | "form" | "picker" | "freeform" | string;
  placeholder?: string;
  picker?: string;
  required?: boolean;
}

export interface CodexSlashCommandOutput {
  kind:
    | "none"
    | "markdown"
    | "monospace-log"
    | "diff"
    | "status-card"
    | "management-screen"
    | "terminal"
    | string;
}

export interface CodexSlashCommandSnapshot {
  generated_at?: string;
  source?: string;
  version?: string;
  commands: CodexSlashCommand[];
}

export interface CodexSkill {
  name: string;
  description?: string;
  path: string;
  scope: string;
  enabled: boolean;
}

export interface CodexSkillsSnapshot {
  cwd?: string;
  skills: CodexSkill[];
}

export interface BrainWorkspaceEntry {
  name: string;
  path: string;
  kind: "directory" | "file" | string;
  size?: number;
  modified_at?: string;
  children: BrainWorkspaceEntry[];
}

export interface BrainWorkspaceTree {
  workspace?: string;
  path?: string;
  generated_at?: string;
  entries: BrainWorkspaceEntry[];
}

export interface BrainWorkspaceFile {
  name: string;
  path: string;
  kind: "file" | string;
  language: "markdown" | "text" | string;
  content: string;
  size?: number;
  modified_at?: string;
}

export interface BrainContextPayload {
  thread_id?: string;
  workspace?: string;
  current?: string;
  memory?: string;
  profile?: string;
  personality?: string;
  host_agent?: any;
  host_adapter?: any;
  delegated_adapter?: any;
  adapters?: any[];
  agents?: any[];
  generated_at?: string;
}

export interface BrainHousekeepingPayload {
  workspace?: string;
  current_path?: string;
  policy_paths?: string[];
  worklog_path?: string;
  open_delegated_agents?: any[];
  changed_paths?: string[];
  recommended_next_steps?: string[];
  generated_at?: string;
}

export interface CodexConversationSnapshotPayload {
  request_id?: string;
  agent_id?: string;
  conversation_id?: string;
  revision: number;
  server_generation?: string;
  conversation: CodexConversation;
}

export interface CodexConversationDeltaPayload {
  request_id?: string;
  agent_id?: string;
  conversation_id?: string;
  revision: number;
  base_revision: number;
  server_generation?: string;
  available?: boolean;
  reason?: string;
  source?: string;
  path?: string;
  session_id?: string;
  cwd?: string;
  updated_at?: string;
  activity?: CodexConversation["activity"] | null;
  upserts: CodexConversation["events"];
  deletes: string[];
}

export interface CodexConversationSyncStatusPayload {
  request_id?: string;
  agent_id?: string;
  conversation_id?: string;
  revision: number;
  server_generation?: string;
  state: "syncing" | "ready" | "unavailable" | string;
  reason?: string;
}

export interface CodexConversationSubscriptionOptions {
  targetId?: string;
  agentId?: string;
  cwd?: string;
  command?: string;
  name?: string;
  startedAt?: number;
  processId?: number;
  conversationScopeKey?: string;
}

export interface CodexConversationSubscriptionHandlers {
  onSnapshot(payload: CodexConversationSnapshotPayload): void;
  onDelta(payload: CodexConversationDeltaPayload): void;
  onSyncStatus(payload: CodexConversationSyncStatusPayload): void;
  onError(error: Error): void;
}

interface ConnectionMeta {
  serverId: string;
  serverName: string;
  serverUrl: string;
  daemonId: string;
  daemonPublicKey: string;
  server: StoredServer;
}

export class DaemonRequestError extends Error {
  readonly code: string | undefined;

  constructor(message: string, code?: string) {
    super(message);
    this.name = "DaemonRequestError";
    this.code = code;
  }
}

export function daemonRequestError(
  message: string,
  code?: string,
): DaemonRequestError {
  return new DaemonRequestError(message, code);
}

class ServerSocket {
  private ws: WebSocket | null = null;
  private reconnectDelay = 1000;
  private readonly maxReconnectDelay = 30000;
  private shouldReconnect = true;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private attemptSequence = 0;

  constructor(
    private meta: ConnectionMeta,
    private emit: (type: string, payload: any) => void,
  ) {}

  updateMeta(server: StoredServer) {
    this.meta = toConnectionMeta(server);
  }

  connect() {
    this.shouldReconnect = true;
    this.reconnectDelay = 1000;
    this.startConnectAttempt();
  }

  disconnect() {
    this.shouldReconnect = false;
    this.attemptSequence += 1;

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }

    this.emit("connection_issue", { issue: null });

    const ws = this.ws;
    this.ws = null;
    ws?.close();
  }

  sendNow(msg: object) {
    sendWebSocketMessageNow(this.ws, msg);
  }

  trySendNow(msg: object) {
    try {
      this.sendNow(msg);
      return true;
    } catch {
      return false;
    }
  }

  get isConnected() {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  private startConnectAttempt() {
    const attemptId = ++this.attemptSequence;
    this.emit("connecting", {});
    void this.doConnect(attemptId);
  }

  private scheduleReconnect() {
    if (!this.shouldReconnect) {
      return;
    }

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
    }

    const delay = this.reconnectDelay;
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (!this.shouldReconnect) {
        return;
      }
      this.startConnectAttempt();
    }, delay);
    this.reconnectDelay = Math.min(
      this.reconnectDelay * 2,
      this.maxReconnectDelay,
    );
  }

  /** Skip backoff and reconnect now (e.g. app returned to foreground). */
  resumeReconnect() {
    if (!this.shouldReconnect || this.isConnected) {
      return;
    }
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.reconnectDelay = 1000;
    this.startConnectAttempt();
  }

  private async reportConnectionIssue(attemptId: number) {
    const issue = await diagnoseConnectionIssue({
      server: this.meta.server,
    });

    if (attemptId !== this.attemptSequence) {
      return;
    }
    if (!this.shouldReconnect) {
      return;
    }
    if (this.ws?.readyState === WebSocket.OPEN) {
      return;
    }

    this.emit("connection_issue", { issue });
  }

  private async doConnect(attemptId: number) {
    let opened = false;

    try {
      const authHeader = await buildAuthorizationHeader({
        daemonId: this.meta.daemonId,
        purpose: "zen-connect",
      });
      if (attemptId !== this.attemptSequence || !this.shouldReconnect) {
        return;
      }

      const wsOptions = { headers: { Authorization: authHeader } };
      const transportURL = await resolveStoredServerURL(this.meta.server);
      if (attemptId !== this.attemptSequence || !this.shouldReconnect) {
        return;
      }
      const serverUrl =
        Platform.OS === "web"
          ? appendAuthorizationQuery(transportURL, authHeader)
          : transportURL;
      const WebSocketCtor = WebSocket as any;
      const ws =
        Platform.OS === "web"
          ? new WebSocketCtor(serverUrl)
          : new WebSocketCtor(serverUrl, [], wsOptions);
      this.ws = ws;

      ws.onopen = () => {
        if (attemptId !== this.attemptSequence) {
          ws.close();
          return;
        }

        opened = true;
        this.reconnectDelay = 1000;
        this.emit("connection_issue", { issue: null });
        this.emit("connected", {});
      };

      ws.onmessage = (event: any) => {
        try {
          const data = JSON.parse(event.data);
          this.emit(data.type, data);
        } catch (error) {
          console.warn("[ws] malformed payload", {
            serverId: this.meta.serverId,
            dataType: typeof event?.data,
            error: error instanceof Error ? error.message : String(error),
            sample:
              typeof event?.data === "string"
                ? event.data.slice(0, 200)
                : String(event?.data),
          });
        }
      };

      ws.onclose = () => {
        if (this.ws === ws) {
          this.ws = null;
        }
        if (attemptId !== this.attemptSequence) {
          return;
        }

        // Transient close (background suspension, network blip). Keep
        // reconnecting; UI should retain caches and show "connecting".
        this.emit("disconnected", { reason: "transport_closed" });
        if (this.shouldReconnect) {
          this.emit("connecting", {});
          void invalidateStoredServerTransport(this.meta.server);
        }
        if (!opened) {
          void this.reportConnectionIssue(attemptId);
        }
        this.scheduleReconnect();
      };

      ws.onerror = () => {
        try {
          ws.close();
        } catch {
          // Ignore close errors from failed handshake attempts.
        }
      };
    } catch {
      if (attemptId !== this.attemptSequence) {
        return;
      }

      this.ws = null;
      this.emit("disconnected", { reason: "transport_closed" });
      if (this.shouldReconnect) {
        this.emit("connecting", {});
        void invalidateStoredServerTransport(this.meta.server);
      }
      void this.reportConnectionIssue(attemptId);
      this.scheduleReconnect();
    }
  }
}

export class MultiServerWebSocketClient {
  private readonly handlers = new Map<string, MessageHandler[]>();
  private readonly connections = new Map<string, ServerSocket>();
  private readonly serverMeta = new Map<string, ConnectionMeta>();

  connectServer(server: StoredServer) {
    const meta = toConnectionMeta(server);
    this.serverMeta.set(server.id, meta);

    const existing = this.connections.get(server.id);
    if (existing) {
      existing.disconnect();
      this.connections.delete(server.id);
    }

    const socket = new ServerSocket(meta, (type, payload) => {
      this.emit(type, server.id, payload);
    });
    this.connections.set(server.id, socket);
    socket.connect();
  }

  disconnectServer(serverId: string) {
    this.connections.get(serverId)?.disconnect();
    this.connections.delete(serverId);
    this.serverMeta.delete(serverId);
    this.emit("disconnected", serverId, { reason: "intentional" });
    this.emit("connection_issue", serverId, { issue: null });
  }

  disconnectAll() {
    for (const serverId of this.connections.keys()) {
      this.disconnectServer(serverId);
    }
  }

  /** On foreground, immediately resume any suspended reconnect backoffs. */
  resumeReconnects() {
    for (const socket of this.connections.values()) {
      socket.resumeReconnect();
    }
  }

  on(type: string, handler: MessageHandler) {
    const existing = this.handlers.get(type) || [];
    this.handlers.set(type, [...existing, handler]);
  }

  off(type: string, handler: MessageHandler) {
    const existing = this.handlers.get(type) || [];
    const remaining = existing.filter((current) => current !== handler);
    if (remaining.length === 0) {
      this.handlers.delete(type);
      return;
    }
    this.handlers.set(type, remaining);
  }

  send(serverId: string, msg: object) {
    const socket = this.connections.get(serverId);
    if (!socket) {
      throw new Error("Daemon is not connected.");
    }
    socket.sendNow(msg);
  }

  private trySendNow(serverId: string, msg: object) {
    return this.connections.get(serverId)?.trySendNow(msg) ?? false;
  }

  private sendRequestNow(
    serverId: string,
    msg: object,
    cleanup: () => void,
    reject: (reason?: unknown) => void,
  ) {
    try {
      this.send(serverId, msg);
    } catch (error) {
      cleanup();
      reject(
        error instanceof Error ? error : new Error("Daemon is not connected."),
      );
    }
  }

  createSession(
    serverId: string,
    options?: {
      targetId?: string;
      cwd?: string;
      command?: string;
      name?: string;
      /** Provider connection for the new Session (managed clients only). */
      connectionId?: string;
      /** Client-selected model, carried end-to-end into the launch. */
      modelId?: string;
    },
  ) {
    const requestId = newProviderRequestId();
    return new Promise<CreateSessionResult>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("session_created", handleCreated);
        this.off("error", handleError);
      };
      const handleCreated = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        if (
          payload.agent_session &&
          typeof payload.agent_session === "object"
        ) {
          this.emit("agent_session_created", serverId, {
            agent_session: payload.agent_session,
          });
        }
        if (typeof payload.agent_id !== "string" || !payload.agent_id) {
          reject(new Error("Daemon returned an invalid session id."));
          return;
        }
        try {
          const persistence = parseOptionalMutationPersistence(payload);
          if (persistence) {
            const classification = classifyMutationPersistence(persistence);
            if (classification === "ambiguous") {
              reject(ambiguousProviderMutation(persistence.warning));
              return;
            }
            if (classification === "not_applied") {
              reject(
                providerErrorFromPayload({
                  code: payload.code || PROVIDER_ERROR_CODES.invalid,
                  message:
                    payload.persistence_warning ||
                    payload.message ||
                    "Session was not created.",
                }),
              );
              return;
            }
          }
          resolve({
            agentId: payload.agent_id,
            persistence,
          });
        } catch (error) {
          reject(
            error instanceof ProviderError
              ? error
              : invalidProviderReply(
                  error instanceof Error
                    ? error.message
                    : "Invalid create_session payload.",
                ),
          );
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        if (payload.code) {
          reject(providerErrorFromPayload(payload));
          return;
        }
        reject(new Error(payload.message || "Failed to create terminal."));
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(
          new ProviderError(
            PROVIDER_ERROR_CODES.timeout,
            "Timed out while creating a new terminal.",
            "timeout",
            true,
          ),
        );
      }, 10000);
      this.on("session_created", handleCreated);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "create_session",
          request_id: requestId,
          target_id: options?.targetId,
          cwd: options?.cwd,
          command: options?.command,
          name: options?.name,
          ...(options?.connectionId?.trim()
            ? { connection_id: options.connectionId.trim() }
            : {}),
          ...(options?.modelId?.trim()
            ? { model_id: options.modelId.trim() }
            : {}),
        },
        cleanup,
        reject,
      );
    });
  }

  listProviders(serverId: string): Promise<ProvidersSnapshot> {
    return this.requestProvidersCatalog(
      serverId,
      { type: "list_providers" },
      "Timed out while loading Providers.",
      true,
    ).then((result) => result.snapshot);
  }

  /**
   * Create/update a Provider connection. The optional credential is written
   * atomically with the connection: empty omits the key (preserving any
   * stored secret), non-empty replaces it. The value is scrubbed after send.
   */
  upsertProviderConnection(
    serverId: string,
    input: {
      connection: ProviderConnectionInput;
      revision: number;
      operation?: "create" | "update";
      credential?: string;
    },
  ): Promise<ProvidersMutationResult> {
    let transientCredential = input.credential?.trim() ?? "";
    const body: Record<string, unknown> = {
      type: "upsert_provider_connection",
      provider_connection: input.connection,
      revision: input.revision,
      operation: input.operation ?? "update",
    };
    if (transientCredential) {
      body.credential = transientCredential;
    }
    return this.requestProvidersCatalog(
      serverId,
      body,
      "Timed out while saving Provider connection.",
      false,
    ).finally(() => {
      transientCredential = "";
    });
  }

  deleteProviderConnection(
    serverId: string,
    connectionId: string,
    revision: number,
  ): Promise<ProvidersMutationResult> {
    return this.requestProvidersCatalog(
      serverId,
      {
        type: "delete_provider_connection",
        connection_id: connectionId,
        revision,
      },
      "Timed out while deleting Provider connection.",
      false,
    );
  }

  setProviderDefault(
    serverId: string,
    input: ProviderDefaultInput,
  ): Promise<ProvidersMutationResult> {
    return this.requestProvidersCatalog(
      serverId,
      {
        type: "set_provider_default",
        client: input.client,
        executor_id: input.client,
        connection_id: input.connectionId,
        ...(input.modelId?.trim()
          ? { model_id: input.modelId.trim() }
          : {}),
        revision: input.revision,
      },
      "Timed out while updating Provider default.",
      false,
    );
  }

  switchProvider(
    serverId: string,
    input: ProviderSwitchInput,
  ): Promise<ProvidersMutationResult> {
    return this.requestProvidersCatalog(
      serverId,
      {
        type: "switch_provider",
        client: input.client,
        executor_id: input.client,
        connection_id: input.connectionId,
        revision: input.revision,
      },
      "Timed out while switching Provider.",
      false,
    );
  }

  /**
   * Persist the client-side model support allowlist of one connection: the
   * full set of discovered models the client wants exposed. The gateway never
   * owns a default model; this write only toggles which models are supported.
   */
  setProviderModels(
    serverId: string,
    input: { connectionId: string; modelIds: string[] },
  ): Promise<ProvidersMutationResult> {
    return this.requestProvidersCatalog(
      serverId,
      {
        type: "set_provider_models",
        connection_id: input.connectionId,
        model_ids: input.modelIds,
      },
      "Timed out while updating model support.",
      false,
    );
  }

  discoverProviderModels(
    serverId: string,
    connectionId: string,
  ): Promise<ProviderModelsResult> {
    const requestId = newProviderRequestId();
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("provider_models", handleModels);
        this.off("error", handleError);
      };
      const handleModels = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        try {
          const parsed = parseProviderModelsResult(payload, connectionId);
          if (!parsed) {
            reject(invalidProviderReply("Daemon returned invalid provider models."));
            return;
          }
          resolve(parsed);
        } catch (error) {
          reject(
            error instanceof ProviderError
              ? error
              : invalidProviderReply(
                  error instanceof Error ? error.message : "Invalid models payload.",
                ),
          );
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(providerErrorFromPayload(payload));
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(
          new ProviderError(
            PROVIDER_ERROR_CODES.timeout,
            "Timed out while discovering models.",
            "timeout",
            true,
          ),
        );
      }, 20000);
      this.on("provider_models", handleModels);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "discover_provider_models",
          request_id: requestId,
          connection_id: connectionId,
        },
        cleanup,
        reject,
      );
    });
  }

  testProviderConnection(
    serverId: string,
    input: { client: "codex" | "claude"; baseUrl: string; apiKey: string },
  ): Promise<ProviderConnectionTestResult> {
    let transientCredential = input.apiKey.trim();
    const baseUrl = input.baseUrl.trim();
    if (!baseUrl || !transientCredential) {
      return Promise.reject(
        new ProviderError(
          PROVIDER_ERROR_CODES.invalid,
          "Enter a Base URL and API key.",
          "credential",
          false,
        ),
      );
    }
    const requestId = newProviderRequestId();
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("provider_connection_test", handleResult);
        this.off("error", handleError);
      };
      const handleResult = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        try {
          const parsed = parseProviderConnectionTestResult(
            payload,
            input.client,
          );
          if (!parsed) {
            reject(invalidProviderReply("Daemon returned an invalid connection test result."));
            return;
          }
          resolve(parsed);
        } catch (error) {
          reject(
            error instanceof ProviderError
              ? error
              : invalidProviderReply(
                  error instanceof Error
                    ? error.message
                    : "Invalid connection test payload.",
                ),
          );
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(providerErrorFromPayload(payload, { credentialWrite: true }));
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(
          new ProviderError(
            PROVIDER_ERROR_CODES.timeout,
            "Connection test timed out.",
            "timeout",
            true,
          ),
        );
      }, 20000);
      this.on("provider_connection_test", handleResult);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "test_provider_connection",
          request_id: requestId,
          provider_connection: {
            preset_id: "custom",
            client: input.client,
            base_url: baseUrl,
            advanced: true,
          },
          credential: transientCredential,
        },
        cleanup,
        reject,
      );
      transientCredential = "";
    });
  }

  /**
   * Test the exact saved connection by stable Provider ID. The daemon resolves
   * the persisted Base URL, compiled protocol and active stored credential ref
   * internally; the App never supplies or receives the secret.
   */
  testSavedProviderConnection(
    serverId: string,
    connectionId: string,
    client: string = "codex",
  ): Promise<ProviderConnectionTestResult> {
    const id = connectionId.trim();
    if (!id) {
      return Promise.reject(
        new ProviderError(
          PROVIDER_ERROR_CODES.invalid,
          "Provider id is required to test the saved connection.",
          "invalid",
          false,
        ),
      );
    }
    const requestId = newProviderRequestId();
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("provider_connection_test", handleResult);
        this.off("error", handleError);
      };
      const handleResult = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        try {
          const parsed = parseProviderConnectionTestResult(payload, client);
          if (!parsed) {
            reject(invalidProviderReply("Daemon returned an invalid connection test result."));
            return;
          }
          resolve(parsed);
        } catch (error) {
          reject(
            error instanceof ProviderError
              ? error
              : invalidProviderReply(
                  error instanceof Error
                    ? error.message
                    : "Invalid connection test payload.",
                ),
          );
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(providerErrorFromPayload(payload, { credentialWrite: false }));
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(
          new ProviderError(
            PROVIDER_ERROR_CODES.timeout,
            "Connection test timed out.",
            "timeout",
            true,
          ),
        );
      }, 20000);
      this.on("provider_connection_test", handleResult);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "test_provider_connection",
          request_id: requestId,
          connection_id: id,
        },
        cleanup,
        reject,
      );
    });
  }

  getThreadRuntime(
    serverId: string,
    agentId: string,
  ): Promise<ThreadRuntimeSelection> {
    const requestId = newProviderRequestId();
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("thread_runtime", handleSelection);
        this.off("error", handleError);
      };
      const handleSelection = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        try {
          const selection = parseThreadRuntimeSelection(
            payload.runtime,
            agentId,
          );
          if (!selection) {
            reject(
              invalidProviderReply(
                "Daemon returned an invalid session provider selection.",
              ),
            );
            return;
          }
          resolve(selection);
        } catch (error) {
          reject(
            error instanceof ProviderError
              ? error
              : invalidProviderReply(
                  error instanceof Error
                    ? error.message
                    : "Invalid session provider payload.",
                ),
          );
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(providerErrorFromPayload(payload));
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(
          new ProviderError(
            PROVIDER_ERROR_CODES.timeout,
            "Timed out while loading session provider.",
            "timeout",
            true,
          ),
        );
      }, 15000);
      this.on("thread_runtime", handleSelection);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "get_thread_runtime",
          request_id: requestId,
          agent_id: agentId,
        },
        cleanup,
        reject,
      );
    });
  }

  setThreadRuntime(
    serverId: string,
    input: {
      agentId: string;
      runtime: import("./providers").ThreadRuntimeChoice;
    },
  ): Promise<ThreadRuntimeMutationResult> {
    const requestId = newProviderRequestId();
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("thread_runtime_set", handleActivated);
        this.off("error", handleError);
      };
      const handleActivated = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        try {
          const persistence = requireAppliedPersistence(payload);
          const selection = parseThreadRuntimeSelection(
            payload.runtime,
            input.agentId,
          );
          if (
            !selection ||
            !assertThreadRuntimeMatches(selection, input)
          ) {
            reject(
              invalidProviderReply(
                "Daemon returned an invalid activation selection.",
              ),
            );
            return;
          }
          resolve({ runtime: selection, persistence });
        } catch (error) {
          reject(
            error instanceof ProviderError
              ? error
              : invalidProviderReply(
                  error instanceof Error
                    ? error.message
                    : "Invalid activation payload.",
                ),
          );
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(providerErrorFromPayload(payload));
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(
          new ProviderError(
            PROVIDER_ERROR_CODES.timeout,
            "Timed out while switching model.",
            "timeout",
            true,
          ),
        );
      }, 20000);
      this.on("thread_runtime_set", handleActivated);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "set_thread_runtime",
          request_id: requestId,
          agent_id: input.agentId,
          runtime: {
            connection_id: input.runtime.connectionId,
            model_id: input.runtime.modelId,
            ...(input.runtime.effect?.trim()
              ? { effect: input.runtime.effect.trim() }
              : {}),
            ...(input.runtime.useDefaultEffect
              ? { use_default_effect: true }
              : {}),
          },
        },
        cleanup,
        reject,
      );
    });
  }

  setProviderCredential(
    serverId: string,
    connectionId: string,
    credential: string,
  ): Promise<ProviderCredentialResult> {
    let transientCredential = credential.trim();
    if (!transientCredential) {
      return Promise.reject(
        new ProviderError(
          PROVIDER_ERROR_CODES.invalid,
          "Enter an API key.",
          "credential",
          false,
        ),
      );
    }
    const requestId = newProviderRequestId();
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("provider_credential", handleResult);
        this.off("error", handleError);
      };
      const handleResult = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        try {
          const parsed = parseProviderCredentialResult(payload, connectionId);
          if (!parsed) {
            reject(
              invalidProviderReply(
                "Daemon returned an invalid credential result.",
              ),
            );
            return;
          }
          resolve(parsed);
        } catch (error) {
          reject(
            error instanceof ProviderError
              ? error
              : invalidProviderReply(
                  error instanceof Error
                    ? error.message
                    : "Invalid credential payload.",
                ),
          );
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(providerErrorFromPayload(payload, { credentialWrite: true }));
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(
          new ProviderError(
            PROVIDER_ERROR_CODES.timeout,
            "Timed out while saving API key.",
            "timeout",
            true,
          ),
        );
      }, 15000);
      this.on("provider_credential", handleResult);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "set_provider_credential",
          request_id: requestId,
          connection_id: connectionId,
          credential: transientCredential,
        },
        cleanup,
        reject,
      );
      transientCredential = "";
    });
  }


  private requestProvidersCatalog(
    serverId: string,
    body: Record<string, unknown>,
    timeoutMessage: string,
    isList: boolean,
  ): Promise<ProvidersMutationResult> {
    if (!this.connections.get(serverId)) {
      return Promise.reject(offlineProviderError());
    }
    const requestId = newProviderRequestId();
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("providers", handleCatalog);
        this.off("error", handleError);
      };
      const handleCatalog = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        try {
          const snapshot = parseProvidersSnapshot(payload);
          if (!snapshot) {
            reject(
              invalidProviderReply(
                "Daemon returned an invalid Providers catalog.",
              ),
            );
            return;
          }
          const persistence = isList
            ? parseOptionalMutationPersistence(payload) ?? {
                applied: true,
                durable: true,
                outcome: "applied",
              }
            : requireAppliedPersistence(payload);
          if (!isList) {
            // requireApplied already validated
          } else if (persistence.ambiguous) {
            reject(ambiguousProviderMutation(persistence.warning));
            return;
          }
          resolve({ snapshot, catalog: snapshot, persistence });
        } catch (error) {
          reject(
            error instanceof ProviderError
              ? error
              : invalidProviderReply(
                  error instanceof Error
                    ? error.message
                    : "Daemon returned an invalid Providers payload.",
                ),
          );
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(providerErrorFromPayload(payload));
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(
          new ProviderError(
            PROVIDER_ERROR_CODES.timeout,
            timeoutMessage,
            "timeout",
            true,
          ),
        );
      }, 15000);
      this.on("providers", handleCatalog);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          ...body,
          request_id: requestId,
        },
        cleanup,
        reject,
      );
    });
  }

  listDir(serverId: string, path?: string) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<{
      path: string;
      entries: { name: string; path: string }[];
    }>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("dir_list", handleList);
        this.off("error", handleError);
      };

      const handleList = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId)
          return;
        cleanup();
        resolve({ path: payload.path, entries: payload.entries ?? [] });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId)
          return;
        cleanup();
        reject(new Error(payload.message || "Failed to list directory."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while listing directory."));
      }, 10000);

      this.on("dir_list", handleList);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "list_dir",
          request_id: requestId,
          cwd: path ?? "",
        },
        cleanup,
        reject,
      );
    });
  }

  getGitDiffStatus(
    serverId: string,
    options?: {
      targetId?: string;
      cwd?: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<GitDiffStatusSnapshot>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("git_diff_status", handleStatus);
        this.off("error", handleError);
      };

      const handleStatus = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(
          (payload.status ?? {
            available: false,
            clean: true,
            file_count: 0,
            staged_file_count: 0,
            unstaged_file_count: 0,
            untracked_file_count: 0,
            additions: 0,
            deletions: 0,
            files: [],
          }) as GitDiffStatusSnapshot,
        );
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to load git diff status."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading git diff status."));
      }, 10000);

      this.on("git_diff_status", handleStatus);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "git_diff_status",
          request_id: requestId,
          target_id: options?.targetId,
          cwd: options?.cwd,
        },
        cleanup,
        reject,
      );
    });
  }

  getGitDiffPatch(
    serverId: string,
    options: {
      targetId?: string;
      cwd?: string;
      path: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<GitDiffPatchPayload>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("git_diff_patch", handlePatch);
        this.off("error", handleError);
      };

      const handlePatch = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(payload.patch as GitDiffPatchPayload);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to load git diff patch."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading git diff patch."));
      }, 10000);

      this.on("git_diff_patch", handlePatch);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "git_diff_patch",
          request_id: requestId,
          target_id: options.targetId,
          cwd: options.cwd,
          path: options.path,
        },
        cleanup,
        reject,
      );
    });
  }

  getGitDiffFileContent(
    serverId: string,
    options: {
      targetId?: string;
      cwd?: string;
      path: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<GitDiffFileContentPayload>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("git_diff_file_content", handleContent);
        this.off("error", handleError);
      };

      const handleContent = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(payload.content as GitDiffFileContentPayload);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(
          new Error(payload.message || "Failed to load git diff file content."),
        );
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading git diff file content."));
      }, 10000);

      this.on("git_diff_file_content", handleContent);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "git_diff_file_content",
          request_id: requestId,
          target_id: options.targetId,
          cwd: options.cwd,
          path: options.path,
        },
        cleanup,
        reject,
      );
    });
  }

  getGitRepoEntries(
    serverId: string,
    options?: {
      targetId?: string;
      cwd?: string;
      path?: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<GitRepoBrowserPayload>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("git_repo_entries", handleEntries);
        this.off("error", handleError);
      };

      const handleEntries = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(payload.browser as GitRepoBrowserPayload);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(
          new Error(payload.message || "Failed to load repository files."),
        );
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading repository files."));
      }, 10000);

      this.on("git_repo_entries", handleEntries);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "git_repo_entries",
          request_id: requestId,
          target_id: options?.targetId,
          cwd: options?.cwd,
          path: options?.path,
        },
        cleanup,
        reject,
      );
    });
  }

  getGitRepoFileContent(
    serverId: string,
    options: {
      targetId?: string;
      cwd?: string;
      path: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<GitRepoFileContentPayload>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("git_repo_file_content", handleContent);
        this.off("error", handleError);
      };

      const handleContent = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(payload.content as GitRepoFileContentPayload);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to load repository file."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading repository file."));
      }, 10000);

      this.on("git_repo_file_content", handleContent);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "git_repo_file_content",
          request_id: requestId,
          target_id: options.targetId,
          cwd: options.cwd,
          path: options.path,
        },
        cleanup,
        reject,
      );
    });
  }

  subscribeCodexConversation(
    serverId: string,
    options: CodexConversationSubscriptionOptions,
    handlers: CodexConversationSubscriptionHandlers,
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    const handleSnapshot = (payload: any) => {
      if (payload.serverId !== serverId || payload.request_id !== requestId) {
        return;
      }
      handlers.onSnapshot(normalizeCodexConversationSnapshotPayload(payload));
    };

    const handleDelta = (payload: any) => {
      if (payload.serverId !== serverId || payload.request_id !== requestId) {
        return;
      }
      handlers.onDelta(normalizeCodexConversationDeltaPayload(payload));
    };

    const handleSyncStatus = (payload: any) => {
      if (payload.serverId !== serverId || payload.request_id !== requestId) {
        return;
      }
      handlers.onSyncStatus(
        normalizeCodexConversationSyncStatusPayload(payload),
      );
    };

    const handleError = (payload: any) => {
      if (payload.serverId !== serverId || payload.request_id !== requestId) {
        return;
      }
      handlers.onError(new Error(payload.message ?? ""));
    };

    const removeHandlers = () => {
      this.off("codex_conversation_snapshot", handleSnapshot);
      this.off("codex_conversation_delta", handleDelta);
      this.off("codex_conversation_sync_status", handleSyncStatus);
      this.off("error", handleError);
    };

    this.on("codex_conversation_snapshot", handleSnapshot);
    this.on("codex_conversation_delta", handleDelta);
    this.on("codex_conversation_sync_status", handleSyncStatus);
    this.on("error", handleError);
    try {
      this.send(serverId, {
        type: "codex_conversation_subscribe",
        request_id: requestId,
        target_id: options.targetId,
        agent_id: options.agentId,
        cwd: options.cwd,
        command: options.command,
        name: options.name,
        started_at: options.startedAt,
        process_id: options.processId,
        conversation_scope_key: options.conversationScopeKey,
      });
    } catch (error) {
      removeHandlers();
      throw error;
    }

    let subscribed = true;
    return () => {
      if (!subscribed) {
        return;
      }
      subscribed = false;
      removeHandlers();
      this.trySendNow(serverId, {
        type: "codex_conversation_unsubscribe",
        request_id: requestId,
        target_id: options.targetId,
        agent_id: options.agentId,
      });
    };
  }

  getCodexSlashCommands(serverId: string) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<CodexSlashCommandSnapshot>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("codex_slash_commands", handleCommands);
        this.off("error", handleError);
      };

      const handleCommands = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        const commands = Array.isArray(payload.commands)
          ? payload.commands
              .map((command: any) => ({
                value: typeof command.value === "string" ? command.value : "",
                name: typeof command.name === "string" ? command.name : "",
                title: typeof command.title === "string" ? command.title : "",
                description:
                  typeof command.description === "string"
                    ? command.description
                    : "",
                source:
                  typeof command.source === "string"
                    ? command.source
                    : undefined,
                category:
                  typeof command.category === "string" && command.category
                    ? command.category
                    : "",
                execution:
                  typeof command.execution === "string" && command.execution
                    ? command.execution
                    : "",
                input: normalizeCodexSlashCommandInput(command.input),
                output: normalizeCodexSlashCommandOutput(command.output),
                interactive: Boolean(command.interactive),
                chat_supported: Boolean(command.chat_supported),
                terminal_supported:
                  typeof command.terminal_supported === "boolean"
                    ? command.terminal_supported
                    : command.execution !== "unsupported",
              }))
              .filter(
                (command: CodexSlashCommand) =>
                  command.value.startsWith("/") && command.name.length > 0,
              )
          : [];
        resolve({
          generated_at:
            typeof payload.generated_at === "string"
              ? payload.generated_at
              : undefined,
          source:
            typeof payload.source === "string" ? payload.source : undefined,
          version:
            typeof payload.version === "string" ? payload.version : undefined,
          commands,
        });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to load Codex commands."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading Codex commands."));
      }, 10000);

      this.on("codex_slash_commands", handleCommands);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "codex_slash_commands",
          request_id: requestId,
        },
        cleanup,
        reject,
      );
    });
  }

  getCodexSkills(
    serverId: string,
    options: {
      cwd?: string;
    } = {},
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<CodexSkillsSnapshot>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("codex_skills", handleSkills);
        this.off("error", handleError);
      };

      const handleSkills = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        const skills = Array.isArray(payload.skills)
          ? payload.skills
              .map((skill: any) => ({
                name: typeof skill.name === "string" ? skill.name : "",
                description:
                  typeof skill.description === "string"
                    ? skill.description
                    : undefined,
                path: typeof skill.path === "string" ? skill.path : "",
                scope:
                  typeof skill.scope === "string" && skill.scope
                    ? skill.scope
                    : "user",
                enabled:
                  typeof skill.enabled === "boolean" ? skill.enabled : true,
              }))
              .filter(
                (skill: CodexSkill) =>
                  skill.name.length > 0 && skill.path.length > 0,
              )
          : [];
        resolve({
          cwd: typeof payload.cwd === "string" ? payload.cwd : options.cwd,
          skills,
        });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to load Codex skills."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading Codex skills."));
      }, 10000);

      this.on("codex_skills", handleSkills);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "codex_skills",
          request_id: requestId,
          cwd: options.cwd,
        },
        cleanup,
        reject,
      );
    });
  }

  getSkillsInventory(
    serverId: string,
    options: { cwd?: string; generation: number },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return new Promise<{ generation: number; inventory: SkillsInventory }>(
      (resolve, reject) => {
        const cleanup = () => {
          if (timer) clearTimeout(timer);
          this.off("skills_inventory", handleInventory);
          this.off("skills_inventory_error", handleError);
        };
        const handleInventory = (payload: any) => {
          if (
            payload.serverId !== serverId ||
            payload.request_id !== requestId
          ) {
            return;
          }
          cleanup();
          if (payload.generation !== options.generation) {
            reject(
              new Error("Daemon returned a stale Skills inventory generation."),
            );
            return;
          }
          try {
            resolve({
              generation: options.generation,
              inventory: normalizeSkillsInventory(payload.inventory),
            });
          } catch (error) {
            reject(error);
          }
        };
        const handleError = (payload: any) => {
          if (
            payload.serverId !== serverId ||
            payload.request_id !== requestId
          ) {
            return;
          }
          cleanup();
          reject(
            new Error(payload.message || "Failed to load installed Skills."),
          );
        };
        const timer = setTimeout(() => {
          cleanup();
          reject(new Error("Timed out while loading installed Skills."));
        }, 15000);
        this.on("skills_inventory", handleInventory);
        this.on("skills_inventory_error", handleError);
        this.sendRequestNow(
          serverId,
          {
            type: "skills_inventory",
            request_id: requestId,
            generation: options.generation,
            cwd: options.cwd,
          },
          cleanup,
          reject,
        );
      },
    );
  }

  getSkillsLeaderboards(
    serverId: string,
    options: { generation: number; limit?: number },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return new Promise<{
      generation: number;
      leaderboards: SkillsLeaderboards;
    }>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("skills_catalog", handleCatalog);
        this.off("skills_catalog_error", handleError);
      };
      const handleCatalog = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        if (payload.generation !== options.generation) {
          reject(
            new Error("Daemon returned a stale Skills catalog generation."),
          );
          return;
        }
        try {
          resolve({
            generation: options.generation,
            leaderboards: normalizeSkillsLeaderboards(payload.leaderboards),
          });
        } catch (error) {
          reject(error);
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(
          new Error(payload.message || "Failed to load skills.sh rankings."),
        );
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading skills.sh rankings."));
      }, 12000);
      this.on("skills_catalog", handleCatalog);
      this.on("skills_catalog_error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "skills_catalog",
          request_id: requestId,
          generation: options.generation,
          limit: options.limit ?? 30,
        },
        cleanup,
        reject,
      );
    });
  }

  searchSkillsCatalog(
    serverId: string,
    options: { query: string; limit?: number; generation: number },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return new Promise<{ generation: number; result: SkillsCatalogResult }>(
      (resolve, reject) => {
        const cleanup = () => {
          if (timer) clearTimeout(timer);
          this.off("skills_search", handleSearch);
          this.off("skills_search_error", handleError);
        };
        const handleSearch = (payload: any) => {
          if (
            payload.serverId !== serverId ||
            payload.request_id !== requestId
          ) {
            return;
          }
          cleanup();
          if (payload.generation !== options.generation) {
            reject(
              new Error("Daemon returned a stale Skills search generation."),
            );
            return;
          }
          try {
            resolve({
              generation: options.generation,
              result: normalizeSkillsCatalogResult(payload.result),
            });
          } catch (error) {
            reject(error);
          }
        };
        const handleError = (payload: any) => {
          if (
            payload.serverId !== serverId ||
            payload.request_id !== requestId
          ) {
            return;
          }
          cleanup();
          reject(new Error(payload.message || "Failed to search skills.sh."));
        };
        const timer = setTimeout(() => {
          cleanup();
          reject(new Error("Timed out while searching skills.sh."));
        }, 12000);
        this.on("skills_search", handleSearch);
        this.on("skills_search_error", handleError);
        this.sendRequestNow(
          serverId,
          {
            type: "skills_search",
            request_id: requestId,
            generation: options.generation,
            prompt: options.query,
            limit: options.limit ?? 20,
          },
          cleanup,
          reject,
        );
      },
    );
  }

  cancelSkillsCatalogSearch(
    serverId: string,
    options: { generation: number },
  ): boolean {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return this.trySendNow(serverId, {
      type: "skills_search_cancel",
      request_id: requestId,
      generation: options.generation,
    });
  }

  buildSkillsCommand(
    serverId: string,
    options: {
      operation: SkillMutationOperation;
      cwd?: string;
      skillId?: string;
      source?: string;
      skillName?: string;
      scope: "project" | "global";
      agents?: ManagedSkillAgent[];
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return new Promise<SkillsMutationCommand>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("skills_command", handleCommand);
        this.off("skills_command_error", handleError);
      };
      const handleCommand = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        try {
          const command = normalizeSkillsMutationCommand(payload.command);
          if (command.operation !== options.operation) {
            throw new Error(
              "Daemon returned a Skills command for a different request.",
            );
          }
          if (command.scope !== options.scope) {
            throw new Error(
              "Daemon returned a Skills command for a different request.",
            );
          }
          if (options.operation === "install") {
            if (
              command.catalogId !== options.skillId ||
              command.source !== options.source ||
              (options.skillName != null &&
                command.skillName !== options.skillName) ||
              command.agents.length !== (options.agents ?? []).length ||
              command.agents.some(
                (agent, index) => agent !== (options.agents ?? [])[index],
              )
            ) {
              throw new Error(
                "Daemon returned a Skills command for a different request.",
              );
            }
          } else if (options.operation === "remove") {
            if (
              (options.skillName != null &&
                command.skillName !== options.skillName) ||
              command.agents.length !== (options.agents ?? []).length ||
              command.agents.some(
                (agent, index) => agent !== (options.agents ?? [])[index],
              )
            ) {
              throw new Error(
                "Daemon returned a Skills command for a different request.",
              );
            }
          } else if (
            options.operation === "update" &&
            (command.agents.length !== 0 || command.skillName !== "")
          ) {
            throw new Error(
              "Daemon returned a Skills command for a different request.",
            );
          }
          resolve(command);
        } catch (error) {
          reject(error);
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Skills command was rejected."));
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while validating the Skills command."));
      }, 15000);
      this.on("skills_command", handleCommand);
      this.on("skills_command_error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "skills_command",
          request_id: requestId,
          operation: options.operation,
          cwd: options.cwd,
          skill_id: options.skillId,
          source: options.source,
          skill_name: options.skillName,
          scope: options.scope,
          agents: options.agents,
        },
        cleanup,
        reject,
      );
    });
  }

  executeSkillsMutation(
    serverId: string,
    options: {
      operation: SkillMutationOperation;
      cwd?: string;
      skillId?: string;
      source?: string;
      skillName?: string;
      scope: "project" | "global";
      agents?: ManagedSkillAgent[];
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return new Promise<SkillsMutationResult>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("skills_mutation_result", handleResult);
        this.off("skills_mutation_error", handleError);
        this.off("error", handleError);
      };
      const handleResult = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        try {
          const result = normalizeSkillsMutationResult(payload.result);
          assertSkillsMutationMatchesRequest(result, options);
          resolve(result);
        } catch (error) {
          reject(error);
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(
          daemonRequestError(
            payload.message || "The Skills mutation failed.",
            payload.code,
          ),
        );
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while running the Skills mutation."));
      }, SKILLS_MUTATION_TIMEOUT_MS);
      this.on("skills_mutation_result", handleResult);
      this.on("skills_mutation_error", handleError);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "skills_mutation",
          request_id: requestId,
          operation: options.operation,
          cwd: options.cwd,
          skill_id: options.skillId,
          source: options.source,
          skill_name: options.skillName,
          scope: options.scope,
          agents: options.agents,
        },
        cleanup,
        reject,
      );
    });
  }

  getPluginsInventory(serverId: string, options: { generation: number }) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return new Promise<{ generation: number; inventory: PluginInventory }>(
      (resolve, reject) => {
        const cleanup = () => {
          if (timer) clearTimeout(timer);
          this.off("plugins_inventory", handleInventory);
          this.off("plugins_inventory_error", handleError);
          this.off("error", handleGenericError);
        };
        const handleInventory = (payload: any) => {
          if (
            payload.serverId !== serverId ||
            payload.request_id !== requestId
          ) {
            return;
          }
          cleanup();
          if (payload.generation !== options.generation) {
            reject(
              new Error("Daemon returned a stale Plugins inventory generation."),
            );
            return;
          }
          try {
            resolve({
              generation: options.generation,
              inventory: normalizePluginsInventory(payload.inventory),
            });
          } catch (error) {
            reject(error);
          }
        };
        const handleError = (payload: any) => {
          if (
            payload.serverId !== serverId ||
            payload.request_id !== requestId
          ) {
            return;
          }
          cleanup();
          reject(
            daemonRequestError(
              payload.message || "Failed to load Plugins.",
              payload.code,
            ),
          );
        };
        // Unknown request types arrive on the generic error channel; preserve
        // their code so the caller can expose the daemon capability error.
        const handleGenericError = handleError;
        const timer = setTimeout(() => {
          cleanup();
          reject(daemonRequestError("Timed out while loading Plugins.", "timeout"));
        }, PLUGINS_INVENTORY_TIMEOUT_MS);
        this.on("plugins_inventory", handleInventory);
        this.on("plugins_inventory_error", handleError);
        this.on("error", handleGenericError);
        this.sendRequestNow(
          serverId,
          {
            type: "plugins_inventory",
            request_id: requestId,
            generation: options.generation,
          },
          cleanup,
          reject,
        );
      },
    );
  }

  buildPluginCommand(
    serverId: string,
    options: {
      operation: PluginMutationOperation;
      pluginId: string;
      scope: "user";
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return new Promise<PluginMutationCommand>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("plugin_command", handleCommand);
        this.off("plugin_command_error", handleError);
      };
      const handleCommand = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        try {
          const command = normalizePluginMutationCommand(payload.command);
          if (
            command.operation !== options.operation ||
            command.pluginId !== options.pluginId ||
            command.scope !== options.scope
          ) {
            throw new Error(
              "Daemon returned a plugin command for a different request.",
            );
          }
          resolve(command);
        } catch (error) {
          reject(error);
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(
          daemonRequestError(
            payload.message || "Plugin command was rejected.",
            payload.code,
          ),
        );
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(daemonRequestError("Timed out while validating the plugin command.", "timeout"));
      }, PLUGIN_COMMAND_TIMEOUT_MS);
      this.on("plugin_command", handleCommand);
      this.on("plugin_command_error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "plugin_command",
          request_id: requestId,
          operation: options.operation,
          plugin_id: options.pluginId,
          scope: options.scope,
        },
        cleanup,
        reject,
      );
    });
  }

  executePluginMutation(
    serverId: string,
    options: {
      operation: PluginMutationOperation;
      pluginId: string;
      scope: "user";
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return new Promise<PluginMutationResult>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("plugin_mutation_result", handleResult);
        this.off("plugin_mutation_error", handleError);
        this.off("error", handleError);
      };
      const handleResult = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        try {
          const result = normalizePluginMutationResult(payload.result);
          assertPluginMutationMatchesRequest(result, options);
          resolve(result);
        } catch (error) {
          reject(error);
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(
          daemonRequestError(
            payload.message || "The Plugin mutation failed.",
            payload.code,
          ),
        );
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while running the Plugin mutation."));
      }, PLUGIN_MUTATION_TIMEOUT_MS);
      this.on("plugin_mutation_result", handleResult);
      this.on("plugin_mutation_error", handleError);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "plugin_mutation",
          request_id: requestId,
          operation: options.operation,
          plugin_id: options.pluginId,
          scope: options.scope,
        },
        cleanup,
        reject,
      );
    });
  }

  getCodexTerminalSnapshot(serverId: string, targetId: string) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<string>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("codex_terminal_snapshot", handleSnapshot);
        this.off("error", handleError);
      };

      const handleSnapshot = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(typeof payload.text === "string" ? payload.text : "");
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(
          new Error(payload.message || "Failed to load Codex terminal output."),
        );
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading Codex terminal output."));
      }, 10000);

      this.on("codex_terminal_snapshot", handleSnapshot);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "codex_terminal_snapshot",
          request_id: requestId,
          target_id: targetId,
        },
        cleanup,
        reject,
      );
    });
  }

  getCodexAsset(
    serverId: string,
    options: {
      path: string;
      cwd?: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<CodexAssetPreview>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("codex_asset", handleAsset);
        this.off("error", handleError);
      };

      const handleAsset = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve({
          path: typeof payload.path === "string" ? payload.path : options.path,
          content_type:
            typeof payload.content_type === "string"
              ? payload.content_type
              : "image/*",
          data_url:
            typeof payload.data_url === "string" ? payload.data_url : "",
        });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to load Codex asset."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading Codex asset."));
      }, 10000);

      this.on("codex_asset", handleAsset);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "codex_asset",
          request_id: requestId,
          path: options.path,
          cwd: options.cwd,
        },
        cleanup,
        reject,
      );
    });
  }

  getSessionFileMetadata(
    serverId: string,
    request: SessionFileRequest,
  ): Promise<SessionFileMetadata> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        clearTimeout(timer);
        this.off("session_file_metadata", handleMetadata);
        this.off("error", handleError);
      };
      const handleMetadata = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        try {
          resolve(normalizeSessionFileMetadata(payload.metadata));
        } catch (error) {
          reject(error);
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        const error = new Error(
          payload.message || "Failed to inspect the Session file.",
        );
        (error as Error & { code?: string }).code = payload.code;
        reject(error);
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while inspecting the Session file."));
      }, 10000);
      this.on("session_file_metadata", handleMetadata);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "session_file_metadata",
          request_id: requestId,
          agent_id: request.agentId,
          process_id: request.processId,
          started_at: request.startedAt,
          path: request.path,
        },
        cleanup,
        reject,
      );
    });
  }

  getSessionFileText(
    serverId: string,
    request: SessionFileRequest & { generation: string },
  ): Promise<SessionFileTextPreview> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        clearTimeout(timer);
        this.off("session_file_text", handleText);
        this.off("error", handleError);
      };
      const handleText = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        try {
          resolve(normalizeSessionFileText(payload.text));
        } catch (error) {
          reject(error);
        }
      };
      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        const error = new Error(
          payload.message || "Failed to read the Session file.",
        );
        (error as Error & { code?: string }).code = payload.code;
        reject(error);
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while reading the Session file."));
      }, 10000);
      this.on("session_file_text", handleText);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "session_file_text",
          request_id: requestId,
          agent_id: request.agentId,
          process_id: request.processId,
          started_at: request.startedAt,
          path: request.path,
          file_generation: request.generation,
        },
        cleanup,
        reject,
      );
    });
  }

  openTerminal(
    serverId: string,
    targetId: string,
    backend: string = "tmux",
    cols?: number,
    rows?: number,
  ) {
    this.send(serverId, {
      type: "terminal_open",
      target_id: targetId,
      backend,
      cols,
      rows,
    });
  }

  sendTerminalInput(serverId: string, sessionId: string, data: string) {
    this.send(serverId, {
      type: "terminal_input",
      session_id: sessionId,
      data,
    });
  }

  resizeTerminal(
    serverId: string,
    sessionId: string,
    cols: number,
    rows: number,
  ) {
    this.send(serverId, {
      type: "terminal_resize",
      session_id: sessionId,
      cols,
      rows,
    });
  }

  scrollTerminal(serverId: string, sessionId: string, lines: number) {
    this.send(serverId, {
      type: "terminal_scroll",
      session_id: sessionId,
      lines,
    });
  }

  cancelTerminalScroll(serverId: string, sessionId: string) {
    this.send(serverId, {
      type: "terminal_scroll_cancel",
      session_id: sessionId,
    });
  }

  focusTerminalPane(
    serverId: string,
    sessionId: string,
    col: number,
    row: number,
  ) {
    this.send(serverId, {
      type: "terminal_focus_pane",
      session_id: sessionId,
      col,
      row,
    });
  }

  closeTerminal(serverId: string, sessionId: string) {
    this.send(serverId, { type: "terminal_close", session_id: sessionId });
  }

  sendAction(
    serverId: string,
    agentId: string,
    action: string,
  ): StructuredCommandReceipt {
    const socket = this.connections.get(serverId);
    if (!socket?.isConnected) {
      throw new Error("Daemon is not connected.");
    }
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return dispatchStructuredCommand({
      requestId,
      eventSource: this,
      sentType: "action_sent",
      failedType: "action_failed",
      matches: (payload) =>
        payload.serverId === serverId && payload.request_id === requestId,
      matchesConnection: (payload) => payload.serverId === serverId,
      sendNow: () => {
        socket.sendNow(
          structuredActionMessage({
            requestId,
            agentId,
            action,
          }),
        );
      },
    });
  }

  sendInput(
    serverId: string,
    agentId: string,
    text: string,
    options?: {
      displayBody?: string;
      conversationScopeKey?: string;
      requestId?: string;
    },
  ): StructuredCommandReceipt {
    const socket = this.connections.get(serverId);
    if (!socket?.isConnected) {
      throw new Error("Daemon is not connected.");
    }

    // Retries of the exact same logical input reuse its stable request id so
    // the daemon's durable receipt ledger stays idempotent; a new or edited
    // input omits requestId and receives a fresh identity.
    const requestId =
      options?.requestId ||
      `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return dispatchStructuredCommand({
      requestId,
      eventSource: this,
      sentType: "input_sent",
      failedType: "input_failed",
      pendingType: "input_pending",
      matches: (payload) =>
        payload.serverId === serverId && payload.request_id === requestId,
      matchesConnection: (payload) => payload.serverId === serverId,
      sendNow: () => {
        socket.sendNow(
          structuredInputMessage({
            requestId,
            agentId,
            text,
            displayBody: options?.displayBody,
            conversationScopeKey: options?.conversationScopeKey,
          }),
        );
      },
    });
  }

  getTerminalSnapshot(serverId: string, targetId: string) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<{ text: string; target_id?: string }>(
      (resolve, reject) => {
        const cleanup = () => {
          if (timer) clearTimeout(timer);
          this.off("terminal_snapshot", handleSnapshot);
          this.off("error", handleError);
        };

        const handleSnapshot = (payload: any) => {
          if (
            payload.serverId !== serverId ||
            payload.request_id !== requestId
          ) {
            return;
          }
          cleanup();
          resolve({
            text: typeof payload.text === "string" ? payload.text : "",
            target_id:
              typeof payload.target_id === "string"
                ? payload.target_id
                : undefined,
          });
        };

        const handleError = (payload: any) => {
          if (
            payload.serverId !== serverId ||
            payload.request_id !== requestId
          ) {
            return;
          }
          cleanup();
          reject(
            new Error(payload.message || "Failed to load terminal snapshot."),
          );
        };

        const timer = setTimeout(() => {
          cleanup();
          reject(new Error("Timed out while loading terminal snapshot."));
        }, 10000);

        this.on("terminal_snapshot", handleSnapshot);
        this.on("error", handleError);
        this.sendRequestNow(
          serverId,
          {
            type: "terminal_snapshot",
            request_id: requestId,
            target_id: targetId,
          },
          cleanup,
          reject,
        );
      },
    );
  }

  sendKey(serverId: string, agentId: string, key: string) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<void>((resolve, reject) => {
      const cleanup = () => {
        clearTimeout(timer);
        this.off("key_sent", handleSent);
        this.off("error", handleError);
      };

      const handleSent = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve();
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to send terminal key."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while sending terminal key."));
      }, 5000);

      this.on("key_sent", handleSent);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "send_key",
          request_id: requestId,
          agent_id: agentId,
          key,
        },
        cleanup,
        reject,
      );
    });
  }

  setActiveAgent(serverId: string, agentId: string | null) {
    this.trySendNow(serverId, {
      type: "set_active_agent",
      agent_id: agentId ?? "",
    });
  }

  clearActiveAgentsExcept(
    selected: { serverId: string; agentId: string } | null,
  ) {
    for (const [serverId] of this.connections) {
      if (selected && selected.serverId === serverId) {
        this.setActiveAgent(serverId, selected.agentId);
      } else {
        this.setActiveAgent(serverId, null);
      }
    }
  }

  getStats(serverId: string): Promise<any> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("stats_data", handleStats);
      };

      const handleStats = (payload: any) => {
        if (payload.serverId !== serverId) return;
        if (payload.request_id && payload.request_id !== requestId) return;
        cleanup();
        resolve(payload);
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Stats request timed out."));
      }, 15000);

      this.on("stats_data", handleStats);
      this.sendRequestNow(
        serverId,
        { type: "get_stats", request_id: requestId },
        cleanup,
        reject,
      );
    });
  }

  getSessionResourceSnapshot(
    serverId: string,
    agentId: string,
  ): Promise<SessionResourceSnapshot> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    const targetAgentId = agentId.trim();

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("session_resource_snapshot", handleSnapshot);
        this.off("error", handleError);
      };

      const handleSnapshot = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        const snapshot = normalizeSessionResourceSnapshot(payload);
        cleanup();
        if (!snapshot || snapshot.agent_id !== targetAgentId) {
          reject(new Error("Invalid session resource snapshot."));
          return;
        }
        resolve(snapshot);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(
          new Error(
            typeof payload.message === "string" && payload.message
              ? payload.message
              : "Session resource snapshot failed.",
          ),
        );
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Session resource snapshot timed out."));
      }, 15000);

      this.on("session_resource_snapshot", handleSnapshot);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "get_session_resource_snapshot",
          request_id: requestId,
          agent_id: targetAgentId,
        },
        cleanup,
        reject,
      );
    });
  }

  /**
   * Fire-and-forget terminate: the daemon tears the Session down and the
   * authoritative removal arrives via `agent_session_archived` or the next
   * full `agent_session_list`. An optional request_id correlates the
   * `error` reply for batch termination; success has no reply.
   */
  killAgent(serverId: string, agentId: string, requestId?: string) {
    this.send(serverId, {
      type: "kill_agent",
      agent_id: agentId,
      ...(requestId ? { request_id: requestId } : {}),
    });
  }

  listAgentSessions(serverId: string) {
    this.send(serverId, { type: "list_agent_sessions" });
  }

  requestBrainSnapshot(serverId: string) {
    this.send(serverId, { type: "brain_snapshot" });
  }

  markBrainWorkRead(serverId: string, workId: string) {
    this.send(serverId, {
      type: "brain_work_read",
      id: workId,
    });
  }

  getBrainContext(serverId: string): Promise<BrainContextPayload> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_context", handleContext);
        this.off("error", handleError);
      };

      const handleContext = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve((payload.context || {}) as BrainContextPayload);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to load Brain context."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading Brain context."));
      }, 15000);

      this.on("brain_context", handleContext);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "brain_context",
          request_id: requestId,
        },
        cleanup,
        reject,
      );
    });
  }

  runBrainGC(serverId: string): Promise<BrainHousekeepingPayload> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_gc", handleGC);
        this.off("error", handleError);
      };

      const handleGC = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve((payload.housekeeping || {}) as BrainHousekeepingPayload);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(
          new Error(payload.message || "Failed to run Brain housekeeping."),
        );
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while running Brain housekeeping."));
      }, 15000);

      this.on("brain_gc", handleGC);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "brain_gc",
          request_id: requestId,
        },
        cleanup,
        reject,
      );
    });
  }

  startNewBrainChat(serverId: string) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<any>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_snapshot", handleSnapshot);
        this.off("error", handleError);
      };

      const handleSnapshot = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(payload.brain || {});
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(
          new Error(payload.message || "Failed to start a new Brain chat."),
        );
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while starting a new Brain chat."));
      }, 30000);

      this.on("brain_snapshot", handleSnapshot);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "brain_chat_new",
          request_id: requestId,
        },
        cleanup,
        reject,
      );
    });
  }

  setBrainExecutor(serverId: string, executorId: string) {
    return this.setExecutorByOperation(
      serverId,
      executorId,
      "brain_set_executor",
      "Brain executor",
    );
  }

  setDelegatedExecutor(serverId: string, executorId: string) {
    return this.setExecutorByOperation(
      serverId,
      executorId,
      "set_delegated_executor",
      "Agents executor",
    );
  }

  private setExecutorByOperation(
    serverId: string,
    executorId: string,
    type: "brain_set_executor" | "set_delegated_executor",
    failureLabel: string,
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<any>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_snapshot", handleSnapshot);
        this.off("error", handleError);
      };

      const handleSnapshot = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(payload.brain || {});
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(
          new Error(payload.message || `Failed to switch ${failureLabel}.`),
        );
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error(`Timed out while switching ${failureLabel}.`));
      }, 15000);

      this.on("brain_snapshot", handleSnapshot);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type,
          request_id: requestId,
          executor_id: executorId,
          adapter_id: executorId,
        },
        cleanup,
        reject,
      );
    });
  }

  getBrainWorkspaceTree(
    serverId: string,
    path = "",
  ): Promise<BrainWorkspaceTree> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_workspace_tree", handleTree);
        this.off("error", handleError);
      };

      const handleTree = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(normalizeBrainWorkspaceTree(payload.workspace_tree));
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to load Brain workspace."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading Brain workspace."));
      }, 15000);

      this.on("brain_workspace_tree", handleTree);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "brain_workspace_tree",
          request_id: requestId,
          path,
        },
        cleanup,
        reject,
      );
    });
  }

  getBrainWorkspaceFile(
    serverId: string,
    path: string,
  ): Promise<BrainWorkspaceFile> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_workspace_file", handleFile);
        this.off("error", handleError);
      };

      const handleFile = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(normalizeBrainWorkspaceFile(payload.file));
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(
          new Error(payload.message || "Failed to load Brain workspace file."),
        );
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading Brain workspace file."));
      }, 15000);

      this.on("brain_workspace_file", handleFile);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "brain_workspace_file",
          request_id: requestId,
          path,
        },
        cleanup,
        reject,
      );
    });
  }

  listSessionServices(serverId: string): Promise<SessionServiceSnapshot> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("session_service_list", handleList);
        this.off("error", handleError);
      };

      const handleList = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve({
          generated_at: payload.generated_at,
          interfaces: Array.isArray(payload.interfaces)
            ? payload.interfaces
            : [],
          services: Array.isArray(payload.services)
            ? payload.services.map(normalizeSessionService)
            : [],
        });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(
          new Error(payload.message || "Failed to load session services."),
        );
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading session services."));
      }, 10000);

      this.on("session_service_list", handleList);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "list_session_services",
          request_id: requestId,
        },
        cleanup,
        reject,
      );
    });
  }

  // ── Calendar ─────────────────────────────────────────────────────────────

  listCalendarItems(serverId: string) {
    this.send(serverId, { type: "list_calendar_items" });
  }

  getCalendarItem(serverId: string, id: string) {
    return this.calendarAction(
      serverId,
      "get_calendar_item",
      "calendar_item",
      { id },
      "Failed to load calendar item.",
    );
  }

  createCalendarItem(serverId: string, item: Partial<CalendarItem>) {
    return this.calendarAction(
      serverId,
      "create_calendar_item",
      "calendar_item_created",
      { calendar_item: item },
      "Failed to create calendar item.",
    );
  }

  updateCalendarItem(serverId: string, item: CalendarItem) {
    return this.calendarAction(
      serverId,
      "update_calendar_item",
      "calendar_item_updated",
      { calendar_item: item, revision: item.revision },
      "Failed to update calendar item.",
    );
  }

  cancelCalendarItem(serverId: string, id: string, revision: number) {
    return this.calendarAction(
      serverId,
      "cancel_calendar_item",
      "calendar_item_cancelled",
      { id, revision },
      "Failed to cancel calendar item.",
    );
  }

  runCalendarItem(serverId: string, id: string) {
    return this.calendarAction(
      serverId,
      "run_calendar_item",
      "calendar_item_running",
      { id },
      "Failed to run calendar action.",
    );
  }

  private calendarAction(
    serverId: string,
    type: string,
    responseType: string,
    payload: Record<string, unknown>,
    fallback: string,
  ): Promise<CalendarItem> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        clearTimeout(timer);
        this.off(responseType, onResponse);
        this.off("error", onError);
      };
      const onResponse = (data: any) => {
        if (data.serverId !== serverId || data.request_id !== requestId) return;
        cleanup();
        resolve(data.calendar_item as CalendarItem);
      };
      const onError = (data: any) => {
        if (data.serverId !== serverId || data.request_id !== requestId) return;
        cleanup();
        const error = new Error(data.message || fallback);
        (error as Error & { code?: string }).code = data.code;
        reject(error);
      };
      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Calendar request timed out."));
      }, 15000);
      this.on(responseType, onResponse);
      this.on("error", onError);
      this.sendRequestNow(
        serverId,
        { type, request_id: requestId, ...payload },
        cleanup,
        reject,
      );
    });
  }

  // ── Work items ───────────────────────────────────────────────────────────

  listWorkItems(serverId: string) {
    this.send(serverId, { type: "list_work_items" });
  }

  writeWorkItem(
    serverId: string,
    options: {
      id?: string;
      project: string;
      path?: string;
      body: string;
      frontmatter?: Record<string, unknown>;
      baseMtime?: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<any>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("work_item_written", handleWritten);
        this.off("error", handleError);
      };

      const handleWritten = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(payload.work_item);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        const error = new Error(
          payload.message || "Failed to write work item.",
        );
        (error as Error & { code?: string; current?: any }).code = payload.code;
        (error as Error & { code?: string; current?: any }).current =
          payload.current;
        reject(error);
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while writing work item."));
      }, 10000);

      this.on("work_item_written", handleWritten);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "write_work_item",
          request_id: requestId,
          id: options.id ?? "",
          project: options.project,
          path: options.path ?? "",
          body: options.body,
          frontmatter: options.frontmatter ?? {},
          base_mtime: options.baseMtime ?? "",
        },
        cleanup,
        reject,
      );
    });
  }

  deleteWorkItem(serverId: string, id: string) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<void>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("work_item_deleted_ack", handleDeleted);
        this.off("error", handleError);
      };

      const handleDeleted = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve();
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to delete work item."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while deleting work item."));
      }, 10000);

      this.on("work_item_deleted_ack", handleDeleted);
      this.on("error", handleError);
      this.sendRequestNow(
        serverId,
        {
          type: "delete_work_item",
          request_id: requestId,
          id,
        },
        cleanup,
        reject,
      );
    });
  }

  isConnected(serverId: string) {
    return this.connections.get(serverId)?.isConnected ?? false;
  }

  connectedServerIds() {
    return [...this.connections.keys()].filter((serverId) =>
      this.isConnected(serverId),
    );
  }

  private emit(type: string, serverId: string, payload: any) {
    const meta = this.serverMeta.get(serverId);
    const data = {
      ...payload,
      serverId,
      serverName: meta?.serverName || serverId,
      serverUrl: meta?.serverUrl || "",
      daemonId: meta?.daemonId || "",
      daemonPublicKey: meta?.daemonPublicKey || "",
    };
    const handlers = this.handlers.get(type) || [];
    handlers.forEach((handler) => handler(data));
  }
}

function normalizeSessionService(value: any): SessionService {
  const service = value && typeof value === "object" ? value : {};
  return {
    ...service,
    id: typeof service.id === "string" ? service.id : "",
    agent_id: typeof service.agent_id === "string" ? service.agent_id : "",
    agent_name:
      typeof service.agent_name === "string" ? service.agent_name : "",
    project: typeof service.project === "string" ? service.project : undefined,
    cwd: typeof service.cwd === "string" ? service.cwd : undefined,
    command: typeof service.command === "string" ? service.command : undefined,
    process: typeof service.process === "string" ? service.process : undefined,
    pid: typeof service.pid === "number" ? service.pid : 0,
    port: typeof service.port === "number" ? service.port : 0,
    protocol: typeof service.protocol === "string" ? service.protocol : "tcp",
    binds: Array.isArray(service.binds) ? service.binds : [],
    urls: Array.isArray(service.urls) ? service.urls : [],
    local_only: Boolean(service.local_only),
  };
}

function normalizeBrainWorkspaceTree(raw: any): BrainWorkspaceTree {
  const source = raw && typeof raw === "object" ? raw : {};
  return {
    workspace:
      typeof source.workspace === "string" ? source.workspace : undefined,
    path: typeof source.path === "string" ? source.path : undefined,
    generated_at:
      typeof source.generated_at === "string" ? source.generated_at : undefined,
    entries: Array.isArray(source.entries)
      ? source.entries
          .map(normalizeBrainWorkspaceEntry)
          .filter((entry: BrainWorkspaceEntry) => entry.name)
      : [],
  };
}

function normalizeBrainWorkspaceEntry(raw: any): BrainWorkspaceEntry {
  const source = raw && typeof raw === "object" ? raw : {};
  return {
    name: typeof source.name === "string" ? source.name : "",
    path: typeof source.path === "string" ? source.path : "",
    kind: typeof source.kind === "string" ? source.kind : "file",
    size: typeof source.size === "number" ? source.size : undefined,
    modified_at:
      typeof source.modified_at === "string" ? source.modified_at : undefined,
    children: Array.isArray(source.children)
      ? source.children
          .map(normalizeBrainWorkspaceEntry)
          .filter((entry: BrainWorkspaceEntry) => entry.name)
      : [],
  };
}

function normalizeBrainWorkspaceFile(raw: any): BrainWorkspaceFile {
  const source = raw && typeof raw === "object" ? raw : {};
  return {
    name: typeof source.name === "string" ? source.name : "",
    path: typeof source.path === "string" ? source.path : "",
    kind: typeof source.kind === "string" ? source.kind : "file",
    language: typeof source.language === "string" ? source.language : "text",
    content: typeof source.content === "string" ? source.content : "",
    size: typeof source.size === "number" ? source.size : undefined,
    modified_at:
      typeof source.modified_at === "string" ? source.modified_at : undefined,
  };
}

function toConnectionMeta(server: StoredServer): ConnectionMeta {
  return {
    serverId: server.id,
    serverName: server.name,
    serverUrl: server.url,
    daemonId: server.daemonId,
    daemonPublicKey: server.daemonPublicKey,
    server,
  };
}

function normalizeCodexConversationSnapshotPayload(
  payload: any,
): CodexConversationSnapshotPayload {
  return {
    request_id:
      typeof payload.request_id === "string" ? payload.request_id : undefined,
    agent_id:
      typeof payload.agent_id === "string" ? payload.agent_id : undefined,
    conversation_id:
      typeof payload.conversation_id === "string"
        ? payload.conversation_id
        : undefined,
    revision:
      typeof payload.revision === "number" && Number.isFinite(payload.revision)
        ? payload.revision
        : 0,
    server_generation:
      typeof payload.generation === "string" ? payload.generation : undefined,
    conversation: normalizeCodexConversation(payload.conversation),
  };
}

function normalizeCodexConversationDeltaPayload(
  payload: any,
): CodexConversationDeltaPayload {
  const normalizedDelta = normalizeCodexConversation({
    available: true,
    updated_at: payload.updated_at,
    activity: payload.activity,
    events: payload.upserts,
  });
  return {
    request_id:
      typeof payload.request_id === "string" ? payload.request_id : undefined,
    agent_id:
      typeof payload.agent_id === "string" ? payload.agent_id : undefined,
    conversation_id:
      typeof payload.conversation_id === "string"
        ? payload.conversation_id
        : undefined,
    revision:
      typeof payload.revision === "number" && Number.isFinite(payload.revision)
        ? payload.revision
        : 0,
    base_revision:
      typeof payload.base_revision === "number" &&
      Number.isFinite(payload.base_revision)
        ? payload.base_revision
        : 0,
    server_generation:
      typeof payload.generation === "string" ? payload.generation : undefined,
    available:
      typeof payload.available === "boolean" ? payload.available : undefined,
    reason: typeof payload.reason === "string" ? payload.reason : undefined,
    source: typeof payload.source === "string" ? payload.source : undefined,
    path: typeof payload.path === "string" ? payload.path : undefined,
    session_id:
      typeof payload.session_id === "string" ? payload.session_id : undefined,
    cwd: typeof payload.cwd === "string" ? payload.cwd : undefined,
    updated_at:
      typeof payload.updated_at === "string" ? payload.updated_at : undefined,
    activity: Object.prototype.hasOwnProperty.call(payload, "activity")
      ? (normalizedDelta.activity ?? null)
      : undefined,
    upserts: normalizedDelta.events,
    deletes: Array.isArray(payload.deletes)
      ? payload.deletes.filter(
          (id: unknown): id is string => typeof id === "string",
        )
      : [],
  };
}

function normalizeCodexConversationSyncStatusPayload(
  payload: any,
): CodexConversationSyncStatusPayload {
  return {
    request_id:
      typeof payload.request_id === "string" ? payload.request_id : undefined,
    agent_id:
      typeof payload.agent_id === "string" ? payload.agent_id : undefined,
    conversation_id:
      typeof payload.conversation_id === "string"
        ? payload.conversation_id
        : undefined,
    revision:
      typeof payload.revision === "number" && Number.isFinite(payload.revision)
        ? payload.revision
        : 0,
    server_generation:
      typeof payload.generation === "string" ? payload.generation : undefined,
    state: typeof payload.state === "string" ? payload.state : "syncing",
    reason: typeof payload.reason === "string" ? payload.reason : undefined,
  };
}

function appendAuthorizationQuery(
  serverUrl: string,
  authHeader: string,
): string {
  try {
    const parsed = new URL(serverUrl);
    parsed.searchParams.set("auth", base64URL(authHeader));
    return parsed.toString();
  } catch {
    return serverUrl;
  }
}

function base64URL(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return globalThis
    .btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}

export const wsClient = new MultiServerWebSocketClient();
