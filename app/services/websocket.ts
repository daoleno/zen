import type { StoredServer } from "./storage";
import { buildAuthorizationHeader } from "./auth";
import { diagnoseConnectionIssue } from "./connectionIssue";

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

  // ── Tasks ────────────────────────────────────────────

  listTasks(serverId: string) {
    this.send(serverId, { type: "list_tasks" });
  }

  listRuns(serverId: string) {
    this.send(serverId, { type: "list_runs" });
  }

  createTask(
    serverId: string,
    options: {
      title: string;
      description?: string;
      attachments?: { name: string; path: string }[];
      priority?: number;
      labels?: string[];
      projectId?: string;
      dueDate?: string;
      skillId?: string;
      cwd?: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<any>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("task_created", handleCreated);
        this.off("error", handleError);
      };

      const handleCreated = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId)
          return;
        cleanup();
        resolve(payload.task);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId)
          return;
        cleanup();
        reject(new Error(payload.message || "Failed to create task."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while creating task."));
      }, 10000);

      this.on("task_created", handleCreated);
      this.on("error", handleError);
      const payload: Record<string, any> = {
        type: "create_task",
        request_id: requestId,
        title: options.title,
        description: options.description ?? "",
        attachments: options.attachments ?? [],
        priority: options.priority ?? 0,
        labels: options.labels ?? [],
        project_id: options.projectId ?? "",
        skill_id: options.skillId ?? "",
        cwd: options.cwd ?? "",
      };
      if (options.dueDate) {
        payload.due_date = options.dueDate;
      }
      this.send(serverId, payload);
    });
  }

  updateTask(
    serverId: string,
    taskId: string,
    updates: {
      title?: string;
      description?: string;
      attachments?: { name: string; path: string }[];
      status?: string;
      priority?: number;
      labels?: string[];
      projectId?: string;
      dueDate?: string | null;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    const payload: Record<string, any> = {
      type: "update_task",
      request_id: requestId,
      task_id: taskId,
    };

    if ("title" in updates) {
      payload.title = updates.title ?? "";
    }
    if ("description" in updates) {
      payload.description = updates.description ?? "";
    }
    if ("attachments" in updates) {
      payload.attachments = updates.attachments ?? [];
    }
    if ("status" in updates) {
      payload.task_status = updates.status ?? "";
    }
    if ("priority" in updates) {
      payload.priority = updates.priority ?? 0;
    }
    if ("labels" in updates) {
      payload.labels = updates.labels ?? [];
    }
    if ("projectId" in updates) {
      payload.project_id = updates.projectId ?? "";
    }
    if ("dueDate" in updates) {
      payload.due_date = updates.dueDate ?? "";
    }

    this.send(serverId, payload);
  }

  deleteTask(serverId: string, taskId: string) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<void>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("task_deleted", handleDeleted);
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

      this.on("task_deleted", handleDeleted);
      this.on("error", handleError);
      this.send(serverId, {
        type: "delete_task",
        request_id: requestId,
        task_id: taskId,
      });
    });
  }

  createRun(
    serverId: string,
    options: {
      taskId: string;
      executionMode?: "spawn_new_session" | "attach_existing_session";
      agentSessionId?: string;
      agentCmd?: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<{ run: any; task?: any }>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("run_created", handleCreated);
        this.off("error", handleError);
      };

      const handleCreated = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId)
          return;
        cleanup();
        resolve({ run: payload.run, task: payload.task });
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId)
          return;
        cleanup();
        reject(new Error(payload.message || "Failed to delegate task."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while creating run."));
      }, 15000);

      this.on("run_created", handleCreated);
      this.on("error", handleError);
      this.send(serverId, {
        type: "create_run",
        request_id: requestId,
        task_id: options.taskId,
        execution_mode: options.executionMode ?? "spawn_new_session",
        agent_session_id: options.agentSessionId ?? "",
        agent_cmd: options.agentCmd ?? "",
      });
    });
  }

  addTaskComment(
    serverId: string,
    options: {
      taskId: string;
      body: string;
      attachments?: { name: string; path: string }[];
      parentCommentId?: string;
      deliveryMode?:
        | "comment"
        | "current_run"
        | "spawn_new_session"
        | "attach_existing_session";
      agentSessionId?: string;
      agentCmd?: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<{ task?: any; comment?: any; run?: any }>(
      (resolve, reject) => {
        const cleanup = () => {
          if (timer) clearTimeout(timer);
          this.off("task_comment_added", handleAdded);
          this.off("error", handleError);
        };

        const handleAdded = (payload: any) => {
          if (
            payload.serverId !== serverId ||
            payload.request_id !== requestId
          ) {
            return;
          }
          cleanup();
          resolve({
            task: payload.task,
            comment: payload.comment,
            run: payload.run,
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
          reject(new Error(payload.message || "Failed to send comment."));
        };

        const timer = setTimeout(() => {
          cleanup();
          reject(new Error("Timed out while sending comment."));
        }, 15000);

        this.on("task_comment_added", handleAdded);
        this.on("error", handleError);
        this.send(serverId, {
          type: "add_task_comment",
          request_id: requestId,
          task_id: options.taskId,
          body: options.body,
          attachments: options.attachments ?? [],
          parent_comment_id: options.parentCommentId ?? "",
          delivery_mode: options.deliveryMode ?? "comment",
          agent_session_id: options.agentSessionId ?? "",
          agent_cmd: options.agentCmd ?? "",
        });
      },
    );
  }

  // ── Skills ───────────────────────────────────────────

  listSkills(serverId: string) {
    this.send(serverId, { type: "list_skills" });
  }

  createSkill(
    serverId: string,
    options: {
      name: string;
      icon?: string;
      agentCmd: string;
      prompt: string;
      cwd?: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;
    this.send(serverId, {
      type: "create_skill",
      request_id: requestId,
      name: options.name,
      icon: options.icon ?? "",
      agent_cmd: options.agentCmd,
      prompt: options.prompt,
      cwd: options.cwd ?? "",
    });
  }

  deleteSkill(serverId: string, skillId: string) {
    this.send(serverId, {
      type: "delete_skill",
      request_id: `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`,
      skill_id: skillId,
    });
  }

  // ── Guidance ─────────────────────────────────────────

  getGuidance(serverId: string) {
    this.send(serverId, { type: "get_guidance" });
  }

  setGuidance(serverId: string, preamble: string, constraints: string[]) {
    this.send(serverId, {
      type: "set_guidance",
      request_id: `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`,
      preamble,
      constraints,
    });
  }

  // ── Projects ─────────────────────────────────────────

  listProjects(serverId: string) {
    this.send(serverId, { type: "list_projects" });
  }

  createProject(
    serverId: string,
    options: {
      name: string;
      icon?: string;
      repoRoot?: string;
      worktreeRoot?: string;
      baseBranch?: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<any>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("project_created", handleCreated);
        this.off("error", handleError);
      };

      const handleCreated = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(payload.project);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to create project."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while creating project."));
      }, 10000);

      this.on("project_created", handleCreated);
      this.on("error", handleError);
      this.send(serverId, {
        type: "create_project",
        request_id: requestId,
        project_name: options.name,
        project_icon: options.icon ?? "",
        repo_root: options.repoRoot ?? "",
        worktree_root: options.worktreeRoot ?? "",
        base_branch: options.baseBranch ?? "",
      });
    });
  }

  updateProject(
    serverId: string,
    options: {
      projectId: string;
      name: string;
      icon?: string;
      repoRoot?: string;
      worktreeRoot?: string;
      baseBranch?: string;
    },
  ) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<any>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("project_updated", handleUpdated);
        this.off("error", handleError);
      };

      const handleUpdated = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        resolve(payload.project);
      };

      const handleError = (payload: any) => {
        if (payload.serverId !== serverId || payload.request_id !== requestId) {
          return;
        }
        cleanup();
        reject(new Error(payload.message || "Failed to update project."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while updating project."));
      }, 10000);

      this.on("project_updated", handleUpdated);
      this.on("error", handleError);
      this.send(serverId, {
        type: "update_project",
        request_id: requestId,
        project_id: options.projectId,
        project_name: options.name,
        project_icon: options.icon ?? "",
        repo_root: options.repoRoot ?? "",
        worktree_root: options.worktreeRoot ?? "",
        base_branch: options.baseBranch ?? "",
      });
    });
  }

  deleteProject(serverId: string, projectId: string) {
    const requestId = `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`;

    return new Promise<void>((resolve, reject) => {
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        this.off("project_deleted", handleDeleted);
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
        reject(new Error(payload.message || "Failed to delete project."));
      };

      const timer = setTimeout(() => {
        cleanup();
        reject(new Error("Timed out while deleting project."));
      }, 10000);

      this.on("project_deleted", handleDeleted);
      this.on("error", handleError);
      this.send(serverId, {
        type: "delete_project",
        request_id: requestId,
        project_id: projectId,
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
