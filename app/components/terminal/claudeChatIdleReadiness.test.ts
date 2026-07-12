// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  conversationUnavailableReason,
  isCodexRequestRunning,
  isConversationSyncingReason,
} from "./CodexChatControllerModel";
import { resolveComposerSendAction } from "./composerSendAction";
import type { CodexConversation } from "../../services/codexConversation";

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

describe("structured chat readiness contract", () => {
  test("fresh structured session with no transcript is empty-ready, not Working", () => {
    const missing = conversation({
      available: false,
      reason: "transcript_not_found",
      active: false,
      events: [],
    });
    expect(isConversationSyncingReason(missing.reason)).toBe(true);
    expect(
      isCodexRequestRunning({
        conversation: missing,
        events: missing.events,
        agentStatus: "running",
      }),
    ).toBe(false);

    const send = resolveComposerSendAction({
      canSend: false,
      connected: true,
      hasComposerContent: false,
      interrupting: false,
      requestRunning: false,
      sending: false,
      startingNewChat: false,
    });
    expect(send.showStopButton).toBe(false);
    expect(send.showStopIndicator).toBe(false);
  });

  test("arbitrary terminal pane text must not become Chat turn state", () => {
    // Even if a legacy payload still carried terminal_snapshot events, turn
    // detection ignores process status and done snapshot cards.
    const leaked = conversation({
      available: true,
      source: "terminal_snapshot",
      active: false,
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
    expect(
      isCodexRequestRunning({
        conversation: leaked,
        events: leaked.events,
        agentStatus: "running",
      }),
    ).toBe(false);
  });

  test("genuine in-flight turn is Working and stoppable", () => {
    const requestRunning = isCodexRequestRunning({
      conversation: conversation({
        available: true,
        source: "claude_code_transcript",
        active: true,
        events: [],
      }),
      events: [
        {
          id: "tool-1",
          seq: 2,
          kind: "command",
          status: "running",
          command: "rg login",
        },
      ],
      agentStatus: "running",
    });
    expect(requestRunning).toBe(true);
    expect(
      resolveComposerSendAction({
        canSend: false,
        connected: true,
        hasComposerContent: false,
        interrupting: false,
        requestRunning,
        sending: false,
        startingNewChat: false,
      }).showStopButton,
    ).toBe(true);
  });

  test("pending queued user send is Working and stoppable", () => {
    const requestRunning = isCodexRequestRunning({
      conversation: conversation({
        available: true,
        active: false,
        events: [],
      }),
      events: [],
      hasPendingUserTurn: true,
      agentStatus: "running",
    });
    expect(requestRunning).toBe(true);
  });

  test("completed or idle turn is not Working", () => {
    expect(
      isCodexRequestRunning({
        conversation: conversation({
          available: true,
          active: false,
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
        }),
        events: [
          {
            id: "assistant-1",
            seq: 2,
            kind: "assistant_message",
            role: "assistant",
            body: "done answering",
            status: "done",
          },
        ],
        agentStatus: "running",
      }),
    ).toBe(false);
  });

  test("malformed transcript is unavailable guidance, not pane dump", () => {
    expect(isConversationSyncingReason("transcript_malformed")).toBe(false);
    expect(conversationUnavailableReason("transcript_malformed")).toContain(
      "Open the terminal",
    );
  });
});
