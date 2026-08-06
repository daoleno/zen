import { describe, expect, test } from "bun:test";
import {
  createFakeInterfaceDevicePerfClock,
  createInterfaceDevicePerfScenarioController,
  firstAssistantEventId,
  interfaceDevicePerfScenarioFingerprint,
  interfaceDevicePerfStreamDurationMs,
  interfaceDevicePerfStreamTargetChars,
  prepareInterfaceDevicePerfScenario,
  resolveInterfaceDevicePerfScenario,
  INTERFACE_DEVICE_PERF_SCENARIOS,
} from "./interfaceDevicePerformanceScenarios";
import {
  createStreamingMarkdownFrameController,
  type AnimationFrameHandle,
  type AnimationFrameScheduler,
} from "./streamingMarkdownFrameController";

function createManualScheduler() {
  type Entry = {
    id: number;
    callback: () => void;
    cancelled: boolean;
  };
  const queue: Entry[] = [];
  let nextId = 1;
  const scheduler: AnimationFrameScheduler = {
    request(callback: () => void): AnimationFrameHandle {
      const entry = { id: nextId, callback, cancelled: false };
      nextId += 1;
      queue.push(entry);
      return entry.id;
    },
    cancel(handle: AnimationFrameHandle) {
      const id = handle as number;
      for (const entry of queue) {
        if (entry.id === id) {
          entry.cancelled = true;
        }
      }
    },
  };
  return {
    scheduler,
    flushAll() {
      while (queue.length > 0) {
        const entry = queue.shift()!;
        if (!entry.cancelled) {
          entry.callback();
        }
      }
    },
  };
}

describe("interfaceDevicePerformanceScenarios", () => {
  test("resolves only known scenario ids and defaults safely", () => {
    expect(INTERFACE_DEVICE_PERF_SCENARIOS).toEqual([
      "50-short",
      "500-mixed",
      "stream-8k",
      "detached-append",
      "detached-prepend",
    ]);
    expect(resolveInterfaceDevicePerfScenario("500-mixed")).toBe("500-mixed");
    expect(resolveInterfaceDevicePerfScenario(["stream-8k"])).toBe("stream-8k");
    expect(resolveInterfaceDevicePerfScenario("nope")).toBe("50-short");
    expect(resolveInterfaceDevicePerfScenario(undefined)).toBe("50-short");
  });

  test("50-short and 500-mixed fixtures are deterministic and content-owned up front", () => {
    const shortA = prepareInterfaceDevicePerfScenario("50-short");
    const shortB = prepareInterfaceDevicePerfScenario("50-short");
    expect(shortA.initialEvents.length).toBe(50);
    expect(shortA.steps).toEqual([]);
    expect(shortA.startsDetached).toBe(false);
    expect(shortA.initialEvents.map((e) => e.id)).toEqual(
      shortB.initialEvents.map((e) => e.id),
    );

    const mixed = prepareInterfaceDevicePerfScenario("500-mixed");
    expect(mixed.initialEvents.length).toBeGreaterThanOrEqual(500);
    const toolCount = mixed.initialEvents.filter((e) => e.kind === "tool").length;
    expect(toolCount).toBeGreaterThanOrEqual(20);
    expect(mixed.startsDetached).toBe(false);
    expect(mixed.steps).toEqual([]);
  });

  test("stream-8k grows to about 8k and ends with an exact terminal body", () => {
    const prepared = prepareInterfaceDevicePerfScenario("stream-8k");
    const assistantId = prepared.streamEventId;
    expect(assistantId).toBe(firstAssistantEventId(prepared.initialEvents));
    if (!assistantId) {
      throw new Error("stream scenario must own its growing event id");
    }
    // Finite growth: 128-char chunks to 8k ⇒ ≤64 steps. Never unbounded.
    expect(prepared.steps.length).toBeGreaterThan(10);
    expect(prepared.steps.length).toBeLessThanOrEqual(64);
    const last = prepared.steps[prepared.steps.length - 1]!;
    expect(last.dueAtMs).toBe(interfaceDevicePerfStreamDurationMs());
    for (let index = 1; index < prepared.steps.length; index += 1) {
      expect(prepared.steps[index]!.dueAtMs).toBeGreaterThan(
        prepared.steps[index - 1]!.dueAtMs,
      );
      const previousBody =
        prepared.steps[index - 1]!.events.find((e) => e.id === assistantId)
          ?.body ?? "";
      const nextBody =
        prepared.steps[index]!.events.find((e) => e.id === assistantId)?.body ??
        "";
      expect(nextBody.length).toBeGreaterThan(previousBody.length);
    }
    const assistant = last.events.find((e) => e.id === assistantId);
    expect(assistant?.partial).toBe(false);
    expect(assistant?.status).toBe("done");
    expect((assistant?.body?.length ?? 0) >= interfaceDevicePerfStreamTargetChars()).toBe(
      true,
    );
    expect(assistant?.body?.length).toBe(interfaceDevicePerfStreamTargetChars());

    const fake = createFakeInterfaceDevicePerfClock(0);
    const controller = createInterfaceDevicePerfScenarioController({
      prepared,
      clock: fake.clock,
    });
    expect(controller.getState().streamChars).toBe(0);
    fake.set(last.dueAtMs);
    expect(controller.tick()).toBe(prepared.steps.length);
    expect(controller.isDone()).toBe(true);
    expect(controller.getState().streamChars).toBeGreaterThanOrEqual(
      interfaceDevicePerfStreamTargetChars(),
    );
    expect(controller.getState().label.startsWith("stream-terminal")).toBe(true);
  });

  test("detached append progresses while every prepended page becomes the true oldest", () => {
    const append = prepareInterfaceDevicePerfScenario("detached-append");
    expect(append.startsDetached).toBe(true);

    const prepend = prepareInterfaceDevicePerfScenario("detached-prepend");
    expect(prepend.startsDetached).toBe(true);
    let previousOldestSeq = Math.min(
      ...prepend.initialEvents.map((event) => event.seq),
    );
    for (let page = 0; page < prepend.steps.length; page += 1) {
      const events = prepend.steps[page]!.events;
      const oldestSeq = Math.min(...events.map((event) => event.seq));
      expect(oldestSeq).toBeLessThan(previousOldestSeq);
      expect(events[0]?.id.startsWith(`hist-p${page}-`)).toBe(true);
      previousOldestSeq = oldestSeq;
    }

    const fake = createFakeInterfaceDevicePerfClock(0);
    const controller = createInterfaceDevicePerfScenarioController({
      prepared: append,
      clock: fake.clock,
    });
    fake.advance(32);
    controller.tick();
    expect(controller.getState().events.length).toBe(
      append.initialEvents.length + 1,
    );
  });

  test("advanceOne ignores clock and fingerprints stay content-free", () => {
    const prepared = prepareInterfaceDevicePerfScenario("stream-8k");
    const fake = createFakeInterfaceDevicePerfClock(0);
    const controller = createInterfaceDevicePerfScenarioController({
      prepared,
      clock: fake.clock,
    });
    expect(controller.advanceOne()).toBe(true);
    const fingerprint = interfaceDevicePerfScenarioFingerprint(
      controller.getState(),
    );
    const encoded = JSON.stringify(fingerprint);
    expect(encoded).not.toContain("abcdefghijklmnopqrstuvwxyz");
    expect(encoded).not.toContain("stream-rev=");
    expect(fingerprint).toMatchObject({
      id: "stream-8k",
      revision: 1,
      done: false,
    });
    expect("events" in fingerprint).toBe(false);
  });

  test("stream terminal flush remains exact through the production coalescer", () => {
    const prepared = prepareInterfaceDevicePerfScenario("stream-8k");
    const assistantId = firstAssistantEventId(prepared.initialEvents);
    const published: string[] = [];
    const manual = createManualScheduler();
    const controller = createStreamingMarkdownFrameController({
      scheduler: manual.scheduler,
      onPublish: (value) => published.push(value),
    });

    const mid = prepared.steps[Math.floor(prepared.steps.length / 2)]!;
    const midBody = mid.events.find((e) => e.id === assistantId)!.body!;
    controller.accept(midBody, true);
    expect(published).toEqual([]);
    manual.flushAll();
    expect(published).toEqual([midBody]);

    const terminal = prepared.steps[prepared.steps.length - 1]!;
    const terminalBody = terminal.events.find(
      (e) => e.id === assistantId,
    )!.body!;
    controller.accept(`${midBody} stale-coalesced`, true);
    controller.accept(terminalBody, false);
    expect(published).toEqual([midBody, terminalBody]);
    manual.flushAll();
    expect(published).toEqual([midBody, terminalBody]);
  });
});
