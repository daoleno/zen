import { describe, expect, test } from "bun:test";
import { ProviderRequestOwner } from "./reconcile";
import { futureDefaultRows } from "./presentation";
import {
  idleProvidersInFlightFlags,
  providersScreenAfterBlur,
} from "./screenLifecycle";
import type { ProvidersSnapshot } from "./types";

function snapshot(revision = 3): ProvidersSnapshot {
  return {
    revision,
    connections: [
      {
        id: "c1",
        name: "DeepSeek",
        clients: ["codex", "claude"],
        credential_ready: true,
        advanced: false,
      },
      {
        id: "c2",
        name: "OpenRouter",
        clients: ["codex"],
        credential_ready: true,
        advanced: false,
      },
      {
        id: "c3",
        name: "Needs key",
        clients: ["claude"],
        credential_ready: false,
        advanced: false,
      },
    ],
    defaults: { codex: { connection_id: "c1" } },
    presets: [],
    models: {},
  };
}

describe("Providers screen blur lifecycle", () => {
  test("blur during catalog load resets flags, keeps catalog, ignores stale reply", () => {
    const owner = new ProviderRequestOwner();
    owner.rebind("srv-a");
    const admission = owner.admitCatalogLoad();
    expect(admission.ok).toBe(true);
    if (!admission.ok) return;

    const catalog = snapshot(2);
    const midLoad = {
      flags: { loading: true, refreshing: true, mutating: false },
      catalog,
    };

    owner.invalidateAll();
    const after = providersScreenAfterBlur(midLoad);

    expect(after.flags).toEqual(idleProvidersInFlightFlags());
    expect(after.catalog).toBe(catalog);
    expect(owner.acceptCatalog(admission.token, 4)).toBe(false);

    const refocus = owner.admitCatalogLoad();
    expect(refocus.ok).toBe(true);
    if (!refocus.ok) return;
    expect(owner.acceptCatalog(refocus.token, 4)).toBe(true);
  });

  test("blur during mutation resets flags; stale settlement is ignored after refocus", () => {
    const owner = new ProviderRequestOwner();
    owner.rebind("srv-a");
    const admission = owner.admitCatalogMutation();
    expect(admission.ok).toBe(true);
    if (!admission.ok) return;

    const catalog = snapshot(5);
    const midMutation = {
      flags: { loading: false, refreshing: false, mutating: true },
      catalog,
    };

    owner.invalidateAll();
    const after = providersScreenAfterBlur(midMutation);

    expect(after.flags).toEqual({
      loading: false,
      refreshing: false,
      mutating: false,
    });
    expect(after.catalog).toEqual(catalog);
    expect(owner.isCurrent(admission.token)).toBe(false);

    owner.settleCatalogMutation(admission.token, {
      refreshRequired: false,
      revision: 6,
    });
    expect(owner.catalogRequiresRefresh()).toBe(false);

    const next = owner.admitCatalogMutation();
    expect(next.ok).toBe(true);
  });
});

describe("Future default rows", () => {
  test("one Codex/Claude surface lists ready options and marks the current choice", () => {
    const rows = futureDefaultRows(snapshot());
    expect(rows.map((row) => row.client)).toEqual(["codex", "claude"]);
    const codex = rows[0];
    expect(codex.currentConnectionId).toBe("c1");
    expect(codex.currentConnectionName).toBe("DeepSeek");
    expect(codex.options.map((option) => option.connectionId)).toEqual([
      "c1",
      "c2",
    ]);
    expect(
      codex.options.find((option) => option.connectionId === "c1")?.selected,
    ).toBe(true);

    const claude = rows[1];
    expect(claude.currentConnectionId).toBeNull();
    expect(claude.options.map((option) => option.connectionId)).toEqual(["c1"]);
    expect(
      claude.options.every((option) => option.connectionId !== "c3"),
    ).toBe(true);
  });
});
