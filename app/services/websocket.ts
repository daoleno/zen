import type { StoredServer } from './storage';

type MessageHandler = (data: any) => void;

interface ConnectionMeta {
  serverId: string;
  serverName: string;
  serverUrl: string;
}

class ServerSocket {
  private ws: WebSocket | null = null;
  private token = '';
  private reconnectDelay = 1000;
  private readonly maxReconnectDelay = 30000;
  private readonly pendingQueue: string[] = [];
  private shouldReconnect = true;

  constructor(
    private meta: ConnectionMeta,
    private emit: (type: string, payload: any) => void,
  ) {}

  updateMeta(server: StoredServer) {
    this.meta = toConnectionMeta(server);
  }

  connect(token: string = '') {
    this.token = token;
    this.shouldReconnect = true;
    this.reconnectDelay = 1000;
    this.emit('connecting', {});
    this.doConnect();
  }

  disconnect() {
    this.shouldReconnect = false;
    this.ws?.close();
    this.ws = null;
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

  private doConnect() {
    try {
      this.ws = new WebSocket(this.meta.serverUrl);

      this.ws.onopen = () => {
        this.reconnectDelay = 1000;
        this.emit('connected', {});

        if (this.token) {
          this.ws?.send(JSON.stringify({ type: 'auth', token: this.token }));
        }

        while (this.pendingQueue.length > 0) {
          const msg = this.pendingQueue.shift()!;
          this.ws?.send(msg);
        }
      };

      this.ws.onmessage = event => {
        try {
          const data = JSON.parse(event.data);
          this.emit(data.type, data);
        } catch {
          // Ignore malformed payloads.
        }
      };

      this.ws.onclose = () => {
        this.emit('disconnected', {});
        if (!this.shouldReconnect) {
          return;
        }
        setTimeout(() => this.doConnect(), this.reconnectDelay);
        this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay);
      };

      this.ws.onerror = () => {
        this.ws?.close();
      };
    } catch {
      if (!this.shouldReconnect) {
        return;
      }
      setTimeout(() => this.doConnect(), this.reconnectDelay);
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay);
    }
  }
}

class MultiServerWebSocketClient {
  private readonly handlers = new Map<string, MessageHandler[]>();
  private readonly connections = new Map<string, ServerSocket>();
  private readonly serverMeta = new Map<string, ConnectionMeta>();

  connectServer(server: StoredServer, token: string = '') {
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
    socket.connect(token);
  }

  disconnectServer(serverId: string) {
    this.connections.get(serverId)?.disconnect();
    this.connections.delete(serverId);
    this.serverMeta.delete(serverId);
    this.emit('disconnected', serverId, {});
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
    this.handlers.set(type, existing.filter(current => current !== handler));
  }

  send(serverId: string, msg: object) {
    this.connections.get(serverId)?.send(msg);
  }

  openTerminal(serverId: string, targetId: string, backend: string = 'tmux', cols?: number, rows?: number) {
    this.send(serverId, { type: 'terminal_open', target_id: targetId, backend, cols, rows });
  }

  sendTerminalInput(serverId: string, sessionId: string, data: string) {
    this.send(serverId, { type: 'terminal_input', session_id: sessionId, data });
  }

  resizeTerminal(serverId: string, sessionId: string, cols: number, rows: number) {
    this.send(serverId, { type: 'terminal_resize', session_id: sessionId, cols, rows });
  }

  scrollTerminal(serverId: string, sessionId: string, lines: number) {
    this.send(serverId, { type: 'terminal_scroll', session_id: sessionId, lines });
  }

  cancelTerminalScroll(serverId: string, sessionId: string) {
    this.send(serverId, { type: 'terminal_scroll_cancel', session_id: sessionId });
  }

  closeTerminal(serverId: string, sessionId: string) {
    this.send(serverId, { type: 'terminal_close', session_id: sessionId });
  }

  sendAction(serverId: string, agentId: string, action: string) {
    this.send(serverId, { type: 'send_action', agent_id: agentId, action });
  }

  setActiveAgent(serverId: string, agentId: string | null) {
    this.send(serverId, { type: 'set_active_agent', agent_id: agentId ?? '' });
  }

  clearActiveAgentsExcept(selected: { serverId: string; agentId: string } | null) {
    for (const [serverId] of this.connections) {
      if (selected && selected.serverId === serverId) {
        this.setActiveAgent(serverId, selected.agentId);
      } else {
        this.setActiveAgent(serverId, null);
      }
    }
  }

  killAgent(serverId: string, agentId: string) {
    this.send(serverId, { type: 'kill_agent', agent_id: agentId });
  }

  listAgents(serverId: string) {
    this.send(serverId, { type: 'list_agents' });
  }

  isConnected(serverId: string) {
    return this.connections.get(serverId)?.isConnected ?? false;
  }

  connectedServerIds() {
    return [...this.connections.keys()].filter(serverId => this.isConnected(serverId));
  }

  private emit(type: string, serverId: string, payload: any) {
    const meta = this.serverMeta.get(serverId);
    const data = {
      ...payload,
      serverId,
      serverName: meta?.serverName || serverId,
      serverUrl: meta?.serverUrl || '',
    };
    const handlers = this.handlers.get(type) || [];
    handlers.forEach(handler => handler(data));
  }
}

function toConnectionMeta(server: StoredServer): ConnectionMeta {
  return {
    serverId: server.id,
    serverName: server.name,
    serverUrl: server.url,
  };
}

export const wsClient = new MultiServerWebSocketClient();
