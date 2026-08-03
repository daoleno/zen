import { afterAll, describe, expect, mock, test } from "bun:test";
import { createHash } from "node:crypto";
import nacl from "tweetnacl";

const integrationEnabled = process.env.ZEN_LINK_IMPORT_E2E === "1";

if (!integrationEnabled) {
  test.skip("Pairing V2 import cross-layer harness runs in isolation", () => {});
} else {
  const secureValues = new Map<string, string>();
  const events: string[] = [];
  const savedInputs: Array<Record<string, unknown>> = [];

  mock.module("react-native", () => ({ Platform: { OS: "ios" } }));
  mock.module("expo-crypto", () => ({
    getRandomBytes: (length: number) => new Uint8Array(length).fill(9),
    randomUUID: () => "00000000-0000-4000-8000-000000000009",
  }));
  mock.module("expo-device", () => ({
    deviceName: "Pairing test phone",
    modelName: "Pairing test phone",
  }));
  mock.module("expo-secure-store", () => ({
    getItemAsync: async (key: string) => secureValues.get(key) || null,
    setItemAsync: async (key: string, value: string) => {
      secureValues.set(key, value);
    },
  }));
  mock.module("@react-native-async-storage/async-storage", () => ({
    default: {
      getItem: async () => null,
      setItem: async () => undefined,
    },
  }));
  mock.module("../modules/zen-link-transport/src", () => ({
    startPinnedTunnel: async (
      key: string,
      host: string,
      port: number,
      pin: string,
      mode: string,
    ) => {
      events.push(`native:${mode}:${host}:${port}:${key}:${pin}`);
      return { port: 19876, rttMs: 0 };
    },
    stopPinnedTunnel: async (key: string) => {
      events.push(`stop:${key}`);
    },
  }));
  mock.module("./storage", () => ({
    markOnboarded: async () => events.push("storage:onboarded"),
    saveServer: async (input: Record<string, unknown>) => {
      events.push("storage:save");
      savedInputs.push(input);
      return { id: "stored-link", ...input };
    },
    setServerAutoConnect: async () => events.push("storage:auto-connect"),
  }));

  const daemonKeyPair = nacl.sign.keyPair.fromSeed(new Uint8Array(32).fill(7));
  const daemonPublicKey = hex(daemonKeyPair.publicKey);
  const daemonId = createHash("sha256")
    .update(daemonKeyPair.publicKey)
    .digest("hex");
  const routeId = "1".repeat(32);
  const transportPin = "2".repeat(64);
  const admissionURL =
    "wss://33333333333333333333333333333333.region.link.test/ws";
  const stableURL =
    "wss://11111111111111111111111111111111.region.link.test/ws";
  const enrollmentToken = "4".repeat(64);

  const originalFetch = globalThis.fetch;
  Object.assign(globalThis, {
    fetch: async (input: string | URL | Request, init?: RequestInit) => {
      const url =
        typeof input === "string"
          ? input
          : input instanceof URL
            ? input.toString()
            : input.url;
      events.push(`fetch:${init?.method || "GET"}:${url}`);
      const request = JSON.parse(String(init?.body || "{}")) as Record<
        string,
        unknown
      >;
      expect(new URL(url).pathname).toBe("/pair");
      expect(request.enrollment_token).toBe(enrollmentToken);
      expect(request.expected_daemon_id).toBe(daemonId);
      expect(request.expected_daemon_public_key).toBe(daemonPublicKey);

      const timestamp = Date.now().toString();
      const nonce = "5".repeat(32);
      const assertion = new TextEncoder().encode(
        ["zen-pair", daemonId, timestamp, nonce].join("\n"),
      );
      return new Response(
        JSON.stringify({
          device_id: request.device_id,
          daemon_id: daemonId,
          daemon_public_key: daemonPublicKey,
          assertion_timestamp: timestamp,
          assertion_nonce: nonce,
          assertion_signature: hex(
            nacl.sign.detached(assertion, daemonKeyPair.secretKey),
          ),
        }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      );
    },
  });

  const { importConnection } = await import("./importConnection");

  afterAll(() => {
    Object.assign(globalThis, { fetch: originalFetch });
    mock.restore();
  });

  describe("Pairing V2 import call sequence", () => {
    test("one on-demand native stream carries the actual /pair request", async () => {
      events.length = 0;
      savedInputs.length = 0;
      secureValues.clear();

      const imported = await importConnection(pairingLink());

      expect(events[0]).toBe(
        `native:on-demand:${new URL(admissionURL).hostname}:443:pair:${routeId}:${transportPin}`,
      );
      expect(events.filter((event) => event.startsWith("fetch:"))).toEqual([
        "fetch:POST:http://127.0.0.1:19876/pair",
      ]);
      expect(events.indexOf(`stop:pair:${routeId}`)).toBeGreaterThan(
        events.findIndex((event) => event.startsWith("fetch:")),
      );
      expect(savedInputs).toEqual([
        expect.objectContaining({
          url: stableURL,
          daemonId,
          daemonPublicKey,
          transportKind: "link",
          transportPin,
          linkRouteId: routeId,
        }),
      ]);
      expect(imported?.id).toBe("stored-link");
    });
  });

  function pairingLink(): string {
    const expiresAt = Date.now() + 60_000;
    const payload = {
      v: 2,
      d: daemonId,
      k: daemonPublicKey,
      e: enrollmentToken,
      r: routeId,
      p: transportPin,
      c: [
        {
          n: "region-a",
          a: admissionURL,
          s: stableURL,
        },
      ],
      x: expiresAt,
      z: "",
    };
    const binding = new TextEncoder().encode(
      [
        "2",
        daemonId,
        daemonPublicKey,
        enrollmentToken,
        routeId,
        transportPin,
        expiresAt.toString(),
        "region-a",
        admissionURL,
        stableURL,
      ].join("\n"),
    );
    const domain = new TextEncoder().encode("zen-link-pairing-v2\u0000");
    const signed = new Uint8Array(domain.length + binding.length);
    signed.set(domain);
    signed.set(binding, domain.length);
    payload.z = hex(nacl.sign.detached(signed, daemonKeyPair.secretKey));
    return `zen://settings?v=2&p=${Buffer.from(
      JSON.stringify(payload),
    ).toString("base64url")}`;
  }

  function hex(bytes: Uint8Array): string {
    return Buffer.from(bytes).toString("hex");
  }
}
