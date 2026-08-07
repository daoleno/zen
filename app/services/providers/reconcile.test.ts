import { describe, expect, test } from "bun:test";
import { ProviderRequestOwner } from "./reconcile";

describe("ProviderRequestOwner", () => {
  test("rejects stale catalog load after a newer load generation", () => {
    const owner = new ProviderRequestOwner();
    owner.rebind("server-a");
    const first = owner.admitCatalogLoad();
    expect(first.ok).toBe(true);
    if (!first.ok) return;
    const second = owner.admitCatalogLoad();
    expect(second.ok).toBe(true);
    if (!second.ok) return;
    expect(owner.acceptCatalog(first.token, 1)).toBe(false);
    expect(owner.acceptCatalog(second.token, 2)).toBe(true);
  });

  test("server/scope rebind invalidates prior tokens", () => {
    const owner = new ProviderRequestOwner();
    owner.rebind("server-a", "agent-1");
    const admitted = owner.admitCatalogLoad();
    expect(admitted.ok).toBe(true);
    if (!admitted.ok) return;
    expect(owner.rebind("server-b", "agent-1")).toBe(true);
    expect(owner.isCurrent(admitted.token)).toBe(false);
    expect(owner.acceptCatalog(admitted.token, 3)).toBe(false);
  });

  test("catalog mutations are exclusive across channels", () => {
    const owner = new ProviderRequestOwner();
    owner.rebind("server-a");
    const first = owner.admitCatalogMutation();
    expect(first.ok).toBe(true);
    const second = owner.admitCatalogMutation();
    expect(second.ok).toBe(false);
    const loadWhileWrite = owner.admitCatalogLoad();
    expect(loadWhileWrite.ok).toBe(false);
  });

  test("ambiguous or uncertain settlement locks writes until refresh", () => {
    const owner = new ProviderRequestOwner();
    owner.rebind("server-a");
    const mutation = owner.admitCatalogMutation();
    expect(mutation.ok).toBe(true);
    if (!mutation.ok) return;
    owner.settleCatalogMutation(mutation.token, {
      refreshRequired: true,
      revision: 4,
    });
    expect(owner.catalogRequiresRefresh()).toBe(true);
    expect(owner.admitCatalogMutation().ok).toBe(false);
    const refresh = owner.admitCatalogLoad();
    expect(refresh.ok).toBe(true);
    if (!refresh.ok) return;
    expect(owner.acceptCatalog(refresh.token, 4)).toBe(true);
    expect(owner.catalogRequiresRefresh()).toBe(false);
    expect(owner.admitCatalogMutation().ok).toBe(true);
  });

  test("activation is exclusive and ambiguous lock requires same-session refresh", () => {
    const owner = new ProviderRequestOwner();
    owner.rebind("server-a", "tmux:@1");
    const first = owner.admitActivation();
    expect(first.ok).toBe(true);
    expect(owner.admitActivation().ok).toBe(false);
    expect(owner.admitSessionLoad().ok).toBe(false);
    if (!first.ok) return;
    owner.settleActivation(first.token, { refreshRequired: true });
    expect(owner.activationRequiresRefresh()).toBe(true);
    expect(owner.admitActivation().ok).toBe(false);
    const refresh = owner.admitSessionLoad();
    expect(refresh.ok).toBe(true);
    if (!refresh.ok) return;
    expect(owner.acceptSession(refresh.token)).toBe(true);
    expect(owner.activationRequiresRefresh()).toBe(false);
    expect(owner.admitActivation().ok).toBe(true);
  });
});
