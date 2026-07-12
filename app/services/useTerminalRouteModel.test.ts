// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  defaultCodexRenderModeForKind,
  resolveCodexRenderMode,
  supportsChatInterface,
} from "../app/terminal/useTerminalRouteModel";

describe("supportsChatInterface", () => {
  test("Claude is eligible for Open Chat / Open terminal like other structured agents", () => {
    expect(supportsChatInterface("claude")).toBe(true);
    expect(supportsChatInterface("codex")).toBe(true);
    expect(supportsChatInterface("cursor")).toBe(true);
    expect(supportsChatInterface("grok")).toBe(true);
  });

  test("plain terminal is not a structured chat agent", () => {
    expect(supportsChatInterface("terminal")).toBe(false);
    expect(supportsChatInterface("shell")).toBe(false);
  });
});

describe("defaultCodexRenderModeForKind", () => {
  test("structured chat agents default to chat", () => {
    for (const kind of ["claude", "codex", "cursor", "grok"] as const) {
      expect(defaultCodexRenderModeForKind(kind)).toBe("chat");
    }
  });

  test("plain terminal defaults to terminal", () => {
    expect(defaultCodexRenderModeForKind("terminal")).toBe("terminal");
  });
});

describe("resolveCodexRenderMode", () => {
  test("Claude with no persisted preference defaults to chat", () => {
    expect(
      resolveCodexRenderMode({
        kind: "claude",
        sessionKey: "server:claude-1",
        storedModes: {},
      }),
    ).toBe("chat");
  });

  test("structured providers default to chat when unset", () => {
    for (const kind of ["claude", "codex", "cursor", "grok"] as const) {
      expect(
        resolveCodexRenderMode({
          kind,
          sessionKey: `server:${kind}-1`,
          storedModes: {},
        }),
      ).toBe("chat");
    }
  });

  test("plain terminal defaults to terminal when unset", () => {
    expect(
      resolveCodexRenderMode({
        kind: "terminal",
        sessionKey: "server:shell-1",
        storedModes: {},
      }),
    ).toBe("terminal");
  });

  test("persisted terminal override wins over chat default", () => {
    const sessionKey = "server:claude-1";
    expect(
      resolveCodexRenderMode({
        kind: "claude",
        sessionKey,
        storedModes: { [sessionKey]: "terminal" },
      }),
    ).toBe("terminal");
  });

  test("persisted chat preference remains authoritative", () => {
    const sessionKey = "server:shell-1";
    expect(
      resolveCodexRenderMode({
        kind: "terminal",
        sessionKey,
        storedModes: { [sessionKey]: "chat" },
      }),
    ).toBe("chat");
  });
});
