import { expect, test } from "bun:test";
import { verifyDesktopServer, type DesktopProofDependencies } from "./desktopConnectionCheck";

const server = { daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64) };
function setup(change: (body: Record<string, unknown>, index: number) => void = () => {}) {
  const calls: { url: string; init: Pick<RequestInit, "headers" | "signal" | "redirect"> }[] = [];
  let authorizations = 0;
  const dependencies: DesktopProofDependencies = {
    authorization: async () => `fresh-probe-${++authorizations}`,
    verify: (input) => input.signatureHex === input.purpose && input.nonceHex === "c".repeat(32),
    fetch: async (url, init) => {
      const index = calls.length; calls.push({ url, init });
      const body = { ...server, daemon_id: server.daemonId, daemon_public_key: server.daemonPublicKey,
        assertion_timestamp: new Date().toISOString(), assertion_nonce: "c".repeat(32), assertion_signature: index === 0 ? "zen-health" : "zen-probe", ok: true };
      change(body, index);
      return { ok: true, status: 200, url, redirected: false, body: new Response(JSON.stringify(body)).body };
    },
  };
  return { dependencies, calls, authorizations: () => authorizations };
}
test("proves paired daemon before authenticating device, with fresh purpose-specific probe", async () => {
  const f = setup();
  await verifyDesktopServer(server, "ws://192.168.1.2:9876/desktop", f.dependencies);
  expect(f.calls.map((call) => call.url)).toEqual(["http://192.168.1.2:9876/health", "http://192.168.1.2:9876/auth-check"]);
  expect(f.calls[0].init.headers).toBeUndefined();
  expect(f.calls[1].init.headers).toEqual({ Authorization: "fresh-probe-1" });
  expect(f.calls.every((call) => call.init.redirect === "error")).toBe(true);
});
test("wrong identity, stale assertions and forged signatures fail before desktop admission", async () => {
  for (const field of ["daemon_id", "daemon_public_key", "assertion_timestamp", "assertion_signature"]) {
    const f = setup((body) => { body[field] = "invalid"; });
    await expect(verifyDesktopServer(server, "wss://host/desktop", f.dependencies)).rejects.toThrow("identity");
    expect(f.authorizations()).toBe(0);
  }
  const f = setup((body, index) => { if (index === 1) body.ok = false; });
  await expect(verifyDesktopServer(server, "wss://host/desktop", f.dependencies)).rejects.toThrow("identity");
});
test("redirects, oversized proofs, revocation and cancellation fail closed", async () => {
  const f = setup();
  for (const response of [
    { ok: true, status: 200, redirected: true, url: "http://public.example/health", body: null },
    { ok: false, status: 401, redirected: false, url: "", body: null },
    { ok: true, status: 200, redirected: false, url: "", body: new Response("x".repeat(8193)).body },
  ]) await expect(verifyDesktopServer(server, "wss://host/desktop", { ...f.dependencies, fetch: async () => response })).rejects.toThrow();
  const controller = new AbortController(); controller.abort();
  await expect(verifyDesktopServer(server, "wss://host/desktop", f.dependencies, controller.signal)).rejects.toThrow("cancelled");
  expect(f.calls).toHaveLength(0);
});
