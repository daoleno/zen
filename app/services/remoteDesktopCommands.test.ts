import { test } from "node:test";
import assert from "node:assert/strict";
import { DesktopCommandQueue, type DesktopCommandTarget } from "./remoteDesktopCommands";
import { desktopStart } from "./remoteDesktopModel";

const flush = () => new Promise<void>((resolve) => setImmediate(resolve));

test("start uses only the server-advertised supported source without X11 fallback", () => {
  assert.deepEqual(desktopStart("wayland", true), { type: "start", source: "wayland", control: true });
  assert.deepEqual(desktopStart("x11", false), { type: "start", source: "x11", control: false });
  for (const source of [undefined, null, "", "headless", "other-server"]) assert.equal(desktopStart(source, true), null);
});

function fixture(timeoutMs = 2000) {
  const sent: { generation: string; sequence: number; payload: object }[] = [];
  const acknowledgements: ((accepted: boolean) => void)[] = [];
  const disconnected: string[] = [];
  const failures: string[] = [];
  const target: DesktopCommandTarget = {
    sendCommand: (generation, sequence, payload) => {
      sent.push({ generation, sequence, payload: JSON.parse(payload) });
      return new Promise((resolve) => acknowledgements.push(resolve));
    },
    disconnect: async (generation) => { disconnected.push(generation); },
  };
  const queue = new DesktopCommandQueue(() => target, (reason) => failures.push(reason), timeoutMs);
  return { queue, sent, acknowledgements, disconnected, failures };
}

test("same-tick button/key down and up are ordered independently of React renders", async () => {
  const f = fixture();
  const generation = f.queue.begin();
  const events = [
    { type: "button", code: 1, down: true }, { type: "button", code: 1, down: false },
    { type: "key", code: 97, down: true }, { type: "key", code: 97, down: false },
  ];
  for (const event of events) assert.equal(f.queue.send(event), true);
  for (let i = 0; i < events.length; i++) {
    await flush();
    assert.equal(f.sent.length, i + 1, "at most one unacknowledged call");
    assert.deepEqual(f.sent[i], { generation, sequence: i + 1, payload: events[i] });
    f.acknowledgements[i](true);
  }
  await flush();
  assert.deepEqual(f.failures, []);
  f.queue.stop();
});

test("payload batches are snapshots and are never split or overwritten", async () => {
  const f = fixture();
  f.queue.begin();
  const batch = { type: "batch", events: [{ type: "key", code: 97, down: true }, { type: "key", code: 97, down: false }] };
  const snapshot = structuredClone(batch);
  f.queue.send(batch);
  batch.events.length = 0;
  f.queue.send({ type: "release" });
  await flush();
  assert.deepEqual(f.sent[0].payload, snapshot);
  f.acknowledgements[0](true);
  await flush();
  assert.deepEqual(f.sent[1].payload, { type: "release" });
  f.queue.stop();
});

test("queue count saturation closes ownership instead of dropping a key-up", async () => {
  const f = fixture();
  const generation = f.queue.begin();
  for (let i = 0; i < DesktopCommandQueue.maxPending; i++) assert(f.queue.send({ type: "key", code: 97, down: true }));
  await flush();
  assert.equal(f.queue.send({ type: "key", code: 97, down: false }), false);
  assert.deepEqual(f.disconnected, [generation]);
  assert.equal(f.failures.length, 1);
  f.acknowledgements[0](true);
  await flush();
  assert.equal(f.sent.length, 1);
  assert.equal(f.queue.send({ type: "release" }), false);
});

test("byte saturation and oversized UTF-8 payloads fail closed", async () => {
  for (const oversized of [false, true]) {
    const f = fixture();
    const generation = f.queue.begin();
    if (oversized) assert.equal(f.queue.send({ text: "\u00e9".repeat(4096) }), false);
    else {
      for (let i = 0; i < 4; i++) assert(f.queue.send({ text: "x".repeat(8000) }));
      assert.equal(f.queue.send({ text: "x".repeat(8000) }), false);
    }
    await flush();
    assert.equal(f.sent.length, 0);
    assert.deepEqual(f.disconnected, [generation]);
    assert.equal(f.failures.length, 1);
  }
});

for (const cause of ["stop", "revoke", "background", "server-switch", "disconnect"]) {
  test(`${cause} discards pending input and stale acknowledgements cannot replay on reconnect`, async () => {
    const f = fixture();
    const old = f.queue.begin();
    f.queue.send({ type: "key", code: 97, down: true });
    f.queue.send({ type: "key", code: 97, down: false });
    await flush();
    f.queue.stop();
    assert.equal(f.queue.send({ type: "button", code: 1, down: true }), false);
    const next = f.queue.begin();
    assert.notEqual(next, old);
    f.queue.send({ type: "start", source: "x11", control: false });
    await flush();
    assert.deepEqual(f.sent.map(({ generation, sequence }) => ({ generation, sequence })), [
      { generation: old, sequence: 1 }, { generation: next, sequence: 1 },
    ]);
    f.acknowledgements[0](true);
    await flush();
    assert.equal(f.sent.length, 2);
    f.acknowledgements[1](true);
    await flush();
    assert.deepEqual(f.failures, []);
    assert.deepEqual(f.disconnected, [old]);
    f.queue.stop();
  });
}

test("stop before the first dispatch prevents even a queued native call", async () => {
  const f = fixture();
  f.queue.begin();
  f.queue.send({ type: "button", code: 1, down: true });
  f.queue.stop();
  await flush();
  assert.deepEqual(f.sent, []);
});

test("native rejection ends ownership without delivering the following input", async () => {
  const f = fixture();
  f.queue.begin();
  f.queue.send({ type: "key", code: 97, down: true });
  f.queue.send({ type: "key", code: 97, down: false });
  await flush();
  f.acknowledgements[0](false);
  await flush();
  assert.equal(f.sent.length, 1);
  assert.equal(f.failures.length, 1);
  assert.equal(f.disconnected.length, 1);
});

test("unacknowledged native calls time out and late completion cannot restart input", async () => {
  const f = fixture(10);
  f.queue.begin();
  f.queue.send({ type: "key", code: 97, down: true });
  f.queue.send({ type: "key", code: 97, down: false });
  await flush();
  await new Promise((resolve) => setTimeout(resolve, 30));
  assert.equal(f.failures.length, 1);
  f.acknowledgements[0](true);
  await flush();
  assert.equal(f.sent.length, 1);
  assert.equal(f.queue.currentGeneration, "");
});

test("a thrown native call fails closed and never advances pending input", async () => {
  let disconnected = 0;
  const failures: string[] = [];
  const queue = new DesktopCommandQueue(() => ({
    sendCommand: () => { throw new Error("destroyed native view"); },
    disconnect: async () => { disconnected++; },
  }), (reason) => failures.push(reason));
  queue.begin();
  queue.send({ type: "key", code: 97, down: true });
  queue.send({ type: "key", code: 97, down: false });
  await flush();
  assert.equal(disconnected, 1);
  assert.equal(failures.length, 1);
  assert.equal(queue.currentGeneration, "");
});

test("destroyed native refs cannot prevent lifecycle invalidation", async () => {
  const queue = new DesktopCommandQueue(() => ({
    sendCommand: async () => true,
    disconnect: () => { throw new Error("missing native tag"); },
  }), () => assert.fail("explicit stop is not an input failure"));
  queue.begin();
  queue.send({ type: "key", code: 97, down: true });
  assert.doesNotThrow(() => queue.stop());
  await flush();
  assert.equal(queue.currentGeneration, "");
});
