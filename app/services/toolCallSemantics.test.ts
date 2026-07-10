// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  buildSemanticActions,
  collapsedToolLabel,
  decodeJSStringLiteral,
  extractBalancedArgs,
  isExecWrapperToolName,
  isUnsafeCollapsedDetail,
  parseExecWrapperCalls,
  primarySemanticAction,
} from "./toolCallSemantics";
import { buildZenTimeline } from "../components/terminal/CodexTimelineModel";
import type { CodexConversationEvent } from "./codexConversation";
import {
  shouldAutoExpandActivity,
  buildCodexTimelineActivityPresentation,
} from "../components/terminal/CodexTimelineActivityModel";
import type { ZenActivityTimelineItem } from "../components/terminal/CodexTimelineActivityTypes";

const FIXTURES = {
  codexExecCommand: `const r = await tools.exec_command({"cmd":"rg -n SemanticAction app","workdir":"/repo","yield_time_ms":10000});
text(r.output);`,
  codexApplyPatch: `const patch = "*** Begin Patch\\n*** Update File: app/services/toolCallSemantics.ts\\n@@\\n-old\\n+new\\n*** End Patch";
text(await tools.apply_patch(patch));`,
  codexUpdatePlan: `const p = await tools.update_plan({plan:[
  {step:"Trace pipeline",status:"in_progress"},
  {step:"Add tests",status:"pending"}
]});
text(p);`,
  codexViewImage: `const r = await tools.view_image({path:"/tmp/fixture-preview.png", detail:"original"});
image(r.image_url);`,
  codexMulti: `const results = await Promise.all([
  tools.exec_command({"cmd":"go test ./work -count=1","workdir":"/repo"}),
  tools.exec_command({"cmd":"bunx tsc --noEmit","workdir":"/repo/app"})
]);`,
  codexEscaped: `const r = await tools.exec_command({"cmd":"echo \\"hello\\" && rg -n 'token'"});`,
  codexMalformed: `const r = await tools.exec_command({"cmd":"unterminated`,
  grokGrep: `{"pattern":"codex","path":"app"}`,
  cursorShell: `{"command":"go test ./work","description":"Run work tests"}`,
};

describe("parseExecWrapperCalls", () => {
  test("single nested exec_command", () => {
    const calls = parseExecWrapperCalls(FIXTURES.codexExecCommand);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.name).toBe("exec_command");
    expect(calls[0]?.object?.cmd).toBe("rg -n SemanticAction app");
  });

  test("apply_patch via const string", () => {
    const calls = parseExecWrapperCalls(FIXTURES.codexApplyPatch);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.name).toBe("apply_patch");
    expect(calls[0]?.text).toContain("*** Update File: app/services/toolCallSemantics.ts");
  });

  test("update_plan object literal", () => {
    const calls = parseExecWrapperCalls(FIXTURES.codexUpdatePlan);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.name).toBe("update_plan");
    expect(Array.isArray(calls[0]?.object?.plan)).toBe(true);
  });

  test("view_image and multiple calls", () => {
    expect(parseExecWrapperCalls(FIXTURES.codexViewImage)[0]?.name).toBe("view_image");
    expect(parseExecWrapperCalls(FIXTURES.codexMulti)).toHaveLength(2);
  });

  test("escaped strings and malformed input", () => {
    const escaped = parseExecWrapperCalls(FIXTURES.codexEscaped);
    expect(escaped[0]?.object?.cmd).toContain('echo "hello"');
    expect(parseExecWrapperCalls(FIXTURES.codexMalformed)).toHaveLength(0);
    expect(extractBalancedArgs("tools.x(", 7)).toBeNull();
    expect(decodeJSStringLiteral(`"a\\nb"`)).toBe("a\nb");
  });

  test("ignores tool-like text in strings, comments, and templates", () => {
    const input = [
      `const r = await tools.exec_command({"cmd":"rg -n tools.apply_patch( app"});`,
      `// tools.view_image({path:"/tmp/x.png"})`,
      `/* tools.update_plan({plan:[]}) */`,
      "const note = `look at tools.exec_command({\"cmd\":\"echo hi\"})`;",
      `text(r.output);`,
    ].join("\n");
    const calls = parseExecWrapperCalls(input);
    expect(calls).toHaveLength(1);
    expect(calls[0]?.name).toBe("exec_command");
    expect(String(calls[0]?.object?.cmd || "")).toContain("tools.apply_patch");
  });

  test("preserves real Promise.all nested calls", () => {
    const calls = parseExecWrapperCalls(`const results = await Promise.all([
  tools.exec_command({"cmd":"go test ./work -count=1"}),
  tools.view_image({path:"/tmp/fixture.png"})
]);`);
    expect(calls.map((call) => call.name)).toEqual(["exec_command", "view_image"]);
  });
});

describe("semantic action model", () => {
  test("maps nested Codex wrappers to friendly labels", () => {
    expect(collapsedToolLabel({
      toolName: "exec",
      input: FIXTURES.codexExecCommand,
      status: "done",
    }).title).toBe("Searched code");

    expect(collapsedToolLabel({
      toolName: "exec",
      input: FIXTURES.codexApplyPatch,
      status: "done",
    }).title).toBe("Updated files");

    expect(collapsedToolLabel({
      toolName: "exec",
      input: FIXTURES.codexUpdatePlan,
      status: "done",
    }).title).toBe("Updated the plan");

    expect(collapsedToolLabel({
      toolName: "exec",
      input: FIXTURES.codexViewImage,
      status: "done",
    }).title).toBe("Opened an image");

    const multi = collapsedToolLabel({
      toolName: "exec",
      input: FIXTURES.codexMulti,
      status: "done",
    });
    expect(multi.title).toBe("Ran 2 commands");
    expect(multi.detail).toBeUndefined();
    expect(multi.children?.length).toBe(2);
    expect(multi.providerToolId).toBe("exec_command");
    expect(multi.children?.map((child) => child.label)).toEqual([
      "Tested the app",
      "Ran a command",
    ]);
  });

  test("keeps direct legacy and provider shapes working", () => {
    expect(primarySemanticAction({
      toolName: "view_image",
      input: `{"path":"/tmp/legacy.png"}`,
      status: "done",
    }).label).toBe("Opened an image");

    expect(primarySemanticAction({
      toolName: "Grep",
      input: FIXTURES.grokGrep,
      status: "done",
    }).kind).toBe("search_code");

    expect(primarySemanticAction({
      toolName: "Shell",
      input: FIXTURES.cursorShell,
      status: "done",
    }).kind).toBe("test_app");

    expect(isExecWrapperToolName("functions.exec")).toBe(true);
    expect(isExecWrapperToolName("exec_command")).toBe(false);
  });

  test("collapsed labels never expose secrets or raw args", () => {
    const label = collapsedToolLabel({
      toolName: "exec",
      input: `const r = await tools.exec_command({"cmd":"cat /home/user/.env && curl https://example.com?token=sk-abc123456789","workdir":"/secret"});`,
      status: "done",
    });
    expect(label.title).toBe("Read files");
    expect(label.detail).toBeUndefined();
    expect(isUnsafeCollapsedDetail(label.title)).toBe(false);
    expect(isUnsafeCollapsedDetail(`{"cmd":"secret"}`)).toBe(true);
    expect(isUnsafeCollapsedDetail("https://example.com/token")).toBe(true);
  });

  test("unknown nested tools fall back to human labels", () => {
    const action = primarySemanticAction({
      toolName: "exec",
      input: `await tools.custom_browser_probe({"target":"local"});`,
      status: "done",
    });
    expect(action.label.toLowerCase()).not.toContain("exec");
    expect(action.label).toContain("Custom Browser Probe");
  });
});

describe("timeline presentation", () => {
  test("buildZenTimeline uses semantic titles for Codex/Grok/Cursor shapes", () => {
    const events: CodexConversationEvent[] = [
      {
        id: "1",
        seq: 1,
        kind: "tool",
        tool_name: "exec",
        input: FIXTURES.codexExecCommand,
        status: "done",
      },
      {
        id: "2",
        seq: 2,
        kind: "tool",
        tool_name: "multi:exec_command,view_image",
        input: FIXTURES.codexMulti,
        status: "done",
      },
      {
        id: "3",
        seq: 3,
        kind: "tool",
        tool_name: "Grep",
        input: FIXTURES.grokGrep,
        status: "done",
      },
      {
        id: "4",
        seq: 4,
        kind: "command",
        command: "go test ./work -count=1",
        status: "done",
        exit_code: 0,
      },
      {
        id: "5",
        seq: 5,
        kind: "tool",
        tool_name: "view_image",
        input: `{"path":"/tmp/legacy.png"}`,
        status: "failed",
      },
      {
        id: "6",
        seq: 6,
        kind: "tool",
        tool_name: "exec",
        input: FIXTURES.codexViewImage,
        status: "running",
      },
    ];

    const timeline = buildZenTimeline(events);
    const activities = timeline.filter((item): item is ZenActivityTimelineItem => item.type === "activity");
    expect(activities.map((item) => item.title)).toEqual([
      "Searched code",
      "Ran 2 commands",
      "Searched code",
      "Tested the app",
      "Opened an image",
      "Opening an image",
    ]);
    expect(activities[0]?.detail).toBeUndefined();
    expect(activities[1]?.detail).toBeUndefined();
    expect(activities[1]?.children?.length).toBe(2);
    expect(activities[1]?.providerToolId).toBe("exec_command");
    expect(activities[4]?.tone).toBe("failed");
    expect(activities[5]?.tone).toBe("running");
    expect(shouldAutoExpandActivity(activities[0]!)).toBe(false);
    expect(shouldAutoExpandActivity(activities[4]!)).toBe(true);
    expect(shouldAutoExpandActivity(activities[5]!)).toBe(false);
    expect(activities[5]?.defaultExpanded).toBe(false);
    expect(activities[5]?.icon).toBe("time-outline");

    const presentation = buildCodexTimelineActivityPresentation(
      activities[1]!,
      { textMuted: "#888", textSubtle: "#666", accent: "#aaa", border: "#333", appBackground: "#111" } as any,
      { red: "#f00", yellow: "#ff0", green: "#0f0" } as any,
    );
    expect(presentation.canExpand).toBe(true);
  });

  test("buildSemanticActions covers blocked and running states", () => {
    expect(buildSemanticActions({
      toolName: "exec",
      input: FIXTURES.codexViewImage,
      status: "running",
    })[0]?.label).toBe("Opening an image");
    expect(buildSemanticActions({
      toolName: "apply_patch",
      files: ["a.ts", "b.ts"],
      status: "blocked",
    })[0]?.status).toBe("blocked");
  });

  test("blocked/error statuses use failed tone and auto-expand; running stays collapsed", () => {
    const events: CodexConversationEvent[] = [
      {
        id: "blocked-cmd",
        seq: 1,
        kind: "command",
        command: "go test ./work -count=1",
        status: "blocked",
        exit_code: 0,
      },
      {
        id: "error-search",
        seq: 2,
        kind: "web_search",
        status: "error",
        input: `{"type":"search","query":"fixture"}`,
      },
      {
        id: "blocked-tool",
        seq: 3,
        kind: "tool",
        tool_name: "view_image",
        input: `{"path":"/tmp/fixture.png"}`,
        status: "blocked",
      },
      {
        id: "running-tool",
        seq: 4,
        kind: "tool",
        tool_name: "Grep",
        input: FIXTURES.grokGrep,
        status: "running",
      },
      {
        id: "search-miss",
        seq: 5,
        kind: "command",
        command: "rg -n missing-token app",
        status: "done",
        exit_code: 1,
        body: "",
      },
    ];

    const activities = buildZenTimeline(events).filter(
      (item): item is ZenActivityTimelineItem => item.type === "activity",
    );
    expect(activities).toHaveLength(5);

    expect(activities[0]?.tone).toBe("failed");
    expect(activities[0]?.defaultExpanded).toBe(true);
    expect(shouldAutoExpandActivity(activities[0]!)).toBe(true);

    expect(activities[1]?.tone).toBe("failed");
    expect(activities[1]?.defaultExpanded).toBe(true);
    expect(shouldAutoExpandActivity(activities[1]!)).toBe(true);

    expect(activities[2]?.tone).toBe("failed");
    expect(activities[2]?.defaultExpanded).toBe(true);
    expect(shouldAutoExpandActivity(activities[2]!)).toBe(true);

    expect(activities[3]?.tone).toBe("running");
    expect(activities[3]?.defaultExpanded).toBe(false);
    expect(shouldAutoExpandActivity(activities[3]!)).toBe(false);

    // Explicit exit-code 1 search miss without failed/blocked/error stays non-failed.
    expect(activities[4]?.tone).toBe("success");
    expect(activities[4]?.defaultExpanded).toBe(false);
  });
});
