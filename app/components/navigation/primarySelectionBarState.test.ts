import { describe, expect, test } from "bun:test";
import {
  clearPrimarySelectionBar,
  registerPrimarySelectionBar,
} from "./primarySelectionBarState";

describe("primarySelectionBarState", () => {
  test("register replaces the active owner content", () => {
    const first = registerPrimarySelectionBar(null, "list", "selection-node");
    expect(first).toEqual({
      ownerId: "list",
      content: "selection-node",
    });

    const second = registerPrimarySelectionBar(first, "brain", "other-node");
    expect(second).toEqual({
      ownerId: "brain",
      content: "other-node",
    });
  });

  test("clear only removes the matching owner", () => {
    const registered = registerPrimarySelectionBar(
      null,
      "list",
      "selection-node",
    );
    expect(clearPrimarySelectionBar(registered, "brain")).toEqual(registered);
    expect(clearPrimarySelectionBar(registered, "list")).toBeNull();
  });

  test("clear on an empty state stays empty", () => {
    expect(clearPrimarySelectionBar(null, "list")).toBeNull();
  });
});
