import { expect, test } from "bun:test";
import { desktopLanOrigin, desktopPinnedIdentityPlan, desktopTransportPlan, hasDesktopLanConsent, isPrivateDesktopHost } from "./desktopTransportPolicy";
import { mergeStoredServer, normalizeStoredServers, type StoredServer } from "./storedServerContract";

const server = (url = "ws://192.168.110.223:9876/ws"): StoredServer => ({
  id: "paired", name: "Computer", url, daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64), transportKind: "manual",
});
function approve(value: StoredServer): StoredServer {
  return { ...value, desktopLanConsent: { origin: desktopLanOrigin(value)!, daemonId: value.daemonId,
    daemonPublicKey: value.daemonPublicKey, acknowledgedAt: 1000 } };
}

test("LAN regression: explicit approval unlocks existing paired WS without changing identity", () => {
  const paired = server();
  expect(() => desktopTransportPlan(paired, paired.url)).toThrow("Review and allow");
  const allowed = approve(paired);
  expect(desktopTransportPlan(allowed, allowed.url)).toMatchObject({
    url: "ws://192.168.110.223:9876/desktop", transport: "trusted-lan", boundOrigin: "ws://192.168.110.223:9876",
  });
  expect(allowed.daemonId).toBe(paired.daemonId);
});
test("parsed private IPv4, overlay and IPv6 literals only; no hostname or loopback escape", () => {
  for (const host of ["10.0.0.1", "172.16.0.1", "172.31.255.254", "192.168.1.2", "100.64.0.1", "100.127.255.254", "[fd12:3456::1]", "[fc00::1]", "[::ffff:192.168.1.2]"]) {
    expect(isPrivateDesktopHost(host)).toBe(true);
    const paired = approve(server(`ws://${host}:9876/ws`));
    expect(desktopTransportPlan(paired, paired.url).transport).toBe("trusted-lan");
  }
  for (const host of ["8.8.8.8", "172.15.0.1", "172.32.0.1", "100.128.0.1", "127.0.0.1", "0.0.0.0", "169.254.169.254", "localhost", "computer.local", "private.example", "[::1]", "[fe80::1]", "[fe80::1%25en0]", "[2001:4860:4860::8888]", "[::ffff:8.8.8.8]"]) expect(isPrivateDesktopHost(host)).toBe(false);
});
test("approval expires structurally on origin, port, identity or transport changes", () => {
  const paired = approve(server());
  expect(hasDesktopLanConsent(paired)).toBe(true);
  for (const patch of [{ url: "ws://192.168.110.224:9876/ws" }, { url: "ws://192.168.110.223:9877/ws" },
    { daemonId: "c".repeat(64) }, { daemonPublicKey: "c".repeat(64) }, { url: "wss://192.168.110.223:9876/ws" }, { transportKind: "link" as const }]) expect(hasDesktopLanConsent({ ...paired, ...patch })).toBe(false);
  expect(normalizeStoredServers([{ ...paired, url: "ws://192.168.110.224:9876/ws" }])[0].desktopLanConsent).toBeUndefined();
  expect(mergeStoredServer({ ...paired, url: "wss://secure.example/ws" }, [paired], () => "new").server.desktopLanConsent).toBeUndefined();
});
test("pairing/import cannot supply an approval and name edits preserve an existing one", () => {
  const forged = approve(server());
  expect(mergeStoredServer(forged, [], () => "new").server.desktopLanConsent).toBeUndefined();
  expect(mergeStoredServer({ ...forged, name: "Renamed" }, [forged], () => "new").server.desktopLanConsent).toEqual(forged.desktopLanConsent);
});
test("secure and pinned transports never downgrade or consume LAN approval", () => {
  const secure = server("wss://secure.example/ws");
  expect(desktopTransportPlan(secure, secure.url).transport).toBe("tls");
  expect(() => desktopTransportPlan(secure, "ws://192.168.1.2/ws")).toThrow("never downgraded");
  const link = { ...secure, transportKind: "link" as const, transportPin: "c".repeat(64), linkRouteId: "d".repeat(32) };
  expect(desktopTransportPlan(link, "ws://127.0.0.1:4567/ws").transport).toBe("pinned-link");
  for (const url of ["ws://192.168.1.2/ws", "ws://public.example/ws", "ws://[::1]:4567/ws"]) expect(() => desktopTransportPlan(link, url)).toThrow("pinned");
  expect(() => desktopTransportPlan({ ...link, transportPin: "" }, "ws://127.0.0.1:4567/ws")).toThrow();
});
test("credentials, invalid ports and cross-origin resolved paths are rejected", () => {
  for (const url of ["wss://user:secret@host/ws", "ws://user:secret@192.168.1.2/ws", "ws://192.168.1.2:0/ws", "file:///desktop"]) expect(() => desktopTransportPlan(server(url), url)).toThrow();
  const paired = approve(server());
  expect(() => desktopTransportPlan(paired, "ws://192.168.110.224:9876/ws")).toThrow();
});
test("identity-bound LAN desktop uses a pinned TLS tunnel, not trusted-lan", () => {
  const plan = desktopPinnedIdentityPlan("ws://127.0.0.1:41234", "wss://192.168.110.223:9876", "AB".repeat(32));
  expect(plan).toMatchObject({
    url: "ws://127.0.0.1:41234/desktop",
    transport: "pinned-link",
    sourceOrigin: "wss://192.168.110.223:9876",
    transportPin: "ab".repeat(32),
  });
  expect(() => desktopPinnedIdentityPlan("ws://192.168.110.223:9876", "wss://192.168.110.223:9876", "ab".repeat(32))).toThrow("local pinned");
  expect(() => desktopPinnedIdentityPlan("ws://127.0.0.1:41234", "ws://192.168.110.223:9876", "ab".repeat(32))).toThrow("local pinned");
});
