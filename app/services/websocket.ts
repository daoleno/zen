type MessageHandler = (data: any) => void;

class WebSocketClient {
  private ws: WebSocket | null = null;
  private url: string = '';
  private token: string = '';
  private reconnectDelay = 1000;
  private maxReconnectDelay = 30000;
  private pendingQueue: string[] = [];
  private handlers: Map<string, MessageHandler[]> = new Map();
  private shouldReconnect = true;

  connect(url: string, token: string = '') {
    this.url = url;
    this.token = token;
    this.shouldReconnect = true;
    this.reconnectDelay = 1000;
    this.doConnect();
  }

  disconnect() {
    this.shouldReconnect = false;
    this.ws?.close();
    this.ws = null;
  }

  on(type: string, handler: MessageHandler) {
    const existing = this.handlers.get(type) || [];
    this.handlers.set(type, [...existing, handler]);
  }

  off(type: string, handler: MessageHandler) {
    const existing = this.handlers.get(type) || [];
    this.handlers.set(type, existing.filter(h => h !== handler));
  }

  send(msg: object) {
    const data = JSON.stringify(msg);
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(data);
    } else {
      this.pendingQueue.push(data);
    }
  }

  sendInput(agentId: string, text: string) {
    this.send({ type: 'send_input', agent_id: agentId, text });
  }

  openTerminal(targetId: string, backend: string = 'tmux', cols?: number, rows?: number) {
    this.send({ type: 'terminal_open', target_id: targetId, backend, cols, rows });
  }

  sendTerminalInput(sessionId: string, data: string) {
    this.send({ type: 'terminal_input', session_id: sessionId, data });
  }

  resizeTerminal(sessionId: string, cols: number, rows: number) {
    this.send({ type: 'terminal_resize', session_id: sessionId, cols, rows });
  }

  scrollTerminal(sessionId: string, lines: number) {
    this.send({ type: 'terminal_scroll', session_id: sessionId, lines });
  }

  cancelTerminalScroll(sessionId: string) {
    this.send({ type: 'terminal_scroll_cancel', session_id: sessionId });
  }

  closeTerminal(sessionId: string) {
    this.send({ type: 'terminal_close', session_id: sessionId });
  }

  sendAction(agentId: string, action: string) {
    this.send({ type: 'send_action', agent_id: agentId, action });
  }

  killAgent(agentId: string) {
    this.send({ type: 'kill_agent', agent_id: agentId });
  }

  listAgents() {
    this.send({ type: 'list_agents' });
  }

  get isConnected() {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  private doConnect() {
    try {
      this.ws = new WebSocket(this.url);
      if (this.token) {
        // Note: WebSocket API doesn't support custom headers in browser/RN.
        // Token will be sent as first message after connect.
      }

      this.ws.onopen = () => {
        this.reconnectDelay = 1000;
        this.emit('connected', {});

        // Send auth token as first message.
        if (this.token) {
          this.ws?.send(JSON.stringify({ type: 'auth', token: this.token }));
        }

        // Flush pending queue.
        while (this.pendingQueue.length > 0) {
          const msg = this.pendingQueue.shift()!;
          this.ws?.send(msg);
        }
      };

      this.ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data);
          this.emit(data.type, data);
        } catch {}
      };

      this.ws.onclose = () => {
        this.emit('disconnected', {});
        if (this.shouldReconnect) {
          setTimeout(() => this.doConnect(), this.reconnectDelay);
          this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay);
        }
      };

      this.ws.onerror = () => {
        this.ws?.close();
      };
    } catch {
      if (this.shouldReconnect) {
        setTimeout(() => this.doConnect(), this.reconnectDelay);
        this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay);
      }
    }
  }

  private emit(type: string, data: any) {
    const handlers = this.handlers.get(type) || [];
    handlers.forEach(h => h(data));
  }
}

export const wsClient = new WebSocketClient();
