// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
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

describe("Interface Chat inline code rendering", () => {
  test("keeps long wrapped inline code typographic without opaque tile fill", () => {
    const source = readFileSync(
      join(import.meta.dir, "InterfaceInlineMessage.tsx"),
      "utf8",
    );
    const codeBranch = source.slice(
      source.indexOf('if (part.kind === "code")'),
      source.indexOf('if (part.kind === "link")'),
    );

    expect(codeBranch).toContain("interfaceInlineCodeStyle(theme, compact)");
    expect(codeBranch).not.toMatch(/backgroundColor\s*:/);
    expect(
      tokenizeInlineMessage(
        "`services/provider/mod.rs` plus `server/lifecycle.ts`",
      ),
    ).toEqual([
      { kind: "code", text: "services/provider/mod.rs" },
      { text: " plus " },
      { kind: "code", text: "server/lifecycle.ts" },
    ]);
  });
});
