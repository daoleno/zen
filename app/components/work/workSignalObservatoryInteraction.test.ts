import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { resolveWorkObservatoryMotion } from "./workSignalObservatoryInteraction";

const sessionListSource = readFileSync(
  join(import.meta.dir, "../../app/(primary)/list.tsx"),
  "utf8",
);

describe("Work activity entry", () => {
  test("opens from the Sessions menu and attention notice, never a pull-down gesture", () => {
    expect(sessionListSource).not.toContain("GestureDetector");
    expect(sessionListSource).not.toContain("WorkSignalPullPreview");
    expect(sessionListSource).toContain('key: "work"');
    expect(sessionListSource).toContain("onOpenWorkActivity={openWorkObservatory}");
  });

  test("removes the modal transition under reduced motion", () => {
    expect(resolveWorkObservatoryMotion(false)).toEqual({
      modalAnimationType: "fade",
    });
    expect(resolveWorkObservatoryMotion(true)).toEqual({
      modalAnimationType: "none",
    });
  });
});
