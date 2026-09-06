import { expect, test } from "bun:test";
import { buildZenTimeline } from "./InterfaceTimelineModel";

test("historical heartbeat text remains one ordinary message without synthesized state", () => {
  const body = "Heartbeat wake:\nagent_id: historical-worker\nstatus: running\n\nHistorical instruction";
  const items = buildZenTimeline([
    { id: "historical", kind: "user_message", seq: 1, body, timestamp: "2026-09-05T00:00:00Z" },
  ]);
  expect(items).toHaveLength(1);
  expect(items[0]).toMatchObject({ type: "message", id: "historical", role: "user", body });
  expect(items[0]).not.toHaveProperty("heartbeatWake");
});
