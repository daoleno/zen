// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  brainWorkspaceEntryAccessibilityLabel,
  brainWorkspaceEntryIconName,
  brainWorkspaceMarkdownPath,
} from "./brainPresentation";

describe("brainWorkspaceMarkdownPath", () => {
  test("detects common markdown extensions", () => {
    expect(brainWorkspaceMarkdownPath("notes.md")).toBe(true);
    expect(brainWorkspaceMarkdownPath("NOTES.MD")).toBe(true);
    expect(brainWorkspaceMarkdownPath("readme.markdown")).toBe(true);
  });

  test("leaves non-markdown paths unmarked", () => {
    expect(brainWorkspaceMarkdownPath("LICENSE")).toBe(false);
    expect(brainWorkspaceMarkdownPath("notes.txt")).toBe(false);
    expect(brainWorkspaceMarkdownPath(".env")).toBe(false);
  });
});

describe("brainWorkspaceEntryAccessibilityLabel", () => {
  test("keeps folder and file semantics without visible type labels", () => {
    expect(brainWorkspaceEntryAccessibilityLabel("directory", "agents")).toBe(
      "Open folder agents",
    );
    expect(brainWorkspaceEntryAccessibilityLabel("file", "AGENTS.md")).toBe(
      "Open file AGENTS.md",
    );
    expect(
      brainWorkspaceEntryAccessibilityLabel("file", "very-long-name-without-extension"),
    ).toBe("Open file very-long-name-without-extension");
  });
});

describe("brainWorkspaceEntryIconName", () => {
  test("uses folder and document icons without restating type labels", () => {
    expect(brainWorkspaceEntryIconName("directory", "agents")).toBe("folder-outline");
    expect(brainWorkspaceEntryIconName("directory", ".hidden")).toBe("folder-outline");
    expect(brainWorkspaceEntryIconName("file", "AGENTS.md")).toBe("document-text-outline");
    expect(brainWorkspaceEntryIconName("file", "LICENSE")).toBe("document-outline");
  });
});
