import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const surfaceSource = readFileSync(
  join(import.meta.dir, "WorkSignalObservatory.tsx"),
  "utf8",
);
const sessionListSource = readFileSync(
  join(import.meta.dir, "../../app/(primary)/list.tsx"),
  "utf8",
);

describe("Work pull surface presentation", () => {
  test("keeps the established pull entry and renders a flat activity list", () => {
    expect(sessionListSource).toContain("<WorkSignalPullPreview");
    expect(sessionListSource).toContain("<WorkSignalObservatory");
    expect(surfaceSource).toContain("buildWorkActivityListModel");
    expect(surfaceSource).toContain("<WorkSection");
    expect(surfaceSource).not.toContain("RelationshipGraph");
    expect(surfaceSource).not.toContain("react-native-svg");
  });

  test("keeps current-server ownership and canonical history counts visible", () => {
    expect(surfaceSource).toContain("useCurrentServer");
    expect(surfaceSource).toContain("brain?.current_work ?? []");
    expect(surfaceSource).toContain(
      "brain?.work_backlog?.historical_results ?? 0",
    );
    expect(surfaceSource).toContain('title="Needs you"');
    expect(surfaceSource).toContain('title="Active"');
    expect(surfaceSource).toContain('title="Recent"');
  });

  test("returns ownerless Needs you actions to Brain after closing the surface", () => {
    expect(surfaceSource).toContain('if (row.action === "open_brain")');
    expect(surfaceSource).toContain("onClose();\n        onOpenBrain();");
  });

  test("respects reduced-motion settings for the modal", () => {
    expect(surfaceSource).toContain("useReducedMotion()");
    expect(surfaceSource).toContain("motion.modalAnimationType");
  });
});
