// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  defaultInterfaceRenderModeForKind,
  resolveInterfaceRenderMode,
  supportsChatInterface,
} from "../app/terminal/useTerminalRouteModel";

describe("supportsChatInterface", () => {
  test("Claude is eligible for Open Chat / Open terminal like other structured agents", () => {
    expect(supportsChatInterface("claude")).toBe(true);
    expect(supportsChatInterface("codex")).toBe(true);
    expect(supportsChatInterface("cursor")).toBe(true);
    expect(supportsChatInterface("grok")).toBe(true);
    expect(supportsChatInterface("pi")).toBe(true);
    expect(supportsChatInterface("opencode")).toBe(true);
  });

  test("plain terminal is not a structured chat agent", () => {
    expect(supportsChatInterface("terminal")).toBe(false);
    expect(supportsChatInterface("shell")).toBe(false);
  });

  test("generic provider is eligible through structured-events capability", () => {
    expect(supportsChatInterface("terminal", { structured_events: true })).toBe(
      true,
    );
    expect(
      supportsChatInterface("future-provider", { structured_events: true }),
    ).toBe(true);
    expect(
      supportsChatInterface("future-provider", { structured_events: false }),
    ).toBe(false);
  });
});

describe("defaultInterfaceRenderModeForKind", () => {
  test("structured chat agents default to chat", () => {
    for (const kind of ["claude", "codex", "cursor", "grok"] as const) {
      expect(defaultInterfaceRenderModeForKind(kind)).toBe("chat");
    }
  });

  test("plain terminal defaults to terminal", () => {
    expect(defaultInterfaceRenderModeForKind("terminal")).toBe("terminal");
  });

  test("generic structured capability defaults to chat", () => {
    expect(
      defaultInterfaceRenderModeForKind("future-provider", {
        structured_events: true,
      }),
    ).toBe("chat");
  });
});

describe("resolveInterfaceRenderMode", () => {
  test("Claude with no persisted preference defaults to chat", () => {
    expect(
      resolveInterfaceRenderMode({
        kind: "claude",
        sessionKey: "server:claude-1",
        storedModes: {},
      }),
    ).toBe("chat");
  });

  test("structured providers default to chat when unset", () => {
    for (const kind of ["claude", "codex", "cursor", "grok"] as const) {
      expect(
        resolveInterfaceRenderMode({
          kind,
          sessionKey: `server:${kind}-1`,
          storedModes: {},
        }),
      ).toBe("chat");
    }
  });

  test("plain terminal defaults to terminal when unset", () => {
    expect(
      resolveInterfaceRenderMode({
        kind: "terminal",
        sessionKey: "server:shell-1",
        storedModes: {},
      }),
    ).toBe("terminal");
  });

  test("generic structured provider defaults to chat when unset", () => {
    expect(
      resolveInterfaceRenderMode({
        kind: "terminal",
        capabilities: { structured_events: true },
        sessionKey: "server:future-1",
        storedModes: {},
      }),
    ).toBe("chat");
  });

  test("persisted terminal override wins over chat default", () => {
    const sessionKey = "server:claude-1";
    expect(
      resolveInterfaceRenderMode({
        kind: "claude",
        sessionKey,
        storedModes: { [sessionKey]: "terminal" },
      }),
    ).toBe("terminal");
  });

  test("persisted chat preference remains authoritative", () => {
    const sessionKey = "server:shell-1";
    expect(
      resolveInterfaceRenderMode({
        kind: "terminal",
        sessionKey,
        storedModes: { [sessionKey]: "chat" },
      }),
    ).toBe("chat");
  });
});
