import { describe, expect, test } from "bun:test";
import { withAlpha } from "./colorWithAlpha";

describe("colorWithAlpha", () => {
  test("converts supported #RRGGBB colors", () => {
    expect(withAlpha("#0F0F14", 0.5)).toBe("rgba(15, 15, 20, 0.5)");
    expect(withAlpha("#f7f8f6", 1)).toBe("rgba(247, 248, 246, 1)");
  });

  test("clamps alpha to the supported range", () => {
    expect(withAlpha("#0F0F14", -1)).toBe("rgba(15, 15, 20, 0)");
    expect(withAlpha("#0F0F14", 2)).toBe("rgba(15, 15, 20, 1)");
  });
});
