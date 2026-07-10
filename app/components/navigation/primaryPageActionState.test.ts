// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  clearPrimaryPageAction,
  registerPrimaryPageAction,
} from "./primaryPageActionState";

describe("primaryPageActionState", () => {
  test("register replaces the active owner content", () => {
    const first = registerPrimaryPageAction(null, "brain", "brain-action");
    expect(first).toEqual({ ownerId: "brain", content: "brain-action" });

    const second = registerPrimaryPageAction(first, "list", "list-action");
    expect(second).toEqual({ ownerId: "list", content: "list-action" });
  });

  test("clear only removes the matching owner", () => {
    const registered = registerPrimaryPageAction(null, "list", "list-action");
    expect(clearPrimaryPageAction(registered, "brain")).toEqual(registered);
    expect(clearPrimaryPageAction(registered, "list")).toBeNull();
  });
});
