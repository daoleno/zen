import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

describe("Composer presentation model control", () => {
  test("presentation owns the host-supplied model control", () => {
    const model = source("InterfaceChatSurfaceModel.ts");
    expect(model).toContain("modelControl: ComposerModelControlPresentation | null;");
    expect(model).toContain("modelControl?: ComposerModelControlPresentation | null;");
    expect(model).toContain("modelControl = null,");
    expect(model).toContain("modelControl,");
  });

  test("the shared Composer chain carries the control and its press handler", () => {
    const section = source("InterfaceChatComposerSection.tsx");
    const composer = source("InterfaceChatComposer.tsx");
    const panel = source("InterfaceComposerPanel.tsx");
    const body = source("InterfaceChatBody.tsx");

    expect(section).toContain("modelControl={presentation.modelControl}");
    expect(section).toContain("onModelControlPress={onModelControlPress}");
    expect(composer).toContain("modelControl={modelControl}");
    expect(composer).toContain("onModelControlPress={onModelControlPress}");
    expect(panel).toContain("modelControl={modelControl}");
    expect(panel).toContain("onModelControlPress={onModelControlPress}");
    expect(body).toContain("onModelControlPress={onModelControlPress}");
  });

  test("the expanding dock consumes the control without inventing one", () => {
    const dock = source("InterfaceComposerExpandingDock.tsx");
    expect(dock).toContain("label={modelControl.label}");
    expect(dock).toContain("accessibilityLabel={modelControl.accessibilityLabel}");
    expect(dock).toContain("modelControl && onModelControlPress ? (");
  });
});
