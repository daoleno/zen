import { describe, expect, test } from "bun:test";
import type { AppStateStatus } from "react-native";
import {
  createTerminalConnectedPresenceHandler,
  currentTerminalPresence,
} from "../app/terminal/terminalPresence";

describe("focused Terminal presence", () => {
  test("active ready focus resolves to the exact current agent", () => {
    expect(
      currentTerminalPresence({
        serverId: "server-1",
        agentId: "agent-1",
        sessionKey: "server-1:agent-1",
        appState: "active",
        focused: true,
      }),
    ).toEqual({ serverId: "server-1", agentId: "agent-1" });
  });

  test("background, unfocused, and unready facts resolve to clear presence", () => {
    const base = {
      serverId: "server-1",
      agentId: "agent-1",
      sessionKey: "server-1:agent-1",
      appState: "active" as AppStateStatus,
      focused: true,
    };
    expect(currentTerminalPresence({ ...base, appState: "background" })).toBeNull();
    expect(currentTerminalPresence({ ...base, focused: false })).toBeNull();
    expect(currentTerminalPresence({ ...base, sessionKey: null })).toBeNull();
  });

  test("each matching reconnect freshly declares current presence once", () => {
    let appState: AppStateStatus = "active";
    const declarations: Array<{ serverId: string; agentId: string } | null> = [];
    const onConnected = createTerminalConnectedPresenceHandler(
      "server-1",
      () => {
        declarations.push(
          currentTerminalPresence({
            serverId: "server-1",
            agentId: "agent-current",
            sessionKey: "session-current",
            appState,
            focused: true,
          }),
        );
      },
    );

    onConnected({ serverId: "server-2" });
    expect(declarations).toEqual([]);

    onConnected({ serverId: "server-1" });
    expect(declarations).toEqual([
      { serverId: "server-1", agentId: "agent-current" },
    ]);

    appState = "background";
    onConnected({ serverId: "server-1" });
    expect(declarations).toEqual([
      { serverId: "server-1", agentId: "agent-current" },
      null,
    ]);

    appState = "active";
    onConnected({ serverId: "server-1" });
    expect(declarations).toEqual([
      { serverId: "server-1", agentId: "agent-current" },
      null,
      { serverId: "server-1", agentId: "agent-current" },
    ]);
  });
});
