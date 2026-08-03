import { beforeEach, describe, expect, mock, test } from "bun:test";

const starts: Array<{
  key: string;
  host: string;
  port: number;
  pin: string;
  mode?: string;
}> = [];
const stops: string[] = [];
let failures = new Set<string>();

mock.module("../modules/zen-link-transport/src", () => ({
  startPinnedTunnel: async (
    key: string,
    host: string,
    port: number,
    pin: string,
    mode?: string,
  ) => {
    starts.push({ key, host, port, pin, mode });
    if (failures.has(host)) {
      throw new Error(`offline ${host}`);
    }
    return {
      port: host.startsWith("fast.") ? 19002 : 19001,
      rttMs: host.startsWith("fast.") ? 10 : 40,
    };
  },
  stopPinnedTunnel: async (key: string) => {
    stops.push(key);
  },
}));

const {
  releasePairingLinkTransport,
  resolveCanonicalServerURL,
  resolvePairingLinkURL,
  resolveStoredServerURL,
} = await import("./pinnedTransport");

beforeEach(() => {
  starts.length = 0;
  stops.length = 0;
  failures = new Set();
});

describe("shared Android/iOS pinned transport owner", () => {
  test("chooses measured Link candidates inside one StoredServer", async () => {
    const resolved = await resolveStoredServerURL({
      id: "one-current-server",
      name: "Workstation",
      url: "wss://slow.link.test/ws",
      daemonId: "1".repeat(64),
      daemonPublicKey: "2".repeat(64),
      transportKind: "link",
      transportPin: "3".repeat(64),
      linkRouteId: "4".repeat(32),
      transportCandidates: [
        { name: "a-slow", kind: "link", url: "wss://slow.link.test/ws" },
        { name: "b-fast", kind: "link", url: "wss://fast.link.test/ws" },
      ],
    });

    expect(starts.map((entry) => entry.host).sort()).toEqual([
      "fast.link.test",
      "slow.link.test",
    ]);
    expect(resolved).toBe("ws://127.0.0.1:19002/ws");
    expect(stops).toEqual(["server:one-current-server:0"]);
  });

  test("fails closed when every pinned candidate is offline", async () => {
    failures = new Set(["a.link.test", "b.link.test"]);
    await expect(
      resolveStoredServerURL({
        id: "offline-server",
        name: "Offline",
        url: "wss://a.link.test/ws",
        daemonId: "1".repeat(64),
        daemonPublicKey: "2".repeat(64),
        transportKind: "link",
        transportPin: "3".repeat(64),
        linkRouteId: "4".repeat(32),
        transportCandidates: [
          { name: "a", kind: "link", url: "wss://a.link.test/ws" },
          { name: "b", kind: "link", url: "wss://b.link.test/ws" },
        ],
      }),
    ).rejects.toThrow("Zen Link is offline");
  });

  test("fails closed instead of returning an unpinned Link endpoint", async () => {
    const linkServer = {
      id: "offline-canonical-server",
      name: "Offline",
      url: "wss://offline.link.test/ws",
      daemonId: "1".repeat(64),
      daemonPublicKey: "2".repeat(64),
      transportKind: "link" as const,
      transportPin: "3".repeat(64),
      linkRouteId: "4".repeat(32),
      transportCandidates: [
        {
          name: "offline",
          kind: "link" as const,
          url: "wss://offline.link.test/ws",
        },
      ],
    };
    failures = new Set(["offline.link.test"]);

    await expect(resolveCanonicalServerURL(linkServer)).rejects.toThrow(
      "Zen Link is offline",
    );
  });

  test("keeps a manual V1 endpoint on its original transport", async () => {
    await expect(
      resolveCanonicalServerURL({
        id: "manual",
        name: "Self-managed",
        url: "wss://cloudflare.example/ws",
        daemonId: "1".repeat(64),
        daemonPublicKey: "2".repeat(64),
        transportKind: "manual",
      }),
    ).resolves.toBe("wss://cloudflare.example/ws");
    expect(starts).toHaveLength(0);
  });

  test("rejects an incomplete Link transport instead of using its raw URL", async () => {
    await expect(
      resolveStoredServerURL({
        id: "needs-setup",
        name: "Needs setup",
        url: "wss://raw.link.test/ws",
        daemonId: "1".repeat(64),
        daemonPublicKey: "2".repeat(64),
        transportKind: "link",
      }),
    ).rejects.toThrow("Zen Link needs setup");
    expect(starts).toHaveLength(0);
  });

  test("releases the one-time enrollment proxy after pairing", async () => {
    await releasePairingLinkTransport("route-one");
    expect(stops).toEqual(["pair:route-one"]);
  });

  test("opens pairing transport on demand without an admission preflight", async () => {
    const resolved = await resolvePairingLinkURL({
      routeId: "4".repeat(32),
      transportPin: "3".repeat(64),
      url: "wss://admission.link.test/ws",
    });
    expect(resolved).toBe("ws://127.0.0.1:19001/ws");
    expect(starts).toEqual([
      {
        key: `pair:${"4".repeat(32)}`,
        host: "admission.link.test",
        port: 443,
        pin: "3".repeat(64),
        mode: "on-demand",
      },
    ]);
  });
});
