// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { InterfaceMarkdownErrorBoundary } from "./InterfaceMarkdownErrorBoundary";
import {
  INTERFACE_MARKDOWN_MOBILE_RENDERER,
  prepareInterfaceMarkdown,
} from "./InterfaceNativeMarkdownBodyPrepare";

describe("Interface native Markdown mobile contract", () => {
  test("makes prose selectable before the native long-press begins", () => {
    const componentDir = import.meta.dir;
    const selectionOwner = readFileSync(
      join(componentDir, "InterfaceChatSurfaceHooks.ts"),
      "utf8",
    );
    const markdownRenderer = readFileSync(
      join(componentDir, "InterfaceNativeMarkdownBody.tsx"),
      "utf8",
    );

    expect(selectionOwner).toContain("const textSelectable = true;");
    expect(selectionOwner).not.toContain("[textSelectable, setTextSelectable]");
    expect(markdownRenderer).toContain("selectable={textSelectable}");

    const touchOwnership = selectionOwner.slice(
      selectionOwner.indexOf("const handleTimelineTouchActiveChange"),
      selectionOwner.indexOf("const attachToLatest"),
    );
    const selectionOwnership = selectionOwner.slice(
      selectionOwner.indexOf("const handleTextSelectionGestureStart"),
      selectionOwner.indexOf("const handleTextSelectionGestureEnd"),
    );
    expect(touchOwnership).not.toContain("cancelTurnFocus");
    expect(selectionOwnership).not.toContain("cancelTurnFocus");
    expect(selectionOwner).toContain('cancelTurnFocus("drag")');
  });

  test("resolves one shared enriched renderer on Android and iOS", () => {
    const componentDir = import.meta.dir;
    expect(INTERFACE_MARKDOWN_MOBILE_RENDERER).toBe("enriched");
    expect(
      existsSync(join(componentDir, "InterfaceNativeMarkdownBody.tsx")),
    ).toBe(true);
    expect(
      existsSync(join(componentDir, "InterfaceNativeMarkdownBody.android.tsx")),
    ).toBe(false);
    expect(
      existsSync(join(componentDir, "InterfaceNativeMarkdownBody.ios.tsx")),
    ).toBe(false);
  });

  test("keeps render/error fallback as the only downgrade path", () => {
    const fallback = "fallback-body";
    const boundary = new InterfaceMarkdownErrorBoundary({
      fallback,
      children: "enriched-body",
      resetKey: "one",
    });
    boundary.state = { failed: false };
    expect(boundary.render()).toBe("enriched-body");

    boundary.state = InterfaceMarkdownErrorBoundary.getDerivedStateFromError(
      new Error("renderer failed"),
    );
    expect(boundary.state).toEqual({ failed: true });
    expect(boundary.render()).toBe(fallback);
  });

  test("normalizes markdown for the shared mobile renderer", () => {
    expect(prepareInterfaceMarkdown("  hello  ", false)).toBe("hello");
    expect(
      prepareInterfaceMarkdown("![alt](https://example.com/x.png)", false),
    ).toBe("[alt](https://example.com/x.png)");
  });
});
