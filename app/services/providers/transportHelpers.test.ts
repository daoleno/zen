import { describe, expect, test } from "bun:test";
import {
  admitCorrelatedProviderReply,
  createSessionBodyOmitsProfileId,
  reconcileProviderMutationReply,
} from "./transportHelpers";

describe("Provider transport helpers", () => {
  test("request correlation rejects wrong server or stale request id", () => {
    expect(
      admitCorrelatedProviderReply({
        expectedServerId: "s1",
        expectedRequestId: "r1",
        serverId: "s1",
        requestId: "r1",
      }),
    ).toEqual({ ok: true });
    expect(
      admitCorrelatedProviderReply({
        expectedServerId: "s1",
        expectedRequestId: "r1",
        serverId: "s2",
        requestId: "r1",
      }).ok,
    ).toBe(false);
    expect(
      admitCorrelatedProviderReply({
        expectedServerId: "s1",
        expectedRequestId: "r1",
        serverId: "s1",
        requestId: "r-stale",
      }).ok,
    ).toBe(false);
  });

  test("ambiguous persistence fails closed for Provider mutations", () => {
    expect(() =>
      reconcileProviderMutationReply({
        persistence_outcome: "unknown",
        persistence_durable: true,
      }),
    ).toThrow(/ambiguous|Persistence/i);
    const ok = reconcileProviderMutationReply({
      persistence_outcome: "applied",
      persistence_durable: true,
    });
    expect(ok.classification).toBe("applied_durable");
  });

  test("New Terminal create_session omits profile_id", () => {
    expect(
      createSessionBodyOmitsProfileId({
        type: "create_session",
        request_id: "r1",
        command: "codex",
      }),
    ).toBe(true);
    expect(
      createSessionBodyOmitsProfileId({
        type: "create_session",
        request_id: "r1",
        command: "codex",
        profile_id: "legacy",
      }),
    ).toBe(false);
  });
});
