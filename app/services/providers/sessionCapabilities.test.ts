import { describe, expect, test } from "bun:test";
import {
  capabilityProviderDisagreementMessage,
  normalizeAgentSessionCapabilities,
  sessionAllowsModelProfileActivation,
  sessionIsManagedReadOnlyProfile,
  sessionSupportsModelProfileAction,
} from "./sessionCapabilities";
import {
  agentReducer,
  initialAgentState,
  type RawAgent,
} from "../../store/agents";

const UPDATED_AT = 1_700_000_000_000;

const baseRaw = (id: string, capabilities?: RawAgent["capabilities"]): RawAgent => ({
  id,
  name: id,
  status: "running",
  updated_at: UPDATED_AT,
  capabilities,
});

describe("flat agent_session Provider Model capabilities", () => {
  test("list and incremental normalization use the same typed flat booleans", () => {
    const caps = {
      structured_events: true,
      model_profile_managed: true,
      model_profile_active_switch: false,
    };
    const fromList = normalizeAgentSessionCapabilities(caps);
    expect(fromList).toEqual({
      structured_events: true,
      model_profile_managed: true,
      model_profile_active_switch: false,
    });

    let state = agentReducer(initialAgentState, {
      type: "UPSERT_SERVER_AGENTS",
      serverId: "s1",
      serverName: "S1",
      serverUrl: "https://s1.test",
      agents: [baseRaw("a1", caps)],
    });
    expect(state.agents[0]?.capabilities).toEqual(fromList);

    state = agentReducer(state, {
      type: "UPSERT_AGENT",
      serverId: "s1",
      serverName: "S1",
      serverUrl: "https://s1.test",
      agent: baseRaw("a1", {
        structured_events: true,
        model_profile_managed: true,
        model_profile_active_switch: true,
      }),
    });
    expect(state.agents[0]?.capabilities).toEqual({
      structured_events: true,
      model_profile_managed: true,
      model_profile_active_switch: true,
    });
  });

  test("native managed true/false is read-only; Responses+Anthropic true/true activate", () => {
    const native = normalizeAgentSessionCapabilities({
      structured_events: true,
      model_profile_managed: true,
      model_profile_active_switch: false,
    });
    expect(sessionSupportsModelProfileAction(native)).toBe(true);
    expect(sessionIsManagedReadOnlyProfile(native)).toBe(true);
    expect(sessionAllowsModelProfileActivation(native)).toBe(false);

    const responses = normalizeAgentSessionCapabilities({
      structured_events: false,
      model_profile_managed: true,
      model_profile_active_switch: true,
    });
    expect(sessionSupportsModelProfileAction(responses)).toBe(true);
    expect(sessionIsManagedReadOnlyProfile(responses)).toBe(false);
    expect(sessionAllowsModelProfileActivation(responses)).toBe(true);

    const anthropic = normalizeAgentSessionCapabilities({
      structured_events: true,
      model_profile_managed: true,
      model_profile_active_switch: true,
    });
    expect(sessionAllowsModelProfileActivation(anthropic)).toBe(true);
  });

  test("ordinary false/false and old-daemon missing fail closed", () => {
    const ordinary = normalizeAgentSessionCapabilities({
      structured_events: true,
      model_profile_managed: false,
      model_profile_active_switch: false,
    });
    expect(sessionSupportsModelProfileAction(ordinary)).toBe(false);
    expect(sessionAllowsModelProfileActivation(ordinary)).toBe(false);

    expect(normalizeAgentSessionCapabilities(undefined)).toBeUndefined();
    expect(normalizeAgentSessionCapabilities(null)).toBeUndefined();
    expect(sessionSupportsModelProfileAction(null)).toBe(false);
    expect(sessionSupportsModelProfileAction(undefined)).toBe(false);
  });

  test("malformed nested legacy shapes and non-booleans fail closed", () => {
    // Nested legacy model_profiles must not authorize.
    const nested = normalizeAgentSessionCapabilities({
      structured_events: true,
      model_profiles: {
        routed: true,
        active_switch: "route_binding",
      },
    });
    expect(nested).toEqual({
      structured_events: true,
      model_profile_managed: false,
      model_profile_active_switch: false,
    });
    expect(sessionSupportsModelProfileAction(nested)).toBe(false);

    const malformed = normalizeAgentSessionCapabilities({
      structured_events: "yes",
      model_profile_managed: 1,
      model_profile_active_switch: "route_binding",
    });
    expect(malformed).toEqual({
      structured_events: false,
      model_profile_managed: false,
      model_profile_active_switch: false,
    });
  });

  test("equality and update propagation across list then incremental", () => {
    const caps = {
      structured_events: false,
      model_profile_managed: true,
      model_profile_active_switch: true,
    };
    let state = agentReducer(initialAgentState, {
      type: "UPSERT_SERVER_AGENTS",
      serverId: "s1",
      serverName: "S1",
      serverUrl: "https://s1.test",
      agents: [baseRaw("tmux:@1", caps)],
    });
    const first = state.agents[0];
    state = agentReducer(state, {
      type: "UPSERT_SERVER_AGENTS",
      serverId: "s1",
      serverName: "S1",
      serverUrl: "https://s1.test",
      agents: [baseRaw("tmux:@1", { ...caps })],
    });
    // Identical capabilities reuse the previous agent object.
    expect(state.agents[0]).toBe(first);

    state = agentReducer(state, {
      type: "UPSERT_AGENT",
      serverId: "s1",
      serverName: "S1",
      serverUrl: "https://s1.test",
      agent: baseRaw("tmux:@1", {
        ...caps,
        model_profile_active_switch: false,
      }),
    });
    expect(state.agents[0]).not.toBe(first);
    expect(state.agents[0]?.capabilities?.model_profile_active_switch).toBe(
      false,
    );
    expect(sessionIsManagedReadOnlyProfile(state.agents[0]?.capabilities)).toBe(
      true,
    );
  });

  test("menu visibility and sheet modes from capability matrix", () => {
    expect(
      sessionSupportsModelProfileAction(
        normalizeAgentSessionCapabilities({
          structured_events: true,
          model_profile_managed: false,
          model_profile_active_switch: false,
        }),
      ),
    ).toBe(false);

    const readonlyNative = normalizeAgentSessionCapabilities({
      structured_events: true,
      model_profile_managed: true,
      model_profile_active_switch: false,
    });
    expect(sessionSupportsModelProfileAction(readonlyNative)).toBe(true);
    expect(sessionIsManagedReadOnlyProfile(readonlyNative)).toBe(true);
    expect(sessionAllowsModelProfileActivation(readonlyNative)).toBe(false);

    const activeRouted = normalizeAgentSessionCapabilities({
      structured_events: false,
      model_profile_managed: true,
      model_profile_active_switch: true,
    });
    expect(sessionSupportsModelProfileAction(activeRouted)).toBe(true);
    expect(sessionAllowsModelProfileActivation(activeRouted)).toBe(true);
  });

  test("capability/route disagreement is typed, never fallback authorization", () => {
    expect(
      capabilityProviderDisagreementMessage({
        managed: true,
        activeSwitch: false,
        selectionFound: false,
        hotSwitchable: false,
      }),
    ).toMatch(/managed Model/i);
    expect(
      capabilityProviderDisagreementMessage({
        managed: true,
        activeSwitch: true,
        selectionFound: true,
        hotSwitchable: false,
      }),
    ).toMatch(/read-only/i);
    expect(
      capabilityProviderDisagreementMessage({
        managed: false,
        activeSwitch: false,
        selectionFound: false,
        hotSwitchable: false,
      }),
    ).toBeNull();
  });
});
