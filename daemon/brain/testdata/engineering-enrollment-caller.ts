/** Run only in its own Bun process: module mocks must not contaminate other tests. */
import { mock } from "bun:test";
import { strict as assert } from "node:assert";
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

assert.ok(process.argv[2], "A generated historical fixture root is required");
const app = resolve(process.argv[2], "app");
assert.equal(JSON.parse(readFileSync(resolve(app, "../.brain-engineering-fixture.json"), "utf8")).revision,
  "558db054675c2e4eb44d1e6b87f0f20bda7b91dd", "Use an owned generated fixture, never the live project");
const require = createRequire(app + "/package.json");
const { verifyDaemonAssertion } = await import(app + "/services/deviceAuthContract.ts");
let proofChecks = 0;
let status = 200;
let body = "";
const calls: string[] = [];

// Keep resolver, URL construction, parsing and assertion verification real.
// Only secure-store authorization, native key IO and network IO are replaced.
mock.module(app + "/services/auth.ts", () => ({
  buildAuthorizationHeader: async () => "synthetic-authorization",
  verifyDaemonAssertion: (input: Parameters<typeof verifyDaemonAssertion>[0]) => {
    proofChecks++;
    return verifyDaemonAssertion(input);
  },
}));
mock.module(require.resolve("expo-modules-core"), () => ({
  requireNativeViewManager: () => null,
  requireNativeModule: () => ({
    moonlightEnrollmentIdentity: async () => ({ certPem: "synthetic-public-cert", fingerprint: "f".repeat(64) }),
    moonlightSignEnrollment: async () => "synthetic-engine-proof",
  }),
}));
mock.module(require.resolve("expo/fetch"), () => ({
  fetch: async (url: string, init: RequestInit) => {
    const path = new URL(url).pathname;
    assert.equal(new URL(url).hostname, "192.0.2.50");
    assert.equal(init.method, "POST");
    assert.equal(init.redirect, "error");
    assert.ok(path === "/desktop/moonlight/enroll/begin" || path === "/desktop/moonlight/enroll/complete");
    calls.push(path);
    const begin = path.endsWith("/begin");
    return {
      ok: begin || status === 200, status: begin ? 200 : status, url, redirected: false,
      body: new Response(begin ? JSON.stringify({ nonce: "b".repeat(64) }) : body).body,
    };
  },
}));
const rejectNetwork = () => { throw new Error("Real network is forbidden in this replay"); };
globalThis.fetch = Object.assign(async () => rejectNetwork(), { preconnect: rejectNetwork });

const { enrollMoonlightConnection } = await import(app + "/services/remoteDesktop.ts");
// HTTPS avoids reasserting a superseded deployment/TLS requirement. No URL is contacted.
const server = { id: "fixture", name: "Fixture", url: "wss://192.0.2.50:9876/ws",
  daemonId: "a".repeat(64), daemonPublicKey: "c".repeat(64) };
type Observation = { value?: string; error?: string; status?: number; message?: string };
async function observe(code: number, response: string): Promise<Observation> {
  status = code;
  body = response;
  calls.length = 0;
  let observation: Observation;
  try {
    observation = { value: await enrollMoonlightConnection(server as never, "owned-device") };
  } catch (error) {
    if (!(error instanceof Error)) throw error;
    observation = { error: error.name, message: error.message, status: (error as Error & { status?: number }).status };
  }
  assert.deepEqual(calls, ["/desktop/moonlight/enroll/begin", "/desktop/moonlight/enroll/complete"]);
  return observation;
}

const jsonPending = await observe(409, JSON.stringify({ reason: "enrollment_pending" }));
// Go's actual http.Error writes plain text and a newline, not a JSON reason.
const textPending = await observe(409, "enrollment_pending\n");
const textForbidden = await observe(403, "desktop_scope_required\n");
const unsignedForeign = await observe(200, JSON.stringify({ enrolled: true, device_id: "other", uuid: "unproved" }));
console.log(JSON.stringify({ jsonPending, textPending, textForbidden, unsignedForeign, proofChecks }));
if (jsonPending.value !== "pending" || textPending.value !== "pending" || textForbidden.status !== 403) {
  console.error("FAIL caller contract: endpoint-shaped pending/forbidden responses must not become JSON SyntaxError");
  process.exitCode = 1;
}
