import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

describe("Zen keyboard lifecycle native authority", () => {
  test("Android snapshots current root IME insets and native focus ancestry", () => {
    const android = source(
      "android/src/main/java/expo/modules/zenkeyboardlifecycle/ZenKeyboardLifecycleModule.kt",
    );

    expect(android).toContain("ViewCompat.getRootWindowInsets(root)");
    expect(android).toContain("WindowInsetsCompat.Type.ime()");
    expect(android).toContain("rootInsets.isVisible(imeType)");
    expect(android).toContain("rootInsets.getInsets(imeType).bottom");
    expect(android).toContain("activity.currentFocus");
    expect(android).toContain(
      "ReactFindViewUtil.findView(root, composerNativeId)",
    );
    expect(android).not.toContain("persistentKeyboardHeight");
    expect(android).not.toContain("isKeyboardVisible");
    expect(android).not.toContain("KeyboardController");
  });

  test("iOS uses current official layout-guide occlusion and first responder evidence", () => {
    const ios = source("ios/ZenKeyboardLifecycleModule.swift");

    expect(ios).toContain("window.keyboardLayoutGuide.layoutFrame");
    expect(ios).toContain("findFirstResponder(in: window)");
    expect(ios).toContain("responder?.isDescendant(of: $0)");
    expect(ios).toContain("activationState == .foregroundActive");
    expect(ios).not.toContain("UIKeyboardWillShowNotification");
    expect(ios).not.toContain("cached");
  });

  test("both snapshots preserve caller lifecycle provenance", () => {
    const android = source(
      "android/src/main/java/expo/modules/zenkeyboardlifecycle/ZenKeyboardLifecycleModule.kt",
    );
    const ios = source("ios/ZenKeyboardLifecycleModule.swift");

    expect(android).toContain('"revision" to revision');
    expect(ios).toContain('"revision": revision');
  });
});
