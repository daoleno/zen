import { describe, expect, test } from "bun:test";
import type { BrainWorkResultEvent } from "./brainWorkEvent";
import { brainWorkEventCardModel } from "./brainWorkEventCardModel";

function event(
  overrides: Partial<BrainWorkResultEvent> = {},
): BrainWorkResultEvent {
  return {
    event_id: "event-1",
    kind: "session.done",
    work_id: "work-1",
    work_title: "publish-release",
    summary:
      "Delegated provider reported done; awaiting exact control completion",
    occurred_at: "2026-08-23T12:00:00Z",
    unread: true,
    review_state: "queued",
    session_state: "open",
    current_result: true,
    ...overrides,
  };
}

describe("Brain Work card density", () => {
  test("collapses lifecycle-only provider prose to a minimal row", () => {
    expect(brainWorkEventCardModel(event())).toEqual({
      density: "minimal",
      facts: [],
    });
  });

  test("keeps real completion lifecycle and Work control fields compact", () => {
    expect(
      brainWorkEventCardModel(
        event({
          phase: "reporting",
          attention: "done",
          event_kind: "done",
          next_action: "Review the delegated Session result.",
          wait_for: "Session control completion",
        }),
      ),
    ).toEqual({ density: "minimal", facts: [] });
  });

  test("projects concise structured execution context without duplicating summary", () => {
    expect(
      brainWorkEventCardModel(
        event({
          summary: "Publishing image",
          phase: "working",
          event_kind: "artifact",
          details_json: JSON.stringify({
            ci_run: 32645890201,
          }),
          wait_for: "Preview iOS archive",
          next_action: "Promote the verified build",
        }),
      ),
    ).toEqual({
      density: "rich",
      summary: "Publishing image",
      facts: [
        "Waiting for Preview iOS archive",
        "CI run: 32645890201",
        "Next: Promote the verified build",
      ],
    });
  });

  test("uses Work next and wait facts only to enrich meaningful context", () => {
    const contexts: Partial<BrainWorkResultEvent>[] = [
      { event_kind: "artifact", summary: "Produced an execution artifact" },
      { event_kind: "risk", summary: "External capacity is constrained" },
      { attention: "user_input", summary: "Choose the execution target" },
    ];

    contexts.forEach((context) => {
      expect(
        brainWorkEventCardModel(
          event({
            ...context,
            wait_for: "External verification",
            next_action: "Continue after verification",
          }),
        ),
      ).toMatchObject({
        density: "rich",
        facts: [
          "Waiting for External verification",
          "Next: Continue after verification",
        ],
      });
    });
  });

  test("degrades unknown details safely to bounded scalar facts", () => {
    const model = brainWorkEventCardModel(
      event({
        summary: "Checking external state",
        details_json: JSON.stringify({
          custom_stage: "mirror",
          nested: { secret: "not rendered" },
          candidates: ["android", "ios"],
          criteria_met: true,
          empty: "   ",
        }),
      }),
    );

    expect(model).toEqual({
      density: "rich",
      summary: "Checking external state",
      facts: [
        "Custom stage: mirror",
        "Candidates: android, ios",
      ],
    });
    expect(JSON.stringify(model)).not.toContain("secret");
    expect(JSON.stringify(model)).not.toContain("Criteria met");
  });

  test("ignores malformed or non-object details instead of inventing context", () => {
    expect(
      brainWorkEventCardModel(event({ details_json: "not-json" })).density,
    ).toBe("minimal");
    expect(
      brainWorkEventCardModel(event({ details_json: '["release"]' })).density,
    ).toBe("minimal");
  });

  test("shows real user input and external blockers as semantic context", () => {
    expect(
      brainWorkEventCardModel(
        event({ attention: "user_input", summary: "Choose a release channel" }),
      ),
    ).toMatchObject({ density: "rich", summary: "Choose a release channel" });
    expect(
      brainWorkEventCardModel(
        event({ attention: "blocked", summary: "Registry is unavailable" }),
      ),
    ).toMatchObject({ density: "rich", summary: "Registry is unavailable" });
  });
});
