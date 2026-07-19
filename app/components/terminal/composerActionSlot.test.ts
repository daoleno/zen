import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import {
  COMPOSER_ACTION_SLOT_WIDTH,
  composerInputWidth,
} from "./composerActionSlot";

describe("Composer trailing action geometry", () => {
  test("uses one identical-width slot for elapsed Stop and Send", () => {
    expect(COMPOSER_ACTION_SLOT_WIDTH).toBe(68);
  });

  test("the TextInput gets every remaining pixel in every rendered layout", () => {
    expect(
      [320, 390, 430].map((width) => composerInputWidth(width, "chatgpt")),
    ).toEqual([154, 224, 264]);
    expect(
      [320, 390, 430].map((width) => composerInputWidth(width, "telegram")),
    ).toEqual([172, 242, 282]);
    expect(
      [320, 390, 430].map((width) => composerInputWidth(width, "classic")),
    ).toEqual([165, 235, 275]);
    expect(
      [320, 390, 430].map((width) => composerInputWidth(width, "ambient")),
    ).toEqual([172, 242, 282]);
  });

  test("rendered panels have one conditional trailing action and a flexible input", () => {
    const panel = readFileSync(
      new URL("./InterfaceComposerPanel.tsx", import.meta.url),
      "utf8",
    );
    const input = readFileSync(
      new URL("./InterfaceComposerInput.tsx", import.meta.url),
      "utf8",
    );
    expect((panel.match(/const actionButton =/g) ?? []).length).toBe(1);
    expect((panel.match(/<ComposerSendButton/g) ?? []).length).toBe(1);
    expect((panel.match(/\{actionButton\}/g) ?? []).length).toBe(2);
    expect(panel).not.toContain("COMPOSER_STOP_SLOT_WIDTH");
    expect(panel).toContain("showStopButton ? stopLabel : sendLabel");
    expect(input).toContain("flex: 1");
    expect(input).toContain("minWidth: 0");
    expect(input).toContain('width: "100%"');
  });
});
