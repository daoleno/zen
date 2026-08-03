import { afterAll, beforeEach, describe, expect, mock, test } from "bun:test";

const integrationEnabled = process.env.ZEN_LINK_BOUNDARY_E2E === "1";

if (!integrationEnabled) {
  test.skip("Link network boundary harness runs in isolation", () => {});
} else {
  const linkServer = {
    id: "link-current",
    name: "Zen Link workstation",
    url: "wss://11111111111111111111111111111111.link.test/ws",
    daemonId: "2".repeat(64),
    daemonPublicKey: "3".repeat(64),
    transportKind: "link" as const,
    transportPin: "4".repeat(64),
    linkRouteId: "1".repeat(32),
    transportCandidates: [
      {
        name: "region-a",
        kind: "link" as const,
        url: "wss://11111111111111111111111111111111.link.test/ws",
      },
    ],
  };
  const manualServer = {
    id: "manual-file-capability",
    name: "Self-managed workstation",
    url: "wss://cloudflare.example/ws",
    daemonId: linkServer.daemonId,
    daemonPublicKey: linkServer.daemonPublicKey,
    transportKind: "manual" as const,
  };

  let nativeStarts = 0;
  let fetchCalls = 0;
  let nativeOffline = true;
  let fetchScenario: "raw-fallback" | "probe-success" | "file-capability" =
    "raw-fallback";
  let authorizationBuilds = 0;
  const observedAuthorizationHeaders: string[] = [];
  const observedFetches: Array<{ url: string; init?: RequestInit }> = [];

  mock.module("react-native", () => ({ Platform: { OS: "ios" } }));
  mock.module("../constants/tokens", () => ({
    Colors: {
      disabledText: "#777",
      warning: "#fa0",
      dangerText: "#f00",
    },
  }));
  mock.module("./auth", () => ({
    buildAuthorizationHeader: async () => {
      authorizationBuilds += 1;
      const timestamp = String(1_800_000_000_000 + authorizationBuilds);
      const nonce = authorizationBuilds.toString(16).padStart(32, "0");
      const header = `ZenDevice v1:device-a:${linkServer.daemonId}:${timestamp}:${nonce}:${"a".repeat(128)}`;
      observedAuthorizationHeaders.push(header);
      return header;
    },
    normalizeDaemonId: (value: string) => value,
    normalizePublicKeyHex: (value: string) => value,
    verifyDaemonAssertion: () => true,
  }));
  mock.module("./pairing", () => ({
    buildHTTPURL: (serverUrl: string, pathname: string) => {
      const parsed = new URL(serverUrl);
      parsed.protocol = parsed.protocol === "ws:" ? "http:" : "https:";
      parsed.pathname = pathname;
      parsed.search = "";
      parsed.hash = "";
      return parsed.toString();
    },
    buildSignedRequestHeaders: async () => ({
      Authorization: "ZenDevice test",
    }),
  }));
  mock.module("./storage", () => ({
    getServerById: async (serverId: string) => {
      if (
        serverId === linkServer.id ||
        serverId === "link-probe-success" ||
        serverId === "link-file-capability"
      ) {
        return { ...linkServer, id: serverId };
      }
      if (serverId === manualServer.id) {
        return manualServer;
      }
      return null;
    },
  }));
  mock.module("../modules/zen-link-transport/src", () => ({
    startPinnedTunnel: async () => {
      nativeStarts += 1;
      if (nativeOffline) {
        throw new Error("pinned candidate offline");
      }
      return { port: 19443, rttMs: 8 };
    },
    stopPinnedTunnel: async () => undefined,
  }));

  const originalFetch = globalThis.fetch;
  Object.assign(globalThis, {
    fetch: async (input: RequestInfo | URL, init?: RequestInit) => {
      fetchCalls += 1;
      const url = String(input);
      observedFetches.push({ url, init });
      if (fetchScenario === "probe-success") {
        return new Response(
          JSON.stringify({
            ok: true,
            daemon_id: linkServer.daemonId,
            daemon_public_key: linkServer.daemonPublicKey,
            assertion_timestamp: "2026-07-26T00:00:00Z",
            assertion_nonce: "b".repeat(32),
            assertion_signature: "c".repeat(128),
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (fetchScenario === "file-capability") {
        return new Response(
          JSON.stringify({
            version: 1,
            device_id: "device-a",
            expires_at_ms: 1_900_000_000_000,
            get_signature: "d".repeat(128),
            head_signature: "e".repeat(128),
          }),
          {
            status: 200,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      throw new Error("raw fetch fallback");
    },
  });

  const { diagnoseConnectionIssue } = await import("./connectionIssue");
  const { measureServerLatency } = await import("./serverLatency");
  const { buildSessionFileBinarySource } = await import("./sessionFilePreview");

  beforeEach(() => {
    nativeStarts = 0;
    fetchCalls = 0;
    nativeOffline = true;
    fetchScenario = "raw-fallback";
    authorizationBuilds = 0;
    observedAuthorizationHeaders.length = 0;
    observedFetches.length = 0;
  });

  afterAll(() => {
    Object.assign(globalThis, { fetch: originalFetch });
    mock.restore();
  });

  describe("Link network paths fail closed at the current StoredServer", () => {
    test("Probe and auth-check never call ordinary fetch when pin transport fails", async () => {
      const issue = await diagnoseConnectionIssue({
        server: linkServer,
      });

      expect(issue.code).toBe("network_unreachable");
      expect(issue.detail).toContain("Zen Link is offline");
      expect(nativeStarts).toBe(1);
      expect(fetchCalls).toBe(0);
    });

    test("latency auth-check never calls ordinary fetch when pin transport fails", async () => {
      await expect(
        measureServerLatency({ server: linkServer }),
      ).rejects.toThrow("Zen Link is offline");
      expect(nativeStarts).toBe(1);
      expect(fetchCalls).toBe(0);
    });

    test("auth-check and the following /ws probe each receive a fresh signed nonce", async () => {
      nativeOffline = false;
      fetchScenario = "probe-success";
      const issue = await diagnoseConnectionIssue({
        server: { ...linkServer, id: "link-probe-success" },
      });

      expect(issue.code).toBe("websocket_upgrade_failed");
      const signedRequests = observedFetches.filter(
        ({ url }) => url.endsWith("/auth-check") || url.endsWith("/ws"),
      );
      expect(signedRequests).toHaveLength(2);
      expect(observedAuthorizationHeaders).toHaveLength(2);
      const [authCheckHeader, wsHeader] = observedAuthorizationHeaders;
      expect(authCheckHeader).not.toBe(wsHeader);
      const authCheckParts = authCheckHeader.split(":");
      const wsParts = wsHeader.split(":");
      expect(authCheckParts[3]).not.toBe(wsParts[3]);
      expect(authCheckParts[4]).not.toBe(wsParts[4]);
      expect(signedRequests.map(({ init }) => init?.headers)).toEqual([
        { Authorization: authCheckHeader },
        { Authorization: wsHeader },
      ]);
    });

    test("Session File including Range/retry source creation has no raw URL fallback", async () => {
      const request = {
        agentId: "agent-a",
        processId: 7,
        startedAt: 1_700_000_000_000,
        path: "/workspace/private.png",
        generation: "generation-a",
      };
      await expect(
        buildSessionFileBinarySource(
          linkServer.id,
          linkServer.daemonId,
          request,
        ),
      ).rejects.toThrow("Zen Link is offline");
      await expect(
        buildSessionFileBinarySource(
          linkServer.id,
          linkServer.daemonId,
          request,
        ),
      ).rejects.toThrow("Zen Link is offline");
      expect(nativeStarts).toBe(2);
      expect(fetchCalls).toBe(0);
    });

    test("Session File source exchanges one fresh nonce for repeatable method-scoped capabilities", async () => {
      nativeOffline = false;
      fetchScenario = "file-capability";
      const request = {
        agentId: "agent-a",
        processId: 7,
        startedAt: 1_700_000_000_000,
        path: "/workspace/private.png",
        generation: "generation-a",
      };

      const source = await buildSessionFileBinarySource(
        "link-file-capability",
        linkServer.daemonId,
        request,
      );

      expect(observedFetches).toHaveLength(1);
      expect(observedFetches[0].url).toBe(
        "http://127.0.0.1:19443/session-file-capability",
      );
      expect(observedFetches[0].init?.method).toBe("POST");
      expect(observedFetches[0].init?.headers).toEqual({
        Authorization: observedAuthorizationHeaders[0],
        "Content-Type": "application/json",
      });
      expect(JSON.parse(String(observedFetches[0].init?.body))).toEqual({
        agent_id: request.agentId,
        process_id: request.processId,
        started_at: request.startedAt,
        path: request.path,
        generation: request.generation,
      });
      const sourceURL = new URL(source.uri);
      expect(sourceURL.pathname).toBe("/session-file");
      expect(sourceURL.searchParams.get("file_cap_device")).toBe("device-a");
      expect(sourceURL.searchParams.get("file_cap_expires")).toBe(
        "1900000000000",
      );
      expect(sourceURL.searchParams.get("file_cap_get")).toBe("d".repeat(128));
      expect(sourceURL.searchParams.get("file_cap_head")).toBe("e".repeat(128));
      expect(sourceURL.searchParams.has("auth")).toBe(false);
      expect(source.headers).toEqual({ "Cache-Control": "no-store" });

      const recoveredSource = await buildSessionFileBinarySource(
        "link-file-capability",
        linkServer.daemonId,
        request,
      );
      expect(recoveredSource.uri).toBe(source.uri);
      expect(observedFetches).toHaveLength(2);
      expect(observedAuthorizationHeaders).toHaveLength(2);
      expect(observedAuthorizationHeaders[0]).not.toBe(
        observedAuthorizationHeaders[1],
      );
    });

    test("manual V1 Session File keeps its self-managed origin while using the same capability contract", async () => {
      fetchScenario = "file-capability";
      const source = await buildSessionFileBinarySource(
        manualServer.id,
        manualServer.daemonId,
        {
          agentId: "agent-manual",
          processId: 9,
          startedAt: 1_700_000_000_000,
          path: "/workspace/manual.pdf",
          generation: "generation-manual",
        },
      );

      expect(nativeStarts).toBe(0);
      expect(observedFetches[0].url).toBe(
        "https://cloudflare.example/session-file-capability",
      );
      expect(source.uri).toStartWith(
        "https://cloudflare.example/session-file?",
      );
      expect(source.headers).toEqual({ "Cache-Control": "no-store" });
    });
  });
}
