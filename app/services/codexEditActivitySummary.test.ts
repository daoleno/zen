import { describe, expect, test } from "bun:test";
import { buildInterfaceTimelineActivityPresentation } from "../components/terminal/InterfaceTimelineActivityModel";
import type { ZenActivityTimelineItem } from "../components/terminal/InterfaceTimelineActivityTypes";
import { buildZenTimeline } from "../components/terminal/InterfaceTimelineModel";
import { normalizeCodexConversation } from "./codexConversation";

function normalizedPatchActivity(event: Record<string, unknown>) {
  const conversation = normalizeCodexConversation({
    available: true,
    events: [
      {
        id: "synthetic-patch-event",
        seq: 7,
        kind: "patch",
        title: "Patch 1 file",
        source: "codex_rollout",
        ...event,
      },
    ],
  });
  const activities = buildZenTimeline(conversation.events).filter(
    (item): item is ZenActivityTimelineItem => item.type === "activity",
  );
  expect(activities).toHaveLength(1);
  return { activity: activities[0]!, event: conversation.events[0]! };
}

describe("Codex edit activity summaries", () => {
  test("normalizes canonical Codex file facts into the final single-file row title", () => {
    const { activity, event } = normalizedPatchActivity({
      body: "*** Begin Patch\n*** Update File: src/ledger/quote.ts\n@@\n-old\n+new\n*** End Patch",
      files: ["src/ledger/quote.ts"],
      file_changes: [
        {
          path: "src/ledger/quote.ts",
          operation: "update",
          additions: 9,
          deletions: 5,
        },
      ],
    });

    expect(event.file_changes).toEqual([
      {
        path: "src/ledger/quote.ts",
        move_path: undefined,
        operation: "update",
        additions: 9,
        deletions: 5,
      },
    ]);
    expect(activity).toMatchObject({
      id: "synthetic-patch-event",
      title: "Edit src/ledger/quote.ts",
      detail: "+9 −5",
      files: ["src/ledger/quote.ts"],
      defaultExpanded: false,
    });
    expect(activity.fileSummaries).toEqual([
      {
        path: "src/ledger/quote.ts",
        movePath: undefined,
        operation: "update",
        added: 9,
        removed: 5,
      },
    ]);
    expect(
      buildInterfaceTimelineActivityPresentation(
        activity,
        {
          textMuted: "#888",
          textSubtle: "#666",
          accent: "#aaa",
          border: "#333",
          appBackground: "#111",
        } as any,
        { red: "#f00", yellow: "#ff0", green: "#0f0" } as any,
      ).canExpand,
    ).toBe(true);
  });

  test("aggregates multiple known changes with shared target context", () => {
    const { activity } = normalizedPatchActivity({
      title: "Patch 2 files",
      files: ["src/portfolio/chart.tsx", "src/portfolio/summary.ts"],
      file_changes: [
        {
          path: "src/portfolio/summary.ts",
          operation: "update",
          additions: 5,
          deletions: 1,
        },
        {
          path: "src/portfolio/chart.tsx",
          operation: "update",
          additions: 7,
          deletions: 3,
        },
      ],
    });

    expect(activity.title).toBe("Edit src/portfolio/chart.tsx + 1");
    expect(activity.detail).toBe("+12 −4");
    expect(activity.files).toEqual([
      "src/portfolio/chart.tsx",
      "src/portfolio/summary.ts",
    ]);
  });

  test("keeps paths but omits invented line stats when counts are missing", () => {
    const { activity } = normalizedPatchActivity({
      title: "Patch 2 files",
      files: ["src/cache.ts", "tests/cache.test.ts"],
      file_changes: [
        { path: "src/cache.ts", operation: "update" },
        { path: "tests/cache.test.ts", operation: "delete" },
      ],
    });

    expect(activity.title).toBe("Edit src/cache.ts + 1");
    expect(activity.detail).toBe("Done");
    expect(activity.title).not.toContain("+0");
    expect(activity.fileSummaries).toEqual([
      {
        path: "src/cache.ts",
        movePath: undefined,
        operation: "update",
        added: undefined,
        removed: undefined,
      },
      {
        path: "tests/cache.test.ts",
        movePath: undefined,
        operation: "delete",
        added: undefined,
        removed: undefined,
      },
    ]);
  });

  test("retains a truthful legacy fallback when only filenames survived", () => {
    const { activity } = normalizedPatchActivity({
      files: ["src/legacy/cache.ts"],
    });

    expect(activity.title).toBe("Edit src/legacy/cache.ts");
    expect(activity.detail).toBe("Done");
    expect(activity.title).not.toMatch(/\(\+\d+ -\d+\)/);
  });
});
