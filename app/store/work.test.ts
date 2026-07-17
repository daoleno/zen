import { describe, expect, test } from "bun:test";
import { findLinkedWork } from "../app/terminal/TerminalScreenModel";
import { initialWorkState, workReducer } from "./work";

describe("current Work projection", () => {
  test("keeps Calendar route identity, agent linkage, and generic metadata", () => {
    const state = workReducer(initialWorkState, {
      type: "WORK_ITEMS_SNAPSHOT",
      serverId: "server-1",
      serverName: "Local",
      serverUrl: "ws://local",
      workItems: [
        {
          id: "run-1",
          project: "calendar",
          title: "Current scheduled Work",
          body: "# Current scheduled Work\n",
          frontmatter: {
            id: "run-1",
            kind: "calendar_action",
            created: "2026-07-17T03:00:00Z",
            status: "running",
            title: "Current scheduled Work",
            agent_session: "agent-1",
            extra: { legacy_note: "inert" },
          },
          mtime: "2026-07-17T03:01:00Z",
        },
      ],
    });

    const item = state.byKey["server-1:run-1"];
    expect(item).toBeDefined();
    expect(item.frontmatter).toMatchObject({
      kind: "calendar_action",
      status: "running",
      title: "Current scheduled Work",
      agent_session: "agent-1",
      extra: { legacy_note: "inert" },
    });
    expect(findLinkedWork(state.byKey, "server-1", "agent-1")).toBe(item);
  });
});
