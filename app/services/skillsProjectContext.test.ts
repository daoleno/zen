import { describe, expect, test } from "bun:test";
import {
  SkillsAutomaticInventoryOwner,
  selectStableSkillsProjectCwd,
} from "./skillsScreenModel";

describe("stable Skills project context", () => {
  test("does not follow alternating Agent activity between cwd values", () => {
    const first = selectStableSkillsProjectCwd(
      [
        { serverId: "local", cwd: "/repo/alpha", updated_at: 20 },
        { serverId: "local", cwd: "/repo/beta", updated_at: 10 },
      ],
      "local",
      "",
    );
    expect(first).toBe("/repo/alpha");

    const afterBetaActivity = selectStableSkillsProjectCwd(
      [
        { serverId: "local", cwd: "/repo/alpha", updated_at: 20 },
        { serverId: "local", cwd: "/repo/beta", updated_at: 30 },
      ],
      "local",
      first,
    );
    expect(afterBetaActivity).toBe("/repo/alpha");
  });

  test("alternating Agent activity produces one automatic inventory refresh", () => {
    const owner = new SkillsAutomaticInventoryOwner();
    let selected = "";
    let requests = 0;
    for (const [alphaUpdatedAt, betaUpdatedAt] of [
      [20, 10],
      [20, 30],
      [40, 30],
      [40, 50],
    ]) {
      selected = selectStableSkillsProjectCwd(
        [
          {
            serverId: "local",
            cwd: "/repo/alpha",
            updated_at: alphaUpdatedAt,
          },
          {
            serverId: "local",
            cwd: "/repo/beta",
            updated_at: betaUpdatedAt,
          },
        ],
        "local",
        selected,
      );
      if (owner.shouldRefresh(1, `local\u0000${selected}`)) requests += 1;
    }

    expect(selected).toBe("/repo/alpha");
    expect(requests).toBe(1);
  });

  test("chooses a fallback only after the selected cwd disappears", () => {
    expect(
      selectStableSkillsProjectCwd(
        [
          { serverId: "local", cwd: "/repo/beta", updated_at: 30 },
          { serverId: "remote", cwd: "/repo/alpha", updated_at: 40 },
        ],
        "local",
        "/repo/alpha",
      ),
    ).toBe("/repo/beta");
  });

  test("clears a previous cwd when the current server is unavailable", () => {
    expect(selectStableSkillsProjectCwd([], null, "/repo/alpha")).toBe("");
  });
});
