import {
  MERMAID_MAX_INFLIGHT_RENDERS,
  MERMAID_MAX_QUEUED_RENDERS,
  MERMAID_MAX_RENDER_MS,
  MERMAID_MESSAGE_VERSION,
} from "./mermaidLimits";
import { serializeMermaidRenderRequest } from "./mermaidMessages";
import type { MermaidRenderTheme } from "./mermaidTheme";

export type MermaidEngineResult =
  | { ok: true; svg: string; width: number; height: number }
  | { ok: false; error: string };

type PendingRender = {
  requestId: string;
  generation: number;
  source: string;
  theme: MermaidRenderTheme;
  timeoutMs: number;
  onResult: (result: MermaidEngineResult) => void;
  timer: ReturnType<typeof setTimeout> | null;
  injected: boolean;
  abandoned: boolean;
};

let pending = new Map<string, PendingRender>();
let queue: string[] = [];
let inflight: string | null = null;
let engineReady = false;
let inject: ((script: string) => void) | null = null;
let nextRequest = 1;

export function mermaidEngineQueueSnapshot() {
  return {
    queued: queue.length,
    inflight: inflight !== null,
    pending: pending.size,
    ready: engineReady,
  };
}

export function resetMermaidEngineQueue() {
  for (const job of pending.values()) {
    if (job.timer) {
      clearTimeout(job.timer);
    }
  }
  pending = new Map();
  queue = [];
  inflight = null;
  engineReady = false;
  inject = null;
  nextRequest = 1;
}

export function bindMermaidEngineInject(fn: ((script: string) => void) | null) {
  inject = fn;
  if (!fn) {
    engineReady = false;
    failAllPending("engine");
    return;
  }
  flushQueue();
}

export function markMermaidEngineReady() {
  engineReady = true;
  flushQueue();
}

export function failMermaidEngine(error = "engine") {
  engineReady = false;
  failAllPending(error);
}

export function completeMermaidEngineResult(
  requestId: string,
  generation: number,
  result: MermaidEngineResult,
) {
  const job = pending.get(requestId);
  if (!job) {
    if (inflight === requestId) {
      inflight = null;
      flushQueue();
    }
    return;
  }
  if (job.generation !== generation) {
    return;
  }
  settleJob(job, result);
}

export function requestMermaidEngineRender(input: {
  source: string;
  theme: MermaidRenderTheme;
  generation: number;
  timeoutMs?: number;
  onResult: (result: MermaidEngineResult) => void;
}) {
  const waiting = queue.length + (inflight ? 1 : 0);
  if (waiting >= MERMAID_MAX_QUEUED_RENDERS) {
    input.onResult({ ok: false, error: "busy" });
    return () => undefined;
  }
  const requestId = `m${nextRequest}`;
  nextRequest += 1;
  const job: PendingRender = {
    requestId,
    generation: input.generation,
    source: input.source,
    theme: input.theme,
    timeoutMs: input.timeoutMs ?? MERMAID_MAX_RENDER_MS,
    onResult: input.onResult,
    timer: null,
    injected: false,
    abandoned: false,
  };
  pending.set(requestId, job);
  queue.push(requestId);
  job.timer = setTimeout(
    () => {
      const current = pending.get(requestId);
      if (!current || current.abandoned) {
        if (inflight === requestId) {
          inflight = null;
          flushQueue();
        }
        return;
      }
      settleJob(current, { ok: false, error: "timeout" });
    },
    Math.min(job.timeoutMs, MERMAID_MAX_RENDER_MS) + 400,
  );
  flushQueue();
  return () => {
    const current = pending.get(requestId);
    if (!current) {
      return;
    }
    current.abandoned = true;
    if (!current.injected) {
      pending.delete(requestId);
      queue = queue.filter((id) => id !== requestId);
      if (current.timer) {
        clearTimeout(current.timer);
      }
    }
  };
}

function settleJob(job: PendingRender, result: MermaidEngineResult) {
  pending.delete(job.requestId);
  queue = queue.filter((id) => id !== job.requestId);
  if (job.timer) {
    clearTimeout(job.timer);
  }
  if (inflight === job.requestId) {
    inflight = null;
  }
  if (!job.abandoned) {
    job.onResult(result);
  }
  flushQueue();
}

function failAllPending(error: string) {
  const jobs = [...pending.values()];
  pending = new Map();
  queue = [];
  inflight = null;
  for (const job of jobs) {
    if (job.timer) {
      clearTimeout(job.timer);
    }
    if (!job.abandoned) {
      job.onResult({ ok: false, error });
    }
  }
}

function flushQueue() {
  if (
    !engineReady ||
    !inject ||
    inflight ||
    queue.length === 0 ||
    MERMAID_MAX_INFLIGHT_RENDERS < 1
  ) {
    return;
  }
  while (queue.length > 0) {
    const requestId = queue.shift();
    if (!requestId) {
      continue;
    }
    const job = pending.get(requestId);
    if (!job || job.abandoned) {
      continue;
    }
    inflight = job.requestId;
    job.injected = true;
    inject(
      `window.__zenMermaidRender(${serializeMermaidRenderRequest({
        v: MERMAID_MESSAGE_VERSION,
        type: "render",
        requestId: job.requestId,
        generation: job.generation,
        source: job.source,
        theme: job.theme,
        timeoutMs: job.timeoutMs,
      })}); true;`,
    );
    return;
  }
}
