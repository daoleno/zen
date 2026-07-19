import { describe, expect, test } from "bun:test";
import { dismissTerminalToSessions } from "./terminalExitNavigation";

describe("terminal exit navigation", () => {
  test("dismisses to the existing Sessions route", () => {
    const calls: string[] = [];

    dismissTerminalToSessions({
      dismissTo: (href) => calls.push(href),
    });

    expect(calls).toEqual(["/list"]);
  });
});
