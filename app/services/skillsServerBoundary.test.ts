import { describe, expect, test } from "bun:test";
import { SkillsServerRequestOwner } from "./skillsServerBoundary";

describe("Skills current-server request ownership", () => {
  test("a server switch invalidates inventory, catalog, search, mutation, and handoff work", () => {
    const owner = new SkillsServerRequestOwner();
    owner.rebind("server-a");
    const tokens = [
      owner.issue("inventory"),
      owner.issue("catalog"),
      owner.issue("search"),
      owner.issue("mutation"),
      owner.issue("handoff"),
    ];
    expect(tokens.every((token) => owner.isCurrent(token))).toBe(true);

    owner.rebind("server-b");
    expect(tokens.every((token) => owner.isCurrent(token))).toBe(false);
    expect(owner.isCurrent(owner.issue("inventory"))).toBe(true);
  });

  test("new work invalidates only its own channel on the same server", () => {
    const owner = new SkillsServerRequestOwner();
    owner.rebind("server-a");
    const inventory = owner.issue("inventory");
    const firstSearch = owner.issue("search");
    const secondSearch = owner.issue("search");

    expect(owner.isCurrent(inventory)).toBe(true);
    expect(owner.isCurrent(firstSearch)).toBe(false);
    expect(owner.isCurrent(secondSearch)).toBe(true);
  });

  test("unmount invalidation prevents late same-server completion", () => {
    const owner = new SkillsServerRequestOwner();
    owner.rebind("server-a");
    const inventory = owner.issue("inventory");
    owner.invalidateAll();
    expect(owner.isCurrent(inventory)).toBe(false);
  });
});
