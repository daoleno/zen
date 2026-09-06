import { describe, expect, test } from "bun:test";
import {
  brainHostMatchesRoute,
  resolveTerminalRouteWorker,
  routeWorkerProviderModelActionState,
} from "../components/terminal/screen/useTerminalRouteModel";
import {
  sessionAllowsModelProfileActivation,
  sessionIsManagedReadOnlyProfile,
  sessionSupportsModelProfileAction,
  type WorkerSessionCapabilities,
} from "./providers/sessionCapabilities";
import {
  workerReducer,
  initialWorkerState,
  type Worker,
} from "../store/workers";
import { brainReducer, initialBrainState, type BrainWorkerRef } from "../store/brain";
import { makeSessionKey } from "./sessionKeys";

const HOST_ID = "brain-host-1";
const SERVER_ID = "server-a";

function hostRef(
  capabilities?: WorkerSessionCapabilities,
): BrainWorkerRef {
  return {
    id: HOST_ID,
    name: "Brain",
    status: "running",
    cwd: "/zen",
    command: "zen brain",
    capabilities,
  };
}

describe("Brain host_worker Provider Model capabilities", () => {
  test("BrainWorkerRef parsing uses strict normalizeWorkerSessionCapabilities", () => {
    const routed = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: SERVER_ID,
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: {
        host_worker: hostRef({
          structured_events: true,
          model_profile_managed: true,
          model_profile_active_switch: true,
        }),
      },
    });
    expect(routed.byServer[SERVER_ID]?.host_worker?.capabilities).toEqual({
      structured_events: true,
      model_profile_managed: true,
      model_profile_active_switch: true,
    });

    const nestedIgnored = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: SERVER_ID,
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: {
        host_worker: {
          id: HOST_ID,
          name: "Brain",
          status: "running",
          capabilities: {
            structured_events: true,
            model_profiles: { routed: true },
            model_profile_managed: "yes",
            model_profile_active_switch: 1,
          },
        },
      } as never,
    });
    expect(
      nestedIgnored.byServer[SERVER_ID]?.host_worker?.capabilities,
    ).toEqual({
      structured_events: true,
      model_profile_managed: false,
      model_profile_active_switch: false,
    });
  });

  test("routed host action is visible and activatable", () => {
    const sessionKey = makeSessionKey(SERVER_ID, HOST_ID);
    const agent = resolveTerminalRouteWorker({
      storedWorker: undefined,
      routeSessionHint: { name: "hint-name", command: "hint-cmd" },
      sessionKey,
      serverId: SERVER_ID,
      workerId: HOST_ID,
      brainHostWorker: hostRef({
        structured_events: true,
        model_profile_managed: true,
        model_profile_active_switch: true,
      }),
      brainHostServerId: SERVER_ID,
    });
    expect(agent?.capabilities).toEqual({
      structured_events: true,
      model_profile_managed: true,
      model_profile_active_switch: true,
    });
    // Capabilities authorize — not the route hint name/command.
    expect(agent?.name).toBe("Brain");
    const state = routeWorkerProviderModelActionState(agent?.capabilities);
    expect(state.actionVisible).toBe(true);
    expect(state.activationEnabled).toBe(true);
    expect(sessionSupportsModelProfileAction(agent?.capabilities)).toBe(true);
    expect(sessionAllowsModelProfileActivation(agent?.capabilities)).toBe(true);
  });

  test("managed-native host stays hidden (no acknowledged live switch)", () => {
    const agent = resolveTerminalRouteWorker({
      storedWorker: undefined,
      routeSessionHint: {},
      sessionKey: makeSessionKey(SERVER_ID, HOST_ID),
      serverId: SERVER_ID,
      workerId: HOST_ID,
      brainHostWorker: hostRef({
        structured_events: true,
        model_profile_managed: true,
        model_profile_active_switch: false,
      }),
      brainHostServerId: SERVER_ID,
    });
    const state = routeWorkerProviderModelActionState(agent?.capabilities);
    expect(state.actionVisible).toBe(false);
    expect(state.activationEnabled).toBe(false);
    expect(sessionIsManagedReadOnlyProfile(agent?.capabilities)).toBe(true);
  });

  test("missing or false capabilities fail closed (hidden menu)", () => {
    const missing = resolveTerminalRouteWorker({
      storedWorker: undefined,
      routeSessionHint: { name: "Brain", command: "zen brain" },
      sessionKey: makeSessionKey(SERVER_ID, HOST_ID),
      serverId: SERVER_ID,
      workerId: HOST_ID,
      brainHostWorker: hostRef(),
      brainHostServerId: SERVER_ID,
    });
    expect(missing?.capabilities).toBeUndefined();
    expect(routeWorkerProviderModelActionState(missing?.capabilities)).toEqual({
      actionVisible: false,
      activationEnabled: false,
    });

    const falseCaps = resolveTerminalRouteWorker({
      storedWorker: undefined,
      routeSessionHint: {},
      sessionKey: makeSessionKey(SERVER_ID, HOST_ID),
      serverId: SERVER_ID,
      workerId: HOST_ID,
      brainHostWorker: hostRef({
        structured_events: false,
        model_profile_managed: false,
        model_profile_active_switch: false,
      }),
      brainHostServerId: SERVER_ID,
    });
    expect(
      sessionSupportsModelProfileAction(falseCaps?.capabilities),
    ).toBe(false);
  });

  test("wrong server or id is ignored", () => {
    expect(
      brainHostMatchesRoute({
        brainHostWorker: hostRef({
          model_profile_managed: true,
          model_profile_active_switch: true,
          structured_events: true,
        }),
        brainHostServerId: "other-server",
        routeServerId: SERVER_ID,
        routeWorkerId: HOST_ID,
      }),
    ).toBe(false);

    const wrongId = resolveTerminalRouteWorker({
      storedWorker: undefined,
      routeSessionHint: { name: "Brain" },
      sessionKey: makeSessionKey(SERVER_ID, "visible-agent"),
      serverId: SERVER_ID,
      workerId: "visible-agent",
      brainHostWorker: hostRef({
        structured_events: true,
        model_profile_managed: true,
        model_profile_active_switch: true,
      }),
      brainHostServerId: SERVER_ID,
    });
    // Falls back to route hint without host capabilities.
    expect(wrongId?.capabilities).toBeUndefined();
    expect(wrongId?.id).toBe("visible-agent");
  });

  test("reconnect snapshot updates host capabilities", () => {
    let state = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: SERVER_ID,
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: {
        host_worker: hostRef({
          structured_events: true,
          model_profile_managed: true,
          model_profile_active_switch: false,
        }),
      },
    });
    expect(
      state.byServer[SERVER_ID]?.host_worker?.capabilities
        ?.model_profile_active_switch,
    ).toBe(false);

    state = brainReducer(state, {
      type: "BRAIN_SNAPSHOT",
      serverId: SERVER_ID,
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: {
        host_worker: hostRef({
          structured_events: true,
          model_profile_managed: true,
          model_profile_active_switch: true,
        }),
      },
    });
    expect(
      state.byServer[SERVER_ID]?.host_worker?.capabilities,
    ).toEqual({
      structured_events: true,
      model_profile_managed: true,
      model_profile_active_switch: true,
    });

    const agent = resolveTerminalRouteWorker({
      storedWorker: undefined,
      routeSessionHint: {},
      sessionKey: makeSessionKey(SERVER_ID, HOST_ID),
      serverId: SERVER_ID,
      workerId: HOST_ID,
      brainHostWorker: state.byServer[SERVER_ID]?.host_worker,
      brainHostServerId: SERVER_ID,
    });
    expect(sessionAllowsModelProfileActivation(agent?.capabilities)).toBe(
      true,
    );
  });

  test("host merge does not upsert or unhide a double Agent row", () => {
    const beforeWorkers = [...initialWorkerState.workers];
    const brain = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: SERVER_ID,
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: {
        host_worker: hostRef({
          structured_events: true,
          model_profile_managed: true,
          model_profile_active_switch: true,
        }),
        workers: [],
      },
    });
    // Brain agents list stays empty / without host unhide into Agent store.
    expect(brain.byServer[SERVER_ID]?.workers ?? []).toEqual([]);

    const agentsAfter = workerReducer(initialWorkerState, {
      type: "UPSERT_SERVER_WORKERS",
      serverId: SERVER_ID,
      serverName: "Zen",
      serverUrl: "ws://zen",
      workers: [
        {
          id: "visible-1",
          name: "Codex",
          status: "running",
          capabilities: {
            structured_events: true,
            model_profile_managed: true,
            model_profile_active_switch: true,
          },
        },
      ],
    });
    expect(agentsAfter.workers.map((a) => a.id)).toEqual(["visible-1"]);
    expect(agentsAfter.workers.some((a) => a.id === HOST_ID)).toBe(false);

    resolveTerminalRouteWorker({
      storedWorker: undefined,
      routeSessionHint: {},
      sessionKey: makeSessionKey(SERVER_ID, HOST_ID),
      serverId: SERVER_ID,
      workerId: HOST_ID,
      brainHostWorker: brain.byServer[SERVER_ID]?.host_worker,
      brainHostServerId: SERVER_ID,
    });
    // Pure resolve — Agent store unchanged.
    expect(initialWorkerState.workers).toEqual(beforeWorkers);
  });

  test("ordinary visible Agent capabilities remain unchanged when not the host", () => {
    const stored: Worker = {
      key: makeSessionKey(SERVER_ID, "agent-2"),
      id: "agent-2",
      serverId: SERVER_ID,
      serverName: "Zen",
      serverUrl: "ws://zen",
      name: "Codex",
      status: "running",
      summary: "",
      last_output_lines: [],
      updated_at: Date.now(),
      capabilities: {
        structured_events: true,
        model_profile_managed: true,
        model_profile_active_switch: true,
      },
    };
    const resolved = resolveTerminalRouteWorker({
      storedWorker: stored,
      routeSessionHint: { name: "ignored" },
      sessionKey: stored.key,
      serverId: SERVER_ID,
      workerId: "agent-2",
      brainHostWorker: hostRef({
        structured_events: false,
        model_profile_managed: false,
        model_profile_active_switch: false,
      }),
      brainHostServerId: SERVER_ID,
    });
    expect(resolved?.capabilities).toEqual(stored.capabilities);
    expect(resolved?.name).toBe("Codex");
  });
});
