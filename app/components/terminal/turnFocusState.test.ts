import { describe, expect, test } from "bun:test";
import {
  createTurnFocusState,
  reduceTurnFocus,
  resolveTurnFocusAnchorItemId,
  shouldClearTurnFocusForSurfaceLifecycle,
  turnFocusOwnsMomentum,
  turnFocusRowGeometryFromCell,
  turnFocusSuppressesOrdinaryFollow,
  type TurnFocusEvent,
  type TurnFocusState,
} from "./turnFocusState";

const GENERATION = "conversation-a:connection-4";
const FIRST_INTENT_TOKEN = 1;

function withGeometry(state = createTurnFocusState(GENERATION)) {
  const withViewport = reduceTurnFocus(state, {
    type: "geometry",
    viewportHeight: 800,
    topChromeInset: 64,
  }).state;
  return reduceTurnFocus(withViewport, {
    type: "spacer_layout",
    height: 0,
    requestEpoch: withViewport.spacerRequestEpoch,
  }).state;
}

function sampleClearance(
  state: TurnFocusState,
  intentToken = FIRST_INTENT_TOKEN,
  clearance = 180,
  latestOffset = -clearance,
) {
  return reduceTurnFocus(state, {
    type: "clearance_sample",
    intentToken,
    clearance,
    latestOffset,
  }).state;
}

function layoutSpacer(
  state: TurnFocusState,
  height: number,
  requestEpoch = state.spacerRequestEpoch,
) {
  return reduceTurnFocus(state, {
    type: "spacer_layout",
    height,
    requestEpoch,
  });
}

function layoutFocusedRow(
  state: TurnFocusState,
  {
    pendingMessageId = "pending-current",
    height = 100,
    newestEdgeOffset = 0,
  }: {
    pendingMessageId?: string;
    height?: number;
    newestEdgeOffset?: number;
  } = {},
) {
  return reduceTurnFocus(state, {
    type: "row_layout",
    generation: GENERATION,
    pendingMessageId,
    height,
    newestEdgeOffset,
  });
}

function beginFocusedTurn({
  pendingMessageId = "pending-current",
  reducedMotion = false,
}: {
  pendingMessageId?: string;
  reducedMotion?: boolean;
} = {}) {
  let state = withGeometry();
  state = reduceTurnFocus(state, {
    type: "intent",
    generation: GENERATION,
    pendingMessageId,
    intentToken: FIRST_INTENT_TOKEN,
    reducedMotion,
  }).state;
  state = layoutSpacer(state, 0).state;
  state = sampleClearance(state);
  state = layoutFocusedRow(state, { pendingMessageId }).state;

  expect(state).toMatchObject({
    phase: "arming",
    pendingMessageId,
    spacerHeight: 456,
  });

  const headerCommit = layoutSpacer(state, 456);
  expect(headerCommit.effect).toBeUndefined();
  const committed = layoutFocusedRow(headerCommit.state, {
    pendingMessageId,
    newestEdgeOffset: 456,
  });
  expect(committed.effect).toEqual({
    type: "return_to_latest",
    pendingMessageId,
    animated: !reducedMotion,
    latestOffset: -180,
  });
  expect(committed.state.phase).toBe("active");
  return committed.state;
}

describe("current-turn focus state", () => {
  test("reveals a committed anchor once after native zero is proven", () => {
    let state = withGeometry();
    state = reduceTurnFocus(state, {
      type: "intent",
      generation: GENERATION,
      pendingMessageId: "pending-current",
      intentToken: FIRST_INTENT_TOKEN,
      reducedMotion: false,
    }).state;
    const anchorAvailable = {
      type: "anchor_available",
      generation: GENERATION,
      pendingMessageId: "pending-current",
      latestOffset: -180,
    } as unknown as TurnFocusEvent;

    const anchorCommit = reduceTurnFocus(state, anchorAvailable);
    expect(anchorCommit.effect).toBeUndefined();
    const zeroCommit = layoutSpacer(anchorCommit.state, 0);
    expect(zeroCommit.effect).toEqual({
      type: "reveal_anchor",
      pendingMessageId: "pending-current",
      animated: false,
      latestOffset: -180,
    });
    expect(
      reduceTurnFocus(zeroCommit.state, anchorAvailable).effect,
    ).toBeUndefined();
  });

  test("gives every fresh intent its own native zero-spacer transaction", () => {
    const physicallyZero = withGeometry();
    const requested = reduceTurnFocus(physicallyZero, {
      type: "intent",
      generation: GENERATION,
      pendingMessageId: "pending-current",
      intentToken: FIRST_INTENT_TOKEN,
      reducedMotion: false,
    }).state;

    expect(requested.spacerHeight).toBe(0);
    expect(requested.spacerRequestEpoch).toBe(
      physicallyZero.spacerRequestEpoch + 1,
    );
    expect(requested.spacerLayoutEpoch).toBe(physicallyZero.spacerRequestEpoch);
    const provisionalRow = layoutFocusedRow(requested);
    expect(provisionalRow.effect).toBeUndefined();
    expect(provisionalRow.state).toMatchObject({
      phase: "awaiting-row",
      rowHeight: 100,
      spacerLayoutEpoch: physicallyZero.spacerRequestEpoch,
    });
  });

  test("takes the newest-edge offset from the positioned list cell and its causal alias", () => {
    const childLocalLayout = { height: 100, y: 0 };
    const positionedCellLayout = { height: 100, y: 60 };

    expect(childLocalLayout.y).toBe(0);
    expect(
      turnFocusRowGeometryFromCell(
        "pending-current",
        "pending-current",
        positionedCellLayout,
      ),
    ).toEqual({
      pendingMessageId: "pending-current",
      height: 100,
      newestEdgeOffset: 60,
    });
    expect(
      turnFocusRowGeometryFromCell(
        "pending-current",
        "provider-echo",
        positionedCellLayout,
        "provider-echo",
      ),
    ).toEqual({
      pendingMessageId: "pending-current",
      height: 100,
      newestEdgeOffset: 60,
    });
    expect(
      turnFocusRowGeometryFromCell("pending-current", "newer-activity", {
        height: 60,
        y: 0,
      }),
    ).toBeUndefined();
  });

  test("a same-id provider cell becomes measurable when its reducer alias arrives", () => {
    const canonicalProviderCell = {
      id: "provider-echo",
      body: "provider canonical body",
    };
    expect(
      resolveTurnFocusAnchorItemId("pending-current", [canonicalProviderCell]),
    ).toBeUndefined();

    const aliasedProviderCell = {
      ...canonicalProviderCell,
      turnFocusAnchorId: "pending-current",
    };
    expect(aliasedProviderCell.id).toBe(canonicalProviderCell.id);
    expect(
      resolveTurnFocusAnchorItemId("pending-current", [aliasedProviderCell]),
    ).toBe("provider-echo");
  });

  test("only the exact successful pending row arms one animated return", () => {
    let state = withGeometry();
    state = reduceTurnFocus(state, {
      type: "intent",
      generation: GENERATION,
      pendingMessageId: "pending-current",
      intentToken: FIRST_INTENT_TOKEN,
      reducedMotion: false,
    }).state;
    state = layoutSpacer(state, 0).state;
    state = sampleClearance(state);

    const oldAttempt = layoutFocusedRow(state, {
      pendingMessageId: "pending-old",
      height: 92,
    });
    expect(oldAttempt.state).toBe(state);
    expect(oldAttempt.effect).toBeUndefined();

    state = layoutFocusedRow(state).state;
    const matchingHeader = layoutSpacer(state, 456);
    expect(matchingHeader.effect).toBeUndefined();
    const firstCommit = layoutFocusedRow(matchingHeader.state, {
      newestEdgeOffset: 456,
    });
    expect(firstCommit.effect).toEqual({
      type: "return_to_latest",
      pendingMessageId: "pending-current",
      animated: true,
      latestOffset: -180,
    });

    const repeatedLayout = layoutFocusedRow(firstCommit.state, {
      newestEdgeOffset: 456,
    });
    expect(repeatedLayout.effect).toBeUndefined();
    expect(repeatedLayout.state).toMatchObject({
      phase: "active",
      newestEdgeOffset: 0,
      spacerHeight: 456,
    });
  });

  test("provider echo replacement preserves the same active anchor continuously", () => {
    let state = beginFocusedTurn();

    // The provider row is canonical and can have different presentation
    // height, but CellRenderer reports it under the causal pending anchor.
    state = layoutFocusedRow(state, {
      height: 96,
      newestEdgeOffset: 456,
    }).state;
    expect(state).toMatchObject({
      phase: "active",
      rowHeight: 96,
      newestEdgeOffset: 0,
      spacerHeight: 460,
    });

    state = layoutSpacer(state, 460).state;
    const responseGrowth = layoutFocusedRow(state, {
      height: 96,
      newestEdgeOffset: 580,
    });
    expect(responseGrowth.effect).toBeUndefined();
    expect(responseGrowth.state).toMatchObject({
      phase: "active",
      newestEdgeOffset: 120,
      spacerHeight: 340,
    });
  });

  test("positioned turn geometry consumes spacer when flexGrow hides total growth", () => {
    const active = beginFocusedTurn();

    // The mounted spacer contributes 456px to y. A newer sibling grows by
    // 180px while the parent content-container height remains unchanged.
    const boundaryGrowth = layoutFocusedRow(active, {
      newestEdgeOffset: 636,
    });

    expect(boundaryGrowth.effect).toBeUndefined();
    expect(boundaryGrowth.state).toMatchObject({
      phase: "active",
      newestEdgeOffset: 180,
      spacerHeight: 276,
    });
  });

  test("busy Activity and assistant streaming consume the same positioned budget", () => {
    let state = beginFocusedTurn();
    state = layoutFocusedRow(state, { newestEdgeOffset: 516 }).state;
    expect(state.spacerHeight).toBe(396);

    // Applying the matching contraction leaves the focused row's physical y
    // unchanged: 396px spacer + 60px response geometry.
    state = layoutSpacer(state, 396).state;
    state = layoutFocusedRow(state, { newestEdgeOffset: 456 }).state;
    expect(state.spacerHeight).toBe(396);

    state = layoutFocusedRow(state, { newestEdgeOffset: 576 }).state;
    expect(state).toMatchObject({
      newestEdgeOffset: 180,
      spacerHeight: 276,
    });
  });

  test("growth before the first spacer layout retargets and waits for its native height", () => {
    let state = withGeometry();
    state = reduceTurnFocus(state, {
      type: "intent",
      generation: GENERATION,
      pendingMessageId: "pending-growing",
      intentToken: FIRST_INTENT_TOKEN,
      reducedMotion: false,
    }).state;
    state = layoutSpacer(state, 0).state;
    state = sampleClearance(state);
    state = layoutFocusedRow(state, {
      pendingMessageId: "pending-growing",
    }).state;

    // Fabric delivers the header first. Even though actual now matches the
    // old 456px request, that observation alone must never return.
    const matchingOldBudgetHeader = layoutSpacer(state, 456);
    expect(matchingOldBudgetHeader.effect).toBeUndefined();
    expect(matchingOldBudgetHeader.state).toMatchObject({
      phase: "arming",
      spacerLayoutHeight: 456,
      spacerHeight: 456,
    });

    // The following positioned row includes 60px of provider growth from the
    // same transaction, so y - actual retargets the request to 396px.
    const retargetedRow = layoutFocusedRow(matchingOldBudgetHeader.state, {
      pendingMessageId: "pending-growing",
      newestEdgeOffset: 516,
    });
    expect(retargetedRow.effect).toBeUndefined();
    expect(retargetedRow.state).toMatchObject({
      phase: "arming",
      newestEdgeOffset: 60,
      spacerHeight: 396,
    });

    const matchingNewBudgetHeader = layoutSpacer(retargetedRow.state, 396);
    expect(matchingNewBudgetHeader.effect).toBeUndefined();
    const committed = layoutFocusedRow(matchingNewBudgetHeader.state, {
      pendingMessageId: "pending-growing",
      newestEdgeOffset: 456,
    });
    expect(committed.effect).toEqual({
      type: "return_to_latest",
      pendingMessageId: "pending-growing",
      animated: true,
      latestOffset: -180,
    });
    expect(
      layoutFocusedRow(committed.state, {
        pendingMessageId: "pending-growing",
        newestEdgeOffset: 456,
      }).effect,
    ).toBeUndefined();
  });

  test("a matching positioned row proves the spacer before a missing header callback", () => {
    let state = withGeometry();
    state = reduceTurnFocus(state, {
      type: "intent",
      generation: GENERATION,
      pendingMessageId: "pending-row-first",
      intentToken: FIRST_INTENT_TOKEN,
      reducedMotion: false,
    }).state;
    state = layoutSpacer(state, 0).state;
    state = sampleClearance(state);
    state = layoutFocusedRow(state, {
      pendingMessageId: "pending-row-first",
    }).state;
    const spacerRequestEpoch = state.spacerRequestEpoch;

    const rowBeforeHeader = layoutFocusedRow(state, {
      pendingMessageId: "pending-row-first",
      newestEdgeOffset: 456,
    });
    expect(rowBeforeHeader.effect).toEqual({
      type: "return_to_latest",
      pendingMessageId: "pending-row-first",
      animated: true,
      latestOffset: -180,
    });
    expect(rowBeforeHeader.state).toMatchObject({
      phase: "active",
      spacerHeight: 456,
      spacerLayoutHeight: 456,
      spacerLayoutEpoch: spacerRequestEpoch,
      newestEdgeOffset: 0,
    });
  });

  test("already-running Activity is included in the exact row anchor geometry", () => {
    let state = withGeometry();
    state = reduceTurnFocus(state, {
      type: "intent",
      generation: GENERATION,
      pendingMessageId: "pending-queued",
      intentToken: FIRST_INTENT_TOKEN,
      reducedMotion: false,
    }).state;
    state = layoutSpacer(state, 0).state;
    state = sampleClearance(state);
    state = layoutFocusedRow(state, {
      pendingMessageId: "pending-queued",
      newestEdgeOffset: 60,
    }).state;

    expect(state).toMatchObject({
      phase: "arming",
      newestEdgeOffset: 60,
      spacerHeight: 396,
    });
  });

  test("rapid supersession waits through A spacer, zero, and B budget epochs", () => {
    let state = beginFocusedTurn({ pendingMessageId: "pending-a" });
    expect(state.spacerLayoutHeight).toBe(456);

    state = reduceTurnFocus(state, {
      type: "intent",
      generation: GENERATION,
      pendingMessageId: "pending-b",
      intentToken: 2,
      reducedMotion: false,
    }).state;
    state = sampleClearance(state, 2);
    expect(state).toMatchObject({
      phase: "awaiting-row",
      pendingMessageId: "pending-b",
      spacerHeight: 0,
      spacerLayoutHeight: 456,
    });

    const rowWithOldSpacer = layoutFocusedRow(state, {
      pendingMessageId: "pending-b",
      height: 120,
      newestEdgeOffset: 456,
    });
    expect(rowWithOldSpacer.state).toBe(state);

    state = layoutSpacer(state, 0).state;
    state = layoutFocusedRow(state, {
      pendingMessageId: "pending-b",
      height: 120,
    }).state;
    expect(state).toMatchObject({
      phase: "arming",
      pendingMessageId: "pending-b",
      spacerHeight: 436,
    });

    const matchingHeader = layoutSpacer(state, 436);
    expect(matchingHeader.effect).toBeUndefined();
    const committed = layoutFocusedRow(matchingHeader.state, {
      pendingMessageId: "pending-b",
      height: 120,
      newestEdgeOffset: 436,
    });
    expect(committed.effect).toEqual({
      type: "return_to_latest",
      pendingMessageId: "pending-b",
      animated: true,
      latestOffset: -180,
    });
  });

  test("rapid supersession needs no new parent size event when the total is unchanged", () => {
    let state = beginFocusedTurn({ pendingMessageId: "pending-a" });
    state = reduceTurnFocus(state, {
      type: "intent",
      generation: GENERATION,
      pendingMessageId: "pending-b",
      intentToken: 2,
      reducedMotion: false,
    }).state;
    state = sampleClearance(state, 2);
    state = layoutSpacer(state, 0).state;

    // Fabric's parent content-container can retain the same numeric height;
    // exact B cell layout is sufficient after the mounted zero observation.
    state = layoutFocusedRow(state, {
      pendingMessageId: "pending-b",
      height: 120,
    }).state;
    expect(state).toMatchObject({
      phase: "arming",
      pendingMessageId: "pending-b",
      spacerHeight: 436,
    });

    const matchingHeader = layoutSpacer(state, 436);
    expect(matchingHeader.effect).toBeUndefined();
    expect(
      layoutFocusedRow(matchingHeader.state, {
        pendingMessageId: "pending-b",
        height: 120,
        newestEdgeOffset: 436,
      }).effect,
    ).toEqual({
      type: "return_to_latest",
      pendingMessageId: "pending-b",
      animated: true,
      latestOffset: -180,
    });
  });

  test("a nonzero spacer observation invalidates pre-clearance row geometry", () => {
    let state = withGeometry();
    state = reduceTurnFocus(state, {
      type: "intent",
      generation: GENERATION,
      pendingMessageId: "pending-race",
      intentToken: 21,
      reducedMotion: false,
    }).state;
    state = layoutSpacer(state, 0).state;
    state = layoutFocusedRow(state, {
      pendingMessageId: "pending-race",
    }).state;
    expect(state).toMatchObject({
      phase: "awaiting-row",
      rowHeight: 100,
      spacerLayoutHeight: 0,
    });

    state = layoutSpacer(state, 80).state;
    expect(state).toMatchObject({
      phase: "awaiting-row",
      rowHeight: undefined,
      newestEdgeOffset: undefined,
      spacerLayoutHeight: 80,
    });

    state = sampleClearance(state, 21);
    expect(state.phase).toBe("awaiting-row");
    state = layoutSpacer(state, 0).state;
    expect(state.rowHeight).toBeUndefined();

    state = layoutFocusedRow(state, {
      pendingMessageId: "pending-race",
    }).state;
    expect(state).toMatchObject({
      phase: "arming",
      spacerHeight: 456,
    });
  });

  test("a superseding focus owns late momentum from the prior native animation", () => {
    let state = beginFocusedTurn({ pendingMessageId: "pending-a" });
    state = reduceTurnFocus(state, {
      type: "intent",
      generation: GENERATION,
      pendingMessageId: "pending-b",
      intentToken: 2,
      reducedMotion: false,
    }).state;

    // requestTurnFocus does not clear the prior native animation marker, and
    // B's non-idle phase independently owns a late A begin/end pair.
    expect(turnFocusOwnsMomentum(state.phase, 1)).toBe(true);
    expect(turnFocusOwnsMomentum(state.phase, 0)).toBe(true);

    state = sampleClearance(state, 2);
    state = layoutSpacer(state, 0).state;
    state = layoutFocusedRow(state, {
      pendingMessageId: "pending-b",
      height: 120,
    }).state;
    expect(turnFocusOwnsMomentum(state.phase, 0)).toBe(true);

    const matchingHeader = layoutSpacer(state, 436);
    expect(matchingHeader.effect).toBeUndefined();
    const committed = layoutFocusedRow(matchingHeader.state, {
      pendingMessageId: "pending-b",
      height: 120,
      newestEdgeOffset: 436,
    });
    expect(committed.effect?.pendingMessageId).toBe("pending-b");
    expect(turnFocusOwnsMomentum(committed.state.phase, 0)).toBe(true);
    expect(turnFocusOwnsMomentum("idle", 0)).toBe(false);
  });

  test("overlapping automatic returns retain B ownership after A ends", () => {
    let automaticReturnsInFlight = 0;
    automaticReturnsInFlight += 1; // A return issued.
    automaticReturnsInFlight += 1; // B return issued before A settles.
    automaticReturnsInFlight -= 1; // Late A end.

    expect(turnFocusOwnsMomentum("idle", automaticReturnsInFlight)).toBe(true);

    automaticReturnsInFlight -= 1; // B end.
    expect(turnFocusOwnsMomentum("idle", automaticReturnsInFlight)).toBe(false);

    // A missing native end can leave a conservative marker, but real user
    // touch/begin-drag resets it before genuine user momentum is classified.
    automaticReturnsInFlight = 1;
    automaticReturnsInFlight = 0;
    expect(turnFocusOwnsMomentum("idle", automaticReturnsInFlight)).toBe(false);
  });

  test("manual cancellation waits for the current native zero transaction before ordinary follow", () => {
    const active = beginFocusedTurn();
    const activeRequestEpoch = active.spacerRequestEpoch;
    const cancelled = reduceTurnFocus(active, {
      type: "cancel",
      reason: "touch",
    }).state;

    expect(cancelled).toMatchObject({
      phase: "idle",
      spacerHeight: 0,
      spacerLayoutHeight: 456,
      spacerRequestEpoch: activeRequestEpoch + 1,
      spacerLayoutEpoch: activeRequestEpoch,
    });
    expect(
      ["content_size", "viewport_layout"].map(() =>
        turnFocusSuppressesOrdinaryFollow(cancelled),
      ),
    ).toEqual([true, true]);
    expect(turnFocusOwnsMomentum(cancelled.phase, 0)).toBe(false);

    // This zero belonged to the pre-cancel request and reached JS late. Its
    // numeric height alone must not release content/layout following.
    const stalePreCancelZero = layoutSpacer(
      cancelled,
      0,
      activeRequestEpoch,
    ).state;
    expect(
      ["content_size", "viewport_layout"].map(() =>
        turnFocusSuppressesOrdinaryFollow(stalePreCancelZero),
      ),
    ).toEqual([true, true]);

    const physicallyCleared = layoutSpacer(
      stalePreCancelZero,
      0,
      cancelled.spacerRequestEpoch,
    ).state;
    expect(
      ["content_size", "viewport_layout"].map(() =>
        turnFocusSuppressesOrdinaryFollow(physicallyCleared),
      ),
    ).toEqual([false, false]);
  });

  test("surface lifecycle clears blur and restarted subscriptions but not generation zero or one", () => {
    expect(shouldClearTurnFocusForSurfaceLifecycle(true, 0)).toBe(false);
    expect(shouldClearTurnFocusForSurfaceLifecycle(true, 1)).toBe(false);
    expect(shouldClearTurnFocusForSurfaceLifecycle(true, 2)).toBe(true);
    expect(shouldClearTurnFocusForSurfaceLifecycle(false, 1)).toBe(true);
  });

  test("conversation replacement and reconnect reset reject stale generation rows", () => {
    const active = beginFocusedTurn();
    const reset = reduceTurnFocus(active, {
      type: "reset",
      generation: "conversation-b:connection-5",
    }).state;
    expect(reset).toMatchObject({ phase: "idle", spacerHeight: 0 });

    const stale = reduceTurnFocus(reset, {
      type: "row_layout",
      generation: GENERATION,
      pendingMessageId: "pending-current",
      height: 100,
    });
    expect(stale.state).toBe(reset);
    expect(stale.effect).toBeUndefined();
  });

  test("live composer and keyboard clearance update the bounded spacer", () => {
    const state = reduceTurnFocus(beginFocusedTurn(), {
      type: "clearance_sample",
      intentToken: FIRST_INTENT_TOKEN,
      clearance: 260,
      latestOffset: -260,
    }).state;

    expect(state).toMatchObject({
      phase: "active",
      spacerHeight: 376,
    });
  });

  test("keyboard-open focus waits for a correlated post-intent clearance sample", () => {
    let state = withGeometry();
    state = reduceTurnFocus(state, {
      type: "intent",
      generation: GENERATION,
      pendingMessageId: "pending-keyboard-open",
      intentToken: 11,
      reducedMotion: false,
    }).state;
    state = layoutSpacer(state, 0).state;
    state = layoutFocusedRow(state, {
      pendingMessageId: "pending-keyboard-open",
    }).state;

    state = reduceTurnFocus(state, {
      type: "geometry",
      viewportHeight: 800,
      topChromeInset: 80,
    }).state;
    expect(state).toMatchObject({
      phase: "awaiting-row",
      clearanceSampledForIntent: false,
      spacerHeight: 0,
    });

    expect(
      reduceTurnFocus(state, {
        type: "clearance_sample",
        intentToken: 10,
        clearance: 300,
        latestOffset: -300,
      }).state,
    ).toBe(state);

    state = reduceTurnFocus(state, {
      type: "clearance_sample",
      intentToken: 11,
      clearance: 300,
      latestOffset: -300,
    }).state;
    expect(state).toMatchObject({
      phase: "arming",
      clearance: 300,
      clearanceSampledForIntent: true,
      spacerHeight: 320,
    });
  });

  test("the return consumes the correlated sample's offset instead of a stale iOS target", () => {
    let state = withGeometry();
    state = reduceTurnFocus(state, {
      type: "intent",
      generation: GENERATION,
      pendingMessageId: "pending-atomic-offset",
      intentToken: 31,
      reducedMotion: false,
    }).state;
    state = layoutSpacer(state, 0).state;
    state = reduceTurnFocus(state, {
      type: "clearance_sample",
      intentToken: 30,
      clearance: 76,
      latestOffset: -76,
    }).state;
    state = sampleClearance(state, 31, 300, -300);
    state = layoutFocusedRow(state, {
      pendingMessageId: "pending-atomic-offset",
    }).state;
    state = layoutSpacer(state, 336).state;

    expect(
      layoutFocusedRow(state, {
        pendingMessageId: "pending-atomic-offset",
        newestEdgeOffset: 336,
      }).effect,
    ).toEqual({
      type: "return_to_latest",
      pendingMessageId: "pending-atomic-offset",
      animated: true,
      latestOffset: -300,
    });
  });

  test("a delayed clearance sample needs no spacer probe for a zero budget", () => {
    let state = withGeometry();
    state = reduceTurnFocus(state, {
      type: "intent",
      generation: GENERATION,
      pendingMessageId: "pending-no-room",
      intentToken: 12,
      reducedMotion: false,
    }).state;
    state = layoutSpacer(state, 0).state;
    state = layoutFocusedRow(state, {
      pendingMessageId: "pending-no-room",
      height: 100,
      newestEdgeOffset: 500,
    }).state;
    expect(state.phase).toBe("awaiting-row");

    const correlatedClearance = reduceTurnFocus(state, {
      type: "clearance_sample",
      intentToken: 12,
      clearance: 180,
      latestOffset: -180,
    });
    expect(correlatedClearance.effect).toEqual({
      type: "return_to_latest",
      pendingMessageId: "pending-no-room",
      animated: true,
      latestOffset: -180,
    });
    expect(correlatedClearance.state.phase).toBe("idle");
  });

  test("touch, manual drag, momentum, and selection cancel automatic ownership", () => {
    for (const reason of ["touch", "drag", "momentum", "selection"] as const) {
      const cancelled = reduceTurnFocus(beginFocusedTurn(), {
        type: "cancel",
        reason,
      });
      expect(cancelled.effect).toBeUndefined();
      expect(cancelled.state).toMatchObject({
        phase: "idle",
        spacerHeight: 0,
      });
    }
  });

  test("reduced motion uses the same state machine with an immediate return", () => {
    const active = beginFocusedTurn({ reducedMotion: true });
    expect(active).toMatchObject({ phase: "active", spacerHeight: 456 });
  });

  test("streaming shrink restores spacer and actual zero resumes ordinary follow", () => {
    let state = beginFocusedTurn();

    state = layoutFocusedRow(state, { newestEdgeOffset: 556 }).state;
    expect(state.spacerHeight).toBe(356);
    state = layoutSpacer(state, 356).state;
    state = layoutFocusedRow(state, { newestEdgeOffset: 456 }).state;
    expect(state.spacerHeight).toBe(356);

    // The live response contracts by 40px: 356 actual spacer + 60 base y.
    state = layoutFocusedRow(state, { newestEdgeOffset: 416 }).state;
    expect(state.spacerHeight).toBe(396);

    state = layoutSpacer(state, 396).state;
    state = layoutFocusedRow(state, { newestEdgeOffset: 896 }).state;
    expect(state).toMatchObject({ phase: "active", spacerHeight: 0 });

    const zeroHeader = layoutSpacer(state, 0);
    expect(zeroHeader.effect).toBeUndefined();
    expect(zeroHeader.state.phase).toBe("active");
    const exhausted = layoutFocusedRow(zeroHeader.state, {
      newestEdgeOffset: 500,
    });
    expect(exhausted.effect).toBeUndefined();
    expect(exhausted.state).toMatchObject({ phase: "idle", spacerHeight: 0 });
  });
});
