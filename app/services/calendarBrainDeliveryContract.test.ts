import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { resolveNotificationDestination } from "./notificationRouting";

describe("Calendar Brain delivery app contract", () => {
  test("scheduled Work captures its Brain thread and streams that scope", () => {
    const calendar = readFileSync(join(import.meta.dir, "../app/calendar.tsx"), "utf8");
    const websocket = readFileSync(join(import.meta.dir, "websocket.ts"), "utf8");
    const chat = readFileSync(join(import.meta.dir, "../components/terminal/CodexChatSession.ts"), "utf8");

    expect(calendar).toContain("source_thread_id:");
    expect(calendar).toContain("Scheduled Work requires an active Brain conversation");
    expect(websocket).toContain("conversation_scope_key: options.conversationScopeKey");
    expect(chat).toContain("conversationScopeKey,");
  });

  test("result notifications deep-link to the canonical Brain identity", () => {
    expect(
      resolveNotificationDestination({
        screen: "brain",
        server_id: "server-1",
        brain_thread_id: "thread-frozen",
        brain_message_id: "calendar_result:item:run",
      }),
    ).toEqual({
      kind: "brain",
      serverId: "server-1",
      brainThreadId: "thread-frozen",
      brainMessageId: "calendar_result:item:run",
    });
  });
});
