import { describe, expect, mock, test } from "bun:test";
import nacl from "tweetnacl";

mock.module("react-native", () => ({ Platform: { OS: "ios" } }));
mock.module("expo-crypto", () => ({
  getRandomBytes: (length: number) => new Uint8Array(length),
  randomUUID: () => "00000000-0000-4000-8000-000000000000",
}));
mock.module("expo-device", () => ({ deviceName: "Test", modelName: "Test" }));
mock.module("expo-secure-store", () => ({
  getItemAsync: async () => null,
  setItemAsync: async () => undefined,
}));
mock.module("@react-native-async-storage/async-storage", () => ({
  default: {
    getItem: async () => null,
    setItem: async () => undefined,
  },
}));

const { bytesToHex } = await import("./protocolCrypto");
const { parseConnectLink } = await import("./connection");

describe("Pairing V2", () => {
  test("accepts daemon-signed Link route, pin, admission, and stable candidates", () => {
    const seed = new Uint8Array(32).fill(7);
    const keyPair = nacl.sign.keyPair.fromSeed(seed);
    const payload = {
      v: 2,
      d: "1".repeat(64),
      k: bytesToHex(keyPair.publicKey),
      e: "2".repeat(64),
      r: "3".repeat(32),
      p: "4".repeat(64),
      c: [
        {
          n: "region-a",
          a: "wss://55555555555555555555555555555555.a.link.test/ws",
          s: "wss://33333333333333333333333333333333.a.link.test/ws",
        },
        {
          n: "region-b",
          s: "wss://33333333333333333333333333333333.b.link.test/ws",
        },
      ],
      x: Date.now() + 60_000,
      z: "",
    };
    const binding = new TextEncoder().encode(
      [
        "2",
        payload.d,
        payload.k,
        payload.e,
        payload.r,
        payload.p,
        payload.x.toString(),
        "region-a",
        payload.c[0].a,
        payload.c[0].s,
        "region-b",
        "",
        payload.c[1].s,
      ].join("\n"),
    );
    const domain = new TextEncoder().encode("zen-link-pairing-v2\u0000");
    const signed = new Uint8Array(domain.length + binding.length);
    signed.set(domain);
    signed.set(binding, domain.length);
    payload.z = bytesToHex(nacl.sign.detached(signed, keyPair.secretKey));

    const link = `zen://settings?v=2&p=${Buffer.from(
      JSON.stringify(payload),
    ).toString("base64url")}`;
    expect(parseConnectLink(link)).toEqual({
      url: payload.c[0].a!,
      daemonId: payload.d,
      daemonPublicKey: payload.k,
      enrollmentToken: payload.e,
      link: {
        kind: "link",
        routeId: payload.r,
        transportPin: payload.p,
        candidates: [
          {
            name: "region-a",
            admissionUrl: payload.c[0].a!,
            url: payload.c[0].s,
          },
          {
            name: "region-b",
            admissionUrl: undefined,
            url: payload.c[1].s,
          },
        ],
      },
    });

    payload.p = "6".repeat(64);
    const tampered = `zen://settings?v=2&p=${Buffer.from(
      JSON.stringify(payload),
    ).toString("base64url")}`;
    expect(parseConnectLink(tampered)).toBeNull();

    const oversized = `zen://settings?v=2&p=${"A".repeat((64 << 10) + 1)}`;
    expect(parseConnectLink(oversized)).toBeNull();
  });
});
