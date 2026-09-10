import { describe, expect, test } from "bun:test";
import { resolveGitDiffBack } from "./gitDiffNavigation";

describe("Git diff back stack", () => {
  test("the overview is the only state that closes the feature", () => {
    expect(
      resolveGitDiffBack({
        view: "changes",
        hasSelectedFile: false,
        hasBrowserFile: false,
        fileOrigin: "files",
      }),
    ).toBe("close");
  });
  test("hardware back unwinds the reader before closing", () => {
    expect(
      resolveGitDiffBack({
        view: "changes",
        hasSelectedFile: true,
        hasBrowserFile: false,
        fileOrigin: "files",
      }),
    ).toBe("deselect-file");
  });
  test("a working file opened from the reader returns to the reader", () => {
    expect(
      resolveGitDiffBack({
        view: "files",
        hasSelectedFile: true,
        hasBrowserFile: true,
        fileOrigin: "changes",
      }),
    ).toBe("close-browser-file-to-reader");
  });
  test("a working file opened from the browser returns to the browser", () => {
    expect(
      resolveGitDiffBack({
        view: "files",
        hasSelectedFile: false,
        hasBrowserFile: true,
        fileOrigin: "files",
      }),
    ).toBe("close-browser-file-to-browser");
  });
  test("an empty browser returns to the changes list", () => {
    expect(
      resolveGitDiffBack({
        view: "files",
        hasSelectedFile: false,
        hasBrowserFile: false,
        fileOrigin: "files",
      }),
    ).toBe("browser-to-changes");
  });
});
