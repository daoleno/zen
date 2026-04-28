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

type MessageHandler = (data: any) => void;

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
    this.send(serverId, { type: "send_input", agent_id: agentId, text });
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
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("stats_data", handleStats);
      };

      const handleStats = (payload: any) => {
        if (payload.serverId !== serverId) return;
        cleanup();
        resolve(payload);
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Stats request timed out."));
      }, 15000);

      this.on("stats_data", handleStats);
      this.send(serverId, { type: "get_stats" });
    });
  }

  killAgent(serverId: string, agentId: string) {
    this.send(serverId, { type: "kill_agent", agent_id: agentId });
  }

  listAgentSessions(serverId: string) {
    this.send(serverId, { type: "list_agent_sessions" });
  }

  // ── Issues ───────────────────────────────────────────

  listIssues(serverId: string) {
    this.send(serverId, { type: "list_issues" });
  }

  listExecutors(serverId: string) {
    this.send(serverId, { type: "list_executors" });
  }

  writeIssue(
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
        this.off("issue_written", handleWritten);
        this.off("error", handleError);
      };

      const handleWritten = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(payload.issue);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        const error = new Error(payload.message || "Failed to write issue.");
        (error as Error & { code?: string; current?: any }).code = payload.code;
        (error as Error & { code?: string; current?: any }).current = payload.current;
        reject(error);
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while writing issue."));
      }, 10000);

      this.on("issue_written", handleWritten);
      this.on("error", handleError);
      this.send(serverId, {
        type: "write_issue",
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

  sendIssue(serverId: string, id: string) {
    return this.issueAction(serverId, "send_issue", "issue_dispatched", id, "Failed to send issue.");
  }

  redispatchIssue(serverId: string, id: string) {
    return this.issueAction(
      serverId,
      "redispatch_issue",
      "issue_redispatched",
      id,
      "Failed to redispatch issue.",
    );
  }

  deleteIssue(serverId: string, id: string) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<void>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("issue_deleted_ack", handleDeleted);
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
        reject(new Error(payload.message || "Failed to delete issue."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while deleting issue."));
      }, 10000);

      this.on("issue_deleted_ack", handleDeleted);
      this.on("error", handleError);
      this.send(serverId, {
        type: "delete_issue",
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

  private issueAction(
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
        resolve(payload.issue);
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
        reject(new Error(`Timed out while waiting for ${requestType}.`));
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
    };
    const handlers = this.handlers.get(type) || [];
    handlers.forEach((handler) => handler(data));
  }
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

export const wsClient = new MultiServerWebSocketClient();
