import type { StoredServer } from "./storage";
import { Platform } from "react-native";
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
import type { CalendarItem } from "../store/calendar";

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
  "terminal-required" | "insert-only" | "native" | "unsupported" | string;

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
  recent_messages?: any[];
  generated_at?: string;
}

export interface BrainHousekeepingPayload {
  workspace?: string;
  current_path?: string;
  policy_paths?: string[];
  worklog_path?: string;
  open_delegated_agents?: any[];
  recent_message_count?: number;
  backfilled_workspace?: boolean;
  recommended_next_steps?: string[];
  generated_at?: string;
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
      const serverUrl =
        Platform.OS === "web"
          ? appendAuthorizationQuery(this.meta.serverUrl, authHeader)
          : this.meta.serverUrl;
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

        // Transient close (background suspension, network blip). Keep
        // reconnecting; UI should retain caches and show "connecting".
        this.emit("disconnected", { reason: "transport_closed" });
        if (this.shouldReconnect) {
          this.emit("connecting", {});
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
      }
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
        if (
          payload.agent_session &&
          typeof payload.agent_session === "object"
        ) {
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
      handlers.onSyncStatus(
        normalizeCodexConversationSyncStatusPayload(payload),
      );
    };

    const handleError = (payload: any) => {
      if (payload.serverId !== serverId || payload.request_id !== requestId) {
        return;
      }
      handlers.onError(
        new Error(payload.message || "Codex conversation stream failed."),
      );
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
        reject(
          new Error(payload.message || "Failed to load terminal copy buffer."),
        );
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
        this.send(serverId, {
          type: "terminal_snapshot",
          request_id: requestId,
          target_id: targetId,
        });
      },
    );
  }

  sendKey(serverId: string, agentId: string, key: string) {
    const socket = this.connections.get(serverId);
    if (!socket?.isConnected) {
      throw new Error("Daemon is not connected.");
    }
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
      socket.send({
        type: "send_key",
        request_id: requestId,
        agent_id: agentId,
        key,
      });
    });
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
      this.send(serverId, {
        type: "brain_context",
        request_id: requestId,
      });
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
        reject(new Error(payload.message || "Failed to run Brain housekeeping."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while running Brain housekeeping."));
      }, 15000);

      this.on("brain_gc", handleGC);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_gc",
        request_id: requestId,
      });
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
      this.send(serverId, {
        type: "brain_chat_new",
        request_id: requestId,
      });
    });
  }

  setBrainExecutor(serverId: string, executorId: string) {
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
        reject(new Error(payload.message || "Failed to switch Brain executor."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while switching Brain executor."));
      }, 15000);

      this.on("brain_snapshot", handleSnapshot);
      this.on("error", handleError);
      this.send(serverId, {
        type: "brain_set_executor",
        request_id: requestId,
        executor_id: executorId,
        adapter_id: executorId,
      });
    });
  }

  getBrainWorkspaceTree(serverId: string, path = ""): Promise<BrainWorkspaceTree> {
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
      this.send(serverId, {
        type: "brain_workspace_tree",
        request_id: requestId,
        path,
      });
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
      this.send(serverId, {
        type: "brain_workspace_file",
        request_id: requestId,
        path,
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
      this.send(serverId, {
        type: "list_session_services",
        request_id: requestId,
      });
    });
  }

  // ── Calendar ─────────────────────────────────────────────────────────────

  listCalendarItems(serverId: string) {
    this.send(serverId, { type: "list_calendar_items" });
  }

  getCalendarItem(serverId: string, id: string) {
    return this.calendarAction(serverId, "get_calendar_item", "calendar_item", { id }, "Failed to load calendar item.");
  }

  createCalendarItem(serverId: string, item: Partial<CalendarItem>) {
    return this.calendarAction(serverId, "create_calendar_item", "calendar_item_created", { calendar_item: item }, "Failed to create calendar item.");
  }

  updateCalendarItem(serverId: string, item: CalendarItem) {
    return this.calendarAction(serverId, "update_calendar_item", "calendar_item_updated", { calendar_item: item, revision: item.revision }, "Failed to update calendar item.");
  }

  cancelCalendarItem(serverId: string, id: string, revision: number) {
    return this.calendarAction(serverId, "cancel_calendar_item", "calendar_item_cancelled", { id, revision }, "Failed to cancel calendar item.");
  }

  runCalendarItem(serverId: string, id: string) {
    return this.calendarAction(serverId, "run_calendar_item", "calendar_item_running", { id }, "Failed to run calendar action.");
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
      this.send(serverId, { type, request_id: requestId, ...payload });
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
    return this.workItemAction(
      serverId,
      "start_work_item",
      "work_item_started",
      id,
      "Failed to start work item.",
    );
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
    state: typeof payload.state === "string" ? payload.state : "syncing",
    reason: typeof payload.reason === "string" ? payload.reason : undefined,
  };
}

function appendAuthorizationQuery(serverUrl: string, authHeader: string): string {
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
