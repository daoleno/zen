export interface DesktopCommandTarget {
  sendCommand(generation: string, sequence: number, payload: string): Promise<boolean>;
  disconnect(generation: string): Promise<void>;
}

interface PendingCommand {
  sequence: number;
  payload: string;
  bytes: number;
}

let nextGeneration = 0;

// Acknowledgements mean native enqueue, not host execution or video presentation.
export class DesktopCommandQueue {
  static readonly maxPending = 32;
  static readonly maxBytes = 32 * 1024;
  static readonly maxMessageBytes = 8192;
  private pending: PendingCommand[] = [];
  private bytes = 0;
  private sequence = 0;
  private generation = "";
  private timer: ReturnType<typeof setTimeout> | undefined;

  constructor(
    private readonly target: () => DesktopCommandTarget | null,
    private readonly onFailure: (reason: string) => void,
    private readonly timeoutMs = 2000,
  ) {}

  get currentGeneration(): string { return this.generation; }

  begin(): string {
    this.stop();
    this.generation = `desktop-${++nextGeneration}`;
    return this.generation;
  }

  stop(): void {
    const generation = this.generation;
    this.generation = "";
    this.pending = [];
    this.bytes = 0;
    this.sequence = 0;
    clearTimeout(this.timer);
    this.timer = undefined;
    if (generation) {
      try { void this.target()?.disconnect(generation).catch(() => undefined); }
      catch { /* An already destroyed native view cannot retain JS ownership. */ }
    }
  }

  send(command: object): boolean {
    if (!this.generation) return false;
    const payload = JSON.stringify(command);
    const bytes = new TextEncoder().encode(payload).length;
    if (bytes > DesktopCommandQueue.maxMessageBytes ||
      this.pending.length >= DesktopCommandQueue.maxPending ||
      this.bytes + bytes > DesktopCommandQueue.maxBytes) {
      this.fail("Desktop input queue is full.");
      return false;
    }
    this.pending.push({ payload, bytes, sequence: ++this.sequence });
    this.bytes += bytes;
    if (this.pending.length === 1) this.dispatch();
    return true;
  }

  private fail(reason: string): void {
    this.stop();
    this.onFailure(reason);
  }

  private dispatch(): void {
    const generation = this.generation;
    const next = this.pending[0];
    const target = this.target();
    if (!generation || !next) return;
    if (!target) { this.fail("Desktop input is unavailable."); return; }
    this.timer = setTimeout(() => {
      if (this.generation === generation) this.fail("Desktop input timed out.");
    }, this.timeoutMs);
    // Promise continuation is serialized; React renders cannot coalesce calls.
    void Promise.resolve().then(() => {
      if (this.generation !== generation) return false;
      return target.sendCommand(generation, next.sequence, next.payload);
    }).then((accepted) => {
      if (this.generation !== generation) return;
      clearTimeout(this.timer);
      this.timer = undefined;
      if (!accepted) { this.fail("Desktop input was rejected."); return; }
      this.pending.shift();
      this.bytes -= next.bytes;
      this.dispatch();
    }, () => {
      if (this.generation === generation) this.fail("Desktop input connection ended.");
    });
  }
}
