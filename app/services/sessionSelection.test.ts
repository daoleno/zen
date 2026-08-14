import { describe, expect, test } from "bun:test";
import {
  addSessionToSelection,
  countSelectionServers,
  countSessionSelection,
  EMPTY_SESSION_SELECTION,
  isSessionTerminable,
  pruneSessionSelection,
  removeSessionsFromSelection,
  selectionCountLabel,
  sessionTerminationEligibility,
  toggleSessionSelection,
  type SessionSelection,
} from "./sessionSelection";
import type { Agent } from "../store/agents";

function agentWith(
  key: string,
  serverId: string = "srv-a",
): Pick<Agent, "key" | "serverId"> {
  return { key, serverId };
}

describe("toggleSessionSelection", () => {
  test("adds a key that was not selected", () => {
    const next = toggleSessionSelection(EMPTY_SESSION_SELECTION, "k1");
    expect([...next]).toEqual(["k1"]);
  });

  test("removes a key that was selected", () => {
    const selected = toggleSessionSelection(EMPTY_SESSION_SELECTION, "k1");
    const next = toggleSessionSelection(selected, "k1");
    expect(next.size).toBe(0);
  });

  test("never mutates the input set", () => {
    const selected = toggleSessionSelection(EMPTY_SESSION_SELECTION, "k1");
    toggleSessionSelection(selected, "k2");
    expect([...selected]).toEqual(["k1"]);
  });

  test("add and remove helpers are idempotent", () => {
    const once = addSessionToSelection(EMPTY_SESSION_SELECTION, "k1");
    const twice = addSessionToSelection(once, "k1");
    expect(twice).toBe(once);
    const removed = removeSessionsFromSelection(once, ["k1"]);
    expect(removed.size).toBe(0);
    expect(removeSessionsFromSelection(removed, ["k1"])).toBe(removed);
  });
});

describe("pruneSessionSelection", () => {
  test("keeps only keys whose authoritative row still exists", () => {
    const selected = new Set<string>(["k1", "k2", "k3"]);
    const pruned = pruneSessionSelection(selected, ["k1", "k3"]);
    expect([...pruned].sort()).toEqual(["k1", "k3"]);
  });

  test("returns the same reference when nothing disappeared", () => {
    const selected = new Set<string>(["k1"]);
    expect(pruneSessionSelection(selected, ["k1", "k2"])).toBe(selected);
  });

  test("empty selection prunes to itself without work", () => {
    expect(pruneSessionSelection(EMPTY_SESSION_SELECTION, [])).toBe(
      EMPTY_SESSION_SELECTION,
    );
  });

  test("reorder and duplicates in the authoritative list never disturb selection", () => {
    const selected = new Set<string>(["k2", "k1"]);
    const pruned = pruneSessionSelection(selected, ["k3", "k1", "k2", "k1"]);
    expect([...pruned].sort()).toEqual(["k1", "k2"]);
  });
});

describe("countSessionSelection + labels", () => {
  test("counts selected keys", () => {
    const selected = new Set<string>(["a", "b"]);
    expect(countSessionSelection(selected)).toBe(2);
    expect(countSessionSelection(EMPTY_SESSION_SELECTION)).toBe(0);
  });

  test("count label is singular for one and plural otherwise", () => {
    expect(selectionCountLabel(0)).toBe("0 selected");
    expect(selectionCountLabel(1)).toBe("1 selected");
    expect(selectionCountLabel(3)).toBe("3 selected");
  });

  test("countSelectionServers spans distinct daemons only", () => {
    const agents = [
      agentWith("a", "srv-1"),
      agentWith("b", "srv-1"),
      agentWith("c", "srv-2"),
    ];
    expect(countSelectionServers(agents)).toBe(2);
    expect(countSelectionServers([agents[0]])).toBe(1);
    expect(countSelectionServers([])).toBe(0);
  });
});

describe("sessionTerminationEligibility", () => {
  test("connected daemon makes every listed Session terminable", () => {
    expect(sessionTerminationEligibility("connected")).toEqual({
      eligible: true,
      reason: null,
    });
  });

  test("offline daemon excludes rows with a truthful reason", () => {
    for (const state of ["offline", "connecting"] as const) {
      const result = sessionTerminationEligibility(state);
      expect(result.eligible).toBe(false);
      expect(result.reason).toBe("Daemon is not connected");
    }
  });

  test("unknown connection state is not eligible", () => {
    expect(sessionTerminationEligibility(undefined).eligible).toBe(false);
  });

  test("isSessionTerminable reads the per-server connection", () => {
    const connections = {
      "srv-a": "connected",
      "srv-b": "offline",
    } as Record<string, "connected" | "offline">;
    expect(isSessionTerminable(agentWith("a", "srv-a"), connections)).toBe(
      true,
    );
    expect(isSessionTerminable(agentWith("b", "srv-b"), connections)).toBe(
      false,
    );
  });
});

describe("selection is a plain Set", () => {
  test("selection type stays a readonly Set of stable keys", () => {
    const selected: SessionSelection = new Set(["x"]);
    expect(selected instanceof Set).toBe(true);
  });
});
