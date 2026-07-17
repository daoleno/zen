/**
 * A normal terminal FocusIn report is enough for tmux's native `latest`
 * policy to treat this attached client as the most recently active one.
 * Keep this stateless: session correlation and live-only delivery stay owned by
 * the existing input boundary, and non-tmux backends receive no synthetic data.
 */
export function notifyTmuxClientFocus(
  backend: string,
  sendInput: (data: string) => boolean,
): boolean {
  if (backend !== 'tmux') {
    return false;
  }
  return sendInput('\u001b[I');
}

/**
 * Minimal correlation gate for one live terminal attachment.
 *
 * There is intentionally no presentation acknowledgement or replay queue:
 * once the daemon accepts the current open, a blank or non-blank live model is
 * immediately interactive; all non-current events are dropped.
 */
export class TerminalSessionCorrelation {
  private connected = true;
  private opening = false;
  private currentSessionId: string | null = null;

  get sessionId(): string | null {
    return this.currentSessionId;
  }

  get canInteract(): boolean {
    return this.connected && this.currentSessionId !== null;
  }

  beginOpen(): boolean {
    if (!this.connected || this.opening || this.currentSessionId) {
      return false;
    }
    this.opening = true;
    return true;
  }

  abortOpen(): boolean {
    if (!this.opening) {
      return false;
    }
    this.opening = false;
    return true;
  }

  acceptOpened(sessionId: string): boolean {
    const nextSessionId = sessionId.trim();
    if (!this.connected || !this.opening || !nextSessionId) {
      return false;
    }
    this.opening = false;
    this.currentSessionId = nextSessionId;
    return true;
  }

  acceptEvent(sessionId: string): boolean {
    return this.canInteract && sessionId === this.currentSessionId;
  }

  close(sessionId?: string): boolean {
    if (!this.currentSessionId) {
      this.opening = false;
      return false;
    }
    if (sessionId && sessionId !== this.currentSessionId) {
      return false;
    }
    this.currentSessionId = null;
    this.opening = false;
    return true;
  }

  disconnect(): void {
    this.connected = false;
    this.opening = false;
    this.currentSessionId = null;
  }

  connect(): void {
    this.connected = true;
    this.opening = false;
  }
}

/** Correlates already-posted WebView scroll batches across cancellations. */
export class TerminalScrollCorrelation {
  private epoch = 0;
  private currentSessionId: string | null = null;
  private currentToken: string | null = null;

  get context(): { sessionId: string | null; token: string | null } {
    return {
      sessionId: this.currentSessionId,
      token: this.currentToken,
    };
  }

  replace(sessionId: string | null): { sessionId: string | null; token: string | null } {
    this.epoch += 1;
    this.currentSessionId = sessionId;
    this.currentToken = sessionId ? `${sessionId}:${this.epoch}` : null;
    return this.context;
  }

  accept(sessionId: string | null, token: string | null): boolean {
    return this.currentSessionId !== null &&
      sessionId === this.currentSessionId &&
      token === this.currentToken;
  }
}
