import type { CodexConversationEvent } from "./codexConversation";
import type { Agent } from "../store/agents";
import type { PendingUserMessage } from "../components/terminal/InterfaceChatSession";
import type { ProvidersSnapshot } from "./providers/types";

export const SCREENSHOT_DEMO_SERVER_ID = "demo-server";
export const SCREENSHOT_DEMO_SERVER_NAME = "Studio Mac";
export const SCREENSHOT_DEMO_SERVER_URL = "https://demo.invalid";

export const SCREENSHOT_CHAT_PENDING_FIXTURES = [
  "none",
  "pending",
  "failed",
  "long",
] as const;

export type ScreenshotChatPendingFixture =
  (typeof SCREENSHOT_CHAT_PENDING_FIXTURES)[number];

const DEMO_TIMESTAMP = "2026-06-18T09:30:00.000Z";
const DEMO_PENDING_CREATED_AT = "2026-06-18T09:30:12.000Z";

/** Deterministic Pending rows for screenshot-demo; never used by live chat. */
export function screenshotChatPendingUserMessages(
  fixture: ScreenshotChatPendingFixture,
): PendingUserMessage[] {
  if (fixture === "none") {
    return [];
  }
  if (fixture === "failed") {
    return [
      {
        id: "demo-pending-failed",
        body: "Retry this short send after the transport failure.",
        sentText: "Retry this short send after the transport failure.",
        attachments: [],
        createdAt: DEMO_PENDING_CREATED_AT,
        lifecycle: "failed",
        dispatchRequestId: "demo-request-failed",
        dispatchAttemptOrder: 1,
        failureCode: "send_input_failed",
        failureMessage: "Provider unavailable",
        createdAfterMaxSeq: 3,
        createdAfterEventIds: ["chat-assistant"],
      },
    ];
  }
  if (fixture === "long") {
    return [
      {
        id: "demo-pending-long",
        body: [
          "Sending a longer pending bubble so wrapping, grouped spacing,",
          "and the external clock mark stay stable across light and dark.",
          "The bubble geometry must not change when acknowledgement arrives.",
        ].join(" "),
        sentText: [
          "Sending a longer pending bubble so wrapping, grouped spacing,",
          "and the external clock mark stay stable across light and dark.",
          "The bubble geometry must not change when acknowledgement arrives.",
        ].join(" "),
        attachments: [],
        createdAt: DEMO_PENDING_CREATED_AT,
        lifecycle: "pending",
        dispatchRequestId: "demo-request-long",
        dispatchAttemptOrder: 1,
        createdAfterMaxSeq: 3,
        createdAfterEventIds: ["chat-assistant"],
      },
    ];
  }
  return [
    {
      id: "demo-pending-short",
      body: "On my way.",
      sentText: "On my way.",
      attachments: [],
      createdAt: DEMO_PENDING_CREATED_AT,
      lifecycle: "pending",
      dispatchRequestId: "demo-request-short",
      dispatchAttemptOrder: 1,
      createdAfterMaxSeq: 3,
      createdAfterEventIds: ["chat-assistant"],
    },
  ];
}

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
    id: "chat-assistant",
    seq: 3,
    kind: "assistant_message",
    timestamp: DEMO_TIMESTAMP,
    body: "The handoff is ready. The agent is still running on your computer, and the focused checks pass. Open **Terminal** anytime for the live process.",
  },
];

/**
 * Collapsed Tool activity headers for screenshot/demo and geometry coverage:
 * Run + short command, Search with no detail, Run + long ellipsized path.
 */
export const SCREENSHOT_ACTIVITY_HEADER_EVENTS: CodexConversationEvent[] = [
  {
    id: "activity-run-short",
    seq: 10,
    kind: "command",
    timestamp: DEMO_TIMESTAMP,
    command: "sleep 45",
    output: "",
    status: "completed",
    exit_code: 0,
  },
  {
    id: "activity-search",
    seq: 11,
    kind: "tool",
    timestamp: DEMO_TIMESTAMP,
    tool_name: "Grep",
    input: "{}",
    status: "completed",
  },
  {
    id: "activity-run-long",
    seq: 12,
    kind: "command",
    timestamp: DEMO_TIMESTAMP,
    command: "/home/daoleno/workspace/zen/daemon/brain/timeline_test.go",
    output: "",
    status: "completed",
    exit_code: 0,
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
  ...SCREENSHOT_ACTIVITY_HEADER_EVENTS.map((event, index) => ({
    ...event,
    id: `brain-${event.id}`,
    seq: 4 + index,
  })),
  {
    id: "brain-calendar-result",
    seq: 7,
    kind: "status",
    timestamp: "2026-06-18T09:31:00.000Z",
    title: "Daily Hacker News failed",
    body: "Linked Work is no longer observable after restart.",
    status: "failed",
    source: "calendar_result",
  },
  {
    id: "brain-assistant",
    seq: 8,
    kind: "assistant_message",
    timestamp: "2026-06-18T09:32:00.000Z",
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
      cost: 19.39,
      costKnown: true,
      totalTokens: 346_660_000,
      totalTokensKnown: true,
      inputTokens: 4_853_000,
      outputTokens: 1_168_000,
      reasoningTokens: 827_000,
      cacheRead: 339_782_000,
      cacheCreate: 30_000,
      tokenBreakdownKnown: true,
      sessions: 2559,
      models: [
        {
          // Synthetic layout-coverage rows; every value is fictional.
          name: "codex-large",
          totalTokens: 1_640_000,
          totalTokensKnown: true,
          inputTokens: 1_100_000,
          outputTokens: 320_000,
          reasoningTokens: 90_000,
          cacheRead: 120_000,
          cacheCreate: 10_000,
          tokenBreakdownKnown: true,
          cost: 11.62,
          costKnown: true,
          sessions: 26,
        },
        {
          name: "claude-sonnet",
          totalTokens: 840_000,
          totalTokensKnown: true,
          inputTokens: 520_000,
          outputTokens: 190_000,
          reasoningTokens: 60_000,
          cacheRead: 60_000,
          cacheCreate: 10_000,
          tokenBreakdownKnown: true,
          cost: 6.8,
          costKnown: true,
          sessions: 16,
        },
        {
          // Synthetic OpenCode model row: model names kept for layout
          // coverage only; the numbers are representative, not account data.
          name: "deepseek-v4-flash",
          totalTokens: 340_000_000,
          totalTokensKnown: true,
          inputTokens: 3_000_000,
          outputTokens: 640_000,
          reasoningTokens: 670_000,
          cacheRead: 335_680_000,
          cacheCreate: 10_000,
          tokenBreakdownKnown: true,
          cost: 0.97,
          costKnown: true,
          sessions: 2400,
        },
        {
          name: "kimi-k2.5-free",
          totalTokens: 4_180_000,
          totalTokensKnown: true,
          inputTokens: 233_000,
          outputTokens: 18_000,
          reasoningTokens: 7_000,
          cacheRead: 3_922_000,
          cacheCreate: 0,
          tokenBreakdownKnown: true,
          cost: 0,
          costKnown: true,
          sessions: 117,
        },
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

export const SCREENSHOT_PROVIDERS_FIXTURE: ProvidersSnapshot = {
  revision: 41,
  connections: [
    {
      id: "conn-openai",
      name: "OpenAI",
      preset_id: "openai",
      clients: ["codex"],
      credential_ready: true,
      advanced: false,
    },
    {
      id: "conn-deepseek",
      name: "DeepSeek",
      preset_id: "deepseek",
      clients: ["codex", "claude"],
      credential_ready: true,
      advanced: false,
    },
    {
      id: "conn-anthropic",
      name: "Anthropic",
      preset_id: "anthropic",
      clients: ["claude"],
      credential_ready: false,
      advanced: false,
    },
    {
      id: "conn-gateway",
      name: "Studio Gateway",
      preset_id: "custom",
      clients: ["codex", "claude"],
      credential_ready: true,
      advanced: true,
      base_url: "https://gateway.studio.example/v1",
    },
  ],
  defaults: {
    codex: { connection_id: "conn-deepseek", model_id: "deepseek-v4-flash" },
    claude: { connection_id: "conn-gateway", model_id: "claude-sonnet-4-6" },
  },
  presets: [
    {
      id: "openai",
      label: "OpenAI",
      clients: ["codex"],
      advanced: false,
    },
    {
      id: "openrouter",
      label: "OpenRouter",
      clients: ["codex"],
      advanced: false,
    },
    {
      id: "anthropic",
      label: "Anthropic",
      clients: ["claude"],
      advanced: false,
    },
    {
      id: "deepseek",
      label: "DeepSeek",
      clients: ["codex", "claude"],
      advanced: false,
    },
    {
      id: "custom",
      label: "Custom Gateway",
      clients: ["codex", "claude"],
      advanced: true,
    },
  ],
  models: {
    "conn-openai": [{ id: "gpt-5", available: true, source: "discovered" }],
    "conn-deepseek": [
      { id: "deepseek-v4-flash", available: true, source: "discovered" },
      { id: "deepseek-v4-pro", available: true, source: "discovered" },
    ],
    "conn-anthropic": [],
    "conn-gateway": [
      { id: "claude-sonnet-4-6", available: true, source: "discovered" },
      { id: "claude-opus-4-1", available: true, source: "discovered" },
    ],
  },
};
