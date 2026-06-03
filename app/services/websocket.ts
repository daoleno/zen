import type { StoredServer } from "./storage";
import { buildAuthorizationHeader } from "./auth";
import { diagnoseConnectionIssue } from "./connectionIssue";
import type {
  GitDiffFileContentPayload,
  GitDiffPatchPayload,
  GitRepoBrowserPayload,
  GitRepoFileContentPayload,
  GitDiffStatusSnapshot,
} from "./gitDiff";
import type { SessionService, SessionServiceSnapshot } from "./sessionServices";
import {
  normalizeCodexConversation,
  type CodexConversation,
} from "./codexConversation";

type MessageHandler = (data: any) => void;

function normalizeCodexSlashCommandInput(value: any): CodexSlashCommandInput {
  const input = value && typeof value === "object" ? value : {};
  return {
    kind:
      typeof input.kind === "string" && input.kind
        ? input.kind
        : "",
    placeholder:
      typeof input.placeholder === "string" ? input.placeholder : undefined,
    picker: typeof input.picker === "string" ? input.picker : undefined,
    required:
      typeof input.required === "boolean" ? input.required : undefined,
  };
}

function normalizeCodexSlashCommandOutput(value: any): CodexSlashCommandOutput {
  const output = value && typeof value === "object" ? value : {};
  return {
    kind:
      typeof output.kind === "string" && output.kind
        ? output.kind
        : "",
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

export interface CodexConversationSnapshotPayload {
  request_id?: string;
  agent_id?: string;
  conversation_id?: string;
  revision: number;
  conversation: CodexConversation;
}

export interface CodexConversationDeltaPayload {
  request_id?: string;
  agent_id?: string;
  conversation_id?: string;
  revision: number;
  available?: boolean;
  reason?: string;
  source?: string;
  path?: string;
  session_id?: string;
  cwd?: string;
  updated_at?: string;
  active?: boolean;
  upserts: CodexConversation["events"];
  deletes: string[];
}

export interface CodexConversationSyncStatusPayload {
  request_id?: string;
  agent_id?: string;
  conversation_id?: string;
  revision: number;
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
}

export interface CodexConversationSubscriptionHandlers {
  onSnapshot(payload: CodexConversationSnapshotPayload): void;
  onDelta(payload: CodexConversationDeltaPayload): void;
  onSyncStatus(payload: CodexConversationSyncStatusPayload): void;
  onError(error: Error): void;
}

export interface BrainNativeThread {
  id: string;
  native_id?: string;
  provider?: string;
  session_id?: string;
  forked_from_id?: string;
  title?: string;
  preview?: string;
  snippet?: string;
  status?: string;
  cwd?: string;
  path?: string;
  source?: string;
  model_provider?: string;
  ephemeral?: boolean;
  archived?: boolean;
  pinned?: boolean;
  review_state?: string;
  created_at?: string;
  updated_at?: string;
}

export interface BrainNativeThreadGoal {
  thread_id: string;
  objective?: string;
  status?: string;
  token_budget?: number;
  tokens_used?: number;
  time_used_seconds?: number;
  created_at?: string;
  updated_at?: string;
}

export interface BrainThreadsSnapshot {
  adapter?: any;
  threads: BrainNativeThread[];
  next_cursor?: string;
  backwards_cursor?: string;
}

export interface BrainThreadListOptions {
  adapterId?: string;
  limit?: number;
  cursor?: string;
  cwd?: string;
  searchTerm?: string;
  archived?: boolean;
}

export interface BrainThreadArchiveOptions {
  adapterId?: string;
  archived?: boolean;
}

export interface BrainThreadPinOptions {
  pinned?: boolean;
}

export interface BrainThreadReviewStateOptions {
  reviewState?: string;
}

export interface BrainThreadReadOptions {
  adapterId?: string;
  includeTurns?: boolean;
}

export interface BrainThreadForkOptions {
  adapterId?: string;
  cwd?: string;
  model?: string;
  modelProvider?: string;
  developerInstructions?: string;
  baseInstructions?: string;
  ephemeral?: boolean;
  excludeTurns?: boolean;
}

export interface BrainThreadResumeOptions {
  adapterId?: string;
  cwd?: string;
  model?: string;
  modelProvider?: string;
  developerInstructions?: string;
  baseInstructions?: string;
}

export interface BrainThreadResumeResult {
  brain?: any;
  thread: BrainNativeThread;
}

export interface BrainThreadGoalUpdate {
  adapterId?: string;
  objective: string;
  status?: string;
  tokenBudget?: number;
}

interface ConnectionMeta {
  serverId: string;
  serverName: string;
  serverUrl: string;
  daemonId: string;
  daemonPublicKey: string;
}

class ServerSocket {
  private ws: WebSocket | null = null;
  private reconnectDelay = 1000;
  private readonly maxReconnectDelay = 30000;
  private readonly pendingQueue: string[] = [];
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

  send(msg: object) {
    const data = JSON.stringify(msg);
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(data);
      return;
    }
    this.pendingQueue.push(data);
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

  private async reportConnectionIssue(attemptId: number) {
    const issue = await diagnoseConnectionIssue({
      serverUrl: this.meta.serverUrl,
      daemonId: this.meta.daemonId,
      daemonPublicKey: this.meta.daemonPublicKey,
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
      const WebSocketCtor = WebSocket as any;
      const ws = new WebSocketCtor(this.meta.serverUrl, [], wsOptions);
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

        while (this.pendingQueue.length > 0) {
          const msg = this.pendingQueue.shift()!;
          this.ws?.send(msg);
        }
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

        this.emit("disconnected", {});
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
      this.emit("disconnected", {});
      void this.reportConnectionIssue(attemptId);
      this.scheduleReconnect();
    }
  }
}

class MultiServerWebSocketClient {
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
    this.emit("disconnected", serverId, {});
    this.emit("connection_issue", serverId, { issue: null });
  }

  disconnectAll() {
    for (const serverId of this.connections.keys()) {
      this.disconnectServer(serverId);
    }
  }

  on(type: string, handler: MessageHandler) {
    const existing = this.handlers.get(type) || [];
    this.handlers.set(type, [...existing, handler]);
  }

  off(type: string, handler: MessageHandler) {
    const existing = this.handlers.get(type) || [];
    this.handlers.set(
      type,
      existing.filter((current) => current !== handler),
    );
  }

  send(serverId: string, msg: object) {
    this.connections.get(serverId)?.send(msg);
  }

  createSession(
    serverId: string,
    options?: {
      targetId?: string;
      cwd?: string;
      command?: string;
      name?: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<string>((resolve, reject) => {
      const cleanup = () => {
        if (timer) {
          clearTimeout(timer);
        }
        this.off("session_created", handleCreated);
        this.off("error", handleError);
      };

      const handleCreated = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId)
          return;
        cleanup();
        if (payload.agent_session && typeof payload.agent_session === "object") {
          this.emit("agent_session_created", serverId, {
            agent_session: payload.agent_session,
          });
        }
        if (typeof payload.agent_id === "string" && payload.agent_id) {
          resolve(payload.agent_id);
          return;
        }
        reject(new Error("Daemon returned an invalid session id."));
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId)
          return;
        cleanup();
        reject(new Error(payload.message || "Failed to create terminal."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while creating a new terminal."));
      }, 10000);

      this.on("session_created", handleCreated);
      this.on("error", handleError);
      this.send(serverId, {
        type: "create_session",
        request_id: requestId,
        target_id: options?.targetId,
        cwd: options?.cwd,
        command: options?.command,
        name: options?.name,
      });
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
      this.send(serverId, {
        type: "list_dir",
        request_id: requestId,
        cwd: path ?? "",
      });
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
      this.send(serverId, {
        type: "git_diff_status",
        request_id: requestId,
        target_id: options?.targetId,
        cwd: options?.cwd,
      });
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
      this.send(serverId, {
        type: "git_diff_patch",
        request_id: requestId,
        target_id: options.targetId,
        cwd: options.cwd,
        path: options.path,
      });
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
        reject(new Error(payload.message || "Failed to load git diff file content."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading git diff file content."));
      }, 10000);

      this.on("git_diff_file_content", handleContent);
      this.on("error", handleError);
      this.send(serverId, {
        type: "git_diff_file_content",
        request_id: requestId,
        target_id: options.targetId,
        cwd: options.cwd,
        path: options.path,
      });
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
        reject(new Error(payload.message || "Failed to load repository files."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading repository files."));
      }, 10000);

      this.on("git_repo_entries", handleEntries);
      this.on("error", handleError);
      this.send(serverId, {
        type: "git_repo_entries",
        request_id: requestId,
        target_id: options?.targetId,
        cwd: options?.cwd,
        path: options?.path,
      });
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
      this.send(serverId, {
        type: "git_repo_file_content",
        request_id: requestId,
        target_id: options.targetId,
        cwd: options.cwd,
        path: options.path,
      });
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
      handlers.onSyncStatus(normalizeCodexConversationSyncStatusPayload(payload));
    };

    const handleError = (payload: any) => {
      if (payload.serverId !== serverId || payload.request_id !== requestId) {
        return;
      }
      handlers.onError(new Error(payload.message || "Codex conversation stream failed."));
    };

    this.on("codex_conversation_snapshot", handleSnapshot);
    this.on("codex_conversation_delta", handleDelta);
    this.on("codex_conversation_sync_status", handleSyncStatus);
    this.on("error", handleError);
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
    });

    return () => {
      this.off("codex_conversation_snapshot", handleSnapshot);
      this.off("codex_conversation_delta", handleDelta);
      this.off("codex_conversation_sync_status", handleSyncStatus);
      this.off("error", handleError);
      this.send(serverId, {
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
                source: typeof command.source === "string" ? command.source : undefined,
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
            typeof payload.generated_at === "string" ? payload.generated_at : undefined,
          source: typeof payload.source === "string" ? payload.source : undefined,
          version: typeof payload.version === "string" ? payload.version : undefined,
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
      this.send(serverId, {
        type: "codex_slash_commands",
        request_id: requestId,
      });
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
      this.send(serverId, {
        type: "codex_skills",
        request_id: requestId,
        cwd: options.cwd,
      });
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
        reject(new Error(payload.message || "Failed to load Codex terminal output."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading Codex terminal output."));
      }, 10000);

      this.on("codex_terminal_snapshot", handleSnapshot);
      this.on("error", handleError);
      this.send(serverId, {
        type: "codex_terminal_snapshot",
        request_id: requestId,
        target_id: targetId,
      });
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
            typeof payload.content_type === "string" ? payload.content_type : "image/*",
          data_url: typeof payload.data_url === "string" ? payload.data_url : "",
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
      this.send(serverId, {
        type: "codex_asset",
        request_id: requestId,
        path: options.path,
        cwd: options.cwd,
      });
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

  cancelTerminalScroll(serverId: string, sessionId: string) {
    this.send(serverId, {
      type: "terminal_scroll_cancel",
      session_id: sessionId,
    });
  }

  requestTerminalCopyBuffer(serverId: string, sessionId: string) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<string>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("terminal_copy_buffer", handleBuffer);
        this.off("error", handleError);
      };

      const handleBuffer = (payload: any) => {
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
        reject(new Error(payload.message || "Failed to load terminal copy buffer."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading terminal copy buffer."));
      }, 10000);

      this.on("terminal_copy_buffer", handleBuffer);
      this.on("error", handleError);
      this.send(serverId, {
        type: "terminal_copy_buffer",
        request_id: requestId,
        session_id: sessionId,
      });
    });
  }

  closeTerminal(serverId: string, sessionId: string) {
    this.send(serverId, { type: "terminal_close", session_id: sessionId });
  }

  sendAction(serverId: string, agentId: string, action: string) {
    this.send(serverId, { type: "send_action", agent_id: agentId, action });
  }

  sendInput(serverId: string, agentId: string, text: string) {
    const socket = this.connections.get(serverId);
    if (!socket?.isConnected) {
      throw new Error("Daemon is not connected.");
    }
    socket.send({ type: "send_input", agent_id: agentId, text });
  }

  getTerminalSnapshot(serverId: string, targetId: string) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<{ text: string; target_id?: string }>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("terminal_snapshot", handleSnapshot);
        this.off("error", handleError);
      };

      const handleSnapshot = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve({
          text: typeof payload.text === "string" ? payload.text : "",
          target_id:
            typeof payload.target_id === "string" ? payload.target_id : undefined,
        });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to load terminal snapshot."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading terminal snapshot."));
      }, 10000);

      this.on("terminal_snapshot", handleSnapshot);
      this.on("error", handleError);
      this.send(serverId, {
        type: "terminal_snapshot",
        request_id: requestId,
        target_id: targetId,
      });
    });
  }

  sendKey(serverId: string, agentId: string, key: string) {
    const socket = this.connections.get(serverId);
    if (!socket?.isConnected) {
      throw new Error("Daemon is not connected.");
    }
    socket.send({ type: "send_key", agent_id: agentId, key });
  }

  setActiveAgent(serverId: string, agentId: string | null) {
    this.send(serverId, { type: "set_active_agent", agent_id: agentId ?? "" });
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
      this.send(serverId, { type: "get_stats", request_id: requestId });
    });
  }

  killAgent(serverId: string, agentId: string) {
    this.send(serverId, { type: "kill_agent", agent_id: agentId });
  }

  listAgentSessions(serverId: string) {
    this.send(serverId, { type: "list_agent_sessions" });
  }

  requestBrainSnapshot(serverId: string) {
    this.send(serverId, { type: "brain_snapshot" });
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
        reject(new Error(payload.message || "Failed to start a new Brain chat."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while starting a new Brain chat."));
      }, 30000);

      this.on("brain_snapshot", handleSnapshot);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_chat_new",
        request_id: requestId,
      });
    });
  }

  setBrainAdapter(serverId: string, adapterId: string) {
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
        reject(new Error(payload.message || "Failed to switch Brain adapter."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while switching Brain adapter."));
      }, 15000);

      this.on("brain_snapshot", handleSnapshot);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_set_adapter",
        request_id: requestId,
        adapter_id: adapterId,
      });
    });
  }

  listBrainThreads(
    serverId: string,
    options: BrainThreadListOptions = {},
  ): Promise<BrainThreadsSnapshot> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_threads", handleThreads);
        this.off("error", handleError);
      };

      const handleThreads = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve({
          adapter: payload.adapter,
          threads: normalizeBrainNativeThreads(payload.threads),
          next_cursor:
            typeof payload.next_cursor === "string" ? payload.next_cursor : undefined,
          backwards_cursor:
            typeof payload.backwards_cursor === "string" ? payload.backwards_cursor : undefined,
        });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to load Brain threads."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading Brain threads."));
      }, 18000);

      this.on("brain_threads", handleThreads);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_threads",
        request_id: requestId,
        adapter_id: options.adapterId ?? "",
        limit: options.limit ?? 24,
        cursor: options.cursor ?? "",
        cwd: options.cwd ?? "",
        search_term: options.searchTerm ?? "",
        archived: options.archived ?? false,
      });
    });
  }

  archiveBrainThread(
    serverId: string,
    threadId: string,
    options: BrainThreadArchiveOptions = {},
  ): Promise<BrainNativeThread> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_thread_archived", handleArchived);
        this.off("error", handleError);
      };

      const handleArchived = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(normalizeBrainNativeThreads([payload.thread])[0] ?? { id: threadId });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to update Brain thread."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while updating Brain thread."));
      }, 18000);

      this.on("brain_thread_archived", handleArchived);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_thread_archive",
        request_id: requestId,
        adapter_id: options.adapterId ?? "",
        thread_id: threadId,
        archived: options.archived ?? true,
      });
    });
  }

  pinBrainThread(
    serverId: string,
    threadId: string,
    options: BrainThreadPinOptions = {},
  ): Promise<BrainNativeThread> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_thread_pinned", handlePinned);
        this.off("error", handleError);
      };

      const handlePinned = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        const normalized =
          normalizeBrainNativeThreads([payload.thread])[0] ?? { id: threadId };
        resolve({
          ...normalized,
          id:
            typeof payload.thread_id === "string" && payload.thread_id
              ? payload.thread_id
              : normalized.id,
          pinned:
            typeof payload.pinned === "boolean"
              ? payload.pinned
              : normalized.pinned,
        });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to pin Brain thread."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while pinning Brain thread."));
      }, 15000);

      this.on("brain_thread_pinned", handlePinned);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_thread_pin",
        request_id: requestId,
        thread_id: threadId,
        pinned: options.pinned ?? true,
      });
    });
  }

  setBrainThreadReviewState(
    serverId: string,
    threadId: string,
    options: BrainThreadReviewStateOptions = {},
  ): Promise<BrainNativeThread> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_thread_review_state", handleReviewState);
        this.off("error", handleError);
      };

      const handleReviewState = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        const normalized =
          normalizeBrainNativeThreads([payload.thread])[0] ?? { id: threadId };
        resolve({
          ...normalized,
          id:
            typeof payload.thread_id === "string" && payload.thread_id
              ? payload.thread_id
              : normalized.id,
          review_state:
            typeof payload.review_state === "string"
              ? payload.review_state
              : normalized.review_state,
        });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to update Brain thread review state."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while updating Brain thread review state."));
      }, 15000);

      this.on("brain_thread_review_state", handleReviewState);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_thread_review_state",
        request_id: requestId,
        thread_id: threadId,
        review_state: options.reviewState ?? "",
      });
    });
  }

  readBrainThread(
    serverId: string,
    threadId: string,
    options: BrainThreadReadOptions = {},
  ): Promise<BrainNativeThread> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_thread_read", handleRead);
        this.off("error", handleError);
      };

      const handleRead = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(normalizeBrainNativeThreads([payload.thread])[0] ?? { id: threadId });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to read Brain thread."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while reading Brain thread."));
      }, 18000);

      this.on("brain_thread_read", handleRead);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_thread_read",
        request_id: requestId,
        adapter_id: options.adapterId ?? "",
        thread_id: threadId,
        include_turns: options.includeTurns ?? false,
      });
    });
  }

  forkBrainThread(
    serverId: string,
    threadId: string,
    options: BrainThreadForkOptions = {},
  ): Promise<BrainNativeThread> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_thread_forked", handleForked);
        this.off("error", handleError);
      };

      const handleForked = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(normalizeBrainNativeThreads([payload.thread])[0] ?? { id: threadId });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to fork Brain thread."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while forking Brain thread."));
      }, 18000);

      this.on("brain_thread_forked", handleForked);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_thread_fork",
        request_id: requestId,
        adapter_id: options.adapterId ?? "",
        thread_id: threadId,
        cwd: options.cwd ?? "",
        model: options.model ?? "",
        model_provider: options.modelProvider ?? "",
        developer_instructions: options.developerInstructions ?? "",
        base_instructions: options.baseInstructions ?? "",
        ephemeral: options.ephemeral ?? false,
        exclude_turns: options.excludeTurns ?? false,
      });
    });
  }

  resumeBrainThread(
    serverId: string,
    threadId: string,
    options: BrainThreadResumeOptions = {},
  ): Promise<BrainThreadResumeResult> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_thread_resumed", handleResumed);
        this.off("error", handleError);
      };

      const handleResumed = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve({
          brain: payload.brain,
          thread: normalizeBrainNativeThreads([payload.thread])[0] ?? { id: threadId },
        });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to resume Brain thread."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while resuming Brain thread."));
      }, 18000);

      this.on("brain_thread_resumed", handleResumed);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_thread_resume",
        request_id: requestId,
        adapter_id: options.adapterId ?? "",
        thread_id: threadId,
        cwd: options.cwd ?? "",
        model: options.model ?? "",
        model_provider: options.modelProvider ?? "",
        developer_instructions: options.developerInstructions ?? "",
        base_instructions: options.baseInstructions ?? "",
      });
    });
  }

  getBrainThreadGoal(
    serverId: string,
    threadId: string,
    options: BrainThreadReadOptions = {},
  ): Promise<BrainNativeThreadGoal | null> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_thread_goal", handleGoal);
        this.off("error", handleError);
      };

      const handleGoal = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(normalizeBrainNativeThreadGoal(payload.goal));
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to load Brain thread goal."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading Brain thread goal."));
      }, 18000);

      this.on("brain_thread_goal", handleGoal);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_thread_goal_get",
        request_id: requestId,
        adapter_id: options.adapterId ?? "",
        thread_id: threadId,
      });
    });
  }

  setBrainThreadGoal(
    serverId: string,
    threadId: string,
    update: BrainThreadGoalUpdate,
  ): Promise<BrainNativeThreadGoal> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_thread_goal", handleGoal);
        this.off("error", handleError);
      };

      const handleGoal = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        const goal = normalizeBrainNativeThreadGoal(payload.goal);
        if (!goal) {
          reject(new Error("Brain thread goal was not returned."));
          return;
        }
        resolve(goal);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to update Brain thread goal."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while updating Brain thread goal."));
      }, 18000);

      this.on("brain_thread_goal", handleGoal);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_thread_goal_set",
        request_id: requestId,
        adapter_id: update.adapterId ?? "",
        thread_id: threadId,
        objective: update.objective,
        status: update.status ?? "",
        token_budget: update.tokenBudget ?? 0,
      });
    });
  }

  clearBrainThreadGoal(
    serverId: string,
    threadId: string,
    options: BrainThreadReadOptions = {},
  ): Promise<boolean> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("brain_thread_goal_cleared", handleCleared);
        this.off("error", handleError);
      };

      const handleCleared = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(Boolean(payload.cleared));
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to clear Brain thread goal."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while clearing Brain thread goal."));
      }, 18000);

      this.on("brain_thread_goal_cleared", handleCleared);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_thread_goal_clear",
        request_id: requestId,
        adapter_id: options.adapterId ?? "",
        thread_id: threadId,
      });
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
          interfaces: Array.isArray(payload.interfaces) ? payload.interfaces : [],
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
        reject(new Error(payload.message || "Failed to load session services."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while loading session services."));
      }, 10000);

      this.on("session_service_list", handleList);
      this.on("error", handleError);
      this.send(serverId, {
        type: "list_session_services",
        request_id: requestId,
      });
    });
  }

  // ── Work items ───────────────────────────────────────────────────────────

  listWorkItems(serverId: string) {
    this.send(serverId, { type: "list_work_items" });
  }

  listExecutors(serverId: string) {
    this.send(serverId, { type: "list_executors" });
  }

  setWorkDigestProvider(serverId: string, provider: string): Promise<string> {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("work_digest_provider", handleProvider);
        this.off("error", handleError);
      };

      const handleProvider = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        const selected =
          typeof payload.work_digest_provider === "string"
            ? payload.work_digest_provider
            : provider;
        resolve(selected);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to change work analyzer."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while changing work analyzer."));
      }, 15000);

      this.on("work_digest_provider", handleProvider);
      this.on("error", handleError);
      this.send(serverId, {
        type: "set_work_digest_provider",
        request_id: requestId,
        name: provider,
      });
    });
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
        const error = new Error(payload.message || "Failed to write work item.");
        (error as Error & { code?: string; current?: any }).code = payload.code;
        (error as Error & { code?: string; current?: any }).current = payload.current;
        reject(error);
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while writing work item."));
      }, 10000);

      this.on("work_item_written", handleWritten);
      this.on("error", handleError);
      this.send(serverId, {
        type: "write_work_item",
        request_id: requestId,
        id: options.id ?? "",
        project: options.project,
        path: options.path ?? "",
        body: options.body,
        frontmatter: options.frontmatter ?? {},
        base_mtime: options.baseMtime ?? "",
      });
    });
  }

  startWorkItem(serverId: string, id: string) {
    return this.workItemAction(serverId, "start_work_item", "work_item_started", id, "Failed to start work item.");
  }

  rerunWorkItem(serverId: string, id: string) {
    return this.workItemAction(
      serverId,
      "rerun_work_item",
      "work_item_rerun",
      id,
      "Failed to run work item again.",
    );
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
      this.send(serverId, {
        type: "delete_work_item",
        request_id: requestId,
        id,
      });
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

  private workItemAction(
    serverId: string,
    requestType: string,
    responseType: string,
    id: string,
    fallbackMessage: string,
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<any>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off(responseType, handleResponse);
        this.off("error", handleError);
      };

      const handleResponse = (payload: any) => {
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
        reject(new Error(payload.message || fallbackMessage));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while waiting for work item action."));
      }, 15000);

      this.on(responseType, handleResponse);
      this.on("error", handleError);
      this.send(serverId, {
        type: requestType,
        request_id: requestId,
        id,
      });
    });
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
    agent_name: typeof service.agent_name === "string" ? service.agent_name : "",
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

function normalizeBrainNativeThreads(value: any): BrainNativeThread[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value
    .map((raw): BrainNativeThread => {
      const item = raw && typeof raw === "object" ? raw : {};
      return {
        id: typeof item.id === "string" ? item.id : "",
        native_id:
          typeof item.native_id === "string" ? item.native_id : undefined,
        provider: typeof item.provider === "string" ? item.provider : undefined,
        session_id:
          typeof item.session_id === "string" ? item.session_id : undefined,
        forked_from_id:
          typeof item.forked_from_id === "string" ? item.forked_from_id : undefined,
        title: typeof item.title === "string" ? item.title : undefined,
        preview: typeof item.preview === "string" ? item.preview : undefined,
        snippet: typeof item.snippet === "string" ? item.snippet : undefined,
        status: typeof item.status === "string" ? item.status : undefined,
        cwd: typeof item.cwd === "string" ? item.cwd : undefined,
        path: typeof item.path === "string" ? item.path : undefined,
        source: typeof item.source === "string" ? item.source : undefined,
        model_provider:
          typeof item.model_provider === "string" ? item.model_provider : undefined,
        ephemeral:
          typeof item.ephemeral === "boolean" ? item.ephemeral : undefined,
        archived:
          typeof item.archived === "boolean" ? item.archived : undefined,
        pinned:
          typeof item.pinned === "boolean" ? item.pinned : undefined,
        review_state:
          typeof item.review_state === "string" ? item.review_state : undefined,
        created_at:
          typeof item.created_at === "string" ? item.created_at : undefined,
        updated_at:
          typeof item.updated_at === "string" ? item.updated_at : undefined,
      };
    })
    .filter((thread) => thread.id);
}

function normalizeBrainNativeThreadGoal(value: any): BrainNativeThreadGoal | null {
  if (!value || typeof value !== "object") {
    return null;
  }
  const goal: BrainNativeThreadGoal = {
    thread_id: typeof value.thread_id === "string" ? value.thread_id : "",
    objective:
      typeof value.objective === "string" ? value.objective : undefined,
    status: typeof value.status === "string" ? value.status : undefined,
    token_budget:
      typeof value.token_budget === "number" ? value.token_budget : undefined,
    tokens_used:
      typeof value.tokens_used === "number" ? value.tokens_used : undefined,
    time_used_seconds:
      typeof value.time_used_seconds === "number"
        ? value.time_used_seconds
        : undefined,
    created_at:
      typeof value.created_at === "string" ? value.created_at : undefined,
    updated_at:
      typeof value.updated_at === "string" ? value.updated_at : undefined,
  };
  if (!goal.thread_id) {
    return null;
  }
  return goal;
}

function toConnectionMeta(server: StoredServer): ConnectionMeta {
  return {
    serverId: server.id,
    serverName: server.name,
    serverUrl: server.url,
    daemonId: server.daemonId,
    daemonPublicKey: server.daemonPublicKey,
  };
}

function normalizeCodexConversationSnapshotPayload(
  payload: any,
): CodexConversationSnapshotPayload {
  return {
    request_id: typeof payload.request_id === "string" ? payload.request_id : undefined,
    agent_id: typeof payload.agent_id === "string" ? payload.agent_id : undefined,
    conversation_id:
      typeof payload.conversation_id === "string" ? payload.conversation_id : undefined,
    revision:
      typeof payload.revision === "number" && Number.isFinite(payload.revision)
        ? payload.revision
        : 0,
    conversation: normalizeCodexConversation(payload.conversation),
  };
}

function normalizeCodexConversationDeltaPayload(
  payload: any,
): CodexConversationDeltaPayload {
  const normalizedEvents = normalizeCodexConversation({
    available: true,
    events: payload.upserts,
  }).events;
  return {
    request_id: typeof payload.request_id === "string" ? payload.request_id : undefined,
    agent_id: typeof payload.agent_id === "string" ? payload.agent_id : undefined,
    conversation_id:
      typeof payload.conversation_id === "string" ? payload.conversation_id : undefined,
    revision:
      typeof payload.revision === "number" && Number.isFinite(payload.revision)
        ? payload.revision
        : 0,
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
    active: typeof payload.active === "boolean" ? payload.active : undefined,
    upserts: normalizedEvents,
    deletes: Array.isArray(payload.deletes)
      ? payload.deletes.filter((id: unknown): id is string => typeof id === "string")
      : [],
  };
}

function normalizeCodexConversationSyncStatusPayload(
  payload: any,
): CodexConversationSyncStatusPayload {
  return {
    request_id: typeof payload.request_id === "string" ? payload.request_id : undefined,
    agent_id: typeof payload.agent_id === "string" ? payload.agent_id : undefined,
    conversation_id:
      typeof payload.conversation_id === "string" ? payload.conversation_id : undefined,
    revision:
      typeof payload.revision === "number" && Number.isFinite(payload.revision)
        ? payload.revision
        : 0,
    state: typeof payload.state === "string" ? payload.state : "syncing",
    reason: typeof payload.reason === "string" ? payload.reason : undefined,
  };
}

export const wsClient = new MultiServerWebSocketClient();
