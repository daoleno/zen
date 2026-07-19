// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { tokenizeInlineMessage } from "./InterfaceMessageBodyModel";

describe("Interface Chat inline links", () => {
  test("retains the destination used by the tappable inline renderer", () => {
    expect(
      tokenizeInlineMessage("Open [the report](https://example.com/report)."),
    ).toEqual([
      { text: "Open " },
      { kind: "link", text: "the report", url: "https://example.com/report" },
      { text: "." },
    ]);
  });
});
