import { describe, expect, test } from "bun:test";
import type { BrainWorkResultEvent } from "../brain/brainWorkEvent";
import type { ToolDeveloperDetails } from "../../services/toolCallDetails";
import type { ZenTimelineItem } from "./InterfaceTimelineItemView";
import type {
  ZenActivityChild,
  ZenActivityTimelineItem,
} from "./InterfaceTimelineActivityTypes";
import {
  activityChildrenEqual,
  attachmentsEqual,
  brainWorkResultEventsEqual,
  stringRecordsEqual,
  timelineItemsSemanticEqual,
  toolDeveloperDetailsEqual,
} from "./timelineItemsSemanticEqual";
import type { DisplayAttachment } from "./InterfaceTimelineMessage";
import type { HeartbeatWakeEvent } from "./CodexHeartbeatWake";
import type { ZenPlanTimelineItem } from "./InterfaceTimelinePlanTypes";
import type { CodexPlanStep } from "../../services/codexConversation";

function makeActivity(
  overrides: Partial<ZenActivityTimelineItem> = {},
): ZenActivityTimelineItem {
  return {
    type: "activity",
    id: "activity-1",
    timestamp: "2026-08-06T12:00:00.000Z",
    title: "run command",
    tone: "running",
    icon: "terminal-outline",
    developerDetails: makeDeveloperDetails(),
    children: makeChildren(),
    ...overrides,
  };
}

function makeDeveloperDetails(
  overrides: Partial<ToolDeveloperDetails> = {},
): ToolDeveloperDetails {
  return {
    providerToolId: "tool-42",
    rawInput: '{"command":"ls","session_id":"s1"}',
    transport: {
      session_id: "s1",
      call_id: "c1",
      wall_time: "12ms",
    },
    ...overrides,
  };
}

function makeChildren(): ZenActivityChild[] {
  return [
    {
      id: "child-a",
      title: "spawn",
      tone: "success",
      providerToolId: "tool-a",
    },
    {
      id: "child-b",
      title: "await",
      tone: "running",
      providerToolId: "tool-b",
    },
  ];
}

function makeBrainEvent(
  overrides: Partial<BrainWorkResultEvent> = {},
): BrainWorkResultEvent {
  return {
    event_id: "evt-1",
    kind: "session.done",
    work_id: "work-1",
    work_title: "Ship slice",
    summary: "Done",
    session_id: "sess-1",
    session_name: "main",
    occurred_at: "2026-08-06T12:00:00.000Z",
    unread: true,
    review_state: "queued",
    session_state: "open",
    current_result: true,
    ...overrides,
  };
}

function makeBrainItem(
  overrides: Partial<Extract<ZenTimelineItem, { type: "brain-work-event" }>> = {},
): Extract<ZenTimelineItem, { type: "brain-work-event" }> {
  const event = makeBrainEvent();
  return {
    type: "brain-work-event",
    id: "brain-1",
    timestamp: "2026-08-06T12:00:00.000Z",
    event,
    events: [event],
    onPress: () => {},
    ...overrides,
  };
}

function makeAttachment(
  overrides: Partial<DisplayAttachment> = {},
): DisplayAttachment {
  return {
    name: "photo.png",
    path: "/remote/photo.png",
    localUri: "file:///local/photo.png",
    mimeType: "image/png",
    ...overrides,
  };
}

function makeMessage(
  overrides: Partial<Extract<ZenTimelineItem, { type: "message" }>> = {},
): Extract<ZenTimelineItem, { type: "message" }> {
  return {
    type: "message",
    id: "msg-1",
    role: "user",
    timestamp: "2026-08-06T12:00:00.000Z",
    body: "hello",
    attachments: [makeAttachment()],
    ...overrides,
  };
}

function makePlanStep(
  overrides: Partial<CodexPlanStep> = {},
): CodexPlanStep {
  return {
    step: "Ship equality",
    status: "in_progress",
    ...overrides,
  };
}

function makePlan(
  overrides: Partial<ZenPlanTimelineItem> = {},
): ZenPlanTimelineItem {
  return {
    type: "plan",
    id: "plan-1",
    timestamp: "2026-08-06T12:00:00.000Z",
    explanation: "Do the work",
    steps: [makePlanStep(), makePlanStep({ step: "Verify", status: "pending" })],
    ...overrides,
  };
}

/**
 * Byte-for-byte semantic reproduction of the removed production comparator
 * from useInterfaceTimelineItems (pre-extraction). Only developerDetails,
 * children, and Brain Work events used JSON.stringify; files/fileSummaries
 * used the explicit helpers below.
 */
function legacyRemovedProductionTimelineItemsEqual(
  left: ZenTimelineItem,
  right: ZenTimelineItem,
): boolean {
  if (left === right) {
    return true;
  }
  if (
    left.type !== right.type ||
    left.id !== right.id ||
    left.timestamp !== right.timestamp
  ) {
    return false;
  }
  if (left.type === "message" && right.type === "message") {
    return (
      left.role === right.role &&
      left.body === right.body &&
      left.pending === right.pending &&
      left.pendingLifecycle === right.pendingLifecycle &&
      left.pendingLifecycleLabel === right.pendingLifecycleLabel &&
      left.pendingFailureMessage === right.pendingFailureMessage &&
      left.onRetryPending === right.onRetryPending &&
      left.streaming === right.streaming &&
      left.turnFocusAnchorId === right.turnFocusAnchorId &&
      legacyAttachmentsEqualNamePathOnly(left.attachments, right.attachments) &&
      left.heartbeatWake === right.heartbeatWake
    );
  }
  if (left.type === "plan" && right.type === "plan") {
    return (
      left.explanation === right.explanation &&
      left.steps.length === right.steps.length &&
      left.steps.every(
        (step, index) =>
          step.step === right.steps[index]?.step &&
          step.status === right.steps[index]?.status,
      )
    );
  }
  if (left.type === "activity" && right.type === "activity") {
    return (
      left.statusKey === right.statusKey &&
      left.title === right.title &&
      left.tone === right.tone &&
      left.icon === right.icon &&
      left.activityKind === right.activityKind &&
      left.streaming === right.streaming &&
      left.detail === right.detail &&
      left.body === right.body &&
      left.bodyKind === right.bodyKind &&
      left.commandText === right.commandText &&
      left.queryText === right.queryText &&
      left.statusLine === right.statusLine &&
      left.previewPath === right.previewPath &&
      left.defaultExpanded === right.defaultExpanded &&
      left.accessibilityLabel === right.accessibilityLabel &&
      left.providerToolId === right.providerToolId &&
      legacyStringArraysEqual(left.files, right.files) &&
      legacyPatchSummariesEqual(left.fileSummaries, right.fileSummaries) &&
      JSON.stringify(left.developerDetails) ===
        JSON.stringify(right.developerDetails) &&
      JSON.stringify(left.children) === JSON.stringify(right.children)
    );
  }
  if (
    left.type === "brain-work-event" &&
    right.type === "brain-work-event"
  ) {
    return (
      JSON.stringify(left.event) === JSON.stringify(right.event) &&
      left.onPress === right.onPress
    );
  }
  return false;
}

function legacyAttachmentsEqualNamePathOnly(
  left: DisplayAttachment[],
  right: DisplayAttachment[],
) {
  if (left === right) {
    return true;
  }
  if (left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (
      left[index]?.name !== right[index]?.name ||
      left[index]?.path !== right[index]?.path
    ) {
      return false;
    }
  }
  return true;
}

function legacyStringArraysEqual(left?: string[], right?: string[]) {
  if (left === right) {
    return true;
  }
  if (!left || !right || left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) {
      return false;
    }
  }
  return true;
}

function legacyPatchSummariesEqual(
  left?: ZenActivityTimelineItem["fileSummaries"],
  right?: ZenActivityTimelineItem["fileSummaries"],
) {
  if (left === right) {
    return true;
  }
  if (!left || !right || left.length !== right.length) {
    return false;
  }
  for (let index = 0; index < left.length; index += 1) {
    const leftFile = left[index];
    const rightFile = right[index];
    if (
      leftFile?.path !== rightFile?.path ||
      leftFile?.movePath !== rightFile?.movePath ||
      leftFile?.operation !== rightFile?.operation ||
      leftFile?.added !== rightFile?.added ||
      leftFile?.removed !== rightFile?.removed
    ) {
      return false;
    }
  }
  return true;
}

function medianMs(samples: number[]) {
  const sorted = [...samples].sort((left, right) => left - right);
  const middle = Math.floor(sorted.length / 2);
  if (sorted.length % 2 === 0) {
    return (sorted[middle - 1]! + sorted[middle]!) / 2;
  }
  return sorted[middle]!;
}

/** Structurally equal clones preserving transport insertion order. */
function cloneBenchmarkEqualPair(item: ZenTimelineItem): ZenTimelineItem {
  if (item.type === "activity") {
    return {
      ...item,
      developerDetails: item.developerDetails
        ? {
            providerToolId: item.developerDetails.providerToolId,
            rawInput: item.developerDetails.rawInput,
            transport: item.developerDetails.transport
              ? { ...item.developerDetails.transport }
              : undefined,
          }
        : undefined,
      children: item.children
        ? item.children.map((child) => ({ ...child }))
        : undefined,
      files: item.files ? [...item.files] : undefined,
      fileSummaries: item.fileSummaries
        ? item.fileSummaries.map((file) => ({ ...file }))
        : undefined,
    };
  }
  if (item.type === "brain-work-event") {
    return {
      ...item,
      event: { ...item.event },
    };
  }
  return item;
}

function nowMs() {
  const perf = (
    globalThis as { performance?: { now(): number } }
  ).performance;
  return perf?.now?.() ?? Date.now();
}

function buildLargeRawInput(seed: number): string {
  const chunks: string[] = [`{"seed":${seed},"files":[`];
  for (let index = 0; index < 48; index += 1) {
    if (index > 0) {
      chunks.push(",");
    }
    chunks.push(
      `{"path":"src/module-${seed}-${index}.ts","bytes":${1000 + index}}`,
    );
  }
  chunks.push(`],"command":"bun test suite-${seed}"}`);
  return chunks.join("");
}

function buildTransport(seed: number): Record<string, string> {
  return {
    session_id: `session-${seed}`,
    call_id: `call-${seed}`,
    wall_time: `${seed}ms`,
    chunk_id: `chunk-${seed}`,
    token_count: String(seed * 3),
    yield_time_ms: String(seed * 7),
  };
}

function buildChildren(seed: number): ZenActivityChild[] {
  const children: ZenActivityChild[] = [];
  for (let index = 0; index < 12; index += 1) {
    children.push({
      id: `child-${seed}-${index}`,
      title: `step ${seed}.${index}`,
      tone: index % 2 === 0 ? "running" : "success",
      providerToolId: `tool-${seed}-${index}`,
    });
  }
  return children;
}

/** Precompute 500 mixed Activity / Brain Work items outside timed regions. */
function buildEqualityBenchmarkItems(count: number): ZenTimelineItem[] {
  const items: ZenTimelineItem[] = [];
  for (let index = 0; index < count; index += 1) {
    if (index % 5 === 0) {
      const onPress = () => {};
      const event: BrainWorkResultEvent = {
        event_id: `evt-${index}`,
        kind:
          index % 4 === 0
            ? "session.done"
            : index % 4 === 1
              ? "session.failed"
              : index % 4 === 2
                ? "session.needs_input"
                : "session.stale",
        work_id: `work-${index}`,
        work_title: `Work ${index}`,
        summary: `Summary for work ${index} with enough text to exercise field compares.`,
        session_id: `sess-${index}`,
        session_name: `session-${index}`,
        occurred_at: `2026-08-06T12:${String(index % 60).padStart(2, "0")}:00.000Z`,
        unread: index % 3 === 0,
        review_state: index % 2 === 0 ? "queued" : "resolved",
        session_state: index % 2 === 0 ? "open" : "not_required",
        current_result: true,
      };
      items.push({
        type: "brain-work-event",
        id: `brain-${index}`,
        timestamp: `2026-08-06T12:${String(index % 60).padStart(2, "0")}:00.000Z`,
        event,
        events: [event],
        onPress,
      });
      continue;
    }
    items.push({
      type: "activity",
      id: `activity-${index}`,
      timestamp: `2026-08-06T13:${String(index % 60).padStart(2, "0")}:00.000Z`,
      title: `Activity ${index}`,
      tone: index % 2 === 0 ? "running" : "success",
      icon: "terminal-outline",
      statusKey: `status-${index}`,
      detail: `detail-${index}`,
      body: `body-${index}`,
      bodyKind: "terminal",
      commandText: `bun run task-${index}`,
      queryText: `query-${index}`,
      statusLine: `Completed · ${index}ms`,
      providerToolId: `provider-${index}`,
      accessibilityLabel: `Activity ${index}`,
      files: [`a-${index}.ts`, `b-${index}.ts`],
      fileSummaries: [
        {
          path: `src/a-${index}.ts`,
          operation: "update",
          added: index,
          removed: index % 4,
        },
      ],
      developerDetails: {
        providerToolId: `dev-${index}`,
        rawInput: buildLargeRawInput(index),
        transport: buildTransport(index),
      },
      children: buildChildren(index),
    });
  }
  return items;
}

describe("timelineItemsSemanticEqual", () => {
  test("identity fast path returns true without field walks", () => {
    const activity = makeActivity();
    const brain = makeBrainItem();
    expect(timelineItemsSemanticEqual(activity, activity)).toBe(true);
    expect(timelineItemsSemanticEqual(brain, brain)).toBe(true);
  });

  test("equal ToolDeveloperDetails and Activity children reuse", () => {
    const left = makeActivity();
    const right = makeActivity({
      developerDetails: makeDeveloperDetails(),
      children: makeChildren(),
    });
    expect(timelineItemsSemanticEqual(left, right)).toBe(true);
    expect(
      toolDeveloperDetailsEqual(left.developerDetails, right.developerDetails),
    ).toBe(true);
    expect(activityChildrenEqual(left.children, right.children)).toBe(true);
  });

  test("transport records with same pairs in different insertion order are equal", () => {
    const left = makeDeveloperDetails({
      transport: {
        session_id: "s1",
        call_id: "c1",
        wall_time: "12ms",
      },
    });
    const right = makeDeveloperDetails({
      transport: {
        wall_time: "12ms",
        session_id: "s1",
        call_id: "c1",
      },
    });
    expect(stringRecordsEqual(left.transport, right.transport)).toBe(true);
    expect(toolDeveloperDetailsEqual(left, right)).toBe(true);
    expect(
      timelineItemsSemanticEqual(
        makeActivity({ developerDetails: left }),
        makeActivity({ developerDetails: right }),
      ),
    ).toBe(true);
    // Legacy JSON.stringify treats insertion order as unequal — intentional fix.
    expect(JSON.stringify(left.transport) === JSON.stringify(right.transport)).toBe(
      false,
    );
  });

  test("each ToolDeveloperDetails field mutation independently prevents reuse", () => {
    const base = makeDeveloperDetails();
    expect(
      toolDeveloperDetailsEqual(base, makeDeveloperDetails({ providerToolId: "other" })),
    ).toBe(false);
    expect(
      toolDeveloperDetailsEqual(base, makeDeveloperDetails({ rawInput: '{"x":1}' })),
    ).toBe(false);
    expect(
      toolDeveloperDetailsEqual(
        base,
        makeDeveloperDetails({
          transport: { ...base.transport!, call_id: "changed" },
        }),
      ),
    ).toBe(false);
    expect(toolDeveloperDetailsEqual(base, undefined)).toBe(false);
    expect(toolDeveloperDetailsEqual(undefined, base)).toBe(false);
    expect(toolDeveloperDetailsEqual(undefined, undefined)).toBe(true);
    expect(
      toolDeveloperDetailsEqual(
        makeDeveloperDetails({ transport: undefined }),
        makeDeveloperDetails({ transport: { session_id: "s1" } }),
      ),
    ).toBe(false);
    expect(
      stringRecordsEqual({ a: "1" }, { a: "1", b: "2" }),
    ).toBe(false);
  });

  test("each Activity child field mutation and order change prevents reuse", () => {
    const base = makeChildren();
    expect(
      activityChildrenEqual(base, [
        { ...base[0]!, id: "other" },
        base[1]!,
      ]),
    ).toBe(false);
    expect(
      activityChildrenEqual(base, [
        { ...base[0]!, title: "changed" },
        base[1]!,
      ]),
    ).toBe(false);
    expect(
      activityChildrenEqual(base, [
        { ...base[0]!, tone: "failed" },
        base[1]!,
      ]),
    ).toBe(false);
    expect(
      activityChildrenEqual(base, [
        { ...base[0]!, providerToolId: "other-tool" },
        base[1]!,
      ]),
    ).toBe(false);
    expect(activityChildrenEqual(base, [base[1]!, base[0]!])).toBe(false);
    expect(activityChildrenEqual(base, undefined)).toBe(false);
    expect(activityChildrenEqual(undefined, base)).toBe(false);
    expect(activityChildrenEqual(undefined, undefined)).toBe(true);
    expect(
      activityChildrenEqual(
        [{ id: "a", title: "t" }],
        [{ id: "a", title: "t", tone: "running" }],
      ),
    ).toBe(false);
    expect(
      activityChildrenEqual(
        [{ id: "a", title: "t", providerToolId: "p" }],
        [{ id: "a", title: "t" }],
      ),
    ).toBe(false);
  });

  test("each Brain Work event field mutation prevents reuse", () => {
    const base = makeBrainEvent();
    const fields: Array<Partial<BrainWorkResultEvent>> = [
      { event_id: "other" },
      { kind: "session.failed" },
      { work_id: "other-work" },
      { work_title: "Other title" },
      { summary: "Other summary" },
      { session_id: "other-sess" },
      { session_name: "other-name" },
      { occurred_at: "2026-08-06T13:00:00.000Z" },
      { unread: false },
      { review_state: "reviewing" },
      { session_state: "closing" },
      { current_result: false },
    ];
    for (const override of fields) {
      expect(
        brainWorkResultEventsEqual(base, makeBrainEvent(override)),
      ).toBe(false);
    }
    expect(
      brainWorkResultEventsEqual(
        makeBrainEvent({ session_id: undefined }),
        makeBrainEvent({ session_id: "present" }),
      ),
    ).toBe(false);
    expect(
      brainWorkResultEventsEqual(
        makeBrainEvent({ session_name: undefined }),
        makeBrainEvent({ session_name: "present" }),
      ),
    ).toBe(false);
  });

  test("Brain Work callback identity is compared separately from event fields", () => {
    const event = makeBrainEvent();
    const onPressA = () => {};
    const onPressB = () => {};
    const left = makeBrainItem({ event, onPress: onPressA });
    const sameCallback = makeBrainItem({ event: { ...event }, onPress: onPressA });
    const differentCallback = makeBrainItem({
      event: { ...event },
      onPress: onPressB,
    });
    expect(timelineItemsSemanticEqual(left, sameCallback)).toBe(true);
    expect(timelineItemsSemanticEqual(left, differentCallback)).toBe(false);
    expect(
      timelineItemsSemanticEqual(
        makeBrainItem({ event, onPress: undefined }),
        makeBrainItem({ event, onPress: onPressA }),
      ),
    ).toBe(false);
  });

  test("Brain Work grouped revisions and source counts invalidate only that stable row", () => {
    const event = makeBrainEvent();
    const onPress = () => {};
    const base = makeBrainItem({ event, events: [event], onPress });
    const reviewing = makeBrainEvent({
      event_id: "evt-reviewing",
      review_state: "reviewing",
    });

    expect(
      timelineItemsSemanticEqual(
        base,
        makeBrainItem({
          event,
          events: [event, reviewing],
          onPress,
        }),
      ),
    ).toBe(false);
    expect(
      timelineItemsSemanticEqual(
        base,
        makeBrainItem({ event, events: [event], sourceCount: 2, onPress }),
      ),
    ).toBe(false);
  });

  test("Brain Work current projection status invalidates its stable row", () => {
    const event = makeBrainEvent();
    const onPress = () => {};
    const currentWork = {
      work_id: event.work_id,
      revision: 1,
      title: "Current Work",
      status: "waiting" as const,
      unread_result: false,
    };
    const base = makeBrainItem({
      event,
      events: [event],
      currentWork,
      onPress,
    });

    expect(
      timelineItemsSemanticEqual(
        base,
        makeBrainItem({
          event,
          events: [event],
          currentWork: { ...currentWork, status: "done" },
          onPress,
        }),
      ),
    ).toBe(false);
  });

  test("each DisplayAttachment field mutation independently prevents reuse", () => {
    const base = makeAttachment();
    expect(attachmentsEqual([base], [makeAttachment()])).toBe(true);
    expect(
      attachmentsEqual([base], [makeAttachment({ name: "other.png" })]),
    ).toBe(false);
    expect(
      attachmentsEqual([base], [makeAttachment({ path: "/other/path.png" })]),
    ).toBe(false);
    expect(
      attachmentsEqual(
        [base],
        [makeAttachment({ localUri: "file:///other/photo.png" })],
      ),
    ).toBe(false);
    expect(
      attachmentsEqual([base], [makeAttachment({ mimeType: "image/jpeg" })]),
    ).toBe(false);
    expect(
      attachmentsEqual(
        [makeAttachment({ localUri: undefined })],
        [makeAttachment({ localUri: "file:///present.png" })],
      ),
    ).toBe(false);
    expect(
      attachmentsEqual(
        [makeAttachment({ mimeType: undefined })],
        [makeAttachment({ mimeType: "image/png" })],
      ),
    ).toBe(false);
    expect(
      timelineItemsSemanticEqual(
        makeMessage({ attachments: [base] }),
        makeMessage({
          attachments: [makeAttachment({ localUri: "file:///stale.png" })],
        }),
      ),
    ).toBe(false);
    expect(
      timelineItemsSemanticEqual(
        makeMessage({ attachments: [base] }),
        makeMessage({
          attachments: [makeAttachment({ mimeType: "application/pdf" })],
        }),
      ),
    ).toBe(false);
  });

  test("plan steps use indexed equality without Array.every", () => {
    const left = makePlan();
    const right = makePlan({
      steps: [
        makePlanStep(),
        makePlanStep({ step: "Verify", status: "pending" }),
      ],
    });
    expect(timelineItemsSemanticEqual(left, right)).toBe(true);
    expect(
      timelineItemsSemanticEqual(
        left,
        makePlan({
          steps: [
            makePlanStep({ step: "changed" }),
            makePlanStep({ step: "Verify", status: "pending" }),
          ],
        }),
      ),
    ).toBe(false);
    expect(
      timelineItemsSemanticEqual(
        left,
        makePlan({
          steps: [
            makePlanStep({ status: "completed" }),
            makePlanStep({ step: "Verify", status: "pending" }),
          ],
        }),
      ),
    ).toBe(false);
    expect(
      timelineItemsSemanticEqual(
        left,
        makePlan({
          steps: [
            makePlanStep({ step: "Verify", status: "pending" }),
            makePlanStep(),
          ],
        }),
      ),
    ).toBe(false);
  });

  test("heartbeatWake stays conservative identity comparison", () => {
    const wake: HeartbeatWakeEvent = {
      reason: "stale",
      agentId: "agent-1",
      agentName: "Zen",
      status: "wake",
      summary: "resume",
    };
    const left = makeMessage({ heartbeatWake: wake });
    const sameReference = makeMessage({ heartbeatWake: wake });
    const cloneWithSameFields = makeMessage({
      heartbeatWake: { ...wake },
    });
    expect(timelineItemsSemanticEqual(left, sameReference)).toBe(true);
    expect(timelineItemsSemanticEqual(left, cloneWithSameFields)).toBe(false);
    expect(
      timelineItemsSemanticEqual(
        makeMessage({ heartbeatWake: undefined }),
        makeMessage({ heartbeatWake: wake }),
      ),
    ).toBe(false);
  });

  test("production stabilization path no longer uses JSON.stringify for Activity or Brain Work", async () => {
    const hookSource = await Bun.file(
      new URL("./useInterfaceTimelineItems.ts", import.meta.url),
    ).text();
    const equalSource = await Bun.file(
      new URL("./timelineItemsSemanticEqual.ts", import.meta.url),
    ).text();
    expect(hookSource).toContain("timelineItemsSemanticEqual");
    expect(hookSource).not.toContain("JSON.stringify");
    expect(equalSource).not.toContain("JSON.stringify");
    expect(equalSource).not.toMatch(
      /^\s*import(?:\s+type)?\s+.*\bfrom\s+["']react(?:-native)?["']/m,
    );
    expect(equalSource).not.toMatch(/^\s*import\s+React\b/m);
    expect(equalSource).not.toMatch(/\.every\s*\(/);
    expect(equalSource).toContain("localUri");
    expect(equalSource).toContain("mimeType");
    expect(equalSource).toContain("left.heartbeatWake === right.heartbeatWake");
  });

  test("precomputed 500-item repeated-pass benchmark: semantic vs removed production comparator", () => {
    const itemCount = 500;
    const passes = 40;
    const trials = 5;
    // Precompute outside timed regions — fixture build is not comparator CPU.
    const leftItems = buildEqualityBenchmarkItems(itemCount);
    // Structurally equal pairs with identical transport insertion order so
    // legacy and semantic return the same truth value and traverse owners.
    const rightItems = leftItems.map(cloneBenchmarkEqualPair);

    // Shared warmup so cold JIT does not dominate either path.
    for (let warm = 0; warm < 4; warm += 1) {
      for (let index = 0; index < itemCount; index += 1) {
        timelineItemsSemanticEqual(leftItems[index]!, rightItems[index]!);
        legacyRemovedProductionTimelineItemsEqual(
          leftItems[index]!,
          rightItems[index]!,
        );
      }
    }

    const semanticTrialMs: number[] = [];
    const legacyTrialMs: number[] = [];
    let semanticEqualCount = 0;
    let legacyEqualCount = 0;

    function runSemanticPass() {
      let equalCount = 0;
      const start = nowMs();
      for (let pass = 0; pass < passes; pass += 1) {
        for (let index = 0; index < itemCount; index += 1) {
          if (
            timelineItemsSemanticEqual(leftItems[index]!, rightItems[index]!)
          ) {
            equalCount += 1;
          }
        }
      }
      return { ms: nowMs() - start, equalCount };
    }

    function runLegacyPass() {
      let equalCount = 0;
      const start = nowMs();
      for (let pass = 0; pass < passes; pass += 1) {
        for (let index = 0; index < itemCount; index += 1) {
          if (
            legacyRemovedProductionTimelineItemsEqual(
              leftItems[index]!,
              rightItems[index]!,
            )
          ) {
            equalCount += 1;
          }
        }
      }
      return { ms: nowMs() - start, equalCount };
    }

    for (let trial = 0; trial < trials; trial += 1) {
      // Alternate order across trials to reduce first-runner bias.
      if (trial % 2 === 0) {
        const semantic = runSemanticPass();
        const legacy = runLegacyPass();
        semanticTrialMs.push(semantic.ms);
        legacyTrialMs.push(legacy.ms);
        semanticEqualCount += semantic.equalCount;
        legacyEqualCount += legacy.equalCount;
      } else {
        const legacy = runLegacyPass();
        const semantic = runSemanticPass();
        semanticTrialMs.push(semantic.ms);
        legacyTrialMs.push(legacy.ms);
        semanticEqualCount += semantic.equalCount;
        legacyEqualCount += legacy.equalCount;
      }
    }

    const expectedEquals = itemCount * passes * trials;
    expect(semanticEqualCount).toBe(expectedEquals);
    expect(legacyEqualCount).toBe(expectedEquals);
    expect(semanticEqualCount).toBe(legacyEqualCount);

    const semanticMedianMs = medianMs(semanticTrialMs);
    const legacyMedianMs = medianMs(legacyTrialMs);
    const report = {
      benchmark: "timeline-items-semantic-equality",
      note: "Bun wall-clock CPU evidence for comparator invocation against the removed production baseline; not an on-device FPS claim. Timed regions exclude fixture construction. Reordered transport is a separate correctness test.",
      itemCount,
      passes,
      trials,
      comparisonsPerTrial: itemCount * passes,
      semanticTrialMs,
      legacyTrialMs,
      semanticMedianMs,
      legacyMedianMs,
      semanticEqualCount,
      legacyEqualCount,
      ratioMedianSemanticOverLegacy:
        legacyMedianMs > 0 ? semanticMedianMs / legacyMedianMs : null,
    };
    console.log(JSON.stringify(report));
    // Do not assert noisy wall time as correctness — only report numbers.
    expect(semanticMedianMs).toBeGreaterThanOrEqual(0);
    expect(legacyMedianMs).toBeGreaterThanOrEqual(0);
  });
});
