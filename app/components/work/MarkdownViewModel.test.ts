// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { tokenizeMarkdownInline } from "./MarkdownViewModel";

describe("Work Markdown inline links", () => {
  test("retains the destination needed for link activation", () => {
    expect(tokenizeMarkdownInline("Read [the guide](https://example.com/guide)."))
      .toEqual([
        { text: "Read " },
        { kind: "link", text: "the guide", url: "https://example.com/guide" },
        { text: "." },
      ]);
  });

  test("separates an optional Markdown title from the destination", () => {
    expect(tokenizeMarkdownInline('[guide](https://example.com "Documentation")'))
      .toEqual([
        { kind: "link", text: "guide", url: "https://example.com" },
      ]);
  });
});
