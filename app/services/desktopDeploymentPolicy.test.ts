import { expect, test } from "bun:test";
import { desktopPreflightError, type DesktopCapability } from "./desktopConnectionCheck";
import { desktopTransportPlan } from "./desktopTransportPolicy";

function capability(overrides: Partial<DesktopCapability> = {}): DesktopCapability {
  return {
    deviceTrust: "paired_unattended", scopeVersion: 1, requestEncrypted: false, trustedIngress: false,
    identityTls: false, identityServerName: "", transportPin: "", hostStatus: "ready", hostBroker: true,
    currentSession: true, lockLogin: true, surface: "desktop", session: "kde", unattended: true,
    reason: "", recovery: "", moonlight: null, ...overrides,
  };
}

test("trusted LAN HTTP deployment is unattended without a Zen certificate", () => {
  // The server marked the exact request as arriving through the operator's
  // trusted deployment; no Zen TLS or local tunnel is required.
  expect(desktopPreflightError(capability({ trustedIngress: true }))).toBeNull();
  // The same capability without the server-side deployment fact is refused.
  expect(desktopPreflightError(capability({ trustedIngress: false, reason: "desktop_tls_required" }))?.code)
    .toBe("desktop_tls_required");
});

test("trusted deployment plan does not require the legacy LAN consent switch", () => {
  const server = {
    id: "owned", name: "Owned", url: "ws://192.168.1.50:9876/ws",
    daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64), transportKind: "lan",
  } as never;
  const plan = desktopTransportPlan(server, "ws://192.168.1.50:9876/ws", { trustedIngress: true });
  expect(plan.transport).toBe("trusted-lan");
  expect(plan.url).toContain("/desktop");
});
