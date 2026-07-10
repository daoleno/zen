export type PrimaryRouteName = "brain" | "list";
export type DrawerTraceSource =
  | "back"
  | "close-button"
  | "escape"
  | "gesture"
  | "menu"
  | "navigation"
  | "overlay";

export interface ZenInteractionMetadataMap {
  "primary.switch": {
    from: PrimaryRouteName;
    to: PrimaryRouteName;
  };
  "drawer.open": {
    source: DrawerTraceSource;
    target: "closed" | "open";
  };
  "drawer.close": {
    source: DrawerTraceSource;
    target: "closed" | "open";
  };
  "composer.focus": {
    source: "input";
  };
  "agent.update": {
    agent: number | string;
  };
}

export type ZenInteractionName = keyof ZenInteractionMetadataMap;
export type ZenInteractionStatus = "cancelled" | "completed";

export interface ZenInteractionRecord<Name extends ZenInteractionName = ZenInteractionName> {
  id: number;
  name: Name;
  metadata: ZenInteractionMetadataMap[Name];
  status: ZenInteractionStatus;
  startAt: number;
  activationAt?: number;
  commitAt?: number;
  afterPaintAt?: number;
  releaseAt?: number;
  endAt: number;
  durationMs: number;
}

export interface ZenInteractionToken<Name extends ZenInteractionName> {
  readonly id: number;
  readonly name: Name;
  markActivation(at?: number): void;
  markCommit(at?: number): void;
  markAfterPaint(at?: number): void;
  markRelease(at?: number): void;
  end(at?: number): void;
  cancel(at?: number): void;
}

export interface CompletedInteraction<Name extends ZenInteractionName> {
  name: Name;
  metadata: ZenInteractionMetadataMap[Name];
  startAt: number;
  activationAt?: number;
  commitAt?: number;
  afterPaintAt?: number;
  releaseAt?: number;
  endAt: number;
  cancelled?: boolean;
}

const MAX_RECORDS = 200;

export const ZEN_INTERACTION_TRACE_ENABLED =
  typeof __DEV__ !== "undefined" &&
  __DEV__ &&
  process.env.EXPO_PUBLIC_ZEN_INTERACTION_TRACE === "1";

let nextInteractionId = 1;
const records: ZenInteractionRecord[] = [];

function now(): number {
  return typeof performance !== "undefined" &&
    typeof performance.now === "function"
    ? performance.now()
    : Date.now();
}

function markName(id: number, stage: string): string {
  return `zen-interaction:${id}:${stage}`;
}

function markWeb(id: number, stage: string, at: number): void {
  if (
    typeof document === "undefined" ||
    typeof performance === "undefined" ||
    typeof performance.mark !== "function"
  ) {
    return;
  }
  try {
    performance.mark(markName(id, stage), { startTime: at });
  } catch {
    performance.mark(markName(id, stage));
  }
}

function measureWeb(id: number, name: ZenInteractionName): void {
  if (
    typeof document === "undefined" ||
    typeof performance === "undefined" ||
    typeof performance.measure !== "function"
  ) {
    return;
  }
  try {
    performance.measure(
      `zen-interaction:${name}:${id}`,
      markName(id, "start"),
      markName(id, "end"),
    );
  } catch {
    return;
  }
}

function storeRecord(record: ZenInteractionRecord): void {
  records.push(record);
  if (records.length > MAX_RECORDS) {
    records.splice(0, records.length - MAX_RECORDS);
  }
}

function createNoopToken<Name extends ZenInteractionName>(
  name: Name,
): ZenInteractionToken<Name> {
  return {
    id: 0,
    name,
    markActivation() {},
    markCommit() {},
    markAfterPaint() {},
    markRelease() {},
    end() {},
    cancel() {},
  };
}

export function beginInteraction<Name extends ZenInteractionName>(
  name: Name,
  metadata: ZenInteractionMetadataMap[Name],
): ZenInteractionToken<Name> {
  if (!ZEN_INTERACTION_TRACE_ENABLED) {
    return createNoopToken(name);
  }

  const id = nextInteractionId;
  nextInteractionId += 1;
  const startAt = now();
  const stages: Partial<
    Record<"activation" | "afterPaint" | "commit" | "release", number>
  > = {};
  let finished = false;

  markWeb(id, "start", startAt);

  const mark = (
    stage: "activation" | "afterPaint" | "commit" | "release",
    at = now(),
  ) => {
    if (finished || stages[stage] != null) {
      return;
    }
    stages[stage] = at;
    markWeb(id, stage, at);
  };

  const finish = (status: ZenInteractionStatus, at = now()) => {
    if (finished) {
      return;
    }
    finished = true;
    markWeb(id, "end", at);
    storeRecord({
      id,
      name,
      metadata,
      status,
      startAt,
      activationAt: stages.activation,
      commitAt: stages.commit,
      afterPaintAt: stages.afterPaint,
      releaseAt: stages.release,
      endAt: at,
      durationMs: Math.max(0, at - startAt),
    });
    measureWeb(id, name);
  };

  return {
    id,
    name,
    markActivation: (at) => mark("activation", at),
    markCommit: (at) => mark("commit", at),
    markAfterPaint: (at) => mark("afterPaint", at),
    markRelease: (at) => mark("release", at),
    end: (at) => finish("completed", at),
    cancel: (at) => finish("cancelled", at),
  };
}

export function recordCompletedInteraction<Name extends ZenInteractionName>(
  interaction: CompletedInteraction<Name>,
): void {
  if (!ZEN_INTERACTION_TRACE_ENABLED) {
    return;
  }
  const id = nextInteractionId;
  nextInteractionId += 1;
  markWeb(id, "start", interaction.startAt);
  if (interaction.activationAt != null) {
    markWeb(id, "activation", interaction.activationAt);
  }
  if (interaction.commitAt != null) {
    markWeb(id, "commit", interaction.commitAt);
  }
  if (interaction.afterPaintAt != null) {
    markWeb(id, "afterPaint", interaction.afterPaintAt);
  }
  if (interaction.releaseAt != null) {
    markWeb(id, "release", interaction.releaseAt);
  }
  markWeb(id, "end", interaction.endAt);
  storeRecord({
    id,
    name: interaction.name,
    metadata: interaction.metadata,
    status: interaction.cancelled ? "cancelled" : "completed",
    startAt: interaction.startAt,
    activationAt: interaction.activationAt,
    commitAt: interaction.commitAt,
    afterPaintAt: interaction.afterPaintAt,
    releaseAt: interaction.releaseAt,
    endAt: interaction.endAt,
    durationMs: Math.max(0, interaction.endAt - interaction.startAt),
  });
  measureWeb(id, interaction.name);
}

export function snapshotInteractionTraces(): readonly ZenInteractionRecord[] {
  if (!ZEN_INTERACTION_TRACE_ENABLED) {
    return [];
  }
  return records.map((record) => ({ ...record }));
}

export function drainInteractionTraces(): readonly ZenInteractionRecord[] {
  if (!ZEN_INTERACTION_TRACE_ENABLED) {
    return [];
  }
  return records.splice(0, records.length);
}
