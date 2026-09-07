import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = (path: string) => readFileSync(join(import.meta.dir, "..", path), "utf8");

describe("mobile chrome copy", () => {
  test("setup and inventory surfaces omit introductions and interaction tutorials", () => {
    const checks = [
      ["components/onboarding/OnboardingPresentation.tsx", "Your workspace, on this phone."],
      ["components/providers/ProvidersPresentation.tsx", "Configure each client separately."],
      ["components/skills/SkillsPresentation.tsx", "Reading supported Agent locations"],
      ["components/plugins/PluginsPresentation.tsx", "Expand a Skill to read its"],
      ["app/stats.tsx", "history to start collecting data"],
      ["components/terminal/GitDiffSheet.tsx", "Zen is checking the current working tree."],
      ["components/terminal/SessionFilePreviewSheet.tsx", "send it through JSON"],
    ];
    for (const [path, text] of checks) expect(source(path)).not.toContain(text);
  });

  test("optional descriptions do not leave empty text rows, errors remain visible", () => {
    for (const path of ["components/skills/SkillsPresentation.tsx", "components/plugins/PluginsPresentation.tsx", "components/terminal/GitDiffStateCard.tsx", "components/terminal/SessionFilePreviewSheet.tsx"]) {
      expect(source(path)).toContain("{detail ? <Text");
    }
    expect(source("components/skills/SkillsPresentation.tsx")).toContain("detail={state.error}");
    expect(source("components/plugins/PluginsPresentation.tsx")).toContain("detail={props.state.error}");
    expect(source("components/terminal/GitDiffSheet.tsx")).toContain("detail={error}");
    expect(source("components/terminal/SessionFilePreviewSheet.tsx")).toContain('detail={state.error || "The file is unavailable."}');
  });

  test("content and destructive consequences remain independent of chrome", () => {
    expect(source("components/plugins/PluginsPresentation.tsx")).toContain("{copy.description}");
    expect(source("components/skills/SkillsPresentation.tsx")).toContain("{detail.description}");
    expect(source("components/plugins/PluginsPresentation.tsx")).toContain("Permanently removes only this exact copy");
    expect(source("components/skills/SkillsPresentation.tsx")).toContain("Permanently delete this copy");
    expect(source("app/stats.tsx")).toContain("fmtAvailableCost(data.cost, data.costKnown)");
    expect(source("app/stats.tsx")).toContain("unpricedReasonLabel(m.unpricedReason)");
  });
});
