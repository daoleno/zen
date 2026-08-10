import { describe, expect, test } from "bun:test";
import {
  collapsedToolLabel,
  distinctivePath,
  type ToolCallPresentationInput,
} from "./toolCallSemantics";
import { buildZenTimeline } from "../components/terminal/InterfaceTimelineModel";
import type { CodexConversationEvent } from "./codexConversation";
import type { ZenActivityTimelineItem } from "../components/terminal/InterfaceTimelineActivityTypes";
import {
  buildInterfaceTimelineActivityPresentation,
  expandedActivityStatusLine,
} from "../components/terminal/InterfaceTimelineActivityModel";

const absoluteWatcher = "/home/daoleno/workspace/zen/daemon/watcher/watcher.go";
const absoluteTmux = "/home/daoleno/workspace/zen/daemon/terminal/tmux.go";

function title(input: ToolCallPresentationInput) {
  return collapsedToolLabel(input).title;
}

function activities(events: CodexConversationEvent[]) {
  return buildZenTimeline(events).filter(
    (item): item is ZenActivityTimelineItem => item.type === "activity",
  );
}

describe("actionable collapsed tool-call projection", () => {
  test("equivalent provider command shapes converge on one test presentation", () => {
    const shapes: ToolCallPresentationInput[] = [
      {
        toolName: "exec",
        input:
          'const r = await tools.exec_command({"cmd":"go test ./... -count=1","workdir":"/repo"}); text(r.output);',
        status: "done",
      },
      {
        toolName: "Bash",
        input: JSON.stringify({ command: "go test ./... -count=1" }),
        status: "done",
      },
      {
        toolName: "Shell",
        input: JSON.stringify({ command: "go test ./... -count=1" }),
        status: "done",
      },
      {
        toolName: "exec_command",
        input: JSON.stringify({ cmd: "go test ./... -count=1" }),
        status: "done",
      },
      {
        kind: "command",
        command: "go test ./... -count=1",
        status: "done",
      },
    ];

    expect(shapes.map(title)).toEqual(
      Array.from({ length: shapes.length }, () => "Run go test ./..."),
    );
  });

  test("absolute and relative provider paths collapse to the same distinctive target", () => {
    expect(distinctivePath(absoluteWatcher)).toBe("daemon/watcher/watcher.go");
    expect(distinctivePath("daemon/watcher/watcher.go")).toBe(
      "daemon/watcher/watcher.go",
    );
    expect(
      title({
        toolName: "Read",
        input: JSON.stringify({ file_path: absoluteWatcher }),
        status: "done",
      }),
    ).toBe("Read daemon/watcher/watcher.go");
    expect(
      title({
        toolName: "Read",
        files: ["daemon/watcher/watcher.go"],
        status: "done",
      }),
    ).toBe("Read daemon/watcher/watcher.go");
    expect(
      title({
        toolName: "Edit",
        files: [
          absoluteTmux,
          "/home/daoleno/workspace/zen/daemon/terminal/io.go",
        ],
        status: "done",
      }),
    ).toBe("Edit daemon/terminal/tmux.go + 1");
  });

  test("tests, builds, searches, and generic commands expose bounded honest intent", () => {
    expect(
      title({
        kind: "command",
        command: "bun run daemon:build",
        status: "done",
      }),
    ).toBe("Build daemon");
    expect(
      title({
        toolName: "Grep",
        input: JSON.stringify({ pattern: "daemonSocketPath", path: "daemon" }),
        status: "done",
      }),
    ).toBe("Search daemonSocketPath");
    expect(
      title({ kind: "command", command: "git status --short", status: "done" }),
    ).toBe("Run git status --short");

    const longCommand = `printf ${"safe-argument-".repeat(12)} fixtures`;
    const longShapes: ToolCallPresentationInput[] = [
      { kind: "command", command: longCommand, status: "done" },
      {
        toolName: "Shell",
        input: JSON.stringify({ command: longCommand }),
        status: "done",
      },
      {
        toolName: "exec",
        input: `const r = await tools.exec_command(${JSON.stringify({ cmd: longCommand })}); text(r.output);`,
        status: "done",
      },
    ];
    const longTitles = longShapes.map(title);
    const longTitle = longTitles[0]!;
    expect(new Set(longTitles).size).toBe(1);
    expect(longTitle.startsWith("Run printf safe-argument-")).toBe(true);
    expect(Array.from(longTitle).length).toBeLessThanOrEqual(68);
    expect(longTitle.endsWith("…")).toBe(true);
  });

  test("missing and secret-bearing inputs fail closed while expansion remains exact", () => {
    const secretCommand =
      "curl -H 'Authorization: Bearer sk-secret12345678' https://example.test/private";
    expect(title({ kind: "command", command: "", status: "done" })).toBe("Run");
    const secretShapes: ToolCallPresentationInput[] = [
      { kind: "command", command: secretCommand, status: "done" },
      {
        toolName: "Bash",
        input: JSON.stringify({ command: secretCommand }),
        status: "done",
      },
      {
        toolName: "exec",
        input: `const r = await tools.exec_command(${JSON.stringify({ cmd: secretCommand })}); text(r.output);`,
        status: "done",
      },
    ];
    expect(secretShapes.map(title)).toEqual(["Run", "Run", "Run"]);
    expect(
      title({
        toolName: "Read",
        files: ["/home/user/.env.local"],
        status: "done",
      }),
    ).toBe("Read");

    const [activity] = activities([
      {
        id: "secret-command",
        seq: 1,
        kind: "command",
        command: secretCommand,
        status: "done",
        exit_code: 0,
      },
    ]);
    expect(activity?.title).toBe("Run");
    expect(activity?.commandText).toBe(secretCommand);
    expect(activity?.detail).toBe("Succeeded");
  });

  test("settled, failed, running, and search-miss results remain visible", () => {
    const rows = activities([
      {
        id: "passed",
        seq: 1,
        kind: "command",
        command: "go test ./... -count=1",
        status: "done",
        exit_code: 0,
        body: "Wall time: 2.5 seconds\nProcess exited with code 0\nOutput:\nok",
      },
      {
        id: "failed",
        seq: 2,
        kind: "command",
        command: "go test ./daemon/work",
        status: "failed",
        exit_code: 1,
        body: "FAIL daemon/work",
      },
      {
        id: "running",
        seq: 3,
        kind: "command",
        command: "git status --short",
        status: "running",
      },
      {
        id: "search-miss",
        seq: 4,
        kind: "command",
        command: "rg -n absentNeedle daemon",
        status: "done",
        exit_code: 1,
        body: "",
      },
      {
        id: "provider-search-miss",
        seq: 5,
        kind: "tool",
        tool_name: "exec",
        input:
          'const r = await tools.exec_command({"cmd":"rg -n absentNeedle daemon"}); text(r.output);',
        status: "done",
        exit_code: 1,
        output: "",
      },
    ]);

    expect(rows.map((row) => [row.title, row.detail, row.tone])).toEqual([
      ["Run go test ./...", "Passed · 2.5s", "success"],
      ["Run go test ./daemon/work", "Failed · exit 1", "failed"],
      ["Run git status --short", "Running", "running"],
      ["Search absentNeedle", "Done", "success"],
      ["Search absentNeedle", "Done", "success"],
    ]);
    expect(rows[0]?.commandText).toBe("go test ./... -count=1");
    expect(rows[0]?.body).toBe("ok");
  });

  test("expansion does not repeat the collapsed result but keeps richer facts", () => {
    const base = {
      type: "activity" as const,
      id: "status-detail",
      title: "Run go test ./...",
      tone: "success" as const,
      icon: "terminal-outline" as const,
    };
    const duplicate = {
      ...base,
      detail: "Succeeded",
      statusLine: "Succeeded",
    };
    const richer = {
      ...base,
      detail: "Succeeded",
      statusLine: "Succeeded · 2.5s",
    };

    expect(expandedActivityStatusLine(duplicate)).toBeUndefined();
    expect(expandedActivityStatusLine(richer)).toBe("Succeeded · 2.5s");
    expect(
      buildInterfaceTimelineActivityPresentation(
        duplicate,
        {} as Parameters<typeof buildInterfaceTimelineActivityPresentation>[1],
        {} as Parameters<typeof buildInterfaceTimelineActivityPresentation>[2],
      ).canExpand,
    ).toBe(false);
  });

  test("patch providers keep exact files expanded and compact line totals collapsed", () => {
    const [canonical, providerTool] = activities([
      {
        id: "canonical-patch",
        seq: 1,
        kind: "patch",
        status: "done",
        files: [absoluteTmux],
        file_changes: [
          {
            path: absoluteTmux,
            operation: "update",
            additions: 42,
            deletions: 16,
          },
        ],
      },
      {
        id: "provider-edit",
        seq: 2,
        kind: "tool",
        tool_name: "Edit",
        input: JSON.stringify({ file_path: absoluteTmux }),
        files: [absoluteTmux],
        status: "done",
      },
    ]);

    expect(canonical?.title).toBe("Edit daemon/terminal/tmux.go");
    expect(canonical?.detail).toBe("+42 −16");
    expect(canonical?.files).toEqual([absoluteTmux]);
    expect(providerTool?.title).toBe("Edit daemon/terminal/tmux.go");
    expect(providerTool?.detail).toBe("Done");
    expect(providerTool?.files).toEqual([absoluteTmux]);
  });
});
