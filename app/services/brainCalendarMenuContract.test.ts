import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = readFileSync(
  join(import.meta.dir, "../app/(primary)/index.tsx"),
  "utf8",
);

describe("Brain Calendar menu contract", () => {
  test("keeps established Brain actions ahead of Calendar", () => {
    const menu = source.slice(
      source.indexOf("const menuActions"),
      source.indexOf("const renderBrainComposerAccessory"),
    );
    const keys = ["new-chat", "executor", "terminal", "workspace", "calendar"];

    for (let index = 1; index < keys.length; index += 1) {
      expect(menu.indexOf(`key: \"${keys[index - 1]}\"`)).toBeLessThan(
        menu.indexOf(`key: \"${keys[index]}\"`),
      );
    }
  });
});
