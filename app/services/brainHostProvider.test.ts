import { describe, expect, test } from "bun:test";
import {
  brainHostMatchesRoute,
  resolveTerminalRouteAgent,
  routeAgentProviderModelActionState,
} from "../components/terminal/screen/useTerminalRouteModel";
import {
  sessionAllowsModelProfileActivation,
  sessionIsManagedReadOnlyProfile,
  sessionSupportsModelProfileAction,
  type AgentSessionCapabilities,
} from "./providers/sessionCapabilities";
import {
  agentReducer,
  initialAgentState,
  type Agent,
} from "../store/agents";
import { brainReducer, initialBrainState, type BrainAgentRef } from "../store/brain";
import { makeSessionKey } from "./sessionKeys";

const HOST_ID = "brain-host-1";
const SERVER_ID = "server-a";

function hostRef(
  capabilities?: AgentSessionCapabilities,
): BrainAgentRef {
  return {
    id: HOST_ID,
    name: "Brain",
    status: "running",
    cwd: "/zen",
    command: "zen brain",
    capabilities,
  };
}

describe("Brain host_agent Provider Model capabilities", () => {
  test("BrainAgentRef parsing uses strict normalizeAgentSessionCapabilities", () => {
    const routed = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: SERVER_ID,
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: {
        host_agent: hostRef({
          structured_events: true,
          model_profile_managed: true,
          model_profile_active_switch: true,
        }),
      },
    });
    expect(routed.byServer[SERVER_ID]?.host_agent?.capabilities).toEqual({
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
        host_agent: {
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
      nestedIgnored.byServer[SERVER_ID]?.host_agent?.capabilities,
    ).toEqual({
      structured_events: true,
      model_profile_managed: false,
      model_profile_active_switch: false,
    });
  });

  test("routed host action is visible and activatable", () => {
    const sessionKey = makeSessionKey(SERVER_ID, HOST_ID);
    const agent = resolveTerminalRouteAgent({
      storedAgent: undefined,
      routeSessionHint: { name: "hint-name", command: "hint-cmd" },
      sessionKey,
      serverId: SERVER_ID,
      agentId: HOST_ID,
      brainHostAgent: hostRef({
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
    const state = routeAgentProviderModelActionState(agent?.capabilities);
    expect(state.actionVisible).toBe(true);
    expect(state.activationEnabled).toBe(true);
    expect(state.managedReadOnly).toBe(false);
    expect(sessionSupportsModelProfileAction(agent?.capabilities)).toBe(true);
    expect(sessionAllowsModelProfileActivation(agent?.capabilities)).toBe(true);
  });

  test("managed-native host is visible read-only", () => {
    const agent = resolveTerminalRouteAgent({
      storedAgent: undefined,
      routeSessionHint: {},
      sessionKey: makeSessionKey(SERVER_ID, HOST_ID),
      serverId: SERVER_ID,
      agentId: HOST_ID,
      brainHostAgent: hostRef({
        structured_events: true,
        model_profile_managed: true,
        model_profile_active_switch: false,
      }),
      brainHostServerId: SERVER_ID,
    });
    const state = routeAgentProviderModelActionState(agent?.capabilities);
    expect(state.actionVisible).toBe(true);
    expect(state.activationEnabled).toBe(false);
    expect(state.managedReadOnly).toBe(true);
    expect(sessionIsManagedReadOnlyProfile(agent?.capabilities)).toBe(true);
  });

  test("missing or false capabilities fail closed (hidden menu)", () => {
    const missing = resolveTerminalRouteAgent({
      storedAgent: undefined,
      routeSessionHint: { name: "Brain", command: "zen brain" },
      sessionKey: makeSessionKey(SERVER_ID, HOST_ID),
      serverId: SERVER_ID,
      agentId: HOST_ID,
      brainHostAgent: hostRef(),
      brainHostServerId: SERVER_ID,
    });
    expect(missing?.capabilities).toBeUndefined();
    expect(routeAgentProviderModelActionState(missing?.capabilities)).toEqual({
      actionVisible: false,
      activationEnabled: false,
      managedReadOnly: false,
    });

    const falseCaps = resolveTerminalRouteAgent({
      storedAgent: undefined,
      routeSessionHint: {},
      sessionKey: makeSessionKey(SERVER_ID, HOST_ID),
      serverId: SERVER_ID,
      agentId: HOST_ID,
      brainHostAgent: hostRef({
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
        brainHostAgent: hostRef({
          model_profile_managed: true,
          model_profile_active_switch: true,
          structured_events: true,
        }),
        brainHostServerId: "other-server",
        routeServerId: SERVER_ID,
        routeAgentId: HOST_ID,
      }),
    ).toBe(false);

    const wrongId = resolveTerminalRouteAgent({
      storedAgent: undefined,
      routeSessionHint: { name: "Brain" },
      sessionKey: makeSessionKey(SERVER_ID, "visible-agent"),
      serverId: SERVER_ID,
      agentId: "visible-agent",
      brainHostAgent: hostRef({
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
        host_agent: hostRef({
          structured_events: true,
          model_profile_managed: true,
          model_profile_active_switch: false,
        }),
      },
    });
    expect(
      state.byServer[SERVER_ID]?.host_agent?.capabilities
        ?.model_profile_active_switch,
    ).toBe(false);

    state = brainReducer(state, {
      type: "BRAIN_SNAPSHOT",
      serverId: SERVER_ID,
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: {
        host_agent: hostRef({
          structured_events: true,
          model_profile_managed: true,
          model_profile_active_switch: true,
        }),
      },
    });
    expect(
      state.byServer[SERVER_ID]?.host_agent?.capabilities,
    ).toEqual({
      structured_events: true,
      model_profile_managed: true,
      model_profile_active_switch: true,
    });

    const agent = resolveTerminalRouteAgent({
      storedAgent: undefined,
      routeSessionHint: {},
      sessionKey: makeSessionKey(SERVER_ID, HOST_ID),
      serverId: SERVER_ID,
      agentId: HOST_ID,
      brainHostAgent: state.byServer[SERVER_ID]?.host_agent,
      brainHostServerId: SERVER_ID,
    });
    expect(sessionAllowsModelProfileActivation(agent?.capabilities)).toBe(
      true,
    );
  });

  test("host merge does not upsert or unhide a double Agent row", () => {
    const beforeAgents = [...initialAgentState.agents];
    const brain = brainReducer(initialBrainState, {
      type: "BRAIN_SNAPSHOT",
      serverId: SERVER_ID,
      serverName: "Zen",
      serverUrl: "ws://zen",
      brain: {
        host_agent: hostRef({
          structured_events: true,
          model_profile_managed: true,
          model_profile_active_switch: true,
        }),
        agents: [],
      },
    });
    // Brain agents list stays empty / without host unhide into Agent store.
    expect(brain.byServer[SERVER_ID]?.agents ?? []).toEqual([]);

    const agentsAfter = agentReducer(initialAgentState, {
      type: "UPSERT_SERVER_AGENTS",
      serverId: SERVER_ID,
      serverName: "Zen",
      serverUrl: "ws://zen",
      agents: [
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
    expect(agentsAfter.agents.map((a) => a.id)).toEqual(["visible-1"]);
    expect(agentsAfter.agents.some((a) => a.id === HOST_ID)).toBe(false);

    resolveTerminalRouteAgent({
      storedAgent: undefined,
      routeSessionHint: {},
      sessionKey: makeSessionKey(SERVER_ID, HOST_ID),
      serverId: SERVER_ID,
      agentId: HOST_ID,
      brainHostAgent: brain.byServer[SERVER_ID]?.host_agent,
      brainHostServerId: SERVER_ID,
    });
    // Pure resolve — Agent store unchanged.
    expect(initialAgentState.agents).toEqual(beforeAgents);
  });

  test("ordinary visible Agent capabilities remain unchanged when not the host", () => {
    const stored: Agent = {
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
    const resolved = resolveTerminalRouteAgent({
      storedAgent: stored,
      routeSessionHint: { name: "ignored" },
      sessionKey: stored.key,
      serverId: SERVER_ID,
      agentId: "agent-2",
      brainHostAgent: hostRef({
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
