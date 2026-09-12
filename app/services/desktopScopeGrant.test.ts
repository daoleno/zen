import { expect, test } from "bun:test";
import type { StoredServer } from "./storage";
import { DESKTOP_GRANT_PURPOSE, desktopGrantTransport, postDesktopGrant, type DesktopGrantRequestDependencies, type DesktopGrantTunnelFactory } from "./desktopScopeGrantCore";

const daemonId = "a".repeat(64);
const daemonPublicKey = "b".repeat(64);
const server = { daemonId, daemonPublicKey };
const identityTls = { identityTls: true, transportPin: "cd".repeat(32) };

function response(status: number, body: unknown): Pick<Response, "ok" | "status" | "text" | "json"> {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => typeof body === "string" ? body : JSON.stringify(body),
    json: async () => body,
  };
}

function grantDependencies(fetch: DesktopGrantRequestDependencies["fetch"], verify: DesktopGrantRequestDependencies["verify"] = () => true) {
  const identities = ["phone-1"];
  return {
    fetch,
    authorization: async () => "ZenDevice v1:phone-1:" + daemonId,
    identity: async () => ({ deviceId: identities.shift() || "phone-1" }),
    verify,
  } satisfies DesktopGrantRequestDependencies;
}

test("grant purpose literal matches the daemon contract", () => {
  expect(DESKTOP_GRANT_PURPOSE).toBe("zen-device-admin:desktop-grant:POST:/desktop/scope");
});

test("identity pin transport bootstraps scope0 grant on LAN, link and wss only", async () => {
  await expect(desktopGrantTransport(server as StoredServer, "ws://192.168.1.5:9876/ws", { identityTls: false, transportPin: "" }))
    .rejects.toThrow("identity-bound encrypted connection");
  await expect(desktopGrantTransport(server as StoredServer, "https://public.example/ws", { identityTls: true, transportPin: "not-a-pin" }))
    .rejects.toThrow("identity-bound encrypted connection");

  const started: { key: string; host: string; port: number; pin: string }[] = [];
  const stopped: string[] = [];
  const tunnels: DesktopGrantTunnelFactory = {
    start: async (key, host, port, pin) => { started.push({ key, host, port, pin }); return { port: 43210 }; },
    stop: async (key) => { stopped.push(key); },
  };
  const lan = { id: "srv-1", url: "ws://192.168.1.5:9876/ws", transportKind: "manual" } as StoredServer;
  const transport = await desktopGrantTransport(lan, lan.url, identityTls, tunnels);
  expect(transport.origin).toBe("http://127.0.0.1:43210");
  expect(started).toEqual([{ key: "desktop-grant:srv-1", host: "192.168.1.5", port: 9876, pin: identityTls.transportPin }]);
  await transport.release();
  expect(stopped).toEqual(["desktop-grant:srv-1"]);

  const link = { id: "srv-2", url: "wss://link.example/ws", transportKind: "link", transportPin: identityTls.transportPin } as StoredServer;
  await expect(desktopGrantTransport(link, "ws://127.0.0.1:5555/ws", identityTls, tunnels)).resolves.toMatchObject({ origin: "http://127.0.0.1:5555" });
  expect(started).toHaveLength(1);

  const secure = { id: "srv-3", url: "wss://secure.example:9876/ws", transportKind: "manual" } as StoredServer;
  await expect(desktopGrantTransport(secure, secure.url, identityTls, tunnels)).resolves.toMatchObject({ origin: "https://secure.example:9876" });

  const insecure = { id: "srv-4", url: "ws://8.8.8.8:9876/ws", transportKind: "manual" } as StoredServer;
  await expect(desktopGrantTransport(insecure, insecure.url, identityTls, tunnels)).rejects.toThrow("encrypted connection");
});

test("explicit versioned consent uses the signed grant purpose and validates the daemon confirmation", async () => {
  const calls: { url: string; init: any }[] = [];
  const verified: any[] = [];
  await postDesktopGrant("http://127.0.0.1:43210", server, grantDependencies(
    async (url, init) => { calls.push({ url, init }); return response(200, {
      ok: true, device_id: "phone-1", desktop_scope_version: 1,
      assertion_timestamp: "2026-09-12T00:00:00Z", assertion_nonce: "c".repeat(32), assertion_signature: "sig",
    }); },
    (input) => { verified.push(input); return true; },
  ));
  expect(calls).toHaveLength(1);
  expect(calls[0].url).toBe("http://127.0.0.1:43210/desktop/scope");
  expect(calls[0].init.method).toBe("POST");
  expect(calls[0].init.headers.Authorization).toContain("ZenDevice");
  expect(JSON.parse(calls[0].init.body)).toEqual({ desktop_scope_version: 1 });
  expect(verified[0]).toMatchObject({ purpose: "zen-device-admin:desktop-grant:POST:/desktop/scope", daemonId, daemonPublicKey });
});

test("negatives fail closed: other device, forged assertion, insecure transport, server failure and abort", async () => {
  const other = grantDependencies(async () => response(200, { ok: true, device_id: "someone-else", desktop_scope_version: 1 }));
  await expect(postDesktopGrant("http://127.0.0.1:1", server, other)).rejects.toThrow("did not confirm desktop permission");

  const forged = grantDependencies(async () => response(200, { ok: true, device_id: "phone-1", desktop_scope_version: 1 }), () => false);
  await expect(postDesktopGrant("http://127.0.0.1:1", server, forged)).rejects.toThrow("did not match this computer");

  const insecure = grantDependencies(async () => response(403, "desktop_tls_required"));
  await expect(postDesktopGrant("http://127.0.0.1:1", server, insecure)).rejects.toThrow("encrypted connection");

  const broken = grantDependencies(async () => response(500, "desktop_scope_persist_failed"));
  await expect(postDesktopGrant("http://127.0.0.1:1", server, broken)).rejects.toThrow("could not save desktop permission");

  let fetchCalls = 0;
  let observedSignal: AbortSignal | undefined;
  const controller = new AbortController();
  const pending = grantDependencies(async (_url, init) => {
    fetchCalls++;
    observedSignal = init?.signal;
    return new Promise((_, reject) => { init?.signal?.addEventListener("abort", () => reject(new Error("aborted"))); });
  });
  const attempt = postDesktopGrant("http://127.0.0.1:1", server, pending, controller.signal);
  await Promise.resolve();
  await Promise.resolve();
  controller.abort();
  await expect(attempt).rejects.toThrow("Enable cancelled");
  expect(fetchCalls).toBe(1);
  expect(observedSignal?.aborted).toBe(true);
});
