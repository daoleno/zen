// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  decideDisconnectLifecycle,
  resolveBrainActiveServerId,
  shouldShowBrainLoadingState,
} from "./connectionLifecycle";
import { brainReducer, initialBrainState } from "../store/brain";
import { buildChatComposerPlaceholder } from "./chatComposerPresentation";
import { buildCodexStatusMeta } from "../components/terminal/CodexChatControllerModel";

const hydratedBrain = {
  agents: [{ id: "host-1", name: "Brain", status: "running" }],
  host_agent: { id: "host-1", name: "Brain", status: "running" },
  host_adapter: {
    id: "codex",
    name: "Codex",
    provider: "codex",
    capabilities: { structured_events: true },
  },
  adapters: [
    {
      id: "codex",
      name: "Codex",
      provider: "codex",
      capabilities: { structured_events: true },
    },
  ],
  chat_thread_id: "thread-1",
  workspace: "/tmp/brain",
};

describe("decideDisconnectLifecycle", () => {
  test("intentional disconnect goes offline and clears caches", () => {
    expect(decideDisconnectLifecycle("intentional")).toEqual({
      connectionState: "offline",
      clearServerCaches: true,
    });
  });

  test("transport close resumes as connecting and retains caches", () => {
    expect(decideDisconnectLifecycle("transport_closed")).toEqual({
      connectionState: "connecting",
      clearServerCaches: false,
    });
  });

  test("unknown/missing reason is treated as transient resume", () => {
    expect(decideDisconnectLifecycle(undefined)).toEqual({
      connectionState: "connecting",
      clearServerCaches: false,
    });
  });
});

describe("Brain cache across background->foreground resume", () => {
  test("retains hydrated Brain content across transport_closed", () => {
    const hydrated = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: "srv-1",
      serverName: "Local",
      serverUrl: "ws://localhost/ws",
      brain: hydratedBrain,
    });
    expect(hydrated.byServer["srv-1"]?.hydrated).toBe(true);
    expect(hydrated.byServer["srv-1"]?.chat_thread_id).toBe("thread-1");

    const decision = decideDisconnectLifecycle("transport_closed");
    expect(decision.clearServerCaches).toBe(false);

    // Simulate layout: only intentional disconnect dispatches REMOVE_SERVER.
    const retained = decision.clearServerCaches
      ? brainReducer(hydrated, { type: "REMOVE_SERVER", serverId: "srv-1" })
      : hydrated;

    expect(retained.byServer["srv-1"]?.hydrated).toBe(true);
    expect(retained.byServer["srv-1"]?.host_agent?.id).toBe("host-1");
    expect(retained.byServer["srv-1"]?.chat_thread_id).toBe("thread-1");
    expect(
      shouldShowBrainLoadingState({
        hydrated: Boolean(retained.byServer["srv-1"]?.hydrated),
        hasHostAgent: Boolean(retained.byServer["srv-1"]?.host_agent?.id),
      }),
    ).toBe(false);
  });

  test("intentional disconnect clears Brain and shows offline loading", () => {
    const hydrated = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: "srv-1",
      serverName: "Local",
      serverUrl: "ws://localhost/ws",
      brain: hydratedBrain,
    });
    const decision = decideDisconnectLifecycle("intentional");
    expect(decision.clearServerCaches).toBe(true);
    expect(decision.connectionState).toBe("offline");

    const cleared = brainReducer(hydrated, {
      type: "REMOVE_SERVER",
      serverId: "srv-1",
    });
    expect(cleared.byServer["srv-1"]).toBeUndefined();
    expect(
      shouldShowBrainLoadingState({
        hydrated: false,
        hasHostAgent: false,
      }),
    ).toBe(true);
  });

  test("resubscribe after resume keeps same thread identity (no content reset)", () => {
    const before = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: "srv-1",
      serverName: "Local",
      serverUrl: "ws://localhost/ws",
      brain: hydratedBrain,
    });
    const afterReconnectSnapshot = brainReducer(before, {
      type: "BRAIN_SNAPSHOT",
      serverId: "srv-1",
      serverName: "Local",
      serverUrl: "ws://localhost/ws",
      brain: {
        ...hydratedBrain,
        generated_at: "2026-07-12T00:00:00Z",
      },
    });
    expect(afterReconnectSnapshot.byServer["srv-1"]?.chat_thread_id).toBe(
      "thread-1",
    );
    expect(afterReconnectSnapshot.byServer["srv-1"]?.host_agent?.id).toBe(
      "host-1",
    );
  });
});

describe("resolveBrainActiveServerId during resume", () => {
  test("prefers hydrated server while reconnecting (not connected)", () => {
    const id = resolveBrainActiveServerId({
      servers: [
        { id: "other" },
        { id: "brain-host" },
      ],
      connectedServerIds: [],
      brainHydratedByServer: { "brain-host": true },
      connectionStates: {
        other: "offline",
        "brain-host": "connecting",
      },
    });
    expect(id).toBe("brain-host");
  });

  test("prefers connected hydrated over merely hydrated", () => {
    const id = resolveBrainActiveServerId({
      servers: [{ id: "a" }, { id: "b" }],
      connectedServerIds: ["b"],
      brainHydratedByServer: { a: true, b: true },
      connectionStates: { a: "connecting", b: "connected" },
    });
    expect(id).toBe("b");
  });
});

describe("resume presentation for composer and status", () => {
  test("connecting keeps composer available messaging (not unavailable)", () => {
    expect(
      buildChatComposerPlaceholder({
        agentKind: "codex",
        connectionState: "connecting",
        slashQueryActive: false,
      }),
    ).toBe("Connecting…");
  });

  test("true offline shows unavailable composer", () => {
    expect(
      buildChatComposerPlaceholder({
        agentKind: "codex",
        connectionState: "offline",
        slashQueryActive: false,
      }),
    ).toBe("Connection unavailable");
  });

  test("status meta shows Reconnecting while resuming", () => {
    expect(
      buildCodexStatusMeta({
        connectionState: "connecting",
        connectionIssue: null,
        conversation: null,
        events: [],
        sending: false,
      }),
    ).toBe("Reconnecting");
  });

  test("connection issue surfaces genuine failure after resume attempts", () => {
    expect(
      buildCodexStatusMeta({
        connectionState: "connecting",
        connectionIssue: {
          code: "network_unreachable",
          title: "Daemon unreachable",
          detail: "Could not reach the endpoint.",
          hint: "Check that the daemon is running.",
          checkedAt: Date.now(),
        },
        conversation: { events: [] },
        events: [],
        sending: false,
      }),
    ).toBe("Daemon unreachable");
  });
});
