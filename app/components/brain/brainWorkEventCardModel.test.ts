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

  test("keeps exact completion lifecycle metadata compact without execution facts", () => {
    expect(
      brainWorkEventCardModel(
        event({ phase: "reporting", attention: "done", event_kind: "done" }),
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
            digest: "sha256:91aa",
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
        "Digest: sha256:91aa",
      ],
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
