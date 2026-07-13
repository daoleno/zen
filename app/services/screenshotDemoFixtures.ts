import type { CodexConversationEvent } from "./codexConversation";
import type { Agent } from "../store/agents";

export const SCREENSHOT_DEMO_SERVER_ID = "demo-server";
export const SCREENSHOT_DEMO_SERVER_NAME = "Studio Mac";
export const SCREENSHOT_DEMO_SERVER_URL = "https://demo.invalid";

const DEMO_TIMESTAMP = "2026-06-18T09:30:00.000Z";

export const SCREENSHOT_CHAT_EVENTS: CodexConversationEvent[] = [
  {
    id: "chat-user",
    seq: 1,
    kind: "user_message",
    timestamp: DEMO_TIMESTAMP,
    body: "Polish the mobile handoff and verify the smallest layout.",
  },
  {
    id: "chat-plan",
    seq: 2,
    kind: "plan",
    timestamp: DEMO_TIMESTAMP,
    explanation: "I’ll keep the agent running here while you continue from your phone.",
    plan: [
      { step: "Tighten the handoff layout", status: "completed" },
      { step: "Run focused UI checks", status: "completed" },
      { step: "Review the 320 px viewport", status: "in_progress" },
    ],
  },
  {
    id: "chat-command",
    seq: 3,
    kind: "command",
    timestamp: DEMO_TIMESTAMP,
    command: "bun test onboarding && bunx tsc --noEmit",
    output: "12 tests passed\nTypeScript check passed",
    status: "completed",
    exit_code: 0,
  },
  {
    id: "chat-assistant",
    seq: 4,
    kind: "assistant_message",
    timestamp: DEMO_TIMESTAMP,
    body: "The handoff is ready. The agent is still running on your computer, and the focused checks pass. Open **Terminal** anytime for the live process.",
  },
];

export const SCREENSHOT_BRAIN_EVENTS: CodexConversationEvent[] = [
  {
    id: "brain-user",
    seq: 1,
    kind: "user_message",
    timestamp: DEMO_TIMESTAMP,
    body: "Prepare the next release while I’m away from my desk.",
  },
  {
    id: "brain-plan",
    seq: 2,
    kind: "plan",
    timestamp: DEMO_TIMESTAMP,
    explanation: "Brain is carrying the release context and coordinating focused work.",
    plan: [
      { step: "Audit the onboarding flow", status: "completed" },
      { step: "Delegate mobile regression checks", status: "in_progress" },
      { step: "Summarize release readiness", status: "pending" },
    ],
  },
  {
    id: "brain-tool",
    seq: 3,
    kind: "tool",
    timestamp: DEMO_TIMESTAMP,
    tool_name: "agent progress",
    title: "Mobile QA agent",
    input: "Review compact layouts in the sample workspace",
    output: "Running accessibility and viewport checks",
    status: "running",
  },
  {
    id: "brain-assistant",
    seq: 4,
    kind: "assistant_message",
    timestamp: DEMO_TIMESTAMP,
    body: "I’ll keep the workspace and delegated results together here. You can leave this chat and return without rebuilding the context.",
  },
];

export const SCREENSHOT_SESSION_AGENTS: Agent[] = [
  {
    key: "demo-server:atlas-mobile",
    id: "atlas-mobile",
    serverId: SCREENSHOT_DEMO_SERVER_ID,
    serverName: SCREENSHOT_DEMO_SERVER_NAME,
    serverUrl: SCREENSHOT_DEMO_SERVER_URL,
    name: "Mobile handoff",
    project: "atlas-notes",
    cwd: "/Users/demo/Projects/atlas-notes",
    command: "codex",
    summary: "Reviewing the compact onboarding layout",
    status: "running",
    last_output_lines: ["Reviewing the compact onboarding layout"],
    updated_at: Date.parse(DEMO_TIMESTAMP),
  },
  {
    key: "demo-server:release-brain",
    id: "release-brain",
    serverId: SCREENSHOT_DEMO_SERVER_ID,
    serverName: SCREENSHOT_DEMO_SERVER_NAME,
    serverUrl: SCREENSHOT_DEMO_SERVER_URL,
    name: "Release coordinator",
    project: "sample-app",
    cwd: "/Users/demo/Projects/sample-app",
    command: "codex",
    summary: "Delegated mobile regression checks",
    status: "running",
    delegated: true,
    last_output_lines: ["Delegated mobile regression checks"],
    updated_at: Date.parse(DEMO_TIMESTAMP) - 60_000,
  },
  {
    key: "demo-server:api-review",
    id: "api-review",
    serverId: SCREENSHOT_DEMO_SERVER_ID,
    serverName: SCREENSHOT_DEMO_SERVER_NAME,
    serverUrl: SCREENSHOT_DEMO_SERVER_URL,
    name: "API review",
    project: "weather-kit",
    cwd: "/Users/demo/Projects/weather-kit",
    command: "claude",
    summary: "Waiting for a product decision",
    status: "blocked",
    last_output_lines: ["Waiting for a product decision"],
    updated_at: Date.parse(DEMO_TIMESTAMP) - 18 * 60_000,
  },
  {
    key: "demo-server:docs-pass",
    id: "docs-pass",
    serverId: SCREENSHOT_DEMO_SERVER_ID,
    serverName: SCREENSHOT_DEMO_SERVER_NAME,
    serverUrl: SCREENSHOT_DEMO_SERVER_URL,
    name: "Docs refresh",
    project: "garden-journal",
    cwd: "/Users/demo/Projects/garden-journal",
    command: "cursor-agent",
    summary: "README links and examples verified",
    status: "done",
    last_output_lines: ["README links and examples verified"],
    updated_at: Date.parse(DEMO_TIMESTAMP) - 45 * 60_000,
  },
];

export const SCREENSHOT_STATS_FIXTURE = {
  ranges: {
    all: {
      cost: 18.42,
      costKnown: true,
      totalTokens: 2_480_000,
      totalTokensKnown: true,
      inputTokens: 1_720_000,
      outputTokens: 510_000,
      reasoningTokens: 250_000,
      cacheRead: 640_000,
      cacheCreate: 120_000,
      tokenBreakdownKnown: true,
      sessions: 42,
      models: [
        { name: "codex-large", totalTokens: 1_640_000, sessions: 26, cost: 11.62 },
        { name: "claude-sonnet", totalTokens: 840_000, sessions: 16, cost: 6.8 },
      ],
      projects: [
        { name: "atlas-notes", totalTokens: 980_000, sessions: 17, cost: 7.24 },
        { name: "weather-kit", totalTokens: 760_000, sessions: 13, cost: 5.83 },
        { name: "garden-journal", totalTokens: 740_000, sessions: 12, cost: 5.35 },
      ],
      skills: [
        { name: "mobile-qa", calls: 18, projects: ["atlas-notes"] },
        { name: "release-review", calls: 11, projects: ["weather-kit"] },
      ],
      tools: [
        { name: "exec", calls: 96 },
        { name: "apply_patch", calls: 38 },
      ],
      days: [
        { date: "2026-06-14", totalTokens: 320_000, sessions: 6, cost: 2.1 },
        { date: "2026-06-15", totalTokens: 510_000, sessions: 9, cost: 3.8 },
        { date: "2026-06-16", totalTokens: 420_000, sessions: 8, cost: 3.2 },
        { date: "2026-06-17", totalTokens: 610_000, sessions: 11, cost: 4.9 },
        { date: "2026-06-18", totalTokens: 620_000, sessions: 8, cost: 4.42 },
      ],
    },
  },
  codexSubscriptions: [
    {
      authKind: "official",
      state: "available",
      plan: "plus",
      windows: [
        { name: "primary", usedPercent: 34, windowMinutes: 300 },
        { name: "secondary", usedPercent: 18, windowMinutes: 10080 },
      ],
      serverLabel: SCREENSHOT_DEMO_SERVER_NAME,
    },
  ],
} as const;
