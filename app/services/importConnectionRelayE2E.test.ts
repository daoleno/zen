import { afterAll, expect, mock, test } from "bun:test";

const enabled = process.env.ZEN_LINK_RELAY_E2E === "1";

if (!enabled) {
  test.skip("Pairing V2 import through the real Relay runs in isolation", () => {});
} else {
  const pairingLink = process.env.ZEN_LINK_E2E_PAIRING_LINK || "";
  const bridgePort = Number(process.env.ZEN_LINK_E2E_BRIDGE_PORT || "");
  const expectedStableURL = process.env.ZEN_LINK_E2E_STABLE_URL || "";
  if (
    !pairingLink ||
    !Number.isInteger(bridgePort) ||
    bridgePort <= 0 ||
    !expectedStableURL
  ) {
    throw new Error("Zen Link Relay E2E fixture environment is incomplete.");
  }

  const secureValues = new Map<string, string>();
  const savedInputs: Array<Record<string, unknown>> = [];
  let nativeStarts = 0;

  mock.module("react-native", () => ({ Platform: { OS: "ios" } }));
  mock.module("expo-crypto", () => ({
    getRandomBytes: (length: number) => new Uint8Array(length).fill(9),
    randomUUID: () => "00000000-0000-4000-8000-000000000009",
  }));
  mock.module("expo-device", () => ({
    deviceName: "Relay E2E phone",
    modelName: "Relay E2E phone",
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
  mock.module("./storage", () => ({
    markOnboarded: async () => undefined,
    saveServer: async (input: Record<string, unknown>) => {
      savedInputs.push(input);
      return { id: "relay-e2e-server", ...input };
    },
    setServerAutoConnect: async () => undefined,
  }));
  mock.module("../modules/zen-link-transport/src", () => ({
    startPinnedTunnel: async (
      _key: string,
      _serverName: string,
      _port: number,
      _expectedPin: string,
      mode: string,
    ) => {
      expect(mode).toBe("on-demand");
      nativeStarts += 1;
      return { port: bridgePort, rttMs: 0 };
    },
    stopPinnedTunnel: async () => undefined,
  }));

  const { importConnection } = await import("./importConnection");

  afterAll(() => {
    mock.restore();
  });

  test("importConnection reaches daemon /pair through native transport and opaque Relay", async () => {
    const imported = await importConnection(pairingLink);

    expect(nativeStarts).toBe(1);
    expect(imported?.id).toBe("relay-e2e-server");
    expect(savedInputs).toEqual([
      expect.objectContaining({
        url: expectedStableURL,
        transportKind: "link",
      }),
    ]);
  }, 10_000);
}
