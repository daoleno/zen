// @ts-nocheck
import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../../services/codexConversation";
import { buildZenTimeline } from "./InterfaceTimelineModel";

const SCREENSHOT_DIAGRAM = `用户 / 本地 Agent
        |
        v
CLI / Skill
        |
        v
Perpetuo API  ---------------------+
        |                          |
        v                          v
   PostgreSQL               Artifact Store
        |
        | queued Job
        v
   Trusted Worker
        |
        +------ Research Engine
        |          |
        |          ├── 数据切分
        |          ├── 因果回测
        |          ├── 参数选择
        |          ├── 审计
        |          └── Evidence
        |
        v
Strategy Runtime Container
        |
        ├── 读取一条 observation
        ├── 维护内存 state
        └── 返回 target position`;

function assistant(body: string): CodexConversationEvent {
  return {
    id: "assistant-display-text",
    seq: 1,
    kind: "assistant_message",
    role: "assistant",
    body,
  };
}

function renderedBody(body: string) {
  const [item] = buildZenTimeline([assistant(body)]);
  expect(item?.type).toBe("message");
  return item?.type === "message" ? item.body : undefined;
}

function renderedToolBody(output: string) {
  const [item] = buildZenTimeline([
    {
      id: "terminal-tool-output",
      seq: 1,
      kind: "tool",
      tool_name: "exec_command",
      command: "run-demo",
      output,
      status: "done",
      exit_code: 0,
    },
  ]);
  expect(item?.type).toBe("activity");
  return item?.type === "activity" ? item.body : undefined;
}

describe("timeline display-text preservation", () => {
  test("preserves the exact mixed CJK/English fenced diagram", () => {
    const expected = `整体关系是：

\`\`\`text
${SCREENSHOT_DIAGRAM}
\`\`\`

后续说明。`;
    const rolloutText = `\u001B[32m${expected.replaceAll(
      "\n",
      "\r\n",
    )}\u001B[0m`;

    expect(renderedBody(rolloutText)).toBe(expected);
  });

  test("keeps Unicode spinner glyphs literal in assistant content", () => {
    const body = `Plain content:
⠋ Loading stays literal

\`\`\`text
  ⠙ Reading stays literal and indented
◒ Finishing stays literal
\`\`\``;

    expect(renderedBody(body)).toBe(body);
  });

  test("cleans terminal spinners without touching indentation or diagrams", () => {
    const firstOutput = `⠋ Loading context
    indented prose
        |
        \\
        +------ branch
        ├── child`;
    const secondOutput = `  ⠙ Reading files
        └── leaf
| literal pipe
\\ literal backslash
+ literal plus
Done`;

    expect(renderedToolBody(firstOutput)).toBe(`Loading context
    indented prose
        |
        \\
        +------ branch
        ├── child`);
    expect(renderedToolBody(secondOutput)).toBe(`Reading files
        └── leaf
| literal pipe
\\ literal backslash
+ literal plus
Done`);
  });
});
