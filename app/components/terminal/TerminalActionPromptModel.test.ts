import { describe, expect, test } from "bun:test";
import { buildTerminalActionPrompt } from "./TerminalActionPromptModel";
import {
  bumpServerConnectionGeneration,
  isAgentSessionListFreshForConnection,
  liveActionPromptScopeKey,
  stampAgentSessionListGeneration,
  type TransportConnectionState,
} from "../../services/agentSessionListTransport";

const WEEKLY_LIMIT_LINES = [
  "◆ Task completed in 2m45s: Wait for dedicated em",
  "You hit your weekly limit.",
  "1 (O) Upgrade tier          Upgrade to a higher tier for more usage",
  "2 (O) Buy more credits      Purchase credits to keep using Grok Build",
  "↑/↓ navigate · y copy Enter:submit",
  "Esc:unselect | Tab:scrollback |",
];

const GROK_COMMAND = "grok --no-alt-screen --permission-mode bypassPermissions";

type TransportSlice = {
  connectionState: TransportConnectionState;
  connectionGenerationByServer: Record<string, number>;
  agentSessionListGenerationByServer: Record<string, number>;
};

function applyConnection(
  slice: TransportSlice,
  serverId: string,
  connectionState: TransportConnectionState,
): TransportSlice {
  return {
    connectionState,
    connectionGenerationByServer: bumpServerConnectionGeneration(
      slice.connectionGenerationByServer,
      serverId,
      slice.connectionState,
      connectionState,
    ),
    agentSessionListGenerationByServer: slice.agentSessionListGenerationByServer,
  };
}

function applyFullAgentSessionList(slice: TransportSlice, serverId: string): TransportSlice {
  return {
    ...slice,
    agentSessionListGenerationByServer: stampAgentSessionListGeneration({
      connectionState: slice.connectionState,
      connectionGeneration: slice.connectionGenerationByServer[serverId] ?? 0,
      agentSessionListGenerationByServer: slice.agentSessionListGenerationByServer,
      serverId,
    }),
  };
}

function isFresh(slice: TransportSlice, serverId: string) {
  return isAgentSessionListFreshForConnection({
    connectionState: slice.connectionState,
    connectionGeneration: slice.connectionGenerationByServer[serverId] ?? 0,
    agentSessionListGeneration:
      slice.agentSessionListGenerationByServer[serverId] ?? 0,
  });
}

function projectPrompt(
  slice: TransportSlice,
  serverId: string,
  agent: {
    id: string;
    status: string;
    summary?: string;
    command?: string;
    processId?: number;
    startedAt?: number;
    lastOutputLines?: string[];
  },
) {
  if (!isFresh(slice, serverId)) {
    return null;
  }
  return buildTerminalActionPrompt({
    status: agent.status,
    summary: agent.summary,
    lastOutputLines: agent.lastOutputLines,
    command: agent.command,
    scopeKey: liveActionPromptScopeKey({
      agentId: agent.id,
      processId: agent.processId,
      startedAt: agent.startedAt,
      connectionGeneration: slice.connectionGenerationByServer[serverId] ?? 0,
    }),
  });
}

const LIVE_BLOCKED = {
  id: "grok-1",
  status: "blocked",
  command: GROK_COMMAND,
  summary: "↑/↓ navigate · y copy Enter:submit",
  processId: 41413,
  startedAt: 900,
  lastOutputLines: WEEKLY_LIMIT_LINES,
};

describe("buildTerminalActionPrompt", () => {
  test("projects Grok weekly-limit choice menu as display-only Interface state", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      command: GROK_COMMAND,
      summary: "↑/↓ navigate · y copy Enter:submit",
      lastOutputLines: WEEKLY_LIMIT_LINES,
    });

    expect(prompt).not.toBeNull();
    expect(prompt?.actionable).toBe(false);
    expect(prompt?.title).toBe("You hit your weekly limit.");
    expect(prompt?.options.map((option) => option.label)).toEqual([
      "Upgrade tier",
      "Buy more credits",
    ]);
    expect(prompt?.options.map((option) => option.description)).toEqual([
      "Upgrade to a higher tier for more usage",
      "Purchase credits to keep using Grok Build",
    ]);
    expect(prompt?.defaultOptionId).toBeUndefined();
    expect(prompt?.options.every((option) => !option.default && !option.primary)).toBe(true);
    expect(prompt?.inputHints).toContain("navigate");
    expect(prompt?.inputHints?.toLowerCase()).toContain("enter");
  });

  test("preserves proven Grok selection caret without fabricating others", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      command: "grok",
      lastOutputLines: [
        "You hit your free usage limit.",
        "› 1 ○ Upgrade to SuperGrok",
        "  2 ○ Continue free",
        "↑/↓ navigate · enter confirm · esc cancel",
      ],
    });

    expect(prompt?.actionable).toBe(false);
    expect(prompt?.defaultOptionId).toBe("1");
    expect(prompt?.options[0]?.default).toBe(true);
    expect(prompt?.options[1]?.default).toBe(false);
  });

  test("preserves proven filled radio glyph without fabricating others", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      command: "grok",
      lastOutputLines: [
        "Choose a plan",
        "1 ● Upgrade to SuperGrok",
        "2 ○ Continue free",
        "↑/↓ navigate · enter confirm · esc cancel",
      ],
    });

    expect(prompt?.actionable).toBe(false);
    expect(prompt?.defaultOptionId).toBe("1");
  });

  test("does not invent selection when multiple filled markers appear", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      command: "grok",
      lastOutputLines: [
        "Choose a plan",
        "1 ● Upgrade to SuperGrok",
        "2 ● Continue free",
        "↑/↓ navigate · enter confirm · esc cancel",
      ],
    });

    expect(prompt?.defaultOptionId).toBeUndefined();
    expect(prompt?.options.every((option) => !option.default)).toBe(true);
  });

  test("clears when blocked status resolves (stale-session safeguard)", () => {
    expect(
      buildTerminalActionPrompt({
        status: "running",
        command: GROK_COMMAND,
        lastOutputLines: WEEKLY_LIMIT_LINES,
      }),
    ).toBeNull();
    expect(
      buildTerminalActionPrompt({
        status: "unknown",
        command: GROK_COMMAND,
        lastOutputLines: WEEKLY_LIMIT_LINES,
      }),
    ).toBeNull();
  });

  test("clears when pane lines no longer contain a choice menu", () => {
    expect(
      buildTerminalActionPrompt({
        status: "blocked",
        command: GROK_COMMAND,
        lastOutputLines: [
          "│ ❯                                                                        │",
          "╰─────────────────────────────────────── Grok 4.5 (high) · always-approve ─╯",
          "Shift+Tab:mode  │  Ctrl+c:cancel  │  Ctrl+g:send to bg  │  Ctrl+x:shortcuts",
        ],
      }),
    ).toBeNull();
  });

  test("does not invent a card from options without navigate footer", () => {
    expect(
      buildTerminalActionPrompt({
        status: "blocked",
        command: GROK_COMMAND,
        lastOutputLines: [
          "1 (O) Upgrade tier",
          "2 (O) Buy more credits",
          "Shift+Tab:mode  │  Ctrl+c:cancel",
        ],
      }),
    ).toBeNull();
  });

  test("does not invent a card from navigate footer without numbered choices", () => {
    expect(
      buildTerminalActionPrompt({
        status: "blocked",
        command: GROK_COMMAND,
        lastOutputLines: [
          "Resume session",
          "↑/↓ navigate · enter confirm · esc cancel",
        ],
      }),
    ).toBeNull();
  });

  test("keeps Codex approval prompts actionable", () => {
    const prompt = buildTerminalActionPrompt({
      status: "blocked",
      command: "codex --dangerously-bypass-approvals-and-sandbox",
      lastOutputLines: [
        "$ systemctl list-unit-files --state=enabled",
        "› 1. Yes, proceed (y)",
        "  2. No, and tell Codex what to do differently (esc)",
        "Press enter to confirm or esc to cancel",
      ],
    });

    expect(prompt?.actionable).toBe(true);
    expect(prompt?.title).toBe("Waiting for confirmation");
    expect(prompt?.options[0]?.key).toBe("y");
    expect(prompt?.defaultOptionId).toBe("1");
  });

  test("does not invent a card from Grok retry_state / 402 text alone", () => {
    expect(
      buildTerminalActionPrompt({
        status: "blocked",
        command: GROK_COMMAND,
        summary: "Provider request failed",
        lastOutputLines: [
          "API error (status 402 Payment Required): Grok Build usage balance exhausted",
          "Request URL: https://cli-chat-proxy.grok.com/v1/responses",
        ],
      }),
    ).toBeNull();
  });

  for (const command of [
    "codex --dangerously-bypass-approvals-and-sandbox",
    "claude --dangerously-skip-permissions",
    "cursor-agent --force --sandbox disabled",
    "zsh",
    "",
  ]) {
    test(`does not project Grok card under non-Grok command (${command || "unknown"})`, () => {
      expect(
        buildTerminalActionPrompt({
          status: "blocked",
          command,
          lastOutputLines: WEEKLY_LIMIT_LINES,
        }),
      ).toBeNull();
    });
  }

  test("scopes prompt id to the live process/session generation", () => {
    const first = buildTerminalActionPrompt({
      status: "blocked",
      command: GROK_COMMAND,
      scopeKey: liveActionPromptScopeKey({
        agentId: "agent-a",
        processId: 11,
        startedAt: 100,
        connectionGeneration: 1,
      }),
      lastOutputLines: WEEKLY_LIMIT_LINES,
    });
    const second = buildTerminalActionPrompt({
      status: "blocked",
      command: GROK_COMMAND,
      scopeKey: liveActionPromptScopeKey({
        agentId: "agent-a",
        processId: 22,
        startedAt: 200,
        connectionGeneration: 2,
      }),
      lastOutputLines: WEEKLY_LIMIT_LINES,
    });

    expect(first?.id.startsWith("agent-a:11:100:1|")).toBe(true);
    expect(second?.id.startsWith("agent-a:22:200:2|")).toBe(true);
    expect(first?.id).not.toBe(second?.id);
  });
});

describe("agent_session_list connection-generation freshness", () => {
  test("connect -> full snapshot allows current live prompt", () => {
    let slice: TransportSlice = {
      connectionState: "offline",
      connectionGenerationByServer: {},
      agentSessionListGenerationByServer: {},
    };
    slice = applyConnection(slice, "server", "connecting");
    slice = applyConnection(slice, "server", "connected");
    expect(isFresh(slice, "server")).toBe(false);

    slice = applyFullAgentSessionList(slice, "server");
    expect(isFresh(slice, "server")).toBe(true);
    expect(projectPrompt(slice, "server", LIVE_BLOCKED)?.title).toBe(
      "You hit your weekly limit.",
    );
  });

  test("disconnect hides; reconnect with retained identical data stays hidden until new full list", () => {
    let slice: TransportSlice = {
      connectionState: "offline",
      connectionGenerationByServer: {},
      agentSessionListGenerationByServer: {},
    };
    slice = applyConnection(slice, "server", "connecting");
    slice = applyConnection(slice, "server", "connected");
    slice = applyFullAgentSessionList(slice, "server");
    expect(projectPrompt(slice, "server", LIVE_BLOCKED)?.actionable).toBe(false);

    slice = applyConnection(slice, "server", "connecting");
    expect(isFresh(slice, "server")).toBe(false);
    expect(projectPrompt(slice, "server", LIVE_BLOCKED)).toBeNull();

    // Reconnect while retained agent rows still carry the identical menu.
    slice = applyConnection(slice, "server", "connected");
    expect(isFresh(slice, "server")).toBe(false);
    expect(projectPrompt(slice, "server", LIVE_BLOCKED)).toBeNull();

    // Incremental agent upsert is not a full agent_session_list.
    expect(isFresh(slice, "server")).toBe(false);

    slice = applyFullAgentSessionList(slice, "server");
    expect(isFresh(slice, "server")).toBe(true);
    expect(projectPrompt(slice, "server", LIVE_BLOCKED)?.title).toBe(
      "You hit your weekly limit.",
    );
  });

  test("fresh resolved snapshot stays absent after full list", () => {
    let slice: TransportSlice = {
      connectionState: "offline",
      connectionGenerationByServer: {},
      agentSessionListGenerationByServer: {},
    };
    slice = applyConnection(slice, "server", "connected");
    slice = applyFullAgentSessionList(slice, "server");
    expect(isFresh(slice, "server")).toBe(true);
    expect(
      projectPrompt(slice, "server", {
        id: "grok-1",
        status: "unknown",
        command: GROK_COMMAND,
        lastOutputLines: [
          "│ ❯",
          "╰─────────────────────────────────────── Grok 4.5 (high) · always-approve ─╯",
        ],
      }),
    ).toBeNull();
  });
});
