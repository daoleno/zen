import { describe, expect, test } from "bun:test";
import {
  conversationUnavailableReason,
  isConversationSyncingReason,
} from "./CodexChatControllerModel";
import { resolveComposerSendAction } from "./composerSendAction";
import {
  isProviderActivityRunning,
  type CodexConversation,
} from "../../services/codexConversation";

function conversation(
  partial: Partial<CodexConversation> & {
    available: boolean;
  },
): CodexConversation {
  return {
    events: [],
    ...partial,
  };
}

describe("provider chat readiness contract", () => {
  test("fresh provider session with no transcript is empty-ready, not Working", () => {
    const missing = conversation({
      available: false,
      reason: "transcript_not_found",
      events: [],
    });
    expect(isConversationSyncingReason(missing.reason)).toBe(true);
    expect(isProviderActivityRunning(missing.activity)).toBe(false);

    const send = resolveComposerSendAction({
      canSend: false,
      connected: true,
      hasComposerContent: false,
      interrupting: false,
      activityRunning: false,
    });
    expect(send.showStopButton).toBe(false);
  });

  test("arbitrary terminal pane text must not become Chat Activity", () => {
    // Transcript rendering and process status are not lifecycle evidence.
    const leaked = conversation({
      available: true,
      source: "terminal_snapshot",
      events: [
        {
          id: "leak",
          seq: 1,
          kind: "status",
          title: "Terminal snapshot",
          status: "done",
          body: "████ arbitrary Claude Code banner ████\nThinking\nDone\nrg login",
          source: "terminal_snapshot",
        },
      ],
    });
    expect(isProviderActivityRunning(leaked.activity)).toBe(false);
  });

  test("genuine in-flight provider Activity is Working and stoppable", () => {
    const active = conversation({
      available: true,
      source: "claude_code_transcript",
      activity: {
        id: "claude-activity-1",
        status: "running",
        started_at: "2026-07-15T01:00:00.000Z",
      },
      events: [],
    });
    const activityRunning = isProviderActivityRunning(active.activity);
    expect(activityRunning).toBe(true);
    expect(
      resolveComposerSendAction({
        canSend: false,
        connected: true,
        hasComposerContent: false,
        interrupting: false,
        activityRunning,
      }).showStopButton,
    ).toBe(true);
  });

  test("a local pending user row cannot claim Working lifecycle", () => {
    const idle = conversation({
      available: true,
      events: [],
    });
    const activityRunning = isProviderActivityRunning(idle.activity);
    expect(activityRunning).toBe(false);
  });

  test("completed or absent Activity is not Working", () => {
    const completed = conversation({
      available: true,
      activity: {
        id: "claude-activity-1",
        status: "completed",
        started_at: "2026-07-15T01:00:00.000Z",
        settled_at: "2026-07-15T01:00:02.000Z",
      },
      events: [
        {
          id: "user-1",
          seq: 1,
          kind: "user_message",
          role: "user",
          body: "done asking",
        },
        {
          id: "assistant-1",
          seq: 2,
          kind: "assistant_message",
          role: "assistant",
          body: "done answering",
          status: "done",
        },
      ],
    });
    expect(isProviderActivityRunning(completed.activity)).toBe(false);
  });

  test("malformed transcript is unavailable guidance, not pane dump", () => {
    expect(isConversationSyncingReason("transcript_malformed")).toBe(false);
    expect(conversationUnavailableReason("transcript_malformed")).toContain(
      "Open the terminal",
    );
  });
});
