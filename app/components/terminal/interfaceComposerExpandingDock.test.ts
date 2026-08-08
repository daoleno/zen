import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

describe("InterfaceComposerExpandingDock", () => {
  const dock = source("InterfaceComposerExpandingDock.tsx");

  test("owns one Reanimated progress derived from Composer focus", () => {
    expect(dock.match(/useSharedValue\(/g)).toHaveLength(1);
    expect(dock).toContain("const progress = useSharedValue(0);");
    expect(dock).toContain("composerExpansionTarget(focused)");
    expect(dock).toContain("useReducedMotion()");
    expect(dock).toContain("withSpring(target, COMPOSER_SPRING_CONFIG)");
    expect(dock).toContain("composerMotionDisabled(reducedMotion)");
  });

  test("derives every motion from the single progress", () => {
    expect(dock).toContain("composerActionBandHeight(progress.value)");
    expect(dock).toContain("composerExpansionRadius(progress.value)");
    expect(dock).toContain("composerInputHorizontalPadding(progress.value)");
    expect(dock).toContain("composerModelChipReveal(progress.value)");
  });

  test("keeps one trailing Send/Stop slot with stable labels", () => {
    expect(dock.match(/<ComposerSendButton/g)).toHaveLength(1);
    expect(dock).toContain("showStopButton ? onStopPress : onSendPress");
    expect(dock).toContain(
      "accessibilityLabel={showStopButton ? stopLabel : sendLabel}",
    );
    expect(dock).toContain("elapsedStartedAt={providerActivityStartedAt}");
  });

  test("no fake voice contract and no competing animation owner", () => {
    expect(dock).not.toMatch(/mic|microphone|voice/i);
    expect(dock).not.toMatch(/import[^;]*LayoutAnimation/);
    expect(dock).not.toMatch(/import[^;]*withTiming|LayoutAnimationConfig/);
  });

  test("anchors Plus and Send to the capsule bottom so the band carries them", () => {
    expect(dock).toContain("position: \"absolute\"");
    expect(dock).toContain("bottom: COMPOSER_ACTION_BAND_VERTICAL_PADDING,");
    expect(dock).toContain("actionSlotRight");
    expect(dock).toContain("actionSlotLeft");
  });

  test("reveals the model chip only when a host supplies a control", () => {
    expect(dock).toContain("modelControl && onModelControlPress ? (");
    expect(dock).toContain("<ComposerModelChip");
    expect(dock).toContain('pointerEvents={focused ? "auto" : "none"}');
    expect(dock).toContain("accessibilityElementsHidden={!focused}");
    expect(dock).toContain("importantForAccessibility");
  });

  test("preserves the multiline input and upload/action affordances", () => {
    expect(dock).toContain("<InterfaceComposerInput");
    expect(dock).toContain("loading={uploading}");
    expect(dock).toContain('icon={actionMenuExpanded ? "close" : actionMenuIcon}');
  });
});
