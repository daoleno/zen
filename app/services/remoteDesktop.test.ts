import { expect, mock, test } from "bun:test";
import { createRequire } from "node:module";

/**
 * Real prepareDesktopConnection service tests: only network/secure-store/tunnel
 * IO are mocked. Verifies that a directly reachable engine is selected even
 * when the control channel uses the identity-bound pinned tunnel, and that
 * relay-only link routes stay on the old plan.
 */
const root = new URL("..", import.meta.url).pathname.replace(/\/$/, "");
const require = createRequire(root + "/package.json");

let tunnelCalls = 0;
let capability: Record<string, unknown> = {};
const server = {
  id: "owned", name: "Owned", url: "ws://192.168.1.50:9876/ws",
  daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64),
  transportKind: "identity",
};

mock.module(root + "/services/auth.ts", () => ({
  buildAuthorizationHeader: async () => "authorization",
  verifyDaemonAssertion: () => true,
}));
mock.module(root + "/services/pinnedTransport.ts", () => ({
  resolveStoredServerURL: async (value: { url: string }) => value.url,
}));
mock.module(root + "/services/desktopTransportPolicy.ts", () => ({
  desktopLanOrigin: (value: { url: string; transportKind?: string }) =>
    value.transportKind !== "link" && value.url.startsWith("ws://192.168.") ? "http://192.168.1.50:9876" : null,
  desktopPinnedIdentityPlan: () => ({ transport: "pinned-link", url: "ws://127.0.0.1:1234" }),
  desktopTransportPlan: () => ({ transport: "trusted-lan", url: "ws://192.168.1.50:9876/desktop" }),
}));
mock.module(root + "/services/desktopConnectionCheck.ts", () => ({
  DesktopConnectionUnavailable: class extends Error {},
  DesktopPreflightError: class extends Error {
    code = "desktop_preflight";
    recovery = "";
  },
  verifyDesktopServer: async () => {},
  fetchDesktopCapability: async () => capability,
  desktopPreflightError: () => null,
}));
mock.module(root + "/modules/zen-link-transport/src/index.ts", () => ({
  startPinnedTunnel: async () => {
    tunnelCalls++;
    return { port: 1234 };
  },
}));
mock.module("expo/fetch", () => ({ fetch: async () => ({}) }));

const { prepareDesktopConnection } = await import("./remoteDesktop");

const baseCapability = {
  deviceTrust: "paired_unattended", scopeVersion: 1, requestEncrypted: false,
  identityTls: true, identityServerName: "desktop.zen", transportPin: "cd".repeat(32),
  hostStatus: "ready", hostBroker: true, currentSession: true, lockLogin: true,
  surface: "desktop", session: "kde", unattended: true, reason: "", recovery: "",
};

test("direct engine host is selected even when the control channel is identity-pinned", async () => {
  tunnelCalls = 0;
  capability = { ...baseCapability, moonlight: {
    available: true, httpPort: 47989, httpsPort: 47984, appId: 1,
    hostKey: "zen-host", identityKey: "device-1",
  } };
  const plan = JSON.parse(await prepareDesktopConnection(server as never, "gen-1"));
  expect(plan.transport).toBe("moonlight");
  expect(plan.moonlight.host).toBe("192.168.1.50");
  expect(tunnelCalls).toBe(0);
});

test("relay-only link routes are explicit and stay off the native engine", async () => {
  tunnelCalls = 0;
  capability = { ...baseCapability, moonlight: {
    available: true, httpPort: 47989, httpsPort: 47984, appId: 1,
    hostKey: "zen-host", identityKey: "device-1",
  } };
  await expect(prepareDesktopConnection(
    { ...server, transportKind: "link", url: "wss://relay.zen.example/ws" } as never, "gen-2",
  )).rejects.toThrow("Unattended desktop cannot use unencrypted LAN transport.");
  expect(tunnelCalls).toBe(0);
});

test("legacy servers keep the pinned-link control plan", async () => {
  tunnelCalls = 0;
  capability = { ...baseCapability };
  const plan = JSON.parse(await prepareDesktopConnection(server as never, "gen-3"));
  expect(plan.transport).toBe("pinned-link");
  expect(tunnelCalls).toBe(1);
});
