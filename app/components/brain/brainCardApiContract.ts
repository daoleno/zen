import assert from "node:assert/strict";
import { projectZenTimeline } from "../terminal/projectZenTimeline";
import { attachBrainWorkEventActions } from "../terminal/InterfaceTimelineModel";
import { brainCurrentWorkLifecycle, brainWorkEventLifecycle } from "./brainWorkEventPresentation";
import { brainWorkEventCardModel } from "./brainWorkEventCardModel";

// Executed by the Go BDD with real emitted, persisted and API-projected data.
const payload = JSON.parse(await Bun.stdin.text());
let cache: ReturnType<typeof projectZenTimeline>["cache"] | null = null;
for (const phase of ["live", "accepted", "reconnect"] as const) {
  const wire = payload[phase];
  const projection = projectZenTimeline(wire.events, phase === "reconnect" ? null : cache);
  cache = projection.cache;
  const items = attachBrainWorkEventActions(projection.items, undefined, undefined, wire.current_work);
  const cards = items.filter(item => item.type === "brain-work-event");
  assert.equal(cards.length, 1, `${phase}: exactly one lineage card`);
  const card = cards[0];
  assert.equal(card.event.work_id, payload.work_id);
  assert.ok(!card.event.summary.includes("\ufffd"));
  assert.equal(brainWorkEventCardModel(card.event).facts.some(f => /Offline tests|Replay rows/.test(f)), false);
  const lifecycle = card.currentWork ? brainCurrentWorkLifecycle(card.currentWork, card.event) : brainWorkEventLifecycle(card.event);
  assert.equal(lifecycle.lifecycle, phase === "live" ? "reviewing" : "done");
  assert.equal(wire.events.find((event: { id: string }) => event.id === "quoted").body, payload.quoted);
  assert.equal(wire.events.find((event: { id: string }) => event.id === "assistant").body, "整理完成，原始证据保持不变。");
  assert.equal(items.filter(item => item.type === "message").length, 3);
  assert.equal(wire.events.some((event: { id: string }) => ["internal", "internal-duplicate", "legacy-on-disk"].includes(event.id)), false);
}
console.log("Brain card API/mobile live, accepted and reconnect contract passed");
