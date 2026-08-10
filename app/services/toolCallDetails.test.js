import { describe, expect, test } from "bun:test";
import {
  buildExpandedToolDetails,
  cleanUserFacingOutput,
  extractTransportMetadata,
  isTransportOnlyPayload,
  isWaitLikeToolName,
  isWaitSessionPoll,
} from "./toolCallDetails.ts";
import { buildZenTimeline } from "../components/terminal/InterfaceTimelineModel.ts";
import { buildInterfaceTimelineActivityPresentation } from "../components/terminal/InterfaceTimelineActivityModel.ts";

describe("toolCallDetails", () => {
  test("read details prefer file paths over exec_command", () => {
    const details = buildExpandedToolDetails({
      toolName: "exec",
      input: `const r = await tools.exec_command({"cmd":"sed -n \\"1,40p\\" app/services/toolCallSemantics.ts","workdir":"/repo"});`,
      output: "export type SemanticActionKind =",
      status: "done",
      semanticKind: "read_files",
    });
    expect(details.files).toEqual(["app/services/toolCallSemantics.ts"]);
    expect(details.command).toBe(
      'sed -n "1,40p" app/services/toolCallSemantics.ts',
    );
    expect(details.result).toContain("SemanticActionKind");
    expect(details.quietDetail).toBe("app/services/toolCallSemantics.ts");
    expect(details.statusLine).toBe("Done");
    expect(details.developer?.providerToolId).toBeUndefined();
  });

  test("search details surface query", () => {
    const details = buildExpandedToolDetails({
      toolName: "Grep",
      input: JSON.stringify({ pattern: "SemanticAction", path: "app" }),
      status: "done",
      semanticKind: "search_code",
    });
    expect(details.query).toBe("SemanticAction");
    expect(details.quietDetail).toBe("SemanticAction");
  });

  test("wait session poll hides card", () => {
    expect(
      isWaitSessionPoll(
        "wait",
        JSON.stringify({ session_id: 98430, chars: "", yield_time_ms: 30000 }),
      ),
    ).toBe(true);
    const details = buildExpandedToolDetails({
      toolName: "wait",
      input: JSON.stringify({
        session_id: 98430,
        chars: "",
        yield_time_ms: 30000,
      }),
      output: JSON.stringify({
        chunk_id: "build-2",
        wall_time: 7.8274,
        session_id: 98430,
        original_token_count: 4,
      }),
      status: "done",
    });
    expect(details.hideCard).toBe(true);
  });

  test("unlinked wait shows Finished line without raw JSON", () => {
    const details = buildExpandedToolDetails({
      toolName: "wait",
      output: JSON.stringify({
        chunk_id: "build-2",
        wall_time: 7.8274,
        session_id: 98430,
        original_token_count: 4,
      }),
      status: "done",
    });
    expect(details.hideCard).toBeFalsy();
    expect(details.statusLine).toContain("Finished");
    expect(details.statusLine).toContain("7.8s");
    expect(details.result).toBeUndefined();
  });

  test("strips Codex metadata", () => {
    expect(
      cleanUserFacingOutput(
        "Chunk ID: abc\nWall time: 1.0 seconds\nProcess exited with code 0\nOriginal token count: 4\nOutput:\nBUILD SUCCESSFUL",
      ),
    ).toBe("BUILD SUCCESSFUL");
    expect(
      isTransportOnlyPayload(
        JSON.stringify({
          chunk_id: "x",
          wall_time: 1,
          session_id: 1,
          original_token_count: 0,
        }),
      ),
    ).toBe(true);
    expect(
      extractTransportMetadata(undefined, "Wall time: 2.5 seconds\n"),
    ).toEqual({
      wall_time: "2.5",
    });
    expect(isWaitLikeToolName("write_stdin")).toBe(true);
  });
});

describe("timeline wait/read presentation", () => {
  test("read lists paths; wait polls do not become raw cards", () => {
    const events = [
      {
        id: "read-nested",
        seq: 1,
        kind: "tool",
        tool_name: "exec",
        input: `const r = await tools.exec_command({"cmd":"sed -n \\"1,20p\\" app/foo.ts"});`,
        output: "export const foo = 1",
        status: "done",
      },
      {
        id: "cmd",
        seq: 2,
        kind: "command",
        command: "./gradlew assembleDebug",
        status: "running",
        body: "starting",
      },
      {
        id: "wait-poll",
        seq: 3,
        kind: "tool",
        tool_name: "wait",
        input: JSON.stringify({
          session_id: 98430,
          chars: "",
          yield_time_ms: 30000,
        }),
        output: JSON.stringify({
          chunk_id: "build-2",
          wall_time: 7.8274,
          session_id: 98430,
          original_token_count: 4,
        }),
        status: "done",
      },
      {
        id: "wait-alone",
        seq: 4,
        kind: "tool",
        tool_name: "wait",
        output: JSON.stringify({
          chunk_id: "x",
          wall_time: 1.5,
          session_id: 1,
          original_token_count: 1,
        }),
        status: "done",
      },
      {
        id: "claude-read",
        seq: 5,
        kind: "tool",
        tool_name: "Read",
        input: JSON.stringify({ file_path: "/repo/app/bar.ts" }),
        files: ["/repo/app/bar.ts"],
        output: "package main",
        status: "done",
      },
    ];

    const activities = buildZenTimeline(events).filter(
      (item) => item.type === "activity",
    );
    expect(activities.map((item) => item.title)).toEqual([
      "Read app/foo.ts",
      "Build assembleDebug",
      "Finished",
      "Read app/bar.ts",
    ]);

    expect(activities[0].files).toEqual(["app/foo.ts"]);
    expect(activities[0].body).toContain("foo");
    expect(activities[0].commandText).toBe('sed -n "1,20p" app/foo.ts');
    expect(activities[0].detail).toBe("Done");
    expect(activities[0].providerToolId).toBeUndefined();
    expect(activities[0].developerDetails?.providerToolId).toBeUndefined();

    // Poll merged into previous command — no raw wait card with chunk_id body
    expect(activities[1].statusLine || activities[1].detail).toBeTruthy();
    expect(activities[1].detail).toBe("Running");
    expect(JSON.stringify(activities[1])).not.toContain("chunk_id");

    expect(activities[2].title).toBe("Finished");
    expect(activities[2].detail || activities[2].statusLine).toContain(
      "Finished",
    );
    expect(activities[2].body).toBeUndefined();

    expect(activities[3].files[0]).toContain("bar.ts");

    const chrome = {
      textMuted: "#888",
      textSubtle: "#666",
      accent: "#aaa",
      border: "#333",
      appBackground: "#111",
    };
    const theme = { red: "#f00", yellow: "#ff0", green: "#0f0" };
    expect(
      buildInterfaceTimelineActivityPresentation(activities[0], chrome, theme)
        .canExpand,
    ).toBe(true);
  });
});
