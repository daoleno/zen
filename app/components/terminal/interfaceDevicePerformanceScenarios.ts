/**
 * Deterministic Interface device-performance scenarios.
 *
 * Pure fixtures + fake-clock-testable controller. Timed regions must apply
 * precomputed steps only — never build large fixtures or stringify results.
 */

import type { CodexConversationEvent } from "../../services/codexConversation";
import {
  firstAssistantEventId,
  lastAssistantEventId,
  makeComplexTimelineEvents,
  makeMixedTimelineEvents,
} from "./timelineProjectionFixtures";

export const INTERFACE_DEVICE_PERF_SCENARIOS = [
  "50-short",
  "500-mixed",
  "1k-mixed",
  "5k-mixed",
  "10k-mixed",
  "stream-8k",
  "stream-10k",
  "detached-append",
  "detached-append-10k",
  "detached-prepend",
  "detached-prepend-10k",
] as const;

export type InterfaceDevicePerfScenarioId =
  (typeof INTERFACE_DEVICE_PERF_SCENARIOS)[number];

export type InterfaceDevicePerfClock = {
  now(): number;
};

export type InterfaceDevicePerfScenarioStep = {
  /** Absolute clock time when this step becomes due (ms). */
  dueAtMs: number;
  events: CodexConversationEvent[];
  /** Content-free progress marker for summaries/tests. */
  label: string;
};

export type PreparedInterfaceDevicePerfScenario = {
  id: InterfaceDevicePerfScenarioId;
  /** Initial events before any tick (also step 0 when steps is empty). */
  initialEvents: CodexConversationEvent[];
  /** Detached history scenarios begin by driving the real scroll owner. */
  startsDetached: boolean;
  steps: InterfaceDevicePerfScenarioStep[];
  /** Target body length for stream-8k; 0 otherwise. */
  streamTargetChars: number;
  /** Canonical growing assistant event for stream-8k; null otherwise. */
  streamEventId: string | null;
};

export type InterfaceDevicePerfScenarioState = {
  id: InterfaceDevicePerfScenarioId;
  revision: number;
  events: CodexConversationEvent[];
  done: boolean;
  label: string;
  streamChars: number;
};

const BASE_MS = Date.parse("2026-08-06T12:00:00.000Z");
const STREAM_TARGET_CHARS = 8_000;
const STREAM_DURATION_MS = 30_000;
/**
 * Growth per stream-8k revision. Must raise body length each step so the
 * builder reaches STREAM_TARGET_CHARS in a finite, bounded step count.
 * Do not reuse projection-fixture `withAssistantBodyRevision` here — that
 * helper cycles a short body and never approaches 8k.
 */
const STREAM_CHUNK_CHARS = 128;
const DETACHED_HISTORY = 120;
const DETACHED_APPEND_COUNT = 40;
const DETACHED_PREPEND_PAGES = 5;
const DETACHED_PREPEND_PAGE_SIZE = 20;

export function resolveInterfaceDevicePerfScenario(
  value: string | string[] | undefined,
): InterfaceDevicePerfScenarioId {
  const candidate = Array.isArray(value) ? value[0] : value;
  return INTERFACE_DEVICE_PERF_SCENARIOS.includes(
    candidate as InterfaceDevicePerfScenarioId,
  )
    ? (candidate as InterfaceDevicePerfScenarioId)
    : "50-short";
}

export function createFakeInterfaceDevicePerfClock(startMs = 0): {
  clock: InterfaceDevicePerfClock;
  advance(ms: number): void;
  set(ms: number): void;
  now(): number;
} {
  let now = startMs;
  return {
    clock: {
      now() {
        return now;
      },
    },
    advance(ms: number) {
      now += ms;
    },
    set(ms: number) {
      now = ms;
    },
    now() {
      return now;
    },
  };
}

/**
 * Build all scenario fixtures and step event arrays up front.
 * Callers must keep this outside timed projection/render regions.
 */
export function prepareInterfaceDevicePerfScenario(
  id: InterfaceDevicePerfScenarioId,
): PreparedInterfaceDevicePerfScenario {
  switch (id) {
    case "50-short":
      return prepareShort(50);
    case "500-mixed":
      return prepareMixedWithTools(500);
    case "1k-mixed":
      return prepareComplex(1_000, "1k-mixed");
    case "5k-mixed":
      return prepareComplex(5_000, "5k-mixed");
    case "10k-mixed":
      return prepareComplex(10_000, "10k-mixed");
    case "stream-8k":
      return prepareStream8k();
    case "stream-10k":
      return prepareStream10k();
    case "detached-append":
      return prepareDetachedAppend();
    case "detached-append-10k":
      return prepareDetachedAppend(10_000, "detached-append-10k");
    case "detached-prepend":
      return prepareDetachedPrepend();
    case "detached-prepend-10k":
      return prepareDetachedPrepend(10_000, "detached-prepend-10k");
    default: {
      const _exhaustive: never = id;
      return _exhaustive;
    }
  }
}

function prepareComplex(
  count: number,
  id: "1k-mixed" | "5k-mixed" | "10k-mixed",
): PreparedInterfaceDevicePerfScenario {
  return {
    id,
    initialEvents: makeComplexTimelineEvents(count),
    startsDetached: false,
    steps: [],
    streamTargetChars: 0,
    streamEventId: null,
  };
}

function prepareShort(count: number): PreparedInterfaceDevicePerfScenario {
  const events = makeShortMessages(count);
  return {
    id: "50-short",
    initialEvents: events,
    startsDetached: false,
    steps: [],
    streamTargetChars: 0,
    streamEventId: null,
  };
}

function prepareMixedWithTools(
  count: number,
): PreparedInterfaceDevicePerfScenario {
  const events = makeMixedTimelineEvents(count);
  const toolCount = events.filter((event) => event.kind === "tool").length;
  if (toolCount < 15) {
    for (let index = toolCount; index < 20; index += 1) {
      const seq = count + index;
      events.push({
        id: `extra-tool-${index}`,
        seq,
        kind: "tool",
        tool_name: "Read",
        input: `{"path":"fixture-${index}"}`,
        body: `tool-body-${index}`,
        status: "done",
        timestamp: new Date(BASE_MS + seq * 60_000).toISOString(),
      });
    }
  }
  return {
    id: "500-mixed",
    initialEvents: events,
    startsDetached: false,
    steps: [],
    streamTargetChars: 0,
    streamEventId: null,
  };
}

function prepareStream8k(): PreparedInterfaceDevicePerfScenario {
  const base = makeMixedTimelineEvents(12);
  const assistantId = firstAssistantEventId(base);
  // Seed the growing assistant at 0 chars so streamChars starts empty and
  // every prepared step is a strict length increase toward the 8k target.
  const initialEvents = withStreamAssistantBody(base, assistantId, "", false);
  const revisions = buildStreamBodyRevisions(initialEvents, assistantId);
  const steps: InterfaceDevicePerfScenarioStep[] = revisions.map(
    ({ revision, events, body, terminal }, index) => ({
      // Spread the deterministic workload across a profiler-friendly 30s.
      dueAtMs: Math.round(
        ((index + 1) * STREAM_DURATION_MS) / revisions.length,
      ),
      events,
      // Labels stay content-free: revision index + char counts only.
      label: terminal
        ? `stream-terminal chars=${body.length}`
        : `stream chars=${body.length} rev=${revision}`,
    }),
  );
  return {
    id: "stream-8k",
    initialEvents,
    startsDetached: false,
    steps,
    streamTargetChars: STREAM_TARGET_CHARS,
    streamEventId: assistantId,
  };
}

function prepareStream10k(): PreparedInterfaceDevicePerfScenario {
  const base = makeComplexTimelineEvents(10_000);
  const assistantId = lastAssistantEventId(base);
  return prepareStreamScenario("stream-10k", base, assistantId);
}

function prepareStreamScenario(
  id: "stream-10k",
  base: CodexConversationEvent[],
  assistantId: string,
): PreparedInterfaceDevicePerfScenario {
  const initialEvents = withStreamAssistantBody(base, assistantId, "", false);
  const revisions = buildStreamBodyRevisions(initialEvents, assistantId);
  return {
    id,
    initialEvents,
    startsDetached: false,
    steps: revisions.map(({ revision, events, body, terminal }, index) => ({
      dueAtMs: Math.round(
        ((index + 1) * STREAM_DURATION_MS) / revisions.length,
      ),
      events,
      label: terminal
        ? `stream-terminal chars=${body.length}`
        : `stream chars=${body.length} rev=${revision}`,
    })),
    streamTargetChars: STREAM_TARGET_CHARS,
    streamEventId: assistantId,
  };
}

function prepareDetachedAppend(
  historyCount = DETACHED_HISTORY,
  id: "detached-append" | "detached-append-10k" = "detached-append",
): PreparedInterfaceDevicePerfScenario {
  const history =
    historyCount >= 1_000
      ? makeComplexTimelineEvents(historyCount)
      : makeShortMessages(historyCount);
  const steps: InterfaceDevicePerfScenarioStep[] = [];
  let events = history;
  for (let index = 0; index < DETACHED_APPEND_COUNT; index += 1) {
    const seq = historyCount + index;
    const next: CodexConversationEvent = {
      id: `live-append-${index}`,
      seq,
      kind: index % 2 === 0 ? "assistant_message" : "user_message",
      role: index % 2 === 0 ? "assistant" : "user",
      body: `Live append ${index}`,
      timestamp: new Date(BASE_MS + seq * 60_000).toISOString(),
      partial: false,
    };
    events = [...events, next];
    steps.push({
      dueAtMs: (index + 1) * 32,
      events,
      label: `detached-append n=${events.length}`,
    });
  }
  return {
    id,
    initialEvents: history,
    startsDetached: true,
    steps,
    streamTargetChars: 0,
    streamEventId: null,
  };
}

function prepareDetachedPrepend(
  historyCount = DETACHED_HISTORY,
  id: "detached-prepend" | "detached-prepend-10k" = "detached-prepend",
): PreparedInterfaceDevicePerfScenario {
  const history =
    historyCount >= 1_000
      ? makeComplexTimelineEvents(historyCount)
      : makeShortMessages(historyCount);
  const liveEdge = history.map((event) => ({
    ...event,
    seq: event.seq + 10_000,
    id: `edge-${event.id}`,
    timestamp: new Date(BASE_MS + (event.seq + 10_000) * 60_000).toISOString(),
  }));
  const steps: InterfaceDevicePerfScenarioStep[] = [];
  let events = liveEdge;
  for (let page = 0; page < DETACHED_PREPEND_PAGES; page += 1) {
    const older = makeShortMessages(DETACHED_PREPEND_PAGE_SIZE).map(
      (event, index) => {
        // Each newly fetched page is strictly older than the prior oldest.
        const seq = -(page + 1) * DETACHED_PREPEND_PAGE_SIZE + index;
        return {
          ...event,
          id: `hist-p${page}-${event.id}`,
          seq,
          timestamp: new Date(BASE_MS + seq * 60_000).toISOString(),
        };
      },
    );
    events = [...older, ...events];
    steps.push({
      dueAtMs: (page + 1) * 48,
      events,
      label: `detached-prepend pages=${page + 1} n=${events.length}`,
    });
  }
  return {
    id,
    initialEvents: liveEdge,
    startsDetached: true,
    steps,
    streamTargetChars: 0,
    streamEventId: null,
  };
}

function makeShortMessages(count: number): CodexConversationEvent[] {
  const events: CodexConversationEvent[] = [];
  for (let index = 0; index < count; index += 1) {
    const timestamp = new Date(BASE_MS + index * 60_000).toISOString();
    if (index % 2 === 0) {
      events.push({
        id: `short-user-${index}`,
        seq: index,
        kind: "user_message",
        role: "user",
        body: `Short user ${index}`,
        timestamp,
      });
    } else {
      events.push({
        id: `short-assistant-${index}`,
        seq: index,
        kind: "assistant_message",
        role: "assistant",
        body: `Short assistant ${index}`,
        timestamp,
        partial: false,
      });
    }
  }
  return events;
}

/**
 * Deterministic stream body of exact `length` chars. Length is the growth
 * owner; revision is only a stable head marker for debugging.
 */
function streamBodyOfLength(revision: number, length: number): string {
  if (length <= 0) {
    return "";
  }
  const head = `stream-rev=${revision}\n`;
  if (head.length >= length) {
    return head.slice(0, length);
  }
  const fillUnit = "abcdefghijklmnopqrstuvwxyz0123456789\n";
  const fillNeeded = length - head.length;
  const repeats = Math.ceil(fillNeeded / fillUnit.length);
  return head + fillUnit.repeat(repeats).slice(0, fillNeeded);
}

function withStreamAssistantBody(
  events: CodexConversationEvent[],
  assistantId: string,
  body: string,
  terminal: boolean,
): CodexConversationEvent[] {
  return events.map((event) =>
    event.id === assistantId
      ? {
          ...event,
          body,
          partial: !terminal,
          status: terminal ? "done" : "streaming",
        }
      : event,
  );
}

function buildStreamBodyRevisions(
  base: CodexConversationEvent[],
  assistantId: string,
) {
  const revisions: Array<{
    revision: number;
    events: CodexConversationEvent[];
    body: string;
    terminal: boolean;
  }> = [];
  let revision = 0;
  let bodyLength = 0;
  const maxRevisions =
    Math.ceil(STREAM_TARGET_CHARS / STREAM_CHUNK_CHARS) + 1;
  while (bodyLength < STREAM_TARGET_CHARS) {
    revision += 1;
    if (revision > maxRevisions) {
      throw new Error(
        `stream-8k body growth stalled below ${STREAM_TARGET_CHARS} chars`,
      );
    }
    bodyLength = Math.min(
      STREAM_TARGET_CHARS,
      bodyLength + STREAM_CHUNK_CHARS,
    );
    const body = streamBodyOfLength(revision, bodyLength);
    const terminal = bodyLength >= STREAM_TARGET_CHARS;
    revisions.push({
      revision,
      events: withStreamAssistantBody(base, assistantId, body, terminal),
      body,
      terminal,
    });
  }
  return revisions;
}

export function createInterfaceDevicePerfScenarioController(input: {
  prepared: PreparedInterfaceDevicePerfScenario;
  clock: InterfaceDevicePerfClock;
}): {
  getState(): InterfaceDevicePerfScenarioState;
  /** Apply every step whose dueAtMs <= clock.now(). Returns applied count. */
  tick(): number;
  /** Force-apply the next pending step regardless of clock (tests/manual). */
  advanceOne(): boolean;
  isDone(): boolean;
  /** Absolute due time of the next pending step, or null when finished. */
  nextDueAtMs(): number | null;
} {
  const { prepared, clock } = input;
  let stepIndex = 0;
  let events = prepared.initialEvents;
  let label = "initial";
  let revision = 0;

  const streamCharsFromEvents = () =>
    prepared.streamEventId
      ? events.find((event) => event.id === prepared.streamEventId)?.body
          ?.length ?? 0
      : 0;

  const snapshot = (): InterfaceDevicePerfScenarioState => ({
    id: prepared.id,
    revision,
    events,
    done: stepIndex >= prepared.steps.length,
    label,
    streamChars: streamCharsFromEvents(),
  });

  const applyStep = (step: InterfaceDevicePerfScenarioStep) => {
    events = step.events;
    label = step.label;
    revision += 1;
    stepIndex += 1;
  };

  return {
    getState: snapshot,
    tick() {
      let applied = 0;
      while (stepIndex < prepared.steps.length) {
        const step = prepared.steps[stepIndex]!;
        if (step.dueAtMs > clock.now()) {
          break;
        }
        applyStep(step);
        applied += 1;
      }
      return applied;
    },
    advanceOne() {
      if (stepIndex >= prepared.steps.length) {
        return false;
      }
      applyStep(prepared.steps[stepIndex]!);
      return true;
    },
    isDone() {
      return stepIndex >= prepared.steps.length;
    },
    nextDueAtMs() {
      const step = prepared.steps[stepIndex];
      return step ? step.dueAtMs : null;
    },
  };
}

/** Content-free assertion helper: never returns bodies. */
export function interfaceDevicePerfScenarioFingerprint(
  state: InterfaceDevicePerfScenarioState,
) {
  return {
    id: state.id,
    revision: state.revision,
    eventCount: state.events.length,
    done: state.done,
    label: state.label,
    streamChars: state.streamChars,
  };
}

export function interfaceDevicePerfStreamTargetChars() {
  return STREAM_TARGET_CHARS;
}

export function interfaceDevicePerfStreamDurationMs() {
  return STREAM_DURATION_MS;
}

/** Re-export for harness tests that assert fixture reuse. */
export { firstAssistantEventId, makeMixedTimelineEvents };
