/**
 * Child-isolated harness for the real prepareDesktopConnection service.
 * Only network/secure-store/tunnel IO are mocked; the capability proof,
 * preflight parse and transport policy are the production implementations with
 * real Ed25519 signatures. Run via services/remoteDesktop.isolated.test.ts.
 */
import { createRequire } from "node:module";
import { strict as assert } from "node:assert";

const root = new URL("..", import.meta.url).pathname.replace(/\/$/, "");
const require = createRequire(root + "/package.json");
const nacl = require("tweetnacl");

const seed = new Uint8Array(32).fill(23);
const pair = nacl.sign.keyPair.fromSeed(seed);
const publicKey = Buffer.from(pair.publicKey).toString("hex");
const daemonId = "a".repeat(64);
const pin = "bc".repeat(32);
const md = require(root + "/services/deviceAuthContract");
const sign = (bytes: Uint8Array) => Buffer.from(nacl.sign.detached(bytes, pair.secretKey)).toString("hex");

const timestamp = new Date().toISOString();
const nonce = "d".repeat(32);
const assertion = (purpose: string) => sign(md.buildServerAssertionPayload(purpose, daemonId, timestamp, nonce));
const bindingOf = (block: Record<string, unknown>, trustedIngress = false) => [
  String(block.available === true), String(block.http_port), String(block.https_port),
  String(block.app_id), String(block.host_key), String(block.identity_key), String(block.admission ?? ""),
  String(trustedIngress),
].join("\n");
const signDomain = (domain: string, fields: string[]) =>
  sign(Buffer.concat([Buffer.from(domain), Buffer.from(fields.join("\n"))]));
const v1Fields = [daemonId, publicKey, pin, "true"];
const moonlight = { available: true, http_port: 47989, https_port: 47984, app_id: 1, host_key: "zen-host", identity_key: "device-1", admission: "verified" };
const v2Fields = [...v1Fields, bindingOf(moonlight, true)];

let scenario: "moonlight" | "legacy" | "trusted-lan" = "moonlight";
let tunnelCalls = 0;
const server = {
  id: "owned", name: "Owned", url: "ws://192.168.1.50:9876/ws",
  daemonId, daemonPublicKey: publicKey, transportKind: "identity",
};

const bodies = () => {
  const base = {
    ok: true, daemon_id: daemonId, daemon_public_key: publicKey,
    assertion_timestamp: timestamp, assertion_nonce: nonce,
    device_trust: "paired_unattended", desktop_scope_version: 1,
    transport: { request_encrypted: false, identity_tls: true, transport_pin: pin, trusted_ingress: false },
    host: { status: "ready", broker: true, current_session: true },
    connect: { unattended: true },
  };
  if (scenario === "trusted-lan") {
    return {
      "/health": { ...base, assertion_signature: assertion("zen-health") },
      "/auth-check": { ...base, assertion_signature: assertion("zen-probe") },
      "/desktop/capability": { ...base, assertion_signature: assertion("zen-desktop-capability"),
        capability_signature: signDomain("zen-desktop-capability-v1\u0000", v1Fields),
        transport: { identity_tls: true, transport_pin: pin, trusted_ingress: true } },
    };
  }
  if (scenario === "legacy") {
    return {
      "/health": { ...base, assertion_signature: assertion("zen-health") },
      "/auth-check": { ...base, assertion_signature: assertion("zen-probe") },
      "/desktop/capability": { ...base, assertion_signature: assertion("zen-desktop-capability"), capability_signature: signDomain("zen-desktop-capability-v1\u0000", v1Fields) },
    };
  }
  return {
    "/health": { ...base, assertion_signature: assertion("zen-health") },
    "/auth-check": { ...base, assertion_signature: assertion("zen-probe") },
    "/desktop/capability": {
      ...base, assertion_signature: assertion("zen-desktop-capability"),
      capability_signature: signDomain("zen-desktop-capability-v1\u0000", v1Fields),
      capability_signature_v2: signDomain("zen-desktop-capability-v2\u0000", v2Fields),
      moonlight,
    },
  };
};

const { mock } = await import("bun:test");
mock.module(root + "/services/auth.ts", () => ({
  buildAuthorizationHeader: async () => "synthetic-authorization",
  verifyDaemonAssertion: md.verifyDaemonAssertion,
}));
mock.module(root + "/services/pinnedTransport.ts", () => ({
  resolveStoredServerURL: async (value: { url: string }) => value.url,
}));
mock.module(root + "/modules/zen-link-transport/src/index.ts", () => ({
  startPinnedTunnel: async () => {
    tunnelCalls++;
    return { port: 43210 };
  },
}));
mock.module("expo-crypto", () => ({ getRandomBytes: (length: number) => new Uint8Array(length).fill(7) }));
mock.module("expo-modules-core", () => ({
  requireNativeModule: () => ({
    moonlightEnrollmentIdentity: async () => ({ certPem: "test-cert-pem", fingerprint: "test-fingerprint" }),
    moonlightSignEnrollment: async () => "test-signature",
  }),
  requireNativeViewManager: () => null,
}));
const controlCalls: { path: string; body: Record<string, unknown> }[] = [];
mock.module("expo/fetch", () => ({
  fetch: async (url: string, init?: { method?: string; body?: unknown }) => {
    const path = new URL(url).pathname;
    if (init?.method === "POST") {
      const parsed = typeof init.body === "string" ? JSON.parse(init.body) : {};
      controlCalls.push({ path, body: parsed });
      if (path === "/desktop/moonlight/enroll/begin") {
        return { ok: true, status: 200, url, redirected: false, body: new Response(JSON.stringify({
          nonce: "b".repeat(64), daemon_id: daemonId, assertion_timestamp: timestamp, assertion_nonce: nonce,
          assertion_signature: assertion("zen-desktop-capability"),
        })).body };
      }
      if (path === "/desktop/moonlight/enroll/complete") {
        return { ok: true, status: 200, url, redirected: false, body: new Response(JSON.stringify({
          enrolled: true, uuid: "host-uuid", daemon_id: daemonId, assertion_timestamp: timestamp,
          assertion_nonce: nonce, assertion_signature: assertion("zen-desktop-capability"),
        })).body };
      }
      return { ok: false, status: 404, url, redirected: false, body: new Response(JSON.stringify({ reason: "not_found" })).body };
    }
    const body = bodies()[path as keyof ReturnType<typeof bodies>];
    if (!body) return { ok: false, status: 404, url, redirected: false, body: null };
    return { ok: true, status: 200, url, redirected: false, body: new Response(JSON.stringify(body)).body };
  },
}));

const { prepareDesktopConnection, enrollMoonlightConnection } = await import(root + "/services/remoteDesktop.ts");

scenario = "moonlight";
tunnelCalls = 0;
const moonlightPlan = JSON.parse(await prepareDesktopConnection(server as never, "gen-1"));
assert.equal(moonlightPlan.transport, "moonlight");
assert.equal(moonlightPlan.moonlight.host, "192.168.1.50");
assert.equal(tunnelCalls, 0);

scenario = "legacy";
tunnelCalls = 0;
const legacyPlan = JSON.parse(await prepareDesktopConnection(server as never, "gen-2"));
assert.equal(legacyPlan.transport, "pinned-link");
assert.equal(tunnelCalls, 1);

scenario = "moonlight";
const enrollment = await enrollMoonlightConnection(server as never, { ...moonlight } as never);
assert.equal(enrollment.state, "verified");
assert.deepEqual(controlCalls.map((call) => call.path), [
  "/desktop/moonlight/enroll/begin",
  "/desktop/moonlight/enroll/complete",
]);
assert.equal(controlCalls[1].body.signature, "test-signature");
assert.equal(controlCalls[1].body.client_cert_pem, "test-cert-pem");

scenario = "trusted-lan";
tunnelCalls = 0;
const trustedPlan = JSON.parse(await prepareDesktopConnection(server as never, "gen-3"));
assert.equal(trustedPlan.transport, "trusted-lan");
assert.equal(tunnelCalls, 0);

console.log(JSON.stringify({ moonlight: "moonlight", legacy: "pinned-link", trusted: "trusted-lan", tunnelCalls, enrollment, controlCalls: controlCalls.length }));
