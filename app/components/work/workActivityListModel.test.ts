import { describe, expect, test } from "bun:test";
import type { BrainCurrentWork } from "../../store/brain";
import {
  buildWorkActivityListModel,
  type WorkActivityOwner,
} from "./workActivityListModel";

const owner: WorkActivityOwner = {
  sessionId: "brain-agent-worker:@1",
  title: "Release worker",
  status: "running",
  delegated: true,
};

function currentWork(
  overrides: Partial<BrainCurrentWork> = {},
): BrainCurrentWork {
  return {
    work_id: "work-1",
    revision: 1,
    title: "Ship the release",
    status: "running",
    progress_mode: "owned",
    attempt_session_id: owner.sessionId,
    attempt_delegated: true,
    unread_result: false,
    ...overrides,
  };
}

describe("Work activity list model", () => {
  test("groups Needs you, active review, and terminal Work for scanning", () => {
    const model = buildWorkActivityListModel({
      work: [
        currentWork({ work_id: "needs", status: "needs_input" }),
        currentWork({
          work_id: "review",
          attention_state: "reviewing",
        }),
        currentWork({ work_id: "done", status: "done" }),
      ],
      owners: [owner],
      historicalResultCount: 4,
    });

    expect(model.attention.map((row) => row.statusLabel)).toEqual([
      "Needs you",
    ]);
    expect(model.active.map((row) => row.statusLabel)).toEqual([
      "Reviewing",
    ]);
    expect(model.recent.map((row) => row.statusLabel)).toEqual(["Done"]);
    expect(model.historicalResultCount).toBe(4);
  });

  test("uses current Work status as the only source of terminality", () => {
    const model = buildWorkActivityListModel({
      work: [
        currentWork({
          status: "waiting",
          progress_mode: "ready",
          unread_result: true,
        }),
      ],
      owners: [owner],
      historicalResultCount: 0,
    });

    expect(model.active[0]).toMatchObject({
      statusLabel: "Ready",
      terminal: false,
      unread: true,
    });
    expect(model.recent).toHaveLength(0);
  });

  test("links only canonical delegated Sessions and keeps ownerless Needs you actionable", () => {
    const model = buildWorkActivityListModel({
      work: [
        currentWork(),
        currentWork({
          work_id: "ownerless",
          status: "needs_input",
          attempt_session_id: undefined,
          attempt_delegated: false,
        }),
      ],
      owners: [owner, { ...owner, sessionId: "ordinary", delegated: false }],
      historicalResultCount: 0,
    });

    expect(model.active[0]?.owner?.title).toBe("Release worker");
    expect(model.active[0]?.action).toBe("open_session");
    expect(model.attention[0]?.owner).toBeUndefined();
    expect(model.attention[0]?.action).toBe("open_brain");
  });

  test("keeps ownerless Reviewing and Waiting Work on the active surface", () => {
    const model = buildWorkActivityListModel({
      work: [
        currentWork({
          work_id: "ownerless-review",
          attention_state: "reviewing",
          attempt_session_id: undefined,
          attempt_delegated: false,
        }),
        currentWork({
          work_id: "ownerless-wait",
          status: "waiting",
          progress_mode: "waiting",
          attempt_session_id: undefined,
          attempt_delegated: false,
        }),
      ],
      owners: [],
      historicalResultCount: 0,
    });

    expect(model.active.map((row) => row.statusLabel)).toEqual([
      "Reviewing",
      "Waiting",
    ]);
    expect(model.active.every((row) => row.owner === undefined)).toBe(true);
  });

  test("sorts Active Work by lifecycle before unread and title", () => {
    const model = buildWorkActivityListModel({
      work: [
        currentWork({
          work_id: "wait",
          title: "A wait",
          status: "waiting",
          progress_mode: "waiting",
          unread_result: true,
        }),
        currentWork({ work_id: "work", title: "A work" }),
        currentWork({
          work_id: "ready",
          title: "Z ready",
          progress_mode: "ready",
        }),
        currentWork({
          work_id: "review",
          title: "Z review",
          attention_state: "reviewing",
        }),
      ],
      owners: [owner],
      historicalResultCount: 0,
    });

    expect(model.active.map((row) => row.statusLabel)).toEqual([
      "Reviewing",
      "Ready",
      "Working",
      "Waiting",
    ]);
  });

  test("presents canonical cancellation as neutral terminal history", () => {
    const model = buildWorkActivityListModel({
      work: [currentWork({ status: "cancelled" })],
      owners: [owner],
      historicalResultCount: 0,
    });

    expect(model.active).toHaveLength(0);
    expect(model.recent[0]).toMatchObject({
      lifecycle: "cancelled",
      statusLabel: "Cancelled",
      tone: "neutral",
      terminal: true,
    });
  });

  test("redacts canonical identities from visible Work titles", () => {
    const model = buildWorkActivityListModel({
      work: [
        currentWork({
          title: `Release checks (${owner.sessionId})`,
        }),
      ],
      owners: [owner],
      historicalResultCount: 0,
    });

    expect(model.active[0]?.title).toBe("Release checks");
  });

  test("links typed Session waits without treating owner status as Work status", () => {
    const model = buildWorkActivityListModel({
      work: [
        currentWork({
          status: "waiting",
          progress_mode: "waiting",
          attempt_session_id: undefined,
          attempt_delegated: false,
          wake: {
            kind: "session_terminal",
            ref: `session:${owner.sessionId}:turn:provider-turn-1`,
          },
        }),
      ],
      owners: [{ ...owner, status: "failed" }],
      historicalResultCount: 0,
    });

    expect(model.active[0]).toMatchObject({
      statusLabel: "Waiting",
      terminal: false,
      action: "open_session",
    });
  });
});
