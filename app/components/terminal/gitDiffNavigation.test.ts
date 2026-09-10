import { describe, expect, test } from "bun:test";
import { resolveGitDiffBack } from "./gitDiffNavigation";

describe("Git diff back stack", () => {
  test("the overview is the only state that closes the feature", () => {
    expect(
      resolveGitDiffBack({
        view: "changes",
        hasSelectedFile: false,
        hasBrowserFile: false,
      }),
    ).toBe("close");
  });
  test("hardware back unwinds the reader before closing", () => {
    expect(
      resolveGitDiffBack({
        view: "changes",
        hasSelectedFile: true,
        hasBrowserFile: false,
      }),
    ).toBe("deselect-file");
  });
  test("a working file returns to the browser it was opened from", () => {
    expect(
      resolveGitDiffBack({
        view: "files",
        hasSelectedFile: false,
        hasBrowserFile: true,
      }),
    ).toBe("close-browser-file");
  });
  test("an empty browser returns to the changes list", () => {
    expect(
      resolveGitDiffBack({
        view: "files",
        hasSelectedFile: false,
        hasBrowserFile: false,
      }),
    ).toBe("browser-to-changes");
  });
});
