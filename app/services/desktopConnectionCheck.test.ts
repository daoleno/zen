import { expect, test } from "bun:test";
import nacl from "tweetnacl";
import { bytesToHex } from "./protocolCrypto";
import {
  DesktopConnectionUnavailable,
  DesktopPreflightError,
  desktopPreflightError,
  fetchDesktopCapability,
  verifyDesktopServer,
  type DesktopCapability,
  type DesktopProofDependencies,
} from "./desktopConnectionCheck";

const seed = new Uint8Array(32).fill(7);
const pair = nacl.sign.keyPair.fromSeed(seed);
const server = { daemonId: "a".repeat(64), daemonPublicKey: bytesToHex(pair.publicKey) };
const pin = "ab".repeat(32);

function signCapability(identityTls: boolean, transportPin = pin) {
  const payload = new TextEncoder().encode([
    server.daemonId,
    server.daemonPublicKey,
    identityTls ? transportPin : "",
    identityTls ? "true" : "false",
  ].join("\n"));
  const domain = new TextEncoder().encode("zen-desktop-capability-v1\u0000");
  const signed = new Uint8Array(domain.length + payload.length);
  signed.set(domain);
  signed.set(payload, domain.length);
  return bytesToHex(nacl.sign.detached(signed, pair.secretKey));
}

function setup(change: (body: Record<string, unknown>, index: number) => void = () => {}) {
  const calls: { url: string; init: Pick<RequestInit, "headers" | "signal" | "redirect"> }[] = [];
  let authorizations = 0;
  const dependencies: DesktopProofDependencies = {
    authorization: async (purpose) => `fresh-${purpose}-${++authorizations}`,
    verify: (input) => input.signatureHex === input.purpose && input.nonceHex === "c".repeat(32),
    fetch: async (url, init) => {
      const index = calls.length; calls.push({ url, init });
      const purpose = index === 0 ? "zen-health" : index === 1 ? "zen-probe" : "zen-desktop-capability";
      const body: Record<string, unknown> = {
        ...server, daemon_id: server.daemonId, daemon_public_key: server.daemonPublicKey,
        assertion_timestamp: new Date().toISOString(), assertion_nonce: "c".repeat(32),
        assertion_signature: purpose, ok: true,
      };
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
  expect(f.calls[1].init.headers).toEqual({ Authorization: "fresh-zen-probe-1" });
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
    { ok: true, status: 200, redirected: false, url: "", body: new Response("x".repeat(16385)).body },
  ]) await expect(verifyDesktopServer(server, "wss://host/desktop", { ...f.dependencies, fetch: async () => response })).rejects.toThrow();
  const controller = new AbortController(); controller.abort();
  await expect(verifyDesktopServer(server, "wss://host/desktop", f.dependencies, controller.signal)).rejects.toThrow("cancelled");
  expect(f.calls).toHaveLength(0);
});

test("reboot transport failures are retryable without bypassing identity or revocation", async () => {
  const f = setup();
  await expect(verifyDesktopServer(server, "wss://host/desktop", { ...f.dependencies, fetch: async () => { throw new TypeError("offline"); } })).rejects.toBeInstanceOf(DesktopConnectionUnavailable);
  expect(f.authorizations()).toBe(0);
  const wrong = setup((body) => { body.daemon_id = "other"; });
  try {
    await verifyDesktopServer(server, "wss://host/desktop", wrong.dependencies);
    throw new Error("wrong identity accepted");
  } catch (error) {
    expect(error).not.toBeInstanceOf(DesktopConnectionUnavailable);
    expect(String(error)).toContain("identity");
  }
});

test("capability preflight distinguishes scope, identity TLS, host setup and revocation", async () => {
  const capability = (fields: Partial<DesktopCapability>): DesktopCapability => ({
    deviceTrust: "paired_unattended", scopeVersion: 1, requestEncrypted: false, identityTls: true,
    identityServerName: "zen-desktop.invalid", transportPin: pin, hostStatus: "ready", hostBroker: true,
    currentSession: true, lockLogin: true, surface: "locked", session: "locked", unattended: true, reason: "", recovery: "", ...fields,
  });
  expect(desktopPreflightError(capability({}))).toBeNull();
  expect(desktopPreflightError(capability({ unattended: false, scopeVersion: 0, reason: "desktop_scope_required" }) )?.code).toBe("desktop_scope_required");
  expect(desktopPreflightError(capability({ unattended: false, identityTls: false, hostBroker: false, reason: "desktop_tls_required" }) )?.code).toBe("desktop_tls_required");
  expect(desktopPreflightError(capability({ unattended: false, hostBroker: false, currentSession: false, reason: "host_setup_required" }) )?.code).toBe("host_setup_required");
  expect(desktopPreflightError(capability({ unattended: true, hostBroker: false, currentSession: true, lockLogin: false }))).toBeNull();
  const proof: DesktopProofDependencies = {
    authorization: async () => "token",
    verify: (input) => input.signatureHex === input.purpose && input.nonceHex === "c".repeat(32),
    fetch: async (url) => {
      const body = {
        ...server, daemon_id: server.daemonId, daemon_public_key: server.daemonPublicKey,
        assertion_timestamp: new Date().toISOString(), assertion_nonce: "c".repeat(32),
        assertion_signature: "zen-desktop-capability", ok: true,
        device_trust: "legacy_terminal", desktop_scope_version: 0,
        transport: { request_encrypted: false, identity_tls: true, transport_pin: pin, forwarded_headers_trusted: false },
        host: { status: "setup_required", broker: false },
        connect: { unattended: false, reason: "desktop_scope_required", recovery: "scan once" },
        capability_signature: signCapability(true),
      };
      return { ok: true, status: 200, url, redirected: false, body: new Response(JSON.stringify(body)).body };
    },
  };
  const fetched = await fetchDesktopCapability(server, "ws://192.168.110.223:9876/desktop", proof);
  expect(fetched.scopeVersion).toBe(0);
  expect(fetched.identityTls).toBe(true);
  expect(fetched.transportPin).toBe(pin);
  expect(desktopPreflightError(fetched)?.code).toBe("desktop_scope_required");
  await expect(fetchDesktopCapability(server, "wss://host/desktop", {
    ...proof,
    fetch: async () => ({ ok: false, status: 401, redirected: false, url: "", body: null }),
  })).rejects.toBeInstanceOf(DesktopPreflightError);
});

test("forged capability pin signatures fail closed", async () => {
  await expect(fetchDesktopCapability(server, "ws://192.168.1.2:9876/desktop", {
    authorization: async () => "token",
    verify: () => true,
    fetch: async (url) => {
      const body = {
        ...server, daemon_id: server.daemonId, daemon_public_key: server.daemonPublicKey,
        assertion_timestamp: new Date().toISOString(), assertion_nonce: "c".repeat(32),
        assertion_signature: "zen-desktop-capability",
        transport: { identity_tls: true, transport_pin: pin },
        host: {}, connect: {}, capability_signature: signCapability(true, "cd".repeat(32)),
      };
      return { ok: true, status: 200, url, redirected: false, body: new Response(JSON.stringify(body)).body };
    },
  })).rejects.toThrow("capability proof");
});
